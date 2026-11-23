package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDatabaseCreatesUsableSQLiteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin.db")

	if err := EnsureDatabase(path); err != nil {
		t.Fatalf("EnsureDatabase() failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("database file was not created: %v", err)
	}
	if info.Size() == 0 {
		t.Errorf("database file is empty")
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	var value int
	if err := db.QueryRow("SELECT 1").Scan(&value); err != nil {
		t.Fatalf("query database: %v", err)
	}
	if value != 1 {
		t.Errorf("SELECT 1 = %d, want 1", value)
	}
}

func TestEnsureDatabaseIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin.db")

	if err := EnsureDatabase(path); err != nil {
		t.Fatalf("first EnsureDatabase() failed: %v", err)
	}
	if err := EnsureDatabase(path); err != nil {
		t.Fatalf("second EnsureDatabase() failed: %v", err)
	}
}
