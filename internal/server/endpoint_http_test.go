package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longkeyy/go-datax/internal/connector"
)

func createFileSystemDataSource(t *testing.T, handler http.Handler, token, name, root string) dataSourceResponse {
	t.Helper()
	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/data-sources", token, map[string]any{
		"name": name,
		"type": "filesystem",
		"properties": map[string]string{
			"basePath": root,
		},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create filesystem data source status = %d, body = %s", rec.Code, rec.Body.String())
	}
	return decodeDataSource(t, rec)
}

func endpointBody(name, connectorName string, dataSourceID int64, schema []map[string]string) map[string]any {
	return map[string]any{
		"name":         name,
		"connector":    connectorName,
		"dataSourceId": dataSourceID,
		"schema":       schema,
	}
}

func createEndpoint(t *testing.T, handler http.Handler, token, collection string, body map[string]any) endpointResponse {
	t.Helper()
	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/"+collection, token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %s status = %d, body = %s", collection, rec.Code, rec.Body.String())
	}
	var endpoint endpointResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &endpoint); err != nil {
		t.Fatalf("decode endpoint: %v (body=%s)", err, rec.Body.String())
	}
	return endpoint
}

func TestSourceCrudDiscoveryAndReferences(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")
	root := t.TempDir()
	ds := createFileSystemDataSource(t, handler, token, "import-root", root)
	if err := os.WriteFile(filepath.Join(root, "orders.csv"), []byte("id,name\n1,alice\n2,bob\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	body := endpointBody("orders-csv", "csv", ds.ID, []map[string]string{
		{"name": " id ", "type": "int64"},
		{"name": " name ", "type": "string"},
	})
	body["description"] = "订单导出"
	body["path"] = "orders.csv"
	body["options"] = map[string]string{"header": "true"}
	source := createEndpoint(t, handler, token, "sources", body)
	if source.Kind != "source" || source.DataSourceName != "import-root" ||
		source.Path != "orders.csv" || len(source.Schema) != 2 ||
		source.Schema[0].Name != "id" || source.Schema[1].Name != "name" {
		t.Fatalf("source = %+v", source)
	}

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sources", token, body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate source status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/sources?query=orders", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var list []endpointResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != source.ID {
		t.Fatalf("list = %+v", list)
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/sources/"+itoa(source.ID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", rec.Code, rec.Body.String())
	}

	for _, collection := range []string{"sources", "sinks"} {
		rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/"+collection+"/discover", token, map[string]any{
			"connector":    "csv",
			"dataSourceId": ds.ID,
			"path":         "orders.csv",
			"options":      map[string]string{"header": "true"},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s discovery status = %d, body = %s", collection, rec.Code, rec.Body.String())
		}
		var discovered struct {
			Schema connector.Schema `json:"schema"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &discovered); err != nil {
			t.Fatal(err)
		}
		expected := connector.Schema{
			{Name: "id", Type: connector.TypeInt64},
			{Name: "name", Type: connector.TypeString},
		}
		if len(discovered.Schema) != 2 || discovered.Schema[0] != expected[0] ||
			discovered.Schema[1] != expected[1] {
			t.Fatalf("%s discovered = %+v", collection, discovered.Schema)
		}
	}

	updated := endpointBody("orders-csv-v2", "csv", ds.ID, []map[string]string{
		{"name": "id", "type": "int64"},
		{"name": "name", "type": "string"},
		{"name": "score", "type": "float64"},
	})
	updated["path"] = "orders.csv"
	updated["options"] = map[string]string{"header": "true", "nullFormat": "NULL"}
	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/sources/"+itoa(source.ID), token, updated)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var updatedSource endpointResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &updatedSource); err != nil {
		t.Fatal(err)
	}
	if updatedSource.Name != "orders-csv-v2" || len(updatedSource.Schema) != 3 {
		t.Fatalf("updated source = %+v", updatedSource)
	}

	rec = authedJSONRequest(t, handler, http.MethodDelete, "/api/v1/data-sources/"+itoa(ds.ID), token, nil)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "ETL 区") ||
		!strings.Contains(rec.Body.String(), "orders-csv-v2") {
		t.Fatalf("delete referenced data source status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/data-sources/"+itoa(ds.ID), token, map[string]any{
		"name":    "import-root",
		"type":    "sqlite",
		"jdbcUrl": "jdbc:sqlite:" + filepath.Join(root, "switch.db"),
	})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "不能修改类型") {
		t.Fatalf("type change with references status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = authedJSONRequest(t, handler, http.MethodDelete, "/api/v1/sources/"+itoa(source.ID), token, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete source status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = authedJSONRequest(t, handler, http.MethodDelete, "/api/v1/data-sources/"+itoa(ds.ID), token, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete data source status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestSQLiteSourceSinkAndValidation(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")
	dir := t.TempDir()
	ds := createSQLiteDataSourceForJob(t, handler, token, "etl-sqlite", dir)
	dbPath := filepath.Join(dir, "etl-sqlite.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE endpoint_source (id INTEGER, name TEXT)`,
		`INSERT INTO endpoint_source VALUES (1, 'alice'), (2, 'bob')`,
		`CREATE TABLE endpoint_target (id INTEGER, name TEXT)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()

	schema := []map[string]string{
		{"name": "id", "type": "int64"},
		{"name": "name", "type": "string"},
	}
	sourceBody := endpointBody("endpoint", "sqlite", ds.ID, schema)
	sourceBody["table"] = "endpoint_source"
	sourceBody["options"] = map[string]string{"splitPk": "id"}
	source := createEndpoint(t, handler, token, "sources", sourceBody)
	if source.Table != "endpoint_source" {
		t.Fatalf("source = %+v", source)
	}

	sinkBody := endpointBody("endpoint", "sqlite", ds.ID, schema)
	sinkBody["table"] = "endpoint_target"
	sinkBody["options"] = map[string]string{"writeMode": "truncate"}
	sink := createEndpoint(t, handler, token, "sinks", sinkBody)
	if sink.Kind != "sink" || sink.Table != "endpoint_target" {
		t.Fatalf("sink = %+v", sink)
	}

	for _, collection := range []string{"sources", "sinks"} {
		rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/"+collection+"/discover", token, map[string]any{
			"connector":    "sqlite",
			"dataSourceId": ds.ID,
			"table":        "endpoint_source",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s discovery status = %d, body = %s", collection, rec.Code, rec.Body.String())
		}
		var discovered struct {
			Schema connector.Schema `json:"schema"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &discovered); err != nil {
			t.Fatal(err)
		}
		if len(discovered.Schema) != 2 || discovered.Schema[0].Type != connector.TypeInt64 ||
			discovered.Schema[1].Type != connector.TypeString {
			t.Fatalf("%s discovered = %+v", collection, discovered.Schema)
		}
	}

	rec := authedJSONRequest(t, handler, http.MethodGet, "/api/v1/sources", token, nil)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"kind":"sink"`) {
		t.Fatalf("sources list = %d %s", rec.Code, rec.Body.String())
	}
	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/sinks", token, nil)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"kind":"source"`) {
		t.Fatalf("sinks list = %d %s", rec.Code, rec.Body.String())
	}

	// 引用检查应同时列出源和目标。
	rec = authedJSONRequest(t, handler, http.MethodDelete, "/api/v1/data-sources/"+itoa(ds.ID), token, nil)
	if rec.Code != http.StatusConflict ||
		!strings.Contains(rec.Body.String(), "源：endpoint") ||
		!strings.Contains(rec.Body.String(), "目标：endpoint") {
		t.Fatalf("referenced data source status = %d, body = %s", rec.Code, rec.Body.String())
	}

	invalidBodies := map[string]struct {
		collection string
		body       map[string]any
	}{
		"sink with query": {
			collection: "sinks",
			body: func() map[string]any {
				body := endpointBody("bad-sink", "sqlite", ds.ID, schema)
				body["query"] = "SELECT id, name FROM endpoint_source"
				return body
			}(),
		},
		"source with path": {
			collection: "sources",
			body: func() map[string]any {
				body := endpointBody("bad-source", "sqlite", ds.ID, schema)
				body["path"] = "file.csv"
				return body
			}(),
		},
		"source with table and query": {
			collection: "sources",
			body: func() map[string]any {
				body := endpointBody("bad-source", "sqlite", ds.ID, schema)
				body["table"] = "endpoint_source"
				body["query"] = "SELECT id, name FROM endpoint_source"
				return body
			}(),
		},
		"sink with splitPk": {
			collection: "sinks",
			body: func() map[string]any {
				body := endpointBody("bad-sink", "sqlite", ds.ID, schema)
				body["table"] = "endpoint_target"
				body["options"] = map[string]string{"splitPk": "id"}
				return body
			}(),
		},
		"source with writeMode": {
			collection: "sources",
			body: func() map[string]any {
				body := endpointBody("bad-source", "sqlite", ds.ID, schema)
				body["table"] = "endpoint_source"
				body["options"] = map[string]string{"writeMode": "truncate"}
				return body
			}(),
		},
		"missing schema": {
			collection: "sources",
			body: func() map[string]any {
				body := endpointBody("bad-source", "sqlite", ds.ID, nil)
				body["table"] = "endpoint_source"
				return body
			}(),
		},
		"unknown option": {
			collection: "sources",
			body: func() map[string]any {
				body := endpointBody("bad-source", "sqlite", ds.ID, schema)
				body["table"] = "endpoint_source"
				body["options"] = map[string]string{"typo": "true"}
				return body
			}(),
		},
	}
	for name, tc := range invalidBodies {
		t.Run(name, func(t *testing.T) {
			rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/"+tc.collection, token, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
			}
		})
	}

	// 同名 source/sink 各自唯一，但可以跨类型共用。
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sources", token, sourceBody)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate source status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = authedJSONRequest(t, handler, http.MethodDelete, "/api/v1/sources/"+itoa(source.ID), token, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete source status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = authedJSONRequest(t, handler, http.MethodDelete, "/api/v1/sinks/"+itoa(sink.ID), token, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete sink status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = authedJSONRequest(t, handler, http.MethodDelete, "/api/v1/data-sources/"+itoa(ds.ID), token, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete data source status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
