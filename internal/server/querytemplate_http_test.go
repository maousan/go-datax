package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func queryTemplateBody(dataSourceID int64) map[string]any {
	return map[string]any{
		"name":         "users-template",
		"dataSourceId": dataSourceID,
		"table":        "users",
		"columns":      []string{"id", "name"},
		"where":        "id > 0",
		"splitPk":      "id",
	}
}

func TestQueryTemplateAndJob(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")
	dir := t.TempDir()
	reader := createSQLiteDataSourceForJob(t, handler, token, "qt-reader", dir)
	writer := createSQLiteDataSourceForJob(t, handler, token, "qt-writer", dir)

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/query-templates", token,
		queryTemplateBody(reader.ID))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create template status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var template queryTemplateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &template); err != nil {
		t.Fatalf("template is not JSON: %v", err)
	}
	if template.Name != "users-template" || template.DataSourceName != "qt-reader" ||
		len(template.Columns) != 2 {
		t.Errorf("template = %+v", template)
	}

	body := formJobBody("template-job", 0, writer.ID)
	formDef := body["form"].(map[string]any)
	formDef["reader"] = map[string]any{"templateId": template.ID}
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create job status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var job syncJobResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatalf("job is not JSON: %v", err)
	}
	if job.Summary != "users → users_copy" {
		t.Errorf("summary = %q, want users → users_copy", job.Summary)
	}
	if job.QueryTemplate == nil || job.QueryTemplate.ID != template.ID ||
		job.QueryTemplate.Name != template.Name {
		t.Errorf("query template summary = %+v", job.QueryTemplate)
	}
	if job.Form == nil || job.Form.Reader.TemplateID == nil || *job.Form.Reader.TemplateID != template.ID {
		t.Errorf("stored reader = %+v", job.Form)
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
	readerParams := content["reader"].(map[string]any)["parameter"].(map[string]any)
	if table := readerParams["connection"].([]any)[0].(map[string]any)["table"].([]any)[0]; table != "users" {
		t.Errorf("reader table = %v, want users", table)
	}
	columns := readerParams["column"].([]any)
	if len(columns) != 2 || columns[0] != "id" || columns[1] != "name" {
		t.Errorf("reader columns = %v", columns)
	}
	if readerParams["where"] != "id > 0" || readerParams["splitPk"] != "id" {
		t.Errorf("reader where/splitPk = %v/%v", readerParams["where"], readerParams["splitPk"])
	}

	// 模板列数变化后，引用它的任务导出应明确失败。
	updated := queryTemplateBody(reader.ID)
	updated["columns"] = []string{"id", "name", "email"}
	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/query-templates/"+itoa(template.ID), token, updated)
	if rec.Code != http.StatusOK {
		t.Fatalf("update template status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/sync-jobs/"+itoa(job.ID)+"/export", token, nil)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "数量必须一致") {
		t.Errorf("export after template change status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// 引用中的模板不能删除。
	rec = authedJSONRequest(t, handler, http.MethodDelete, "/api/v1/query-templates/"+itoa(template.ID), token, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete referenced template status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "template-job") {
		t.Errorf("delete template message = %s", rec.Body.String())
	}

	rec = authedJSONRequest(t, handler, http.MethodDelete, "/api/v1/sync-jobs/"+itoa(job.ID), token, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete job status = %d", rec.Code)
	}

	// 模板引用的数据源也不能删除。
	rec = authedJSONRequest(t, handler, http.MethodDelete, "/api/v1/data-sources/"+itoa(reader.ID), token, nil)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "users-template") {
		t.Errorf("delete template data source status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = authedJSONRequest(t, handler, http.MethodDelete, "/api/v1/query-templates/"+itoa(template.ID), token, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete template status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = authedJSONRequest(t, handler, http.MethodDelete, "/api/v1/data-sources/"+itoa(reader.ID), token, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete data source after template removed status = %d", rec.Code)
	}
}

func TestQueryTemplateValidation(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")
	dir := t.TempDir()
	reader := createSQLiteDataSourceForJob(t, handler, token, "qt-validation", dir)

	emptyColumns := queryTemplateBody(reader.ID)
	emptyColumns["columns"] = []string{}
	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/query-templates", token, emptyColumns)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty columns status = %d, want 400", rec.Code)
	}

	missingSource := queryTemplateBody(9999)
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/query-templates", token, missingSource)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing source status = %d, want 400", rec.Code)
	}

	body := queryTemplateBody(reader.ID)
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/query-templates", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create template status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var template queryTemplateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &template); err != nil {
		t.Fatalf("template is not JSON: %v", err)
	}
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/query-templates", token, body)
	if rec.Code != http.StatusConflict {
		t.Errorf("duplicate template status = %d, want 409", rec.Code)
	}

	writer := createSQLiteDataSourceForJob(t, handler, token, "qt-validation-writer", dir)
	jobBody := formJobBody("template-mismatch", 0, writer.ID)
	jobForm := jobBody["form"].(map[string]any)
	jobForm["reader"] = map[string]any{"templateId": template.ID}
	jobWriter := jobForm["writer"].(map[string]any)
	jobWriter["column"] = []string{"id", "name", "email"}
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs", token, jobBody)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "数量必须一致") {
		t.Errorf("mismatched columns status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestQueryTemplateSQLAndJob(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")
	dir := t.TempDir()
	reader := createSQLiteDataSourceForJob(t, handler, token, "qt-sql-reader", dir)
	writer := createSQLiteDataSourceForJob(t, handler, token, "qt-sql-writer", dir)

	body := map[string]any{
		"name":         "users-sql-template",
		"description":  "带排序的增量读取",
		"dataSourceId": reader.ID,
		"sql":          "SELECT id, name FROM users WHERE id > 0 ORDER BY id",
		"columns":      []string{"id", "name"},
	}
	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/query-templates", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create sql template status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var template queryTemplateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &template); err != nil {
		t.Fatalf("template is not JSON: %v", err)
	}
	if template.SQL == "" || template.Description != "带排序的增量读取" {
		t.Errorf("template sql/description = %q/%q", template.SQL, template.Description)
	}
	if template.Table != "" || template.Where != "" || template.SplitPk != "" {
		t.Errorf("sql template should not keep table/where/splitPk: %+v", template)
	}

	jobBody := formJobBody("sql-template-job", 0, writer.ID)
	jobForm := jobBody["form"].(map[string]any)
	jobForm["reader"] = map[string]any{"templateId": template.ID}
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs", token, jobBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create job status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var job syncJobResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatalf("job is not JSON: %v", err)
	}
	if job.Summary != "SQL 数据集 → users_copy" {
		t.Errorf("summary = %q", job.Summary)
	}
	if job.QueryTemplate == nil || job.QueryTemplate.SQL == "" {
		t.Errorf("query template summary = %+v", job.QueryTemplate)
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
	readerParams := content["reader"].(map[string]any)["parameter"].(map[string]any)
	wantQuery := "SELECT \"id\", \"name\" FROM (SELECT id, name FROM users WHERE id > 0\n) dx_template_src"
	if readerParams["querySql"] != wantQuery {
		t.Errorf("querySql = %v, want %s", readerParams["querySql"], wantQuery)
	}
	conn := readerParams["connection"].([]any)[0].(map[string]any)
	if _, ok := conn["table"]; ok {
		t.Errorf("querySql mode should not set connection.table: %v", conn)
	}
	for _, key := range []string{"column", "where", "splitPk"} {
		if _, ok := readerParams[key]; ok {
			t.Errorf("querySql mode should not set %s: %v", key, readerParams)
		}
	}

	invalid := map[string]any{
		"name":         "bad-sql",
		"dataSourceId": reader.ID,
		"sql":          "SELECT id FROM users; DROP TABLE users",
		"columns":      []string{"id"},
	}
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/query-templates", token, invalid)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "一条 SQL") {
		t.Errorf("multi statement status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
