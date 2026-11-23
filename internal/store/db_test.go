package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func openTestStore(t *testing.T, path string) *Store {
	t.Helper()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() failed: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenCreatesDatabaseAndMigrates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin.db")
	s := openTestStore(t, path)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("database file was not created: %v", err)
	}
	if info.Size() == 0 {
		t.Error("database file is empty")
	}

	if _, err := s.ListDataSources(""); err != nil {
		t.Fatalf("data_sources table is not usable: %v", err)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin.db")
	openTestStore(t, path)
	second := openTestStore(t, path)

	if _, err := second.ListDataSources(""); err != nil {
		t.Fatalf("reopened store is not usable: %v", err)
	}
}

func TestDataSourceCrud(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "admin.db"))

	ds := &DataSource{
		Name:       "prod-mysql",
		Type:       "mysql",
		JDBCUrl:    "jdbc:mysql://localhost:3306/app",
		Username:   "root",
		Password:   "secret",
		Properties: StringMap{"useSSL": "false"},
	}
	if err := s.CreateDataSource(ds); err != nil {
		t.Fatalf("CreateDataSource() failed: %v", err)
	}
	if ds.ID == 0 {
		t.Fatal("expected an assigned id")
	}

	loaded, err := s.GetDataSource(ds.ID)
	if err != nil {
		t.Fatalf("GetDataSource() failed: %v", err)
	}
	if loaded.Name != ds.Name || loaded.Properties["useSSL"] != "false" {
		t.Errorf("loaded = %+v, want name and properties preserved", loaded)
	}

	loaded.Name = "prod-mysql-renamed"
	if err := s.UpdateDataSource(&loaded); err != nil {
		t.Fatalf("UpdateDataSource() failed: %v", err)
	}
	renamed, err := s.GetDataSource(ds.ID)
	if err != nil {
		t.Fatalf("GetDataSource() after update failed: %v", err)
	}
	if renamed.Name != "prod-mysql-renamed" {
		t.Errorf("name = %q, want prod-mysql-renamed", renamed.Name)
	}

	if err := s.DeleteDataSource(ds.ID); err != nil {
		t.Fatalf("DeleteDataSource() failed: %v", err)
	}
	if _, err := s.GetDataSource(ds.ID); !errors.Is(err, ErrDataSourceNotFound) {
		t.Errorf("GetDataSource() after delete = %v, want ErrDataSourceNotFound", err)
	}
}

func TestDataSourceSearchAndDuplicateName(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "admin.db"))

	for _, name := range []string{"Alpha", "Beta"} {
		if err := s.CreateDataSource(&DataSource{
			Name: name, Type: "mysql", JDBCUrl: "jdbc:mysql://localhost/db",
		}); err != nil {
			t.Fatalf("CreateDataSource(%s) failed: %v", name, err)
		}
	}

	matches, err := s.ListDataSources("alp")
	if err != nil {
		t.Fatalf("ListDataSources() failed: %v", err)
	}
	if len(matches) != 1 || matches[0].Name != "Alpha" {
		t.Errorf("search results = %+v, want only Alpha", matches)
	}

	err = s.CreateDataSource(&DataSource{
		Name: "alpha", Type: "mysql", JDBCUrl: "jdbc:mysql://localhost/db",
	})
	if !errors.Is(err, ErrDuplicateDataSourceName) {
		t.Errorf("duplicate create = %v, want ErrDuplicateDataSourceName", err)
	}
}

func TestListRunsForPrune(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "admin.db"))
	now := time.Now()

	for i := 0; i < 5; i++ {
		run := &JobRun{
			Status:    RunStatusSucceeded,
			Trigger:   RunTriggerManual,
			StartedAt: now.Add(-time.Duration(i) * time.Hour),
		}
		if err := s.CreateJobRun(run); err != nil {
			t.Fatalf("CreateJobRun() failed: %v", err)
		}
	}

	overflow, err := s.ListRunsForPrune(2, 0)
	if err != nil {
		t.Fatalf("ListRunsForPrune() failed: %v", err)
	}
	if len(overflow) != 3 {
		t.Errorf("overflow count = %d, want 3", len(overflow))
	}

	if err := s.CreateJobRun(&JobRun{
		Status:    RunStatusSucceeded,
		Trigger:   RunTriggerManual,
		StartedAt: now.Add(-48 * time.Hour),
	}); err != nil {
		t.Fatalf("CreateJobRun() failed: %v", err)
	}

	expired, err := s.ListRunsForPrune(0, 1)
	if err != nil {
		t.Fatalf("ListRunsForPrune() failed: %v", err)
	}
	if len(expired) != 1 {
		t.Errorf("expired count = %d, want 1", len(expired))
	}
}

func TestUpdateDataSourceVersionKeepsUpdatedAt(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "admin.db"))
	ds := &DataSource{Name: "versioned", Type: "sqlite", JDBCUrl: "jdbc:sqlite:/tmp/v.db"}
	if err := s.CreateDataSource(ds); err != nil {
		t.Fatalf("CreateDataSource() failed: %v", err)
	}
	before := ds.UpdatedAt

	if err := s.UpdateDataSourceVersion(ds.ID, "3.42.0"); err != nil {
		t.Fatalf("UpdateDataSourceVersion() failed: %v", err)
	}

	loaded, err := s.GetDataSource(ds.ID)
	if err != nil {
		t.Fatalf("GetDataSource() failed: %v", err)
	}
	if loaded.Version != "3.42.0" {
		t.Errorf("version = %q, want 3.42.0", loaded.Version)
	}
	if !loaded.UpdatedAt.Equal(before) {
		t.Errorf("updatedAt changed: %v -> %v", before, loaded.UpdatedAt)
	}
}

func TestDataSourcePersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin.db")

	first, err := Open(path)
	if err != nil {
		t.Fatalf("Open() failed: %v", err)
	}
	if err := first.CreateDataSource(&DataSource{
		Name: "persisted", Type: "postgres", JDBCUrl: "jdbc:postgresql://localhost/db",
	}); err != nil {
		t.Fatalf("CreateDataSource() failed: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	second := openTestStore(t, path)
	items, err := second.ListDataSources("")
	if err != nil {
		t.Fatalf("ListDataSources() failed: %v", err)
	}
	if len(items) != 1 || items[0].Name != "persisted" {
		t.Errorf("items = %+v, want the persisted data source", items)
	}
}
