package connector

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

func sqliteConfig(jdbcURL string, schema Schema, table string, extra map[string]string) Config {
	options := map[string]string{"table": table}
	for key, value := range extra {
		options[key] = value
	}
	return Config{
		Connector:      "sqlite",
		DataSourceType: "sqlite",
		JDBCUrl:        "jdbc:sqlite:" + jdbcURL,
		Schema:         schema,
		Options:        options,
	}
}

func openTestSQLite(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSQLiteSourceSplitsAndSinkRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "people.db")
	db := openTestSQLite(t, path)
	if _, err := db.Exec(`CREATE TABLE people (
		id INTEGER PRIMARY KEY,
		name TEXT,
		score REAL,
		active INTEGER,
		created TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 10; index++ {
		if _, err := db.Exec(`INSERT INTO people VALUES (?, ?, ?, ?, ?)`,
			index, "person-"+string(rune('a'+index-1)), float64(index)+0.5, index%2 == 0,
			"2024-01-02T03:04:05Z"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`CREATE TABLE people_copy (
		id INTEGER PRIMARY KEY,
		name TEXT,
		score REAL,
		active INTEGER,
		created TEXT
	)`); err != nil {
		t.Fatal(err)
	}

	schema := sampleSchema()
	source, err := NewSource(sqliteConfig(path, schema, "people", map[string]string{"splitPk": "id"}))
	if err != nil {
		t.Fatal(err)
	}
	splits, err := source.Splits(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(splits) != 4 {
		t.Fatalf("got %d splits, want 4", len(splits))
	}
	rows := readAll(t, source)
	if len(rows) != 10 {
		t.Fatalf("got %d rows, want 10", len(rows))
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}

	sink, err := NewSink(sqliteConfig(path, schema, "people_copy", map[string]string{"writeMode": "truncate"}))
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.WriteBatch(context.Background(), Batch{Schema: schema, Rows: rows[:4]}); err != nil {
		t.Fatal(err)
	}
	if err := sink.WriteBatch(context.Background(), Batch{Schema: schema, Rows: rows[4:]}); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM people_copy`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 10 {
		t.Fatalf("copied %d rows, want 10", count)
	}
	var name, created string
	var active int
	if err := db.QueryRow(`SELECT name, active, created FROM people_copy WHERE id = 2`).Scan(&name, &active, &created); err != nil {
		t.Fatal(err)
	}
	if name != "person-b" || active != 1 {
		t.Fatalf("unexpected copied row: %q %d", name, active)
	}
	if _, err := time.Parse(time.RFC3339Nano, created); err != nil {
		t.Fatalf("created is not RFC3339: %q", created)
	}
}

func TestSQLiteDiscovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "discover.db")
	db := openTestSQLite(t, path)
	if _, err := db.Exec(`CREATE TABLE typed (id INTEGER, name TEXT, score REAL, active BOOLEAN, created TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO typed VALUES (1, 'alice', 9.5, 1, '2024-01-02')`); err != nil {
		t.Fatal(err)
	}

	schema, err := Discover(context.Background(), Config{
		Connector:      "sqlite",
		DataSourceType: "sqlite",
		JDBCUrl:        "jdbc:sqlite:" + path,
		Options:        map[string]string{"table": "typed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	expected := Schema{
		{Name: "id", Type: TypeInt64},
		{Name: "name", Type: TypeString},
		{Name: "score", Type: TypeFloat64},
		{Name: "active", Type: TypeBool},
		{Name: "created", Type: TypeString},
	}
	if !schemaEqual(schema, expected) {
		t.Fatalf("discovered %#v, want %#v", schema, expected)
	}

	querySchema, err := Discover(context.Background(), Config{
		Connector:      "sqlite",
		DataSourceType: "sqlite",
		JDBCUrl:        "jdbc:sqlite:" + path,
		Options:        map[string]string{"query": "SELECT id, name FROM typed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(querySchema) != 2 || querySchema[0].Name != "id" || querySchema[1].Name != "name" {
		t.Fatalf("unexpected query schema: %#v", querySchema)
	}
}

func TestSQLiteSourceRowError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.db")
	db := openTestSQLite(t, path)
	if _, err := db.Exec(`CREATE TABLE bad (id TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO bad VALUES ('not-a-number')`); err != nil {
		t.Fatal(err)
	}
	source, err := NewSource(sqliteConfig(path, Schema{{Name: "id", Type: TypeInt64}}, "bad", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	splits, err := source.Splits(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = source.ReadBatch(context.Background(), splits[0])
	var rowErr *RowError
	if !errors.As(err, &rowErr) || rowErr.Row != 0 {
		t.Fatalf("expected RowError at row 0, got %v", err)
	}
	if _, err := source.ReadBatch(context.Background(), splits[0]); err == nil {
		t.Fatal("expected EOF after failure")
	}
}

func TestSQLiteSplitPkIncludesNullRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nullable.db")
	db := openTestSQLite(t, path)
	if _, err := db.Exec(`CREATE TABLE nullable (id INTEGER PRIMARY KEY, seq INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO nullable VALUES (1, 1), (2, 2), (3, NULL)`); err != nil {
		t.Fatal(err)
	}
	schema := Schema{
		{Name: "id", Type: TypeInt64},
		{Name: "seq", Type: TypeInt64},
	}
	source, err := NewSource(sqliteConfig(path, schema, "nullable", map[string]string{"splitPk": "seq"}))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	rows := readAll(t, source)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3: %#v", len(rows), rows)
	}
	foundNull := false
	for _, row := range rows {
		if row[0] == int64(3) && row[1] == nil {
			foundNull = true
		}
	}
	if !foundNull {
		t.Fatalf("NULL split key row was lost: %#v", rows)
	}
}

func TestSQLiteSourceSplitsReadConcurrently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent.db")
	db := openTestSQLite(t, path)
	if _, err := db.Exec(`CREATE TABLE rows (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	const total = 200
	for index := 1; index <= total; index++ {
		if _, err := db.Exec(`INSERT INTO rows VALUES (?)`, index); err != nil {
			t.Fatal(err)
		}
	}
	source, err := NewSource(sqliteConfig(path, Schema{{Name: "id", Type: TypeInt64}}, "rows", map[string]string{"splitPk": "id"}))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	ctx := context.Background()
	splits, err := source.Splits(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(splits) < 2 {
		t.Fatalf("expected multiple splits, got %d", len(splits))
	}

	counts := make([]int, len(splits))
	errs := make(chan error, len(splits))
	var wg sync.WaitGroup
	for index, split := range splits {
		wg.Add(1)
		go func(index int, split Split) {
			defer wg.Done()
			for {
				batch, err := source.ReadBatch(ctx, split)
				if err == io.EOF {
					return
				}
				if err != nil {
					errs <- err
					return
				}
				counts[index] += len(batch.Rows)
			}
		}(index, split)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	read := 0
	for _, count := range counts {
		read += count
	}
	if read != total {
		t.Fatalf("read %d rows, want %d (per split: %v)", read, total, counts)
	}
}

func TestSQLiteSinkSchemaMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mismatch.db")
	db := openTestSQLite(t, path)
	if _, err := db.Exec(`CREATE TABLE target (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	sink, err := NewSink(sqliteConfig(path, Schema{{Name: "id", Type: TypeInt64}}, "target", map[string]string{"writeMode": "truncate"}))
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	err = sink.WriteBatch(context.Background(), Batch{
		Schema: Schema{{Name: "other", Type: TypeInt64}},
		Rows:   [][]any{{int64(1)}},
	})
	if err == nil {
		t.Fatal("expected schema mismatch")
	}
}

func TestSQLiteValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "validate.db")
	for name, options := range map[string]map[string]string{
		"missing table and query": {},
		"both table and query":    {"table": "t", "query": "SELECT 1"},
		"splitPk with query":      {"query": "SELECT 1", "splitPk": "id"},
		"bad write mode":          {"table": "t", "writeMode": "upsert"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewSource(sqliteConfig(path, sampleSchema(), "", options))
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestSQLiteTruncateClearsExistingRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "truncate.db")
	db := openTestSQLite(t, path)
	if _, err := db.Exec(`CREATE TABLE target (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO target VALUES (1), (2)`); err != nil {
		t.Fatal(err)
	}
	schema := Schema{{Name: "id", Type: TypeInt64}}
	sink, err := NewSink(sqliteConfig(path, schema, "target", map[string]string{"writeMode": "truncate"}))
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.WriteBatch(context.Background(), Batch{Schema: schema, Rows: [][]any{{int64(3)}}}); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT id FROM target ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if len(ids) != 1 || ids[0] != 3 {
		t.Fatalf("unexpected ids: %v", ids)
	}
}
