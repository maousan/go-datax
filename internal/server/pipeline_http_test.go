package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/longkeyy/go-datax/internal/store"
)

func newPipelineTestServer(t *testing.T) (*Server, http.Handler, *store.Store) {
	t.Helper()
	if testing.Short() {
		t.Skip("builds and spawns etl-worker")
	}

	workerBin := filepath.Join(t.TempDir(), "etl-worker")
	if runtime.GOOS == "windows" {
		workerBin += ".exe"
	}
	build := exec.Command("go", "build", "-o", workerBin, "github.com/longkeyy/go-datax/cmd/etl-worker")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build etl-worker: %v\n%s", err, output)
	}

	dataStore := openTestStore(t)
	cfg := DefaultConfig(t.TempDir())
	cfg.Auth = testAuthConfig(t)
	cfg.Worker = WorkerConfig{Bin: workerBin, Listen: "127.0.0.1:0"}
	cfg.path = filepath.Join(t.TempDir(), "server.json")
	assets := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>app</html>")}}
	srv, err := New(cfg, "test", assets, dataStore)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if err := srv.StartWorker(); err != nil {
		t.Fatalf("StartWorker() failed: %v", err)
	}
	t.Cleanup(srv.StopWorker)
	return srv, srv.Handler(), dataStore
}

func seedPipelineSourceAndSink(t *testing.T, dataStore *store.Store, base, sourcePath, sinkPath string) (int64, int64) {
	t.Helper()

	schemaJSON := `[{"name":"id","type":"int64"},{"name":"name","type":"string"}]`
	files := &store.DataSource{Name: "pipeline-files", Type: "filesystem", Properties: store.StringMap{"basePath": base}}
	if err := dataStore.CreateDataSource(files); err != nil {
		t.Fatal(err)
	}
	source := &store.Endpoint{
		Kind:         store.EndpointKindSource,
		Name:         "pipeline-csv",
		Connector:    "csv",
		DataSourceID: files.ID,
		Path:         sourcePath,
		SchemaJSON:   schemaJSON,
		Options:      store.StringMap{"header": "true"},
	}
	if err := dataStore.CreateEndpoint(source); err != nil {
		t.Fatal(err)
	}

	database := &store.DataSource{Name: "pipeline-db", Type: "sqlite", JDBCUrl: "jdbc:sqlite:" + sinkPath}
	if err := dataStore.CreateDataSource(database); err != nil {
		t.Fatal(err)
	}
	sink := &store.Endpoint{
		Kind:         store.EndpointKindSink,
		Name:         "pipeline-sqlite",
		Connector:    "sqlite",
		DataSourceID: database.ID,
		TableName:    "dst",
		SchemaJSON:   schemaJSON,
		Options:      store.StringMap{"writeMode": "truncate"},
	}
	if err := dataStore.CreateEndpoint(sink); err != nil {
		t.Fatal(err)
	}
	return source.ID, sink.ID
}

func createPipelineTestTarget(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE dst (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatal(err)
	}
}

func pipelineRunByID(t *testing.T, handler http.Handler, token string, id int64) pipelineRunResponse {
	t.Helper()
	rec := authedJSONRequest(t, handler, http.MethodGet, "/api/v1/pipeline-runs/"+itoa(id), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get run status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var run pipelineRunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	return run
}

func waitPipelineRun(t *testing.T, handler http.Handler, token string, id int64) pipelineRunResponse {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		run := pipelineRunByID(t, handler, token, id)
		if run.Status != store.PipelineRunStatusRunning {
			return run
		}
		if time.Now().After(deadline) {
			t.Fatalf("pipeline run #%d did not finish: %+v", id, run)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func targetRowCount(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM dst`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestPipelineValidation(t *testing.T) {
	_, handler, dataStore := newPipelineTestServer(t)
	token := loginToken(t, handler, "admin", "secret")
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "source.csv"), []byte("id,name\n1,a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sinkPath := filepath.Join(t.TempDir(), "sink.db")
	createPipelineTestTarget(t, sinkPath)
	sourceID, sinkID := seedPipelineSourceAndSink(t, dataStore, base, "source.csv", sinkPath)

	sink, err := dataStore.GetEndpoint(store.EndpointKindSink, sinkID)
	if err != nil {
		t.Fatal(err)
	}
	sink.SchemaJSON = `[{"name":"id","type":"int64"}]`
	if err := dataStore.UpdateEndpoint(&sink); err != nil {
		t.Fatal(err)
	}

	body := map[string]any{
		"name":     "validation-pipeline",
		"sourceId": sourceID,
		"sinkId":   sinkID,
	}
	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var pipeline pipelineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &pipeline); err != nil {
		t.Fatal(err)
	}
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines/"+itoa(pipeline.ID)+"/runs", token, nil)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "尚未发布") {
		t.Fatalf("unpublished run status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines/"+itoa(pipeline.ID)+"/publish", token, nil)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "字段不一致") {
		t.Fatalf("mismatched publish status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines", token, body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate pipeline status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestPipelineRunFailureRecorded(t *testing.T) {
	_, handler, dataStore := newPipelineTestServer(t)
	token := loginToken(t, handler, "admin", "secret")
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "source.csv"), []byte("id,name\n1,a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sinkPath := filepath.Join(t.TempDir(), "sink.db")
	createPipelineTestTarget(t, sinkPath)
	sourceID, sinkID := seedPipelineSourceAndSink(t, dataStore, base, "source.csv", sinkPath)
	sink, err := dataStore.GetEndpoint(store.EndpointKindSink, sinkID)
	if err != nil {
		t.Fatal(err)
	}
	sink.TableName = "missing_table"
	if err := dataStore.UpdateEndpoint(&sink); err != nil {
		t.Fatal(err)
	}

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines", token, map[string]any{
		"name":     "failing-pipeline",
		"sourceId": sourceID,
		"sinkId":   sinkID,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var pipeline pipelineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &pipeline); err != nil {
		t.Fatal(err)
	}
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines/"+itoa(pipeline.ID)+"/publish", token, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("publish status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines/"+itoa(pipeline.ID)+"/runs", token, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("run status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var run pipelineRunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	run = waitPipelineRun(t, handler, token, run.ID)
	if run.Status != store.PipelineRunStatusFailed || run.ErrorSummary == "" {
		t.Fatalf("failed run = %+v", run)
	}
	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/pipeline-runs/"+itoa(run.ID)+"/log", token, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "done status=failed") {
		t.Fatalf("failure log status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestPipelinePublishRunAndDraftIsolation(t *testing.T) {
	_, handler, dataStore := newPipelineTestServer(t)
	token := loginToken(t, handler, "admin", "secret")
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "source.csv"), []byte("id,name\n1,a\n2,b\n3,c\n4,d\n5,e\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "source2.csv"), []byte("id,name\n10,x\n11,y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sinkPath := filepath.Join(t.TempDir(), "sink.db")
	createPipelineTestTarget(t, sinkPath)
	sourceID, sinkID := seedPipelineSourceAndSink(t, dataStore, base, "source.csv", sinkPath)
	filesDS, err := dataStore.ListDataSources("pipeline-files")
	if err != nil || len(filesDS) != 1 {
		t.Fatalf("files data source = %+v, err = %v", filesDS, err)
	}
	source2 := &store.Endpoint{
		Kind:         store.EndpointKindSource,
		Name:         "pipeline-csv-2",
		Connector:    "csv",
		DataSourceID: filesDS[0].ID,
		Path:         "source2.csv",
		SchemaJSON:   `[{"name":"id","type":"int64"},{"name":"name","type":"string"}]`,
		Options:      store.StringMap{"header": "true"},
	}
	if err := dataStore.CreateEndpoint(source2); err != nil {
		t.Fatal(err)
	}

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines", token, map[string]any{
		"name":        "csv-to-sqlite",
		"description": "tracer bullet",
		"sourceId":    sourceID,
		"sinkId":      sinkID,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create pipeline status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var pipeline pipelineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &pipeline); err != nil {
		t.Fatal(err)
	}
	if pipeline.Source == nil || pipeline.Source.Name != "pipeline-csv" || pipeline.Sink == nil {
		t.Fatalf("pipeline = %+v", pipeline)
	}
	rec = authedJSONRequest(t, handler, http.MethodDelete, "/api/v1/sources/"+itoa(sourceID), token, nil)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "csv-to-sqlite") {
		t.Fatalf("delete referenced source status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines/"+itoa(pipeline.ID)+"/publish", token, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("publish status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var version pipelineVersionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &version); err != nil {
		t.Fatal(err)
	}
	if version.VersionNumber != 1 || version.Source.Name != "pipeline-csv" || version.Sink.Name != "pipeline-sqlite" {
		t.Fatalf("version = %+v", version)
	}
	if strings.Contains(rec.Body.String(), "jdbc:sqlite") {
		t.Fatalf("version snapshot leaked the DataSource connection fields: %s", rec.Body.String())
	}

	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines/"+itoa(pipeline.ID)+"/runs", token, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("run status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var run pipelineRunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if run.Status != store.PipelineRunStatusRunning || run.VersionNumber != 1 {
		t.Fatalf("run = %+v", run)
	}

	// SSE works both for a live stream and for the persisted log fallback.
	sse := authedJSONRequest(t, handler, http.MethodGet, "/api/v1/pipeline-runs/"+itoa(run.ID)+"/events", token, nil)
	if sse.Code != http.StatusOK || !strings.Contains(sse.Body.String(), "event: done") ||
		!strings.Contains(sse.Body.String(), `"status":"succeeded"`) {
		t.Fatalf("SSE status = %d, body = %s", sse.Code, sse.Body.String())
	}

	run = waitPipelineRun(t, handler, token, run.ID)
	if run.Status != store.PipelineRunStatusSucceeded || run.ReadRecords != 5 || run.WriteRecords != 5 {
		t.Fatalf("finished run = %+v", run)
	}
	if count := targetRowCount(t, sinkPath); count != 5 {
		t.Fatalf("target count = %d, want 5", count)
	}
	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/pipeline-runs/"+itoa(run.ID)+"/log", token, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "done status=succeeded") {
		t.Fatalf("log status = %d, body = %s", rec.Code, rec.Body.String())
	}

	activePipelineID := pipeline.ID
	activeRun := &store.PipelineRun{
		PipelineID:   &activePipelineID,
		PipelineName: pipeline.Name,
		Status:       store.PipelineRunStatusRunning,
		Trigger:      store.PipelineRunTriggerManual,
	}
	if err := dataStore.CreatePipelineRun(activeRun); err != nil {
		t.Fatal(err)
	}
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines/"+itoa(pipeline.ID)+"/runs", token, nil)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "已有运行中") {
		t.Fatalf("duplicate run status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if err := dataStore.FinishPipelineRun(activeRun.ID, store.PipelineRunStatusFailed, "test cleanup"); err != nil {
		t.Fatal(err)
	}

	// Editing the draft must not mutate the published version.
	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/pipelines/"+itoa(pipeline.ID), token, map[string]any{
		"name":        "csv-to-sqlite",
		"description": "draft changed",
		"sourceId":    source2.ID,
		"sinkId":      sinkID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update draft status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/pipeline-versions/"+itoa(version.ID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get version status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var storedVersion pipelineVersionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &storedVersion); err != nil {
		t.Fatal(err)
	}
	if storedVersion.Source.Name != "pipeline-csv" {
		t.Fatalf("published version changed: %+v", storedVersion.Source)
	}
	rec = authedJSONRequest(t, handler, http.MethodDelete, "/api/v1/sources/"+itoa(sourceID), token, nil)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "已发布") {
		t.Fatalf("delete version-referenced source status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/pipelines/"+itoa(pipeline.ID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get pipeline status = %d", rec.Code)
	}
	var updated pipelineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Source == nil || updated.Source.Name != "pipeline-csv-2" {
		t.Fatalf("draft source = %+v", updated.Source)
	}

	// A second run still executes version 1. Changing the shared DataSource is
	// resolved at run time, so the version should read the new base path.
	secondBase := t.TempDir()
	if err := os.WriteFile(filepath.Join(secondBase, "source.csv"), []byte("id,name\n20,x\n21,y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := filesDS[0]
	files.Properties = store.StringMap{"basePath": secondBase}
	if err := dataStore.UpdateDataSource(&files); err != nil {
		t.Fatal(err)
	}
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines/"+itoa(pipeline.ID)+"/runs", token, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("second run status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var secondRun pipelineRunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &secondRun); err != nil {
		t.Fatal(err)
	}
	secondRun = waitPipelineRun(t, handler, token, secondRun.ID)
	if secondRun.Status != store.PipelineRunStatusSucceeded || secondRun.WriteRecords != 2 {
		t.Fatalf("second run = %+v", secondRun)
	}
	if count := targetRowCount(t, sinkPath); count != 2 {
		t.Fatalf("target count after second run = %d, want version 1 rows from the updated data source", count)
	}

	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines/"+itoa(pipeline.ID)+"/publish", token, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("second publish status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var secondVersion pipelineVersionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &secondVersion); err != nil {
		t.Fatal(err)
	}
	if secondVersion.VersionNumber != 2 || secondVersion.Source.Name != "pipeline-csv-2" {
		t.Fatalf("second version = %+v", secondVersion)
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/pipelines/"+itoa(pipeline.ID)+"/versions", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("versions status = %d", rec.Code)
	}
	var versions []pipelineVersionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &versions); err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0].VersionNumber != 2 || versions[1].VersionNumber != 1 {
		t.Fatalf("versions = %+v", versions)
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/pipeline-runs?pipelineId="+itoa(pipeline.ID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("run list status = %d", rec.Code)
	}
	var runs struct {
		Items []pipelineRunResponse `json:"items"`
		Total int64                 `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &runs); err != nil {
		t.Fatal(err)
	}
	if runs.Total != 3 || len(runs.Items) != 3 {
		t.Fatalf("runs = %+v", runs)
	}
}
