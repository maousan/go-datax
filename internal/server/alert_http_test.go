package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/longkeyy/go-datax/internal/notify"
	"github.com/longkeyy/go-datax/internal/store"
)

type alertConfigTestResponse struct {
	Enabled        bool `json:"enabled"`
	TimeoutSeconds int  `json:"timeoutSeconds"`
	RetentionDays  int  `json:"retentionDays"`
	Channels       []struct {
		ID           string   `json:"id"`
		Name         string   `json:"name"`
		Type         string   `json:"type"`
		Enabled      bool     `json:"enabled"`
		Events       []string `json:"events"`
		URL          string   `json:"url"`
		Secret       string   `json:"secret"`
		SMTPHost     string   `json:"smtpHost"`
		SMTPUsername string   `json:"smtpUsername"`
		SMTPPassword string   `json:"smtpPassword"`
		From         string   `json:"from"`
		To           []string `json:"to"`
		UseTLS       bool     `json:"useTLS"`
	} `json:"channels"`
}

func TestAlertConfigAPI(t *testing.T) {
	dataStore := openTestStore(t)
	_, handler := newTestServerWithStore(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	}, testAuthConfig(t), dataStore)
	token := loginToken(t, handler, "admin", "secret")

	rec := authedJSONRequest(t, handler, http.MethodGet, "/api/v1/alerts/config", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get config status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var current alertConfigTestResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &current); err != nil {
		t.Fatalf("config is not JSON: %v", err)
	}
	if current.Enabled || current.TimeoutSeconds != 10 || current.RetentionDays != 90 {
		t.Errorf("default config = %+v", current)
	}
	if current.Channels == nil || len(current.Channels) != 0 {
		t.Errorf("default channels = %+v, want empty", current.Channels)
	}

	invalid := map[string]any{
		"enabled":        true,
		"timeoutSeconds": 61,
		"retentionDays":  90,
		"channels":       []any{},
	}
	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/alerts/config", token, invalid)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid config status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
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
		"timeoutSeconds": 5,
		"retentionDays":  30,
		"channels": []map[string]any{
			{
				"name":    "ops-webhook",
				"type":    "webhook",
				"enabled": true,
				"events":  []string{notify.EventFailed, notify.EventRecovered},
				"url":     webhook.URL,
			},
			{
				"name":         "ops-mail",
				"type":         "email",
				"enabled":      false,
				"events":       []string{notify.EventFailed},
				"smtpHost":     "smtp.example.com",
				"smtpPort":     465,
				"smtpUsername": "alert@example.com",
				"smtpPassword": "smtp-secret",
				"from":         "alert@example.com",
				"to":           []string{"ops@example.com"},
				"useTLS":       true,
			},
		},
	}
	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/alerts/config", token, valid)
	if rec.Code != http.StatusOK {
		t.Fatalf("update config status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var saved alertConfigTestResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatalf("updated config is not JSON: %v", err)
	}
	if !saved.Enabled || saved.TimeoutSeconds != 5 || saved.RetentionDays != 30 {
		t.Errorf("saved config = %+v", saved)
	}
	if len(saved.Channels) != 2 {
		t.Fatalf("saved channels = %+v", saved.Channels)
	}
	for _, channel := range saved.Channels {
		if channel.ID == "" {
			t.Errorf("channel %q has no ID", channel.Name)
		}
		if channel.Type == "email" && channel.SMTPPassword != "******" {
			t.Errorf("smtp password = %q, want masked", channel.SMTPPassword)
		}
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/alerts/config", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("reload config status = %d", rec.Code)
	}
	var reloaded alertConfigTestResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &reloaded); err != nil {
		t.Fatalf("reloaded config is not JSON: %v", err)
	}
	if reloaded.TimeoutSeconds != 5 || len(reloaded.Channels) != 2 {
		t.Errorf("reloaded config = %+v", reloaded)
	}

	testChannel := map[string]any{
		"channel": map[string]any{
			"name":    "test-webhook",
			"type":    "webhook",
			"enabled": true,
			"events":  []string{notify.EventFailed},
			"url":     webhook.URL,
		},
		"timeoutSeconds": 5,
	}
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/alerts/config/test", token, testChannel)
	if rec.Code != http.StatusOK {
		t.Fatalf("test channel status = %d, body = %s", rec.Code, rec.Body.String())
	}
	select {
	case body := <-webhookBodies:
		if !strings.Contains(body, `"event":"test"`) {
			t.Errorf("test webhook body = %s", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("test webhook did not receive a request")
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/alerts/logs", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list logs status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var logs struct {
		Items []struct {
			Event       string `json:"event"`
			Status      string `json:"status"`
			ChannelName string `json:"channelName"`
			ChannelType string `json:"channelType"`
			Error       string `json:"error"`
		} `json:"items"`
		Total    int64 `json:"total"`
		Page     int   `json:"page"`
		PageSize int   `json:"pageSize"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &logs); err != nil {
		t.Fatalf("logs are not JSON: %v", err)
	}
	if logs.Total != 1 || len(logs.Items) != 1 {
		t.Fatalf("logs = %+v", logs)
	}
	if logs.Items[0].Event != notify.EventTest || logs.Items[0].Status != "success" {
		t.Errorf("log item = %+v", logs.Items[0])
	}
	if logs.Page != 1 || logs.PageSize != 20 {
		t.Errorf("pagination = %d/%d", logs.Page, logs.PageSize)
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/alerts/logs?status=failed&page=1&pageSize=5", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("filtered logs status = %d", rec.Code)
	}
	logs.Total = -1
	if err := json.Unmarshal(rec.Body.Bytes(), &logs); err != nil {
		t.Fatalf("filtered logs are not JSON: %v", err)
	}
	if logs.Total != 0 || len(logs.Items) != 0 {
		t.Errorf("failed-only logs = %+v", logs)
	}

	rec = authedJSONRequest(t, handler, http.MethodDelete, "/api/v1/alerts/logs", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear logs status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var cleared struct {
		Deleted int64 `json:"deleted"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cleared); err != nil {
		t.Fatalf("clear response is not JSON: %v", err)
	}
	if cleared.Deleted != 1 {
		t.Errorf("deleted = %d, want 1", cleared.Deleted)
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/alerts/logs", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list cleared logs status = %d", rec.Code)
	}
	logs.Total = -1
	if err := json.Unmarshal(rec.Body.Bytes(), &logs); err != nil {
		t.Fatalf("cleared logs are not JSON: %v", err)
	}
	if logs.Total != 0 {
		t.Errorf("logs after clear = %+v", logs)
	}
}

func TestAlertTestFailureStatus(t *testing.T) {
	handler := newTestHandler(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	})
	token := loginToken(t, handler, "admin", "secret")

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer failing.Close()

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/alerts/config/test", token, map[string]any{
		"channel": map[string]any{
			"name":    "failing",
			"type":    "webhook",
			"enabled": true,
			"events":  []string{notify.EventFailed},
			"url":     failing.URL,
		},
		"timeoutSeconds": 5,
	})
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("test status = %d, want 502 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestAlertConfigMigratesLegacyNotification(t *testing.T) {
	dataStore := openTestStore(t)
	legacy := map[string]any{
		"enabled":        true,
		"type":           notify.TypeFeishu,
		"url":            "https://example.com/feishu",
		"events":         []string{notify.EventFailed},
		"timeoutSeconds": 5,
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy setting: %v", err)
	}
	if err := dataStore.SaveSetting("notifications", string(data)); err != nil {
		t.Fatalf("SaveSetting() failed: %v", err)
	}

	_, handler := newTestServerWithStore(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	}, testAuthConfig(t), dataStore)
	token := loginToken(t, handler, "admin", "secret")

	rec := authedJSONRequest(t, handler, http.MethodGet, "/api/v1/alerts/config", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get config status = %d", rec.Code)
	}
	var cfg alertConfigTestResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("config is not JSON: %v", err)
	}
	if !cfg.Enabled || len(cfg.Channels) != 1 {
		t.Fatalf("migrated config = %+v", cfg)
	}
	if cfg.Channels[0].Type != "feishu" || cfg.Channels[0].URL != "https://example.com/feishu" {
		t.Errorf("migrated channel = %+v", cfg.Channels[0])
	}
}

func TestAlertLogsFilterBySource(t *testing.T) {
	dataStore := openTestStore(t)
	_, handler := newTestServerWithStore(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	}, testAuthConfig(t), dataStore)
	token := loginToken(t, handler, "admin", "secret")
	pipelineID := int64(11)
	pipelineRunID := int64(22)
	entries := []store.AlertLog{
		{SourceKind: store.AlertSourceDataX, Event: "failed", Status: store.AlertStatusSuccess, SyncJobName: "datax-job"},
		{SourceKind: "", Event: "failed", Status: store.AlertStatusSuccess, SyncJobName: "legacy-job"},
		{
			SourceKind: store.AlertSourcePipeline, Event: "succeeded", Status: store.AlertStatusSuccess,
			PipelineID: &pipelineID, PipelineName: "orders-etl", PipelineRunID: &pipelineRunID, VersionNumber: 4,
		},
	}
	for i := range entries {
		if err := dataStore.CreateAlertLog(&entries[i]); err != nil {
			t.Fatal(err)
		}
	}

	rec := authedJSONRequest(t, handler, http.MethodGet, "/api/v1/alerts/logs?sourceKind=pipeline", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("pipeline logs status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Items []alertLogResponse `json:"items"`
		Total int64              `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Total != 1 || len(payload.Items) != 1 {
		t.Fatalf("pipeline logs = %+v", payload)
	}
	item := payload.Items[0]
	if item.SourceKind != store.AlertSourcePipeline || item.PipelineID == nil || *item.PipelineID != pipelineID ||
		item.PipelineRunID == nil || *item.PipelineRunID != pipelineRunID ||
		item.PipelineName != "orders-etl" || item.VersionNumber != 4 {
		t.Fatalf("pipeline log item = %+v", item)
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/alerts/logs?sourceKind=datax", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("datax logs status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Total != 2 || len(payload.Items) != 2 {
		t.Fatalf("datax logs including legacy = %+v", payload)
	}
	for _, item := range payload.Items {
		if item.SourceKind != store.AlertSourceDataX {
			t.Fatalf("legacy source kind = %q", item.SourceKind)
		}
	}
}
