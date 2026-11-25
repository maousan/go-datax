package worker

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/glebarez/go-sqlite"
	"github.com/longkeyy/go-datax/internal/connector"
)

func pipelineSpec(t *testing.T, sourcePath, sinkPath string) PipelineSpec {
	t.Helper()
	schema := connector.Schema{
		{Name: "id", Type: connector.TypeInt64},
		{Name: "name", Type: connector.TypeString},
	}
	return PipelineSpec{
		Source: EndpointSpec{
			Name:           "src",
			Connector:      "sqlite",
			DataSourceType: "sqlite",
			JDBCUrl:        "jdbc:sqlite:" + sourcePath,
			Schema:         schema,
			Options:        map[string]string{"table": "src", "splitPk": "id"},
		},
		Sink: EndpointSpec{
			Name:           "dst",
			Connector:      "sqlite",
			DataSourceType: "sqlite",
			JDBCUrl:        "jdbc:sqlite:" + sinkPath,
			Schema:         schema,
			Options:        map[string]string{"table": "dst", "writeMode": "truncate"},
		},
	}
}

func createPipelineDB(t *testing.T, path string, create string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(create); err != nil {
		t.Fatal(err)
	}
}

func TestRunPipelineCopiesRows(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	sinkPath := filepath.Join(dir, "sink.db")
	createPipelineDB(t, sourcePath, `CREATE TABLE src (id INTEGER PRIMARY KEY, name TEXT)`)
	createPipelineDB(t, sinkPath, `CREATE TABLE dst (id INTEGER PRIMARY KEY, name TEXT)`)

	db, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 300; index++ {
		if _, err := db.Exec(`INSERT INTO src VALUES (?, ?)`, index, "row"); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()

	var events []Event
	status, err := RunPipeline(context.Background(), pipelineSpec(t, sourcePath, sinkPath), func(event Event) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatalf("RunPipeline() failed: %v", err)
	}
	if status != StatusSucceeded {
		t.Fatalf("status = %q, want succeeded", status)
	}
	var done Event
	for _, event := range events {
		if event.Type == "done" {
			done = event
		}
	}
	if done.Status != StatusSucceeded || done.ReadRecords != 300 || done.WriteRecords != 300 {
		t.Fatalf("done = %+v", done)
	}

	db, err = sql.Open("sqlite", sinkPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM dst`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 300 {
		t.Fatalf("target rows = %d, want 300", count)
	}
}

func TestRunPipelineRejectsSchemaMismatch(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	sinkPath := filepath.Join(dir, "sink.db")
	createPipelineDB(t, sourcePath, `CREATE TABLE src (id INTEGER PRIMARY KEY, name TEXT)`)
	createPipelineDB(t, sinkPath, `CREATE TABLE dst (id INTEGER PRIMARY KEY, name TEXT)`)
	spec := pipelineSpec(t, sourcePath, sinkPath)
	spec.Sink.Schema = connector.Schema{{Name: "id", Type: connector.TypeInt64}}

	var done Event
	status, err := RunPipeline(context.Background(), spec, func(event Event) {
		if event.Type == "done" {
			done = event
		}
	})
	if err == nil || status != StatusFailed {
		t.Fatalf("status = %q, err = %v, want failed", status, err)
	}
	if done.Status != StatusFailed || done.Error == "" {
		t.Fatalf("done = %+v", done)
	}
}
