package server

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"testing"

	"github.com/longkeyy/go-datax/internal/execution"
)

func TestAccountSettings(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")

	rec := authedJSONRequest(t, handler, http.MethodGet, "/api/v1/settings/account", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get account status = %d", rec.Code)
	}
	var account struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &account); err != nil {
		t.Fatalf("account is not JSON: %v", err)
	}
	if account.Username != "admin" {
		t.Errorf("username = %q, want admin", account.Username)
	}

	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/settings/account", token, map[string]string{
		"username":        "admin",
		"currentPassword": "wrong",
		"newPassword":     "newsecret123",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong password status = %d, want 400", rec.Code)
	}

	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/settings/account", token, map[string]string{
		"username":        "admin",
		"currentPassword": "secret",
		"newPassword":     "short",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("weak password status = %d, want 400", rec.Code)
	}

	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/settings/account", token, map[string]string{
		"username":        "admin",
		"currentPassword": "secret",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no changes status = %d, want 400", rec.Code)
	}

	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/settings/account", token, map[string]string{
		"username":        "operator",
		"currentPassword": "secret",
		"newPassword":     "newsecret123",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update account status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// 旧令牌因为 JWT 密钥轮换而失效。
	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/auth/me", token, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("old token status = %d, want 401", rec.Code)
	}

	rec = postJSON(t, handler, "/api/v1/auth/login", map[string]string{
		"username": "admin",
		"password": "secret",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("old credentials status = %d, want 401", rec.Code)
	}

	rec = postJSON(t, handler, "/api/v1/auth/login", map[string]string{
		"username": "operator",
		"password": "newsecret123",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("new credentials status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var login struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &login); err != nil {
		t.Fatalf("login is not JSON: %v", err)
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/settings/account", login.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get updated account status = %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &account); err != nil {
		t.Fatalf("updated account is not JSON: %v", err)
	}
	if account.Username != "operator" {
		t.Errorf("updated username = %q, want operator", account.Username)
	}
}

func TestRuntimeSettingsAPI(t *testing.T) {
	dataStore := openTestStore(t)
	srv, handler := newRunTestServer(t, dataStore, func(string) *exec.Cmd { return successCommand() }, 2)
	token := loginToken(t, handler, "admin", "secret")

	rec := authedJSONRequest(t, handler, http.MethodGet, "/api/v1/settings/runtime", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get runtime status = %d", rec.Code)
	}
	var current execution.RuntimeSettings
	if err := json.Unmarshal(rec.Body.Bytes(), &current); err != nil {
		t.Fatalf("runtime is not JSON: %v", err)
	}
	if current.MaxConcurrent != 2 || current.MaxRuns != 100 {
		t.Errorf("initial runtime = %+v", current)
	}

	invalid := execution.RuntimeSettings{MaxConcurrent: 0, MaxRuns: 100, MaxAgeDays: 30, MaxLogBytes: 1 << 20}
	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/settings/runtime", token, invalid)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid runtime status = %d, want 400", rec.Code)
	}

	updated := execution.RuntimeSettings{
		MaxConcurrent: 4,
		MaxRuns:       250,
		MaxAgeDays:    14,
		MaxLogBytes:   5 * 1024 * 1024,
	}
	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/settings/runtime", token, updated)
	if rec.Code != http.StatusOK {
		t.Fatalf("update runtime status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := srv.executor.Runtime(); got != updated {
		t.Errorf("manager runtime = %+v, want %+v", got, updated)
	}

	// 新服务启动时应该应用已保存的运行参数，而不是配置默认值。
	restarted, restartedHandler := newRunTestServer(t, dataStore, func(string) *exec.Cmd { return successCommand() }, 1)
	restartedToken := loginToken(t, restartedHandler, "admin", "secret")
	rec = authedJSONRequest(t, restartedHandler, http.MethodGet, "/api/v1/settings/runtime", restartedToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("restarted get runtime status = %d", rec.Code)
	}
	var reloaded execution.RuntimeSettings
	if err := json.Unmarshal(rec.Body.Bytes(), &reloaded); err != nil {
		t.Fatalf("reloaded runtime is not JSON: %v", err)
	}
	if reloaded != updated {
		t.Errorf("reloaded runtime = %+v, want %+v", reloaded, updated)
	}
	if got := restarted.executor.Runtime(); got != updated {
		t.Errorf("restarted manager runtime = %+v, want %+v", got, updated)
	}
}
