package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
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
