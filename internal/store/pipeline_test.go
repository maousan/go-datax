package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestPipelineAndVersionCRUD(t *testing.T) {
	dataStore, err := Open(filepath.Join(t.TempDir(), "admin.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()

	ds := &DataSource{Name: "db", Type: "sqlite", JDBCUrl: "jdbc:sqlite:test.db"}
	if err := dataStore.CreateDataSource(ds); err != nil {
		t.Fatal(err)
	}
	source := &Endpoint{Kind: EndpointKindSource, Name: "src", Connector: "sqlite", DataSourceID: ds.ID, TableName: "src", SchemaJSON: "[]"}
	sink := &Endpoint{Kind: EndpointKindSink, Name: "dst", Connector: "sqlite", DataSourceID: ds.ID, TableName: "dst", SchemaJSON: "[]"}
	if err := dataStore.CreateEndpoint(source); err != nil {
		t.Fatal(err)
	}
	if err := dataStore.CreateEndpoint(sink); err != nil {
		t.Fatal(err)
	}

	pipeline := &Pipeline{Name: "Orders", SourceID: source.ID, SinkID: sink.ID}
	if err := dataStore.CreatePipeline(pipeline); err != nil {
		t.Fatal(err)
	}
	duplicate := &Pipeline{Name: "orders", SourceID: source.ID, SinkID: sink.ID}
	if err := dataStore.CreatePipeline(duplicate); !errors.Is(err, ErrDuplicatePipelineName) {
		t.Fatalf("duplicate error = %v, want ErrDuplicatePipelineName", err)
	}

	referencing, err := dataStore.PipelinesUsingEndpoint(source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(referencing) != 1 || referencing[0].ID != pipeline.ID {
		t.Fatalf("pipelines using source = %+v", referencing)
	}

	version := &PipelineVersion{
		PipelineID: pipeline.ID,
		SourceName: "src",
		SinkName:   "dst",
		Summary:    "src → dst",
		SourceJSON: `{"name":"src"}`,
		SinkJSON:   `{"name":"dst"}`,
	}
	if err := dataStore.CreatePipelineVersion(version); err != nil {
		t.Fatal(err)
	}
	if version.VersionNumber != 1 {
		t.Fatalf("first version = %d, want 1", version.VersionNumber)
	}
	second := &PipelineVersion{
		PipelineID: pipeline.ID,
		SourceName: "src",
		SinkName:   "dst",
		SourceJSON: `{"name":"src"}`,
		SinkJSON:   `{"name":"dst"}`,
	}
	if err := dataStore.CreatePipelineVersion(second); err != nil {
		t.Fatal(err)
	}
	if second.VersionNumber != 2 {
		t.Fatalf("second version = %d, want 2", second.VersionNumber)
	}
	latest, err := dataStore.LatestPipelineVersion(pipeline.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID != second.ID || latest.VersionNumber != 2 {
		t.Fatalf("latest = %+v", latest)
	}
	versions, err := dataStore.ListPipelineVersions(pipeline.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0].VersionNumber != 2 || versions[1].VersionNumber != 1 {
		t.Fatalf("versions = %+v", versions)
	}
}

func TestPipelineRunListAndStats(t *testing.T) {
	dataStore, err := Open(filepath.Join(t.TempDir(), "admin.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()

	pipelineID := int64(7)
	run := &PipelineRun{
		PipelineID:    &pipelineID,
		PipelineName:  "orders",
		VersionNumber: 3,
		Status:        PipelineRunStatusRunning,
		Trigger:       PipelineRunTriggerManual,
	}
	if err := dataStore.CreatePipelineRun(run); err != nil {
		t.Fatal(err)
	}
	active, err := dataStore.HasActivePipelineRun(pipelineID)
	if err != nil {
		t.Fatal(err)
	}
	if !active {
		t.Fatal("expected an active run")
	}
	if err := dataStore.UpdatePipelineRunLogPath(run.ID, "run.log"); err != nil {
		t.Fatal(err)
	}
	if err := dataStore.FinishPipelineRunDetails(run.ID, PipelineRunStatusSucceeded, "", 12, 12, 120); err != nil {
		t.Fatal(err)
	}
	loaded, err := dataStore.GetPipelineRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != PipelineRunStatusSucceeded || loaded.ReadRecords != 12 ||
		loaded.WriteRecords != 12 || loaded.LogPath != "run.log" || loaded.FinishedAt == nil {
		t.Fatalf("loaded run = %+v", loaded)
	}
	active, err = dataStore.HasActivePipelineRun(pipelineID)
	if err != nil {
		t.Fatal(err)
	}
	if active {
		t.Fatal("completed run should not be active")
	}
	runs, total, err := dataStore.ListPipelineRuns(PipelineRunFilter{PipelineID: pipelineID, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(runs) != 1 || runs[0].ID != run.ID {
		t.Fatalf("runs = %+v total = %d", runs, total)
	}
}
