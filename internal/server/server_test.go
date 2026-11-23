package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/longkeyy/go-datax/internal/execution"
	"github.com/longkeyy/go-datax/internal/store"
)

func testAuthConfig(t *testing.T) AuthConfig {
	t.Helper()

	hash, err := HashPassword("secret")
	if err != nil {
		t.Fatalf("HashPassword() failed: %v", err)
	}
	return AuthConfig{
		Username:  "admin",
		Password:  hash,
		TokenTTL:  "12h",
		JWTSecret: strings.Repeat("s", minJWTSecretBytes),
	}
}

func newTestServer(t *testing.T, assets map[string]string, auth AuthConfig) (*Server, http.Handler) {
	t.Helper()

	return newTestServerWithStore(t, assets, auth, openTestStore(t))
}

func newTestServerWithStore(t *testing.T, assets map[string]string, auth AuthConfig, dataStore *store.Store) (*Server, http.Handler) {
	t.Helper()

	return newTestServerWithExecutor(t, assets, auth, dataStore, nil)
}

func newTestServerWithExecutor(t *testing.T, assets map[string]string, auth AuthConfig, dataStore *store.Store, executor *execution.Manager) (*Server, http.Handler) {
	t.Helper()

	fsys := fstest.MapFS{}
	for name, content := range assets {
		fsys[name] = &fstest.MapFile{Data: []byte(content)}
	}

	cfg := DefaultConfig("")
	cfg.Auth = auth
	cfg.path = filepath.Join(t.TempDir(), "server.json")

	srv, err := New(cfg, "test-version", fsys, dataStore, executor)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	return srv, srv.Handler()
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()

	dataStore, err := store.Open(filepath.Join(t.TempDir(), "admin.db"))
	if err != nil {
		t.Fatalf("store.Open() failed: %v", err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })
	return dataStore
}

func newTestHandler(t *testing.T, assets map[string]string) http.Handler {
	t.Helper()
	_, handler := newTestServer(t, assets, testAuthConfig(t))
	return handler
}

func postJSON(t *testing.T, handler http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	switch v := body.(type) {
	case string:
		reader = strings.NewReader(v)
	default:
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(data)
	}

	req := httptest.NewRequest(http.MethodPost, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func authorizedRequest(method, path, token string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func loginToken(t *testing.T, handler http.Handler, username, password string) string {
	t.Helper()

	rec := postJSON(t, handler, "/api/v1/auth/login", map[string]string{
		"username": username,
		"password": password,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("login response is not JSON: %v", err)
	}
	if body.Token == "" {
		t.Fatal("login returned an empty token")
	}
	return body.Token
}

func signedExpiredToken(t *testing.T, auth AuthConfig) string {
	t.Helper()

	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   auth.Username,
		Issuer:    tokenIssuer,
		IssuedAt:  jwt.NewNumericDate(now.Add(-2 * time.Hour)),
		ExpiresAt: jwt.NewNumericDate(now.Add(-time.Hour)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := token.SignedString([]byte(auth.JWTSecret))
	if err != nil {
		t.Fatalf("sign expired token: %v", err)
	}
	return signed
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

func TestLoginReturnsTokenAndExpiry(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})

	rec := postJSON(t, handler, "/api/v1/auth/login", map[string]string{
		"username": "admin",
		"password": "secret",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expiresAt"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if body.Token == "" {
		t.Error("token is empty")
	}

	expiresAt, err := time.Parse(time.RFC3339, body.ExpiresAt)
	if err != nil {
		t.Fatalf("expiresAt %q is not RFC3339: %v", body.ExpiresAt, err)
	}
	if time.Until(expiresAt) <= 11*time.Hour {
		t.Errorf("expiresAt is too soon: %v", expiresAt)
	}
}

func TestLoginRejectsInvalidCredentialsWithUniformError(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})

	wrongPassword := postJSON(t, handler, "/api/v1/auth/login", map[string]string{
		"username": "admin",
		"password": "wrong",
	})
	unknownUser := postJSON(t, handler, "/api/v1/auth/login", map[string]string{
		"username": "someone-else",
		"password": "secret",
	})

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"wrong password": wrongPassword,
		"unknown user":   unknownUser,
	} {
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want %d", name, rec.Code, http.StatusUnauthorized)
		}
	}

	if wrongPassword.Body.String() != unknownUser.Body.String() {
		t.Errorf("login errors leak which field was wrong: %q vs %q",
			wrongPassword.Body.String(), unknownUser.Body.String())
	}
}

func TestLoginRequiresUsernameAndPassword(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})

	rec := postJSON(t, handler, "/api/v1/auth/login", map[string]string{"username": "admin"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestMeRequiresValidToken(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})

	for name, token := range map[string]string{
		"missing token":   "",
		"malformed token": "not-a-jwt",
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, authorizedRequest(http.MethodGet, "/api/v1/auth/me", token))

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want %d", name, rec.Code, http.StatusUnauthorized)
		}
	}
}

func TestMeReturnsCurrentUser(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authorizedRequest(http.MethodGet, "/api/v1/auth/me", token))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Username  string `json:"username"`
		ExpiresAt string `json:"expiresAt"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if body.Username != "admin" {
		t.Errorf("username = %q, want admin", body.Username)
	}
	if _, err := time.Parse(time.RFC3339, body.ExpiresAt); err != nil {
		t.Errorf("expiresAt %q is not RFC3339: %v", body.ExpiresAt, err)
	}
}

func TestExpiredTokenIsRejected(t *testing.T) {
	auth := testAuthConfig(t)
	_, handler := newTestServer(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	}, auth)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authorizedRequest(http.MethodGet, "/api/v1/auth/me", signedExpiredToken(t, auth)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestTokenSurvivesServerRestart(t *testing.T) {
	dataDir := t.TempDir()
	configPath := filepath.Join(dataDir, "server.json")
	cfg := DefaultConfig(dataDir)
	generated, err := InitializeAuth(&cfg, configPath)
	if err != nil {
		t.Fatalf("InitializeAuth() failed: %v", err)
	}

	assets := map[string]string{"index.html": "<html><body>app-shell</body></html>"}
	_, first := newTestServer(t, assets, cfg.Auth)
	token := loginToken(t, first, cfg.Auth.Username, generated.Password)

	reloaded, err := LoadOrCreate(configPath, dataDir)
	if err != nil {
		t.Fatalf("LoadOrCreate() failed: %v", err)
	}
	_, restarted := newTestServer(t, assets, reloaded.Auth)

	rec := httptest.NewRecorder()
	restarted.ServeHTTP(rec, authorizedRequest(http.MethodGet, "/api/v1/auth/me", token))

	if rec.Code != http.StatusOK {
		t.Fatalf("status after restart = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestAuthEndpointsRejectWrongMethods(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/login", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET login status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestFrontendRoutesFallBackToIndex(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})

	for _, path := range []string{"/", "/runs", "/sync-jobs/42/edit", "/sync-jobs/v1.2", "/login"} {
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

func TestUnknownAPIRequiresAuthentication(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/does-not-exist", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestUnknownAPIWithTokenReturnsJSON404(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authorizedRequest(http.MethodGet, "/api/v1/does-not-exist", token))

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
	if unchanged.Listen != cfg.Listen || unchanged.DataDir != cfg.DataDir {
		t.Errorf("empty overrides changed config: %+v", unchanged)
	}
}
