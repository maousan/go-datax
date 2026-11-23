package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func newTestHandler(t *testing.T, assets map[string]string) http.Handler {
	t.Helper()

	fsys := fstest.MapFS{}
	for name, content := range assets {
		fsys[name] = &fstest.MapFile{Data: []byte(content)}
	}

	srv, err := New("test-version", fsys)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	return srv.Handler()
}

func TestHealthReportsStatusAndVersion(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v (body=%q)", err, rec.Body.String())
	}
	if body.Status != "ok" {
		t.Errorf("status field = %q, want \"ok\"", body.Status)
	}
	if body.Version != "test-version" {
		t.Errorf("version field = %q, want \"test-version\"", body.Version)
	}
}

func TestHealthRejectsNonGET(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/health", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if allow := rec.Header().Get("Allow"); allow != "GET" {
		t.Errorf("Allow = %q, want GET", allow)
	}
}

func TestFrontendRoutesFallBackToIndex(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})

	for _, path := range []string{"/", "/runs", "/sync-jobs/42/edit", "/sync-jobs/v1.2"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if rec.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want %d", path, rec.Code, http.StatusOK)
			continue
		}
		if !strings.Contains(rec.Body.String(), "app-shell") {
			t.Errorf("GET %s did not serve the app shell: %q", path, rec.Body.String())
		}
	}
}

func TestUnknownAPIReturnsJSON404(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/does-not-exist", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("content-type = %q, want JSON", ct)
	}

	var body struct {
		Error apiError `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v (body=%q)", err, rec.Body.String())
	}
	if body.Error.Code != "not_found" {
		t.Errorf("error.code = %q, want \"not_found\"", body.Error.Code)
	}
}

func TestMissingStaticAssetReturns404(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html":     "<html><body>app-shell</body></html>",
		"assets/app.js":  "console.log('ok')",
		"assets/app.css": "body{}",
	})

	for _, path := range []string{"/assets/missing.js", "/missing.js", "/missing.ico"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want %d", path, rec.Code, http.StatusNotFound)
		}
		if strings.Contains(rec.Body.String(), "app-shell") {
			t.Errorf("GET %s should not fall back to the app shell", path)
		}
	}
}

func TestIndexIsNotCached(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if cache := rec.Header().Get("Cache-Control"); cache != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cache)
	}
}

func TestMissingUIReturnsServiceUnavailable(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"assets/app.js": "console.log('ok')",
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/runs", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(rec.Body.String(), "前端尚未构建") {
		t.Errorf("expected build instructions, got %q", rec.Body.String())
	}
}

func TestLoadOrCreateWritesDefaultConfig(t *testing.T) {
	dataDir := t.TempDir()
	configPath := filepath.Join(dataDir, "nested", "server.json")

	cfg, err := LoadOrCreate(configPath, dataDir)
	if err != nil {
		t.Fatalf("LoadOrCreate() failed: %v", err)
	}

	if cfg.Listen != DefaultListen {
		t.Errorf("listen = %q, want %q", cfg.Listen, DefaultListen)
	}
	if cfg.DataDir != dataDir {
		t.Errorf("dataDir = %q, want %q", cfg.DataDir, dataDir)
	}

	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config file was not created: %v", err)
	}
}

func TestEnsureDataDirsCreatesDirectories(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())

	if err := EnsureDataDirs(cfg); err != nil {
		t.Fatalf("EnsureDataDirs() failed: %v", err)
	}

	for _, dir := range []string{cfg.DataDir, cfg.LogsDir(), cfg.SnapshotsDir()} {
		if info, err := os.Stat(dir); err != nil {
			t.Errorf("directory %s was not created: %v", dir, err)
		} else if !info.IsDir() {
			t.Errorf("%s is not a directory", dir)
		}
	}
}

func TestLoadOrCreateReadsExistingConfig(t *testing.T) {
	dataDir := t.TempDir()
	configPath := filepath.Join(dataDir, "server.json")
	content := `{"listen":"127.0.0.1:9090","dataDir":"` + filepath.ToSlash(dataDir) + `","retention":{"maxRuns":7}}`
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	cfg, err := LoadOrCreate(configPath, dataDir)
	if err != nil {
		t.Fatalf("LoadOrCreate() failed: %v", err)
	}
	if cfg.Listen != "127.0.0.1:9090" {
		t.Errorf("listen = %q, want 127.0.0.1:9090", cfg.Listen)
	}
	if cfg.Retention.MaxRuns != 7 {
		t.Errorf("maxRuns = %d, want 7", cfg.Retention.MaxRuns)
	}
}

func TestWithOverridesPrefersCommandLineValues(t *testing.T) {
	cfg := DefaultConfig("D:/data/original")

	overridden := cfg.WithOverrides("127.0.0.1:9091", "D:/data/override")
	if overridden.Listen != "127.0.0.1:9091" {
		t.Errorf("listen = %q, want 127.0.0.1:9091", overridden.Listen)
	}
	if overridden.DataDir != "D:/data/override" {
		t.Errorf("dataDir = %q, want D:/data/override", overridden.DataDir)
	}

	unchanged := cfg.WithOverrides("", "")
	if unchanged != cfg {
		t.Errorf("empty overrides changed config: %+v", unchanged)
	}
}
