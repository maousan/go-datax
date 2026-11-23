package dbexplorer

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/longkeyy/go-datax/internal/store"
)

func TestSQLiteExplorerBrowsesAndQueries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "explorer.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, note TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (name, note) VALUES ('alice', 'a'), ('bob', NULL)`); err != nil {
		t.Fatalf("insert rows: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	explorer, err := Open(context.Background(), store.DataSource{
		Type:    "sqlite",
		JDBCUrl: "jdbc:sqlite:" + path,
	})
	if err != nil {
		t.Fatalf("open explorer: %v", err)
	}
	defer explorer.Close()

	schemas, err := explorer.Schemas(context.Background())
	if err != nil {
		t.Fatalf("schemas: %v", err)
	}
	if len(schemas) != 1 || schemas[0].Name != "main" || schemas[0].TableCount != 1 {
		t.Fatalf("unexpected schemas: %#v", schemas)
	}

	tables, err := explorer.Tables(context.Background(), "main")
	if err != nil {
		t.Fatalf("tables: %v", err)
	}
	if len(tables) != 1 || tables[0].Name != "users" || tables[0].Type != "table" {
		t.Fatalf("unexpected tables: %#v", tables)
	}

	columns, err := explorer.Columns(context.Background(), "main", "users")
	if err != nil {
		t.Fatalf("columns: %v", err)
	}
	if len(columns) != 3 || columns[1].Name != "name" || columns[1].Nullable {
		t.Fatalf("unexpected columns: %#v", columns)
	}

	result, err := explorer.Rows(context.Background(), "main", "users", "id > 0", "id DESC", 1, 1)
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	if result.Total != 2 || len(result.Rows) != 1 {
		t.Fatalf("unexpected rows result: %#v", result)
	}
	if got := asString(result.Rows[0][1]); got != "bob" {
		t.Fatalf("unexpected first row: %#v", result.Rows[0])
	}

	query, err := explorer.Query(context.Background(), "SELECT name FROM users ORDER BY id", 1)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !query.Truncated || len(query.Rows) != 1 || asString(query.Rows[0][0]) != "alice" {
		t.Fatalf("unexpected query result: %#v", query)
	}
}

func TestRowsRejectsUnsafeFragments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "explorer.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE t (id INTEGER)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	db.Close()

	explorer, err := Open(context.Background(), store.DataSource{
		Type:    "sqlite",
		JDBCUrl: "jdbc:sqlite:" + path,
	})
	if err != nil {
		t.Fatalf("open explorer: %v", err)
	}
	defer explorer.Close()

	_, err = explorer.Rows(context.Background(), "", "t", "1=1; DROP TABLE t", "", 1, 20)
	if !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("expected ErrInvalidQuery, got %v", err)
	}
}

func TestValidateReadOnlySQL(t *testing.T) {
	allowed := []string{
		"SELECT * FROM users",
		"select 1;",
		"WITH x AS (SELECT 1) SELECT * FROM x",
		"SHOW TABLES",
		"EXPLAIN SELECT 1",
	}
	for _, statement := range allowed {
		if err := validateReadOnlySQL(statement); err != nil {
			t.Errorf("expected %q to be allowed: %v", statement, err)
		}
	}

	rejected := []string{
		"",
		";",
		"UPDATE users SET name = 'x'",
		"DELETE FROM users",
		"DROP TABLE users",
		"SELECT 1; SELECT 2",
		"SELECT * FROM users -- comment",
		"SELECT * INTO backup FROM users",
		"WITH x AS (DELETE FROM users RETURNING *) SELECT * FROM x",
	}
	for _, statement := range rejected {
		if err := validateReadOnlySQL(statement); !errors.Is(err, ErrInvalidQuery) {
			t.Errorf("expected %q to be rejected, got %v", statement, err)
		}
	}
}
