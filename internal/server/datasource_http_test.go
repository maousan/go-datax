package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/longkeyy/go-datax/internal/store"
)

func authedJSONRequest(t *testing.T, handler http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var reader *strings.Reader
	if body == nil {
		reader = strings.NewReader("")
	} else {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = strings.NewReader(string(data))
	}

	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

func decodeDataSource(t *testing.T, rec *httptest.ResponseRecorder) dataSourceResponse {
	t.Helper()

	var ds dataSourceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &ds); err != nil {
		t.Fatalf("response is not a data source: %v (body=%q)", err, rec.Body.String())
	}
	return ds
}

func createDataSource(t *testing.T, handler http.Handler, token, name, dsType, jdbcUrl, username, password string) dataSourceResponse {
	t.Helper()

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/data-sources", token, map[string]any{
		"name":     name,
		"type":     dsType,
		"jdbcUrl":  jdbcUrl,
		"username": username,
		"password": password,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	return decodeDataSource(t, rec)
}

func TestDataSourceCrudAndMasking(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")

	created := createDataSource(t, handler, token,
		"prod-mysql", "mysql", "jdbc:mysql://localhost:3306/app", "root", "s3cret")
	if created.ID == 0 {
		t.Error("expected an assigned id")
	}
	if created.Password != maskedPassword {
		t.Errorf("created password = %q, want %q", created.Password, maskedPassword)
	}

	rec := authedJSONRequest(t, handler, http.MethodGet, "/api/v1/data-sources", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "s3cret") {
		t.Errorf("list leaked the password: %s", rec.Body.String())
	}
	var items []dataSourceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("list is not an array: %v", err)
	}
	if len(items) != 1 || items[0].Name != "prod-mysql" {
		t.Errorf("list = %+v, want prod-mysql", items)
	}

	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/data-sources/"+itoa(created.ID), token, map[string]any{
		"name":     "prod-mysql-renamed",
		"type":     "mysql",
		"jdbcUrl":  "jdbc:mysql://localhost:3306/app",
		"username": "root",
		"password": "",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", rec.Code, rec.Body.String())
	}
	updated := decodeDataSource(t, rec)
	if updated.Name != "prod-mysql-renamed" {
		t.Errorf("updated name = %q, want prod-mysql-renamed", updated.Name)
	}
	if updated.Password != maskedPassword {
		t.Errorf("updated password = %q, want mask", updated.Password)
	}

	rec = authedJSONRequest(t, handler, http.MethodDelete, "/api/v1/data-sources/"+itoa(created.ID), token, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/data-sources/"+itoa(created.ID), token, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("get after delete status = %d, want 404", rec.Code)
	}
}

func TestDataSourceDuplicateName(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")

	createDataSource(t, handler, token, "Alpha", "mysql", "jdbc:mysql://localhost/app", "root", "")

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/data-sources", token, map[string]any{
		"name":     "alpha",
		"type":     "mysql",
		"jdbcUrl":  "jdbc:mysql://localhost/app",
		"username": "root",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}

	var body struct {
		Error apiError `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if body.Error.Code != "duplicate_name" {
		t.Errorf("error.code = %q, want duplicate_name", body.Error.Code)
	}
}

func TestDataSourceValidation(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")

	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing name", map[string]any{"type": "mysql", "jdbcUrl": "jdbc:mysql://x", "username": "root"}},
		{"unknown type", map[string]any{"name": "a", "type": "oracle", "jdbcUrl": "jdbc:oracle:x", "username": "root"}},
		{"missing url", map[string]any{"name": "a", "type": "mysql", "username": "root"}},
		{"wrong prefix", map[string]any{"name": "a", "type": "mysql", "jdbcUrl": "jdbc:postgresql://x", "username": "root"}},
		{"missing username", map[string]any{"name": "a", "type": "postgres", "jdbcUrl": "jdbc:postgresql://x"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/data-sources", token, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d (body=%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}

func TestDataSourceSearch(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")

	createDataSource(t, handler, token, "Alpha", "mysql", "jdbc:mysql://localhost/app", "root", "")
	createDataSource(t, handler, token, "Beta", "postgres", "jdbc:postgresql://localhost/app", "postgres", "")

	rec := authedJSONRequest(t, handler, http.MethodGet, "/api/v1/data-sources?query=alp", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var items []dataSourceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if len(items) != 1 || items[0].Name != "Alpha" {
		t.Errorf("items = %+v, want only Alpha", items)
	}
}

func TestDataSourceRequiresAuthentication(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})

	rec := authedJSONRequest(t, handler, http.MethodGet, "/api/v1/data-sources", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestDataSourcePersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin.db")
	auth := testAuthConfig(t)
	assets := map[string]string{"index.html": "<html><body>app-shell</body></html>"}

	firstStore, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open() failed: %v", err)
	}
	_, first := newTestServerWithStore(t, assets, auth, firstStore)
	token := loginToken(t, first, "admin", "secret")
	createDataSource(t, first, token, "persisted", "sqlite", "jdbc:sqlite:/tmp/app.db", "", "")
	if err := firstStore.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	secondStore, err := store.Open(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() { _ = secondStore.Close() })
	_, second := newTestServerWithStore(t, assets, auth, secondStore)

	rec := authedJSONRequest(t, second, http.MethodGet, "/api/v1/data-sources", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var items []dataSourceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if len(items) != 1 || items[0].Name != "persisted" {
		t.Errorf("items = %+v, want persisted data source", items)
	}
}

func TestMSSQLDataSourceAcceptsJTDSURL(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/data-sources", token, map[string]any{
		"name":     "warehouse-db",
		"type":     "mssql",
		"jdbcUrl":  "jdbc:jtds:sqlserver://db.example.com:1433/sample_db",
		"username": "app_user",
		"password": "P@ss:w/ord",
		"properties": map[string]string{
			"key":    "warehouse",
			"driver": "com.example.Driver",
		},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	created := decodeDataSource(t, rec)
	if created.Type != "mssql" {
		t.Errorf("type = %q, want mssql", created.Type)
	}
	if created.Properties["key"] != "warehouse" {
		t.Errorf("properties = %+v, want key=warehouse", created.Properties)
	}
	if strings.Contains(rec.Body.String(), "P@ss:w/ord") {
		t.Error("response leaked the password")
	}
}

func TestConnectionTestForUnsavedSQLite(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")
	dbPath := filepath.Join(t.TempDir(), "probe.db")
	createSQLiteDB(t, dbPath)

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/data-sources/test", token, map[string]any{
		"name":    "probe",
		"type":    "sqlite",
		"jdbcUrl": "jdbc:sqlite:" + dbPath,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var result struct {
		OK        bool   `json:"ok"`
		LatencyMs int64  `json:"latencyMs"`
		Message   string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if !result.OK {
		t.Errorf("ok = false, message = %q", result.Message)
	}
	if result.Message != "连接成功" {
		t.Errorf("message = %q, want 连接成功", result.Message)
	}
}

func TestConnectionTestForSavedDataSource(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")
	dbPath := filepath.Join(t.TempDir(), "saved.db")
	createSQLiteDB(t, dbPath)
	created := createDataSource(t, handler, token, "saved-sqlite", "sqlite", "jdbc:sqlite:"+dbPath, "", "")

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/data-sources/test", token, map[string]any{
		"id": created.ID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var result struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if !result.OK {
		t.Errorf("saved connection test failed: %s", result.Message)
	}
}

func TestConnectionTestReportsFailure(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/data-sources/test", token, map[string]any{
		"name":     "unreachable",
		"type":     "mysql",
		"jdbcUrl":  "jdbc:mysql://127.0.0.1:1/app",
		"username": "root",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var result struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if result.OK {
		t.Error("expected the connection test to fail")
	}
	if result.Message == "" {
		t.Error("expected an error message")
	}
}

func TestConnectionTestRejectsUnsupportedType(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/data-sources/test", token, map[string]any{
		"name":    "oracle",
		"type":    "oracle",
		"jdbcUrl": "jdbc:oracle:thin:@localhost:1521/xe",
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func createSQLiteDB(t *testing.T, path string) {
	t.Helper()

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE probe (id INTEGER)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
}

func TestDataSourceRenameConflict(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")

	createDataSource(t, handler, token, "Alpha", "mysql", "jdbc:mysql://localhost/app", "root", "")
	second := createDataSource(t, handler, token, "Beta", "mysql", "jdbc:mysql://localhost/app", "root", "")

	rec := authedJSONRequest(t, handler, http.MethodPut, "/api/v1/data-sources/"+itoa(second.ID), token, map[string]any{
		"name":     "alpha",
		"type":     "mysql",
		"jdbcUrl":  "jdbc:mysql://localhost/app",
		"username": "root",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (body=%s)", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestDataSourcePreservesPropertiesWhenOmitted(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/data-sources", token, map[string]any{
		"name":       "with-props",
		"type":       "mysql",
		"jdbcUrl":    "jdbc:mysql://localhost/app",
		"username":   "root",
		"properties": map[string]string{"key": "warehouse"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	created := decodeDataSource(t, rec)

	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/data-sources/"+itoa(created.ID), token, map[string]any{
		"name":     "with-props-renamed",
		"type":     "mysql",
		"jdbcUrl":  "jdbc:mysql://localhost/app",
		"username": "root",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", rec.Code, rec.Body.String())
	}
	updated := decodeDataSource(t, rec)
	if updated.Properties["key"] != "warehouse" {
		t.Errorf("properties were wiped: %+v", updated.Properties)
	}

	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/data-sources/"+itoa(created.ID), token, map[string]any{
		"name":       "with-props-renamed",
		"type":       "mysql",
		"jdbcUrl":    "jdbc:mysql://localhost/app",
		"username":   "root",
		"properties": map[string]string{},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", rec.Code, rec.Body.String())
	}
	cleared := decodeDataSource(t, rec)
	if len(cleared.Properties) != 0 {
		t.Errorf("explicit empty properties did not clear: %+v", cleared.Properties)
	}
}

func TestDataSourceRejectsEmbeddedCredentials(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")

	cases := []struct {
		name    string
		dsType  string
		jdbcURL string
	}{
		{"mysql", "mysql", "jdbc:mysql://root:secret@localhost:3306/app"},
		{"postgres", "postgres", "jdbc:postgresql://root:secret@localhost:5432/app"},
		{"mssql", "mssql", "jdbc:sqlserver://root:secret@localhost:1433;DatabaseName=app"},
		{"oracle", "oracle", "jdbc:oracle:thin:@app:secret@localhost:1521/orcl"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/data-sources", token, map[string]any{
				"name":     tc.name + "-embedded",
				"type":     tc.dsType,
				"jdbcUrl":  tc.jdbcURL,
				"username": "root",
			})
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestConnectionTestDoesNotChangeRecords(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")
	dbPath := filepath.Join(t.TempDir(), "probe.db")
	createSQLiteDB(t, dbPath)
	created := createDataSource(t, handler, token, "probe", "sqlite", "jdbc:sqlite:"+dbPath, "", "")

	listDataSources := func() []dataSourceResponse {
		rec := authedJSONRequest(t, handler, http.MethodGet, "/api/v1/data-sources", token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("list status = %d", rec.Code)
		}
		var items []dataSourceResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
			t.Fatalf("list is not JSON: %v", err)
		}
		return items
	}

	before := listDataSources()
	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/data-sources/test", token, map[string]any{
		"id": created.ID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("test status = %d", rec.Code)
	}
	after := listDataSources()

	if len(before) != len(after) {
		t.Fatalf("record count changed: %d -> %d", len(before), len(after))
	}
	if before[0].UpdatedAt != after[0].UpdatedAt {
		t.Errorf("record was modified by the connection test: %s -> %s", before[0].UpdatedAt, after[0].UpdatedAt)
	}
}

func TestConnectionTestWithIDUsesSubmittedOverrides(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")
	existingPath := filepath.Join(t.TempDir(), "existing.db")
	createSQLiteDB(t, existingPath)
	created := createDataSource(t, handler, token, "override", "sqlite", "jdbc:sqlite:"+existingPath, "", "")

	missingPath := filepath.Join(t.TempDir(), "missing.db")
	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/data-sources/test", token, map[string]any{
		"id":      created.ID,
		"type":    "sqlite",
		"jdbcUrl": "jdbc:sqlite:" + missingPath,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var overridden struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &overridden); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if overridden.OK {
		t.Error("expected the submitted (missing) URL to be used")
	}

	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/data-sources/test", token, map[string]any{
		"id": created.ID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("id-only test status = %d", rec.Code)
	}
	var saved struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if !saved.OK {
		t.Error("expected the saved data source connection to succeed")
	}
}

func TestBlankPasswordSurvivesUpdate(t *testing.T) {
	dataStore, err := store.Open(filepath.Join(t.TempDir(), "admin.db"))
	if err != nil {
		t.Fatalf("store.Open() failed: %v", err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })

	_, handler := newTestServerWithStore(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	}, testAuthConfig(t), dataStore)
	token := loginToken(t, handler, "admin", "secret")

	created := createDataSource(t, handler, token, "pwd-db", "sqlite", "jdbc:sqlite:/tmp/pwd.db", "", "original")

	rec := authedJSONRequest(t, handler, http.MethodPut, "/api/v1/data-sources/"+itoa(created.ID), token, map[string]any{
		"name":     "pwd-db",
		"type":     "sqlite",
		"jdbcUrl":  "jdbc:sqlite:/tmp/pwd.db",
		"username": "",
		"password": "",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", rec.Code, rec.Body.String())
	}

	stored, err := dataStore.GetDataSource(created.ID)
	if err != nil {
		t.Fatalf("GetDataSource() failed: %v", err)
	}
	if stored.Password != "original" {
		t.Errorf("stored password = %q, want original", stored.Password)
	}
}

func TestConnectionTestStoresVersionWithoutTouchingUpdatedAt(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")
	dbPath := filepath.Join(t.TempDir(), "version.db")
	createSQLiteDB(t, dbPath)
	created := createDataSource(t, handler, token, "versioned", "sqlite", "jdbc:sqlite:"+dbPath, "", "")

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/data-sources/test", token, map[string]any{
		"id": created.ID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("test status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/data-sources/"+itoa(created.ID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d", rec.Code)
	}
	updated := decodeDataSource(t, rec)
	if updated.Version == "" {
		t.Error("expected version to be stored after a successful connection test")
	}
	if updated.UpdatedAt != created.UpdatedAt {
		t.Errorf("updatedAt changed: %s -> %s", created.UpdatedAt, updated.UpdatedAt)
	}
}

func TestMergeDataSourceKeepsPasswordWhenBlankOrMasked(t *testing.T) {
	existing := store.DataSource{
		Name:     "a",
		Type:     "mysql",
		JDBCUrl:  "jdbc:mysql://localhost/app",
		Username: "root",
		Password: "original",
	}
	base := dataSourceRequest{
		Name:     "a",
		Type:     "mysql",
		JDBCUrl:  "jdbc:mysql://localhost/app",
		Username: "root",
	}

	blank := base
	merged, err := mergeDataSource(existing, blank)
	if err != nil {
		t.Fatalf("mergeDataSource() failed: %v", err)
	}
	if merged.Password != "original" {
		t.Errorf("blank password changed the stored value: %q", merged.Password)
	}

	masked := base
	masked.Password = maskedPassword
	merged, err = mergeDataSource(existing, masked)
	if err != nil {
		t.Fatalf("mergeDataSource() failed: %v", err)
	}
	if merged.Password != "original" {
		t.Errorf("masked password changed the stored value: %q", merged.Password)
	}

	replaced := base
	replaced.Password = "new-secret"
	merged, err = mergeDataSource(existing, replaced)
	if err != nil {
		t.Fatalf("mergeDataSource() failed: %v", err)
	}
	if merged.Password != "new-secret" {
		t.Errorf("password = %q, want new-secret", merged.Password)
	}
}

func TestFileSystemDataSourceCrudAndTest(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")
	root := t.TempDir()

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/data-sources", token, map[string]any{
		"name":    "import-root",
		"type":    "filesystem",
		"jdbcUrl": "",
		"properties": map[string]string{
			"basePath": root,
			"ui.color": "#123456",
		},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	created := decodeDataSource(t, rec)
	if created.Type != "filesystem" {
		t.Errorf("type = %q, want filesystem", created.Type)
	}
	if created.JDBCUrl != "" {
		t.Errorf("jdbcUrl = %q, want empty", created.JDBCUrl)
	}
	if created.Password != "" {
		t.Errorf("password = %q, want empty", created.Password)
	}
	if created.Properties["basePath"] != root {
		t.Errorf("basePath = %q, want %q", created.Properties["basePath"], root)
	}

	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/data-sources/test", token, map[string]any{
		"id":         created.ID,
		"type":       "filesystem",
		"properties": map[string]string{"basePath": root},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("saved test status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var result struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode test result: %v", err)
	}
	if !result.OK {
		t.Errorf("saved filesystem test failed: %s", result.Message)
	}

	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/data-sources/test", token, map[string]any{
		"type":       "filesystem",
		"properties": map[string]string{"basePath": root},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("unsaved test status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/data-sources/test", token, map[string]any{
		"type":       "filesystem",
		"properties": map[string]string{"basePath": "relative/path"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("relative path test status = %d, want 400", rec.Code)
	}

	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/data-sources", token, map[string]any{
		"name":       "missing-root",
		"type":       "filesystem",
		"jdbcUrl":    "",
		"properties": map[string]string{},
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing basePath status = %d, want 400", rec.Code)
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/data-sources/"+itoa(created.ID)+"/schemas", token, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("explorer status = %d, want 400", rec.Code)
	}

	rec = authedJSONRequest(t, handler, http.MethodDelete, "/api/v1/data-sources/"+itoa(created.ID), token, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestDataSourceComposesJDBCURLFromProperties(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/data-sources", token, map[string]any{
		"name":     "composed-mysql",
		"type":     "mysql",
		"username": "root",
		"password": "secret",
		"properties": map[string]string{
			"host":     "db.example.com",
			"port":     "3306",
			"database": "app",
		},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	created := decodeDataSource(t, rec)
	if created.JDBCUrl != "jdbc:mysql://db.example.com:3306/app" {
		t.Errorf("jdbcUrl = %q, want composed MySQL URL", created.JDBCUrl)
	}
}
