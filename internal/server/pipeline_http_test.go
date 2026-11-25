package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
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
	"github.com/longkeyy/go-datax/internal/transform"
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

func TestPipelineRunRetention(t *testing.T) {
	dataStore := openTestStore(t)
	srv, _ := newTestServerWithStore(t, map[string]string{"index.html": "<html>app</html>"}, testAuthConfig(t), dataStore)
	if err := dataStore.SaveSetting(runtimeSettingKey,
		`{"maxConcurrent":2,"maxRuns":1,"maxAgeDays":30,"maxLogBytes":1048576}`); err != nil {
		t.Fatal(err)
	}
	logDir := t.TempDir()
	var logs []string
	for index := 0; index < 3; index++ {
		path := filepath.Join(logDir, fmt.Sprintf("run-%d.log", index))
		if err := os.WriteFile(path, []byte("log"), 0o644); err != nil {
			t.Fatal(err)
		}
		logs = append(logs, path)
		run := &store.PipelineRun{
			PipelineName: "retention",
			Status:       store.PipelineRunStatusSucceeded,
			Trigger:      store.PipelineRunTriggerManual,
			StartedAt:    time.Now(),
			LogPath:      path,
		}
		if err := dataStore.CreatePipelineRun(run); err != nil {
			t.Fatal(err)
		}
	}
	srv.prunePipelineRuns()
	runs, total, err := dataStore.ListPipelineRuns(store.PipelineRunFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(runs) != 1 {
		t.Fatalf("retained runs = %+v total = %d", runs, total)
	}
	for _, path := range logs[:2] {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed", path)
		}
	}
	if _, err := os.Stat(logs[2]); err != nil {
		t.Fatalf("newest log missing: %v", err)
	}
}

func TestPipelineTransformsPreviewAndRun(t *testing.T) {
	_, handler, dataStore := newPipelineTestServer(t)
	token := loginToken(t, handler, "admin", "secret")
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "source.csv"), []byte("id,name\n1,alice\n2,bob\n3,carol\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sinkPath := filepath.Join(t.TempDir(), "sink.db")
	db, err := sql.Open("sqlite", sinkPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE dst2 (id INTEGER PRIMARY KEY, label TEXT)`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	sourceID, sinkID := seedPipelineSourceAndSink(t, dataStore, base, "source.csv", sinkPath)
	sink, err := dataStore.GetEndpoint(store.EndpointKindSink, sinkID)
	if err != nil {
		t.Fatal(err)
	}
	sink.TableName = "dst2"
	sink.SchemaJSON = `[{"name":"id","type":"int64"},{"name":"label","type":"string"}]`
	if err := dataStore.UpdateEndpoint(&sink); err != nil {
		t.Fatal(err)
	}

	transforms := []any{
		map[string]any{
			"type": "project",
			"project": []any{
				map[string]any{"name": "id", "type": "int64", "expr": "id"},
				map[string]any{"name": "label", "type": "string", "expr": "upper(name)"},
			},
		},
		map[string]any{"type": "filter", "condition": "id > 1"},
	}
	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines/transform-preview", token, map[string]any{
		"sourceId":   sourceID,
		"transforms": transforms,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("preview status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var preview transform.Preview
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if len(preview.Steps) != 2 || len(preview.OutputSchema) != 2 ||
		preview.OutputSchema[1].Name != "label" {
		t.Fatalf("preview = %+v", preview)
	}

	body := map[string]any{
		"name":       "transform-pipeline",
		"sourceId":   sourceID,
		"sinkId":     sinkID,
		"transforms": transforms,
	}
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var pipeline pipelineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &pipeline); err != nil {
		t.Fatal(err)
	}
	if len(pipeline.Transforms) != 2 || pipeline.TransformPreview == nil ||
		len(pipeline.TransformPreview.OutputSchema) != 2 {
		t.Fatalf("pipeline transforms = %+v preview = %+v", pipeline.Transforms, pipeline.TransformPreview)
	}

	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines/"+itoa(pipeline.ID)+"/publish", token, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("publish status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var version pipelineVersionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &version); err != nil {
		t.Fatal(err)
	}
	if len(version.Transforms) != 2 || version.TransformLibraryVersion != transform.FunctionLibraryVersion {
		t.Fatalf("version transforms = %+v library version = %d", version.Transforms, version.TransformLibraryVersion)
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
	if run.Status != store.PipelineRunStatusSucceeded || run.ReadRecords != 3 || run.WriteRecords != 2 {
		t.Fatalf("run = %+v", run)
	}
	db, err = sql.Open("sqlite", sinkPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT id, label FROM dst2 ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var id int
		var label string
		if err := rows.Scan(&id, &label); err != nil {
			t.Fatal(err)
		}
		got = append(got, fmt.Sprintf("%d:%s", id, label))
	}
	if strings.Join(got, ",") != "2:BOB,3:CAROL" {
		t.Fatalf("target rows = %v", got)
	}

	invalid := map[string]any{
		"name":       "invalid-transform",
		"sourceId":   sourceID,
		"sinkId":     sinkID,
		"transforms": []any{map[string]any{"type": "filter", "condition": "missing > 1"}},
	}
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines", token, invalid)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "missing") {
		t.Fatalf("invalid transform status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestPipelineRunCancel(t *testing.T) {
	_, handler, dataStore := newPipelineTestServer(t)
	token := loginToken(t, handler, "admin", "secret")
	base := t.TempDir()
	var builder strings.Builder
	builder.WriteString("id,name\n")
	for index := 0; index < 200000; index++ {
		fmt.Fprintf(&builder, "%d,row\n", index+1)
	}
	if err := os.WriteFile(filepath.Join(base, "source.csv"), []byte(builder.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	sinkPath := filepath.Join(t.TempDir(), "sink.db")
	createPipelineTestTarget(t, sinkPath)
	sourceID, sinkID := seedPipelineSourceAndSink(t, dataStore, base, "source.csv", sinkPath)

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines", token, map[string]any{
		"name":     "cancel-pipeline",
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
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipeline-runs/"+itoa(run.ID)+"/cancel", token, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("cancel status = %d, body = %s", rec.Code, rec.Body.String())
	}
	run = waitPipelineRun(t, handler, token, run.ID)
	if run.Status != store.PipelineRunStatusCanceled {
		t.Fatalf("canceled run = %+v", run)
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

	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipeline-runs/"+itoa(run.ID)+"/rerun", token, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("rerun status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var retried pipelineRunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &retried); err != nil {
		t.Fatal(err)
	}
	if retried.ID == run.ID || retried.Trigger != store.PipelineRunTriggerRerun {
		t.Fatalf("retried run = %+v", retried)
	}
	retried = waitPipelineRun(t, handler, token, retried.ID)
	if retried.Status != store.PipelineRunStatusFailed {
		t.Fatalf("rerun result = %+v", retried)
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
		StartedAt:    time.Now(),
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

	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipeline-versions/"+itoa(version.ID)+"/runs", token, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("run version status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var versionRun pipelineRunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &versionRun); err != nil {
		t.Fatal(err)
	}
	if versionRun.VersionNumber != 1 {
		t.Fatalf("version run = %+v", versionRun)
	}
	versionRun = waitPipelineRun(t, handler, token, versionRun.ID)
	if versionRun.Status != store.PipelineRunStatusSucceeded || versionRun.WriteRecords != 2 {
		t.Fatalf("version run result = %+v", versionRun)
	}

	rec = authedJSONRequest(t, handler, http.MethodPost,
		"/api/v1/pipelines/"+itoa(pipeline.ID)+"/versions/"+itoa(version.ID)+"/restore", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("restore draft status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var restored pipelineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Source == nil || restored.Source.Name != "pipeline-csv" || restored.SourceID != sourceID {
		t.Fatalf("restored draft = %+v", restored)
	}

	// Restore must preserve the frozen endpoint definition even if the live
	// Source endpoint changes afterwards.
	sourceEndpoint, err := dataStore.GetEndpoint(store.EndpointKindSource, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	sourceEndpoint.Path = "changed.csv"
	if err := dataStore.UpdateEndpoint(&sourceEndpoint); err != nil {
		t.Fatal(err)
	}
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines/"+itoa(pipeline.ID)+"/publish", token, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("restore publish status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var restoredVersion pipelineVersionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &restoredVersion); err != nil {
		t.Fatal(err)
	}
	if restoredVersion.Source.Path != "source.csv" || len(restoredVersion.Source.Schema) != 2 {
		t.Fatalf("restored version used the edited endpoint: %+v", restoredVersion.Source)
	}

	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/pipelines/"+itoa(pipeline.ID), token, map[string]any{
		"name":        pipeline.Name,
		"description": pipeline.Description,
		"sourceId":    source2.ID,
		"sinkId":      sinkID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update after restore status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var changed pipelineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &changed); err != nil {
		t.Fatal(err)
	}
	if changed.DraftVersionNumber != 0 || changed.Source == nil || changed.Source.Name != "pipeline-csv-2" {
		t.Fatalf("draft snapshot was not cleared: %+v", changed)
	}
}

func TestPipelineResponseKeepsStaleTransformDraftReadable(t *testing.T) {
	pipeline := store.Pipeline{
		ID:             1,
		Name:           "stale-transform",
		SourceID:       11,
		SinkID:         12,
		TransformsJSON: `[{"type":"filter","condition":"missing > 1"}]`,
	}
	source := store.Endpoint{
		ID:         11,
		Kind:       store.EndpointKindSource,
		Name:       "source",
		Connector:  "csv",
		SchemaJSON: `[{"name":"id","type":"int64"}]`,
	}
	sink := store.Endpoint{
		ID:         12,
		Kind:       store.EndpointKindSink,
		Name:       "sink",
		Connector:  "sqlite",
		SchemaJSON: `[{"name":"id","type":"int64"}]`,
	}

	response, err := pipelineResponseFrom(pipeline, source, sink, "files", "db", nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("pipelineResponseFrom() failed: %v", err)
	}
	if response.TransformPreview != nil {
		t.Fatalf("TransformPreview = %+v, want nil", response.TransformPreview)
	}
	if len(response.Transforms) != 1 {
		t.Fatalf("transforms = %+v", response.Transforms)
	}
}
