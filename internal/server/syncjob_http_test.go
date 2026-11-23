package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/longkeyy/go-datax/internal/store"
)

func createSQLiteDataSourceForJob(t *testing.T, handler http.Handler, token, name, dir string) dataSourceResponse {
	t.Helper()

	dbPath := filepath.Join(dir, name+".db")
	createSQLiteDB(t, dbPath)
	return createDataSource(t, handler, token, name, "sqlite", "jdbc:sqlite:"+dbPath, "", "")
}

func formJobBody(name string, readerID, writerID int64) map[string]any {
	return map[string]any{
		"name":          name,
		"authoringMode": "form",
		"form": map[string]any{
			"reader": map[string]any{
				"dataSourceId": readerID,
				"table":        "users",
				"column":       []string{"id", "name"},
				"where":        "id > 0",
				"splitPk":      "id",
			},
			"writer": map[string]any{
				"dataSourceId": writerID,
				"table":        "users_copy",
				"column":       []string{"id", "name"},
				"writeMode":    "insert",
			},
			"setting": map[string]any{
				"channel": 2,
				"errorLimit": map[string]any{
					"record":     0,
					"percentage": 0.02,
				},
			},
		},
	}
}

func TestSyncJobFormCrudAndExport(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")
	dir := t.TempDir()
	reader := createSQLiteDataSourceForJob(t, handler, token, "reader", dir)
	writer := createSQLiteDataSourceForJob(t, handler, token, "writer", dir)

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs", token,
		formJobBody("users-sync", reader.ID, writer.ID))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var created syncJobResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("response is not a sync job: %v", err)
	}
	if created.AuthoringMode != "form" {
		t.Errorf("mode = %q, want form", created.AuthoringMode)
	}
	if created.Summary != "users → users_copy" {
		t.Errorf("summary = %q, want users → users_copy", created.Summary)
	}
	if created.Form == nil || created.Form.Setting.Channel != 2 {
		t.Errorf("form definition not preserved: %+v", created.Form)
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/sync-jobs", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	var jobs []syncJobResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &jobs); err != nil {
		t.Fatalf("list is not JSON: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Name != "users-sync" {
		t.Errorf("jobs = %+v, want users-sync", jobs)
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/sync-jobs/"+itoa(created.ID)+"/export", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var config map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &config); err != nil {
		t.Fatalf("export is not JSON: %v", err)
	}
	content := config["job"].(map[string]any)["content"].([]any)[0].(map[string]any)
	readerConfig := content["reader"].(map[string]any)
	writerConfig := content["writer"].(map[string]any)
	if readerConfig["name"] != "sqlitereader" {
		t.Errorf("reader plugin = %v, want sqlitereader", readerConfig["name"])
	}
	if writerConfig["name"] != "sqlitewriter" {
		t.Errorf("writer plugin = %v, want sqlitewriter", writerConfig["name"])
	}
	readerParams := readerConfig["parameter"].(map[string]any)
	if readerParams["where"] != "id > 0" || readerParams["splitPk"] != "id" {
		t.Errorf("reader params missing where/splitPk: %+v", readerParams)
	}
	channel := config["job"].(map[string]any)["setting"].(map[string]any)["speed"].(map[string]any)["channel"]
	if channel != float64(2) {
		t.Errorf("channel = %v, want 2", channel)
	}

	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs/"+itoa(created.ID)+"/duplicate", token, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("duplicate status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var copyJob syncJobResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &copyJob); err != nil {
		t.Fatalf("duplicate response is not JSON: %v", err)
	}
	if !strings.Contains(copyJob.Name, "副本") {
		t.Errorf("copy name = %q, want a 副本 suffix", copyJob.Name)
	}

	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/sync-jobs/"+itoa(created.ID), token, map[string]any{
		"name":          "users-sync-renamed",
		"authoringMode": "form",
		"form":          formJobBody("x", reader.ID, writer.ID)["form"],
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = authedJSONRequest(t, handler, http.MethodDelete, "/api/v1/sync-jobs/"+itoa(created.ID), token, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", rec.Code)
	}
}

func TestSyncJobValidation(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")
	dir := t.TempDir()
	reader := createSQLiteDataSourceForJob(t, handler, token, "reader", dir)
	writer := createSQLiteDataSourceForJob(t, handler, token, "writer", dir)

	mismatch := formJobBody("mismatch", reader.ID, writer.ID)
	mismatch["form"].(map[string]any)["writer"].(map[string]any)["column"] = []string{"id"}
	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs", token, mismatch)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("column mismatch status = %d, want 400", rec.Code)
	}

	missing := formJobBody("missing-source", reader.ID, 9999)
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs", token, missing)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing data source status = %d, want 400", rec.Code)
	}

	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs", token, map[string]any{
		"name":          "bad-raw",
		"authoringMode": "raw",
		"raw":           map[string]any{"job": map[string]any{"content": []any{}}},
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid raw status = %d, want 400", rec.Code)
	}
}

func TestSyncJobRawImportAndExport(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")

	config := map[string]any{
		"job": map[string]any{
			"setting": map[string]any{"speed": map[string]any{"channel": 1}},
			"content": []any{
				map[string]any{
					"reader": map[string]any{
						"name":      "streamreader",
						"parameter": map[string]any{"sliceRecordCount": 10, "column": []any{"id"}},
					},
					"writer": map[string]any{
						"name":      "streamwriter",
						"parameter": map[string]any{"print": true},
					},
				},
			},
		},
	}

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs/import", token, map[string]any{
		"name":   "stream-import",
		"config": config,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("import status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var imported syncJobResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &imported); err != nil {
		t.Fatalf("import response is not JSON: %v", err)
	}
	if imported.AuthoringMode != "raw" {
		t.Errorf("mode = %q, want raw", imported.AuthoringMode)
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/sync-jobs/"+itoa(imported.ID)+"/export", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export status = %d", rec.Code)
	}
	var exported map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &exported); err != nil {
		t.Fatalf("export is not JSON: %v", err)
	}
	content := exported["job"].(map[string]any)["content"].([]any)[0].(map[string]any)
	if content["reader"].(map[string]any)["name"] != "streamreader" {
		t.Errorf("exported reader name changed: %+v", content["reader"])
	}

	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/sync-jobs/"+itoa(imported.ID), token, map[string]any{
		"name":          "stream-import",
		"authoringMode": "form",
		"form":          formJobBody("x", 1, 2)["form"],
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("mode switch status = %d, want 400", rec.Code)
	}
}

func TestOracleFormJobExportUsesOraclePlugins(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/data-sources", token, map[string]any{
		"name":     "oracle-db",
		"type":     "oracle",
		"jdbcUrl":  "jdbc:oracle:thin:@//db.example.com:1521/orcl",
		"username": "app_user",
		"password": "secret",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create datasource status = %d, body = %s", rec.Code, rec.Body.String())
	}
	created := decodeDataSource(t, rec)

	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs", token,
		formJobBody("oracle-sync", created.ID, created.ID))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create job status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var job syncJobResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatalf("job response is not JSON: %v", err)
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/sync-jobs/"+itoa(job.ID)+"/export", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var config map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &config); err != nil {
		t.Fatalf("export is not JSON: %v", err)
	}
	content := config["job"].(map[string]any)["content"].([]any)[0].(map[string]any)
	reader := content["reader"].(map[string]any)
	writer := content["writer"].(map[string]any)
	if reader["name"] != "oraclereader" || writer["name"] != "oraclewriter" {
		t.Errorf("plugins = %v -> %v, want oraclereader -> oraclewriter", reader["name"], writer["name"])
	}

	readerConnection := reader["parameter"].(map[string]any)["connection"].([]any)[0].(map[string]any)
	writerConnection := writer["parameter"].(map[string]any)["connection"].([]any)[0].(map[string]any)
	if _, ok := readerConnection["jdbcUrl"].(string); !ok {
		t.Errorf("oracle reader jdbcUrl should be a string, got %T", readerConnection["jdbcUrl"])
	}
	if _, ok := writerConnection["jdbcUrl"].(string); !ok {
		t.Errorf("oracle writer jdbcUrl should be a string, got %T", writerConnection["jdbcUrl"])
	}
}

func TestDataSourceDeleteBlockedWhenReferenced(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")
	dir := t.TempDir()
	reader := createSQLiteDataSourceForJob(t, handler, token, "reader", dir)
	writer := createSQLiteDataSourceForJob(t, handler, token, "writer", dir)

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs", token,
		formJobBody("references-reader", reader.ID, writer.ID))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create job status = %d", rec.Code)
	}

	rec = authedJSONRequest(t, handler, http.MethodDelete, "/api/v1/data-sources/"+itoa(reader.ID), token, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete status = %d, want 409 (body=%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Error apiError `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if body.Error.Code != "data_source_in_use" {
		t.Errorf("error.code = %q, want data_source_in_use", body.Error.Code)
	}
	if !strings.Contains(body.Error.Message, "references-reader") {
		t.Errorf("message does not name the job: %q", body.Error.Message)
	}
}

func createRawJobForRuns(t *testing.T, dataStore *store.Store, name string) store.SyncJob {
	t.Helper()

	job := store.SyncJob{
		Name:          name,
		AuthoringMode: "raw",
		Definition:    `{"job":{"content":[{"reader":{"name":"streamreader","parameter":{}},"writer":{"name":"streamwriter","parameter":{}}}]}}`,
	}
	if err := dataStore.CreateSyncJob(&job); err != nil {
		t.Fatalf("CreateSyncJob() failed: %v", err)
	}
	return job
}

func seedJobRun(t *testing.T, dataStore *store.Store, job store.SyncJob, status string, started time.Time, duration time.Duration, summary string) store.JobRun {
	t.Helper()

	jobID := job.ID
	run := store.JobRun{
		SyncJobID:    &jobID,
		SyncJobName:  job.Name,
		Status:       status,
		Trigger:      store.RunTriggerManual,
		StartedAt:    started,
		ErrorSummary: summary,
	}
	if status != store.RunStatusRunning {
		finished := started.Add(duration)
		run.FinishedAt = &finished
	}
	if err := dataStore.CreateJobRun(&run); err != nil {
		t.Fatalf("CreateJobRun() failed: %v", err)
	}
	return run
}

func TestSyncJobLastRunAndStats(t *testing.T) {
	dataStore := openTestStore(t)
	_, handler := newTestServerWithStore(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	}, testAuthConfig(t), dataStore)
	token := loginToken(t, handler, "admin", "secret")

	job := createRawJobForRuns(t, dataStore, "nightly-sync")
	empty := createRawJobForRuns(t, dataStore, "empty-job")

	now := time.Now()
	seedJobRun(t, dataStore, job, store.RunStatusSucceeded, now.AddDate(0, 0, -40), 10*time.Second, "")
	seedJobRun(t, dataStore, job, store.RunStatusSucceeded, now.Add(-3*time.Hour), 30*time.Second, "")
	failed := seedJobRun(t, dataStore, job, store.RunStatusFailed, now.Add(-2*time.Hour), 60*time.Second, "boom")
	seedJobRun(t, dataStore, job, store.RunStatusCanceled, now.Add(-time.Hour), 90*time.Second, "")
	running := seedJobRun(t, dataStore, job, store.RunStatusRunning, now.Add(-30*time.Minute), 0, "")

	rec := authedJSONRequest(t, handler, http.MethodGet, "/api/v1/sync-jobs", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var jobs []syncJobResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &jobs); err != nil {
		t.Fatalf("list is not JSON: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("jobs = %d, want 2", len(jobs))
	}
	var listed, emptyListed *syncJobResponse
	for i := range jobs {
		switch jobs[i].ID {
		case job.ID:
			listed = &jobs[i]
		case empty.ID:
			emptyListed = &jobs[i]
		}
	}
	if listed == nil || listed.LastRun == nil {
		t.Fatalf("job lastRun missing: %+v", listed)
	}
	if listed.LastRun.ID != running.ID || listed.LastRun.Status != store.RunStatusRunning {
		t.Errorf("lastRun = %+v, want running run %d", listed.LastRun, running.ID)
	}
	if emptyListed == nil || emptyListed.LastRun != nil {
		t.Errorf("empty job lastRun = %+v, want nil", emptyListed)
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/sync-jobs/"+itoa(job.ID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var detail syncJobResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("detail is not JSON: %v", err)
	}
	if detail.LastRun == nil || detail.LastRun.ID != running.ID {
		t.Errorf("detail lastRun = %+v, want run %d", detail.LastRun, running.ID)
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/sync-jobs/"+itoa(job.ID)+"/stats?days=30", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("stats status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var stats struct {
		JobID              int64        `json:"jobId"`
		WindowDays         int          `json:"windowDays"`
		Total              int64        `json:"total"`
		Succeeded          int64        `json:"succeeded"`
		Failed             int64        `json:"failed"`
		Canceled           int64        `json:"canceled"`
		Running            int64        `json:"running"`
		SuccessRate        float64      `json:"successRate"`
		AvgDurationSeconds float64      `json:"avgDurationSeconds"`
		LastFailedRun      *runResponse `json:"lastFailedRun"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("stats is not JSON: %v", err)
	}
	if stats.JobID != job.ID || stats.WindowDays != 30 {
		t.Errorf("stats identity = %+v", stats)
	}
	if stats.Total != 4 || stats.Succeeded != 1 || stats.Failed != 1 || stats.Canceled != 1 || stats.Running != 1 {
		t.Errorf("stats counts = %+v, want 4/1/1/1/1 (old run excluded)", stats)
	}
	if diff := stats.SuccessRate - 1.0/3.0; diff > 0.001 || diff < -0.001 {
		t.Errorf("successRate = %v, want 1/3 (running excluded)", stats.SuccessRate)
	}
	if diff := stats.AvgDurationSeconds - 60; diff > 0.5 || diff < -0.5 {
		t.Errorf("avgDurationSeconds = %v, want ~60", stats.AvgDurationSeconds)
	}
	if stats.LastFailedRun == nil || stats.LastFailedRun.ID != failed.ID {
		t.Errorf("lastFailedRun = %+v, want run %d", stats.LastFailedRun, failed.ID)
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/sync-jobs/"+itoa(empty.ID)+"/stats", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty stats status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var emptyStats struct {
		Total       int64        `json:"total"`
		SuccessRate float64      `json:"successRate"`
		LastFailed  *runResponse `json:"lastFailedRun"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &emptyStats); err != nil {
		t.Fatalf("empty stats is not JSON: %v", err)
	}
	if emptyStats.Total != 0 || emptyStats.SuccessRate != 0 || emptyStats.LastFailed != nil {
		t.Errorf("empty stats = %+v, want zero values", emptyStats)
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/sync-jobs/9999/stats", token, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown job stats status = %d, want 404", rec.Code)
	}
}

func TestSyncJobRetryPolicy(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")
	dir := t.TempDir()
	reader := createSQLiteDataSourceForJob(t, handler, token, "retry-reader", dir)
	writer := createSQLiteDataSourceForJob(t, handler, token, "retry-writer", dir)

	body := formJobBody("retry-policy", reader.ID, writer.ID)
	body["retryMax"] = 3
	body["retryIntervalSeconds"] = 15

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var created syncJobResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("create response is not JSON: %v", err)
	}
	if created.RetryMax != 3 || created.RetryIntervalSeconds != 15 {
		t.Errorf("retry policy = %d/%ds, want 3/15s", created.RetryMax, created.RetryIntervalSeconds)
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/sync-jobs/"+itoa(created.ID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d", rec.Code)
	}
	var fetched syncJobResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &fetched); err != nil {
		t.Fatalf("get response is not JSON: %v", err)
	}
	if fetched.RetryMax != 3 || fetched.RetryIntervalSeconds != 15 {
		t.Errorf("fetched retry policy = %d/%ds, want 3/15s", fetched.RetryMax, fetched.RetryIntervalSeconds)
	}

	body["name"] = "retry-policy-updated"
	body["retryMax"] = 0
	body["retryIntervalSeconds"] = 30
	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/sync-jobs/"+itoa(created.ID), token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var updated syncJobResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("update response is not JSON: %v", err)
	}
	if updated.RetryMax != 0 || updated.RetryIntervalSeconds != 0 {
		t.Errorf("updated retry policy = %d/%ds, want 0/0 (interval cleared)", updated.RetryMax, updated.RetryIntervalSeconds)
	}

	body["retryMax"] = 11
	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/sync-jobs/"+itoa(created.ID), token, body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid retryMax status = %d, want 400", rec.Code)
	}
}

func rawStreamContent() map[string]any {
	return map[string]any{
		"reader": map[string]any{
			"name":      "streamreader",
			"parameter": map[string]any{"sliceRecordCount": 1, "column": []any{"id"}},
		},
		"writer": map[string]any{
			"name":      "streamwriter",
			"parameter": map[string]any{"print": false},
		},
	}
}

func TestRawConfigGuardrails(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")

	multi := map[string]any{
		"name":          "multi-content",
		"authoringMode": "raw",
		"raw": map[string]any{
			"job": map[string]any{"content": []any{rawStreamContent(), rawStreamContent()}},
		},
	}
	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs", token, multi)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("multi content status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
	var apiErr apiErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &apiErr); err != nil {
		t.Fatalf("error response is not JSON: %v", err)
	}
	if !strings.Contains(apiErr.Error.Message, "content[0]") {
		t.Errorf("multi content message = %q", apiErr.Error.Message)
	}

	transformerContent := rawStreamContent()
	transformerContent["transformer"] = []any{map[string]any{"name": "dx_filter"}}
	transformer := map[string]any{
		"name":          "with-transformer",
		"authoringMode": "raw",
		"raw": map[string]any{
			"job": map[string]any{"content": []any{transformerContent}},
		},
	}
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs", token, transformer)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("transformer status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &apiErr); err != nil {
		t.Fatalf("error response is not JSON: %v", err)
	}
	if !strings.Contains(apiErr.Error.Message, "transformer") {
		t.Errorf("transformer message = %q", apiErr.Error.Message)
	}

	single := map[string]any{
		"name":          "single-content",
		"authoringMode": "raw",
		"raw": map[string]any{
			"job": map[string]any{"content": []any{rawStreamContent()}},
		},
	}
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs", token, single)
	if rec.Code != http.StatusCreated {
		t.Fatalf("single content status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

type apiErrorResponse struct {
	Error apiError `json:"error"`
}

func TestFormWriteModeValidation(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")
	dir := t.TempDir()
	reader := createSQLiteDataSourceForJob(t, handler, token, "mode-reader", dir)
	pgWriter := createDataSource(t, handler, token, "mode-pg", "postgres",
		"jdbc:postgresql://localhost:5432/test", "postgres", "postgres")
	oracleWriter := createDataSource(t, handler, token, "mode-oracle", "oracle",
		"jdbc:oracle:thin:@//db.example.com:1521/orcl", "scott", "tiger")

	withWriteMode := func(name string, writerID int64, mode string) map[string]any {
		body := formJobBody(name, reader.ID, writerID)
		formDef := body["form"].(map[string]any)
		writer := formDef["writer"].(map[string]any)
		writer["writeMode"] = mode
		return body
	}

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs", token,
		withWriteMode("pg-update", pgWriter.ID, "update"))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "不支持 writeMode") {
		t.Fatalf("pg update status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs", token,
		withWriteMode("pg-insert", pgWriter.ID, "insert"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("pg insert status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs", token,
		withWriteMode("oracle-update-no-keys", oracleWriter.ID, "update"))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "需要指定主键列") {
		t.Fatalf("oracle plain update status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs", token,
		withWriteMode("oracle-update-keys", oracleWriter.ID, "update(id)"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("oracle update(id) status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs", token,
		withWriteMode("oracle-update-missing", oracleWriter.ID, "update(missing)"))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "不在写入列中") {
		t.Fatalf("oracle missing key status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestLegacyRawGuardrailsOnRun(t *testing.T) {
	dataStore := openTestStore(t)
	_, handler := newRunTestServer(t, dataStore, nil, 2)
	token := loginToken(t, handler, "admin", "secret")

	multi := store.SyncJob{
		Name:          "legacy-multi",
		AuthoringMode: "raw",
		Definition:    `{"job":{"content":[{"reader":{"name":"streamreader","parameter":{}},"writer":{"name":"streamwriter","parameter":{}}},{"reader":{"name":"streamreader","parameter":{}},"writer":{"name":"streamwriter","parameter":{}}}]}}`,
	}
	if err := dataStore.CreateSyncJob(&multi); err != nil {
		t.Fatalf("CreateSyncJob() failed: %v", err)
	}
	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs/"+itoa(multi.ID)+"/runs", token, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("legacy multi run status = %d, want 422 (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "content[0]") {
		t.Errorf("legacy multi message = %s", rec.Body.String())
	}

	transformer := store.SyncJob{
		Name:          "legacy-transformer",
		AuthoringMode: "raw",
		Definition:    `{"job":{"content":[{"reader":{"name":"streamreader","parameter":{}},"writer":{"name":"streamwriter","parameter":{}},"transformer":[{"name":"dx_filter"}]}]}}`,
	}
	if err := dataStore.CreateSyncJob(&transformer); err != nil {
		t.Fatalf("CreateSyncJob() failed: %v", err)
	}
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs/"+itoa(transformer.ID)+"/runs", token, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("legacy transformer run status = %d, want 422 (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "transformer") {
		t.Errorf("legacy transformer message = %s", rec.Body.String())
	}
}
