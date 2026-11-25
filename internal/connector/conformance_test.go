package connector

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// Conformance tests require disposable MySQL/PostgreSQL databases:
//
//	ETL_MYSQL_JDBC_URL=jdbc:mysql://127.0.0.1:3306/etl
//	ETL_MYSQL_USER=root
//	ETL_MYSQL_PASSWORD=secret
//	ETL_POSTGRES_JDBC_URL=jdbc:postgresql://127.0.0.1:5432/etl
//	ETL_POSTGRES_USER=postgres
//	ETL_POSTGRES_PASSWORD=secret
//
// Missing credentials skip the corresponding suite; CI stays green without DBs.

func TestMySQLConformance(t *testing.T) {
	runSQLConformance(t, sqlConformanceCase{
		name:    "mysql",
		dialect: mysqlDialect,
		urlEnv:  "ETL_MYSQL_JDBC_URL",
		userEnv: "ETL_MYSQL_USER",
		passEnv: "ETL_MYSQL_PASSWORD",
		createTable: func(name string) string {
			return fmt.Sprintf(`CREATE TABLE %s (
  id BIGINT NOT NULL,
  name VARCHAR(100) NOT NULL,
  amount DECIMAL(18,4) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  note VARCHAR(100) NULL,
  payload VARBINARY(32) NULL,
  PRIMARY KEY (id)
)`, mysqlDialect.quote(name))
		},
	})
}

func TestPostgresConformance(t *testing.T) {
	runSQLConformance(t, sqlConformanceCase{
		name:    "postgres",
		dialect: postgresDialect,
		urlEnv:  "ETL_POSTGRES_JDBC_URL",
		userEnv: "ETL_POSTGRES_USER",
		passEnv: "ETL_POSTGRES_PASSWORD",
		createTable: func(name string) string {
			return fmt.Sprintf(`CREATE TABLE %s (
  id BIGINT NOT NULL PRIMARY KEY,
  name TEXT NOT NULL,
  amount NUMERIC(18,4) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  note TEXT NULL,
  payload BYTEA NULL
)`, postgresDialect.quote(name))
		},
	})
}

type sqlConformanceCase struct {
	name        string
	dialect     sqlDialect
	urlEnv      string
	userEnv     string
	passEnv     string
	createTable func(string) string
}

func runSQLConformance(t *testing.T, tc sqlConformanceCase) {
	t.Helper()
	jdbcURL := os.Getenv(tc.urlEnv)
	if jdbcURL == "" {
		t.Skipf("%s is not set; skipping %s conformance", tc.urlEnv, tc.name)
	}
	baseCfg := Config{
		Connector:      tc.name,
		DataSourceType: tc.name,
		JDBCUrl:        jdbcURL,
		Username:       os.Getenv(tc.userEnv),
		Password:       os.Getenv(tc.passEnv),
		Options:        map[string]string{},
	}
	driver, dsn, err := tc.dialect.open(baseCfg)
	if err != nil {
		t.Fatalf("build %s DSN: %v", tc.name, err)
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatalf("open %s: %v", tc.name, err)
	}
	defer db.Close()
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("ping %s: %v", tc.name, err)
	}

	suffix := time.Now().UnixNano()
	sourceTable := fmt.Sprintf("etl_conf_src_%d", suffix)
	targetTable := fmt.Sprintf("etl_conf_dst_%d", suffix)
	for _, table := range []string{sourceTable, targetTable} {
		if _, err := db.Exec(tc.createTable(table)); err != nil {
			t.Fatalf("create %s: %v", table, err)
		}
		table := table
		defer func() { _, _ = db.Exec("DROP TABLE " + tc.dialect.quote(table)) }()
	}

	schema := Schema{
		{Name: "id", Type: TypeInt64},
		{Name: "name", Type: TypeString},
		{Name: "amount", Type: TypeDecimal},
		{Name: "created_at", Type: TypeTimestamp},
		{Name: "note", Type: TypeString},
		{Name: "payload", Type: TypeBytes},
	}
	baseTime := time.Date(2024, 1, 2, 3, 4, 5, 123456000, time.UTC)
	seed := [][]any{
		{int64(1), "one", "1234567890.1234", baseTime, nil, []byte{1, 2}},
		{int64(2), "two", "2.5000", baseTime.Add(time.Minute), "note", nil},
		{int64(3), "three", "3.0001", baseTime.Add(2 * time.Minute), nil, []byte{3}},
		{int64(4), "four", "4.0000", baseTime.Add(3 * time.Minute), nil, nil},
		{int64(5), "five", "5.0000", baseTime.Add(4 * time.Minute), "五", nil},
	}
	insertRows(t, db, schema, sourceTable, tc.dialect, seed)

	discovered, err := Discover(context.Background(), Config{
		Connector:      tc.name,
		DataSourceType: tc.name,
		JDBCUrl:        jdbcURL,
		Username:       baseCfg.Username,
		Password:       baseCfg.Password,
		Options:        map[string]string{"table": sourceTable},
	})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(discovered) != len(schema) {
		t.Fatalf("discovered schema = %+v, want %+v", discovered, schema)
	}
	for index := range schema {
		if !equalIdentifier(discovered[index].Name, schema[index].Name) || discovered[index].Type != schema[index].Type {
			t.Fatalf("discovered[%d] = %+v, want %+v", index, discovered[index], schema[index])
		}
	}

	sourceCfg := baseCfg
	sourceCfg.Schema = discovered
	sourceCfg.Options = map[string]string{"table": sourceTable, "splitPk": "id"}
	source, err := NewSource(sourceCfg)
	if err != nil {
		t.Fatalf("NewSource(): %v", err)
	}
	rows := readAll(t, source)
	if err := source.Close(); err != nil {
		t.Fatalf("source close: %v", err)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][0].(int64) < rows[j][0].(int64) })
	if len(rows) != len(seed) {
		t.Fatalf("read %d rows, want %d: %#v", len(rows), len(seed), rows)
	}
	if rows[0][2] != "1234567890.1234" {
		t.Fatalf("decimal = %#v", rows[0][2])
	}
	if rows[0][4] != nil {
		t.Fatalf("null note = %#v", rows[0][4])
	}
	if created, ok := rows[0][3].(time.Time); !ok || !created.Equal(baseTime) {
		t.Fatalf("timestamp = %#v, want %v", rows[0][3], baseTime)
	}
	if payload, ok := rows[0][5].([]byte); !ok || len(payload) != 2 || payload[0] != 1 {
		t.Fatalf("bytes = %#v", rows[0][5])
	}

	// truncate+insert
	insertRows(t, db, schema, targetTable, tc.dialect, [][]any{
		{int64(99), "stale", "0.0000", baseTime, nil, nil},
	})
	sinkCfg := baseCfg
	sinkCfg.Schema = schema
	sinkCfg.Options = map[string]string{"table": targetTable, "writeMode": "truncate"}
	sink, err := NewSink(sinkCfg)
	if err != nil {
		t.Fatalf("NewSink(truncate): %v", err)
	}
	truncated := [][]any{
		{int64(10), "ten", "10.0001", baseTime, nil, []byte{1, 2, 3}},
		{int64(11), "eleven", "11.5000", baseTime, "note", nil},
	}
	if err := sink.WriteBatch(context.Background(), Batch{Schema: schema, Rows: truncated}); err != nil {
		t.Fatalf("truncate write: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("sink close: %v", err)
	}
	assertTargetRows(t, db, schema, targetTable, tc.dialect, map[int64][]any{
		10: {int64(10), "ten", "10.0001", baseTime, nil, []byte{1, 2, 3}},
		11: {int64(11), "eleven", "11.5000", baseTime, "note", nil},
	})

	// insert
	insertCfg := sinkCfg
	insertCfg.Options = map[string]string{"table": targetTable, "writeMode": "insert"}
	sink, err = NewSink(insertCfg)
	if err != nil {
		t.Fatalf("NewSink(insert): %v", err)
	}
	if err := sink.WriteBatch(context.Background(), Batch{Schema: schema, Rows: [][]any{
		{int64(12), "twelve", "12.0000", baseTime, nil, nil},
	}}); err != nil {
		t.Fatalf("insert write: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("sink close: %v", err)
	}
	assertTargetRows(t, db, schema, targetTable, tc.dialect, map[int64][]any{
		10: {int64(10), "ten", "10.0001", baseTime, nil, []byte{1, 2, 3}},
		11: {int64(11), "eleven", "11.5000", baseTime, "note", nil},
		12: {int64(12), "twelve", "12.0000", baseTime, nil, nil},
	})

	// native upsert
	upsertCfg := sinkCfg
	upsertCfg.Options = map[string]string{"table": targetTable, "writeMode": "upsert"}
	sink, err = NewSink(upsertCfg)
	if err != nil {
		t.Fatalf("NewSink(upsert): %v", err)
	}
	updatedTime := baseTime.Add(24 * time.Hour)
	if err := sink.WriteBatch(context.Background(), Batch{Schema: schema, Rows: [][]any{
		{int64(10), "TEN", "99.9900", updatedTime, "updated", []byte{9}},
	}}); err != nil {
		t.Fatalf("upsert write: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("sink close: %v", err)
	}
	assertTargetRows(t, db, schema, targetTable, tc.dialect, map[int64][]any{
		10: {int64(10), "TEN", "99.9900", updatedTime, "updated", []byte{9}},
		11: {int64(11), "eleven", "11.5000", baseTime, "note", nil},
		12: {int64(12), "twelve", "12.0000", baseTime, nil, nil},
	})
}

func insertRows(t *testing.T, db *sql.DB, schema Schema, table string, dialect sqlDialect, rows [][]any) {
	t.Helper()
	statement := "INSERT INTO " + dialect.quote(table) + " (" +
		joinQuoted(schema, dialect) + ") VALUES (" +
		joinPlaceholders(len(schema), dialect) + ")"
	for index, row := range rows {
		values := make([]any, len(row))
		for columnIndex, value := range row {
			values[columnIndex] = dialect.driverValue(value)
		}
		if _, err := db.Exec(statement, values...); err != nil {
			t.Fatalf("insert row %d: %v", index, err)
		}
	}
}

func assertTargetRows(t *testing.T, db *sql.DB, schema Schema, table string, dialect sqlDialect, expected map[int64][]any) {
	t.Helper()
	query := "SELECT " + joinQuoted(schema, dialect) + " FROM " + dialect.quote(table) + " ORDER BY " + dialect.quote("id")
	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	defer rows.Close()
	var found []int64
	for rows.Next() {
		raw := make([]any, len(schema))
		pointers := make([]any, len(schema))
		for index := range raw {
			pointers[index] = &raw[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatalf("scan target: %v", err)
		}
		row, err := scanCanonicalRowValues(raw, schema)
		if err != nil {
			t.Fatalf("coerce target: %v", err)
		}
		id := row[0].(int64)
		found = append(found, id)
		want, ok := expected[id]
		if !ok {
			t.Fatalf("unexpected target row %+v", row)
		}
		assertRowValues(t, row, want)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("target rows: %v", err)
	}
	if len(found) != len(expected) {
		t.Fatalf("target ids = %v, want %d rows", found, len(expected))
	}
}

func scanCanonicalRowValues(raw []any, schema Schema) ([]any, error) {
	row := make([]any, len(schema))
	for index, value := range raw {
		converted, err := coerceValue(value, schema[index].Type)
		if err != nil {
			return nil, fmt.Errorf("column %s: %w", schema[index].Name, err)
		}
		row[index] = converted
	}
	return row, nil
}

func assertRowValues(t *testing.T, got, want []any) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("row = %#v, want %#v", got, want)
	}
	for index := range want {
		switch expected := want[index].(type) {
		case time.Time:
			actual, ok := got[index].(time.Time)
			if !ok || !actual.Equal(expected) {
				t.Fatalf("column %d = %#v, want %v", index, got[index], expected)
			}
		case []byte:
			actual, ok := got[index].([]byte)
			if !ok || string(actual) != string(expected) {
				t.Fatalf("column %d = %#v, want %#v", index, got[index], expected)
			}
		default:
			if got[index] != expected {
				t.Fatalf("column %d = %#v, want %#v", index, got[index], expected)
			}
		}
	}
}

func joinQuoted(schema Schema, dialect sqlDialect) string {
	return strings.Join(quotedNames(schema, dialect.quote), ", ")
}

func joinPlaceholders(count int, dialect sqlDialect) string {
	return strings.Join(placeholders(count, dialect.placeholder), ", ")
}

func equalIdentifier(left, right string) bool {
	return len(left) == len(right) && strings.EqualFold(left, right)
}
