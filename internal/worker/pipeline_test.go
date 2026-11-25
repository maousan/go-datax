package worker

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/glebarez/go-sqlite"
	"github.com/longkeyy/go-datax/internal/connector"
	"github.com/longkeyy/go-datax/internal/transform"
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

func TestRunPipelineAppliesTransforms(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	sinkPath := filepath.Join(dir, "sink.db")
	createPipelineDB(t, sourcePath, `CREATE TABLE src (id INTEGER PRIMARY KEY, name TEXT)`)
	createPipelineDB(t, sinkPath, `CREATE TABLE dst (id INTEGER PRIMARY KEY, label TEXT)`)
	db, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	for index, name := range []string{"alice", "bob", "carol", "dave"} {
		if _, err := db.Exec(`INSERT INTO src VALUES (?, ?)`, index+1, name); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()

	spec := PipelineSpec{
		Source: EndpointSpec{
			Name:           "src",
			Connector:      "sqlite",
			DataSourceType: "sqlite",
			JDBCUrl:        "jdbc:sqlite:" + sourcePath,
			Schema: connector.Schema{
				{Name: "id", Type: connector.TypeInt64},
				{Name: "name", Type: connector.TypeString},
			},
			Options: map[string]string{"table": "src"},
		},
		Transforms: []transform.Step{
			{Type: transform.StepProject, Project: []transform.ProjectColumn{
				{Name: "id", Type: connector.TypeInt64, Expr: "id"},
				{Name: "label", Type: connector.TypeString, Expr: "upper(name)"},
			}},
			{Type: transform.StepFilter, Condition: "id > 2"},
		},
		Sink: EndpointSpec{
			Name:           "dst",
			Connector:      "sqlite",
			DataSourceType: "sqlite",
			JDBCUrl:        "jdbc:sqlite:" + sinkPath,
			Schema: connector.Schema{
				{Name: "id", Type: connector.TypeInt64},
				{Name: "label", Type: connector.TypeString},
			},
			Options: map[string]string{"table": "dst", "writeMode": "truncate"},
		},
	}

	var done Event
	status, err := RunPipeline(context.Background(), spec, func(event Event) {
		if event.Type == "done" {
			done = event
		}
	})
	if err != nil || status != StatusSucceeded {
		t.Fatalf("RunPipeline() status=%q err=%v", status, err)
	}
	if done.ReadRecords != 4 || done.WriteRecords != 2 {
		t.Fatalf("done = %+v, want read=4 write=2", done)
	}
	db, err = sql.Open("sqlite", sinkPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var label string
	if err := db.QueryRow(`SELECT label FROM dst WHERE id = 3`).Scan(&label); err != nil {
		t.Fatal(err)
	}
	if label != "CAROL" {
		t.Fatalf("label = %q, want CAROL", label)
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

func TestNormalizePipelineSpecRejectsUnknownTransformLibrary(t *testing.T) {
	spec := pipelineSpec(t, "unused", "unused")
	spec.TransformLibraryVersion = transform.FunctionLibraryVersion + 1
	if _, err := normalizePipelineSpec(spec); err == nil {
		t.Fatal("normalizePipelineSpec() accepted an unsupported transform library version")
	}
	spec.TransformLibraryVersion = 0
	normalized, err := normalizePipelineSpec(spec)
	if err != nil {
		t.Fatalf("normalizePipelineSpec() failed: %v", err)
	}
	if normalized.TransformLibraryVersion != transform.FunctionLibraryVersion {
		t.Fatalf("library version = %d, want %d", normalized.TransformLibraryVersion, transform.FunctionLibraryVersion)
	}
}

func TestEndpointSpecConfigCarriesCredentials(t *testing.T) {
	cfg := EndpointSpec{Connector: "mysql", DataSourceType: "mysql", Username: "app_user", Password: "secret"}.Config()
	if cfg.Username != "app_user" || cfg.Password != "secret" {
		t.Fatalf("config credentials = %q/%q", cfg.Username, cfg.Password)
	}
}
