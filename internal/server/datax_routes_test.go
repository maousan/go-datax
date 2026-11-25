package server

import (
	"net/http"
	"testing"
)

func TestDataXAPIPathsAreNamespaced(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")

	oldPaths := []string{
		"/api/v1/sync-jobs",
		"/api/v1/query-templates",
		"/api/v1/collect-tasks",
		"/api/v1/runs",
	}
	for _, path := range oldPaths {
		rec := authedJSONRequest(t, handler, http.MethodGet, path, token, nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("old path %s returned %d, want 404", path, rec.Code)
		}
	}

	newPaths := []string{
		"/api/v1/datax/sync-jobs",
		"/api/v1/datax/datasets",
		"/api/v1/datax/collect-tasks",
		"/api/v1/datax/runs",
	}
	for _, path := range newPaths {
		rec := authedJSONRequest(t, handler, http.MethodGet, path, token, nil)
		if rec.Code != http.StatusOK {
			t.Errorf("new path %s returned %d, want 200: %s", path, rec.Code, rec.Body.String())
		}
	}
}
