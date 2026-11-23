package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/longkeyy/go-datax/internal/execution"
	"github.com/longkeyy/go-datax/internal/notify"
)

func TestNotificationSettingsAPI(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")

	rec := authedJSONRequest(t, handler, http.MethodGet, "/api/v1/settings/notifications", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get settings status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var current notify.Config
	if err := json.Unmarshal(rec.Body.Bytes(), &current); err != nil {
		t.Fatalf("settings is not JSON: %v", err)
	}
	if current.Enabled || current.Type != notify.TypeGeneric || current.TimeoutSeconds != 10 {
		t.Errorf("default settings = %+v", current)
	}

	invalid := map[string]any{
		"enabled":        true,
		"type":           notify.TypeGeneric,
		"url":            "ftp://example.com",
		"events":         []string{notify.EventFailed},
		"timeoutSeconds": 10,
	}
	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/settings/notifications", token, invalid)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid settings status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}

	webhookBodies := make(chan string, 4)
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		webhookBodies <- string(body)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer webhook.Close()

	valid := map[string]any{
		"enabled":        true,
		"type":           notify.TypeGeneric,
		"url":            webhook.URL,
		"events":         []string{notify.EventFailed, notify.EventRecovered},
		"timeoutSeconds": 5,
	}
	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/settings/notifications", token, valid)
	if rec.Code != http.StatusOK {
		t.Fatalf("update settings status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var saved notify.Config
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatalf("updated settings is not JSON: %v", err)
	}
	if !saved.Enabled || saved.URL != webhook.URL || saved.TimeoutSeconds != 5 {
		t.Errorf("saved settings = %+v", saved)
	}
	if len(saved.Events) != 2 || saved.Events[0] != notify.EventFailed || saved.Events[1] != notify.EventRecovered {
		t.Errorf("saved events = %v", saved.Events)
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/settings/notifications", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("reload settings status = %d", rec.Code)
	}
	var reloaded notify.Config
	if err := json.Unmarshal(rec.Body.Bytes(), &reloaded); err != nil {
		t.Fatalf("reloaded settings is not JSON: %v", err)
	}
	if reloaded.URL != webhook.URL || !reloaded.Enabled {
		t.Errorf("reloaded settings = %+v", reloaded)
	}

	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/settings/notifications/test", token, valid)
	if rec.Code != http.StatusOK {
		t.Fatalf("test notification status = %d, body = %s", rec.Code, rec.Body.String())
	}
	select {
	case body := <-webhookBodies:
		if !strings.Contains(body, `"event":"test"`) {
			t.Errorf("test webhook body = %s", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("test webhook did not receive a request")
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer failing.Close()
	failingConfig := map[string]any{
		"enabled":        true,
		"type":           notify.TypeGeneric,
		"url":            failing.URL,
		"events":         []string{notify.EventFailed},
		"timeoutSeconds": 5,
	}
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/settings/notifications/test", token, failingConfig)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("failing test status = %d, want 502 (body=%s)", rec.Code, rec.Body.String())
	}
}

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
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password status = %d, want 401", rec.Code)
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
