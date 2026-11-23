package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/longkeyy/go-datax/internal/store"
)

func sampleRun(status string) store.JobRun {
	jobID := int64(7)
	started := time.Date(2026, 11, 24, 10, 0, 0, 0, time.Local)
	finished := started.Add(90 * time.Second)
	exitCode := 3
	return store.JobRun{
		ID:           42,
		SyncJobID:    &jobID,
		SyncJobName:  "nightly-sync",
		Status:       status,
		Trigger:      store.RunTriggerManual,
		StartedAt:    started,
		FinishedAt:   &finished,
		ExitCode:     &exitCode,
		ErrorSummary: "boom\nat line 1",
	}
}

func captureWebhook(t *testing.T, status int, response string) (*httptest.Server, <-chan string) {
	t.Helper()

	bodies := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Body.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		bodies <- b.String()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)
	return server, bodies
}

func waitBody(t *testing.T, bodies <-chan string) string {
	t.Helper()
	select {
	case body := <-bodies:
		return body
	case <-time.After(2 * time.Second):
		t.Fatal("webhook did not receive a request")
		return ""
	}
}

func requireNoBody(t *testing.T, bodies <-chan string) {
	t.Helper()
	select {
	case body := <-bodies:
		t.Fatalf("unexpected webhook request: %s", body)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "disabled without url is allowed",
			cfg:  Config{Enabled: false, Type: TypeGeneric, TimeoutSeconds: 10, Events: []string{EventFailed}},
		},
		{
			name:    "enabled without url",
			cfg:     Config{Enabled: true, Type: TypeGeneric, TimeoutSeconds: 10, Events: []string{EventFailed}},
			wantErr: true,
		},
		{
			name:    "invalid url scheme",
			cfg:     Config{Enabled: true, Type: TypeGeneric, URL: "ftp://example.com", TimeoutSeconds: 10, Events: []string{EventFailed}},
			wantErr: true,
		},
		{
			name:    "invalid type",
			cfg:     Config{Enabled: true, Type: "sms", URL: "https://example.com", TimeoutSeconds: 10, Events: []string{EventFailed}},
			wantErr: true,
		},
		{
			name:    "enabled without events",
			cfg:     Config{Enabled: true, Type: TypeGeneric, URL: "https://example.com", TimeoutSeconds: 10, Events: []string{}},
			wantErr: true,
		},
		{
			name:    "timeout too large",
			cfg:     Config{Enabled: true, Type: TypeGeneric, URL: "https://example.com", TimeoutSeconds: 61, Events: []string{EventFailed}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNotifyFailedGeneric(t *testing.T) {
	server, bodies := captureWebhook(t, http.StatusOK, `{"ok":true}`)

	notifier := New(nil)
	if err := notifier.Update(Config{
		Enabled:        true,
		Type:           TypeGeneric,
		URL:            server.URL,
		Events:         []string{EventFailed},
		TimeoutSeconds: 5,
	}); err != nil {
		t.Fatalf("Update() failed: %v", err)
	}

	notifier.Notify(sampleRun(store.RunStatusFailed), false)

	var payload struct {
		Event           string  `json:"event"`
		Status          string  `json:"status"`
		JobID           int64   `json:"jobId"`
		JobName         string  `json:"jobName"`
		RunID           int64   `json:"runId"`
		RetryAttempt    int     `json:"retryAttempt"`
		DurationSeconds float64 `json:"durationSeconds"`
		ExitCode        *int    `json:"exitCode"`
		ErrorSummary    string  `json:"errorSummary"`
	}
	if err := json.Unmarshal([]byte(waitBody(t, bodies)), &payload); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if payload.Event != EventFailed || payload.Status != store.RunStatusFailed {
		t.Errorf("event/status = %q/%q, want failed/failed", payload.Event, payload.Status)
	}
	if payload.JobID != 7 || payload.JobName != "nightly-sync" || payload.RunID != 42 {
		t.Errorf("payload identity = %+v", payload)
	}
	if payload.DurationSeconds != 90 {
		t.Errorf("duration = %v, want 90", payload.DurationSeconds)
	}
	if payload.ExitCode == nil || *payload.ExitCode != 3 {
		t.Errorf("exitCode = %v, want 3", payload.ExitCode)
	}
	if !strings.Contains(payload.ErrorSummary, "boom at line 1") {
		t.Errorf("errorSummary = %q", payload.ErrorSummary)
	}
}

func TestNotifyEventFiltering(t *testing.T) {
	server, bodies := captureWebhook(t, http.StatusOK, "ok")

	notifier := New(nil)
	if err := notifier.Update(Config{
		Enabled:        true,
		Type:           TypeSlack,
		URL:            server.URL,
		Events:         []string{EventFailed},
		TimeoutSeconds: 5,
	}); err != nil {
		t.Fatalf("Update() failed: %v", err)
	}

	// 成功事件未订阅。
	notifier.Notify(sampleRun(store.RunStatusSucceeded), false)
	requireNoBody(t, bodies)

	// 失败但会自动重试，不发送。
	notifier.Notify(sampleRun(store.RunStatusFailed), true)
	requireNoBody(t, bodies)
}

func TestNotifyRecovered(t *testing.T) {
	server, bodies := captureWebhook(t, http.StatusOK, "ok")

	notifier := New(nil)
	if err := notifier.Update(Config{
		Enabled:        true,
		Type:           TypeSlack,
		URL:            server.URL,
		Events:         []string{EventRecovered},
		TimeoutSeconds: 5,
	}); err != nil {
		t.Fatalf("Update() failed: %v", err)
	}

	run := sampleRun(store.RunStatusSucceeded)
	run.Trigger = store.RunTriggerRetry
	run.RetryAttempt = 1
	notifier.Notify(run, false)

	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(waitBody(t, bodies)), &payload); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if !strings.Contains(payload.Text, "任务已恢复") || !strings.Contains(payload.Text, "自动重试 #1") {
		t.Errorf("slack text = %q", payload.Text)
	}
}

func TestBuildBodyTypes(t *testing.T) {
	run := sampleRun(store.RunStatusFailed)
	tests := []struct {
		typ      string
		wantKeys []string
	}{
		{TypeFeishu, []string{`"msg_type":"text"`, `"text":`}},
		{TypeDingTalk, []string{`"msgtype":"text"`, `"content":`}},
		{TypeWeCom, []string{`"msgtype":"text"`, `"content":`}},
		{TypeSlack, []string{`"text":`}},
	}

	for _, tt := range tests {
		t.Run(tt.typ, func(t *testing.T) {
			body, err := buildBody(Config{Type: tt.typ}, EventFailed, run)
			if err != nil {
				t.Fatalf("buildBody() failed: %v", err)
			}
			text := string(body)
			for _, key := range tt.wantKeys {
				if !strings.Contains(text, key) {
					t.Errorf("body %s does not contain %s", text, key)
				}
			}
			if !strings.Contains(text, "任务失败") {
				t.Errorf("body does not contain event title: %s", text)
			}
		})
	}
}

func TestSendTest(t *testing.T) {
	server, bodies := captureWebhook(t, http.StatusOK, `{"errcode":0}`)

	notifier := New(nil)
	if err := notifier.SendTest(Config{
		Type:           TypeDingTalk,
		URL:            server.URL,
		TimeoutSeconds: 5,
	}); err != nil {
		t.Fatalf("SendTest() failed: %v", err)
	}

	body := waitBody(t, bodies)
	if !strings.Contains(body, "通知测试") {
		t.Errorf("test body = %s", body)
	}
}

func TestSendTestFailure(t *testing.T) {
	server, _ := captureWebhook(t, http.StatusInternalServerError, "boom")

	notifier := New(nil)
	err := notifier.SendTest(Config{
		Type:           TypeGeneric,
		URL:            server.URL,
		TimeoutSeconds: 5,
	})
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("SendTest() error = %v, want HTTP 500", err)
	}

	server2, _ := captureWebhook(t, http.StatusOK, `{"errcode":310000,"errmsg":"invalid token"}`)
	err = notifier.SendTest(Config{
		Type:           TypeDingTalk,
		URL:            server2.URL,
		TimeoutSeconds: 5,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid token") {
		t.Fatalf("SendTest() error = %v, want webhook error message", err)
	}
}
