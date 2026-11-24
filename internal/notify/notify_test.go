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

func TestEventFor(t *testing.T) {
	tests := []struct {
		name      string
		run       store.JobRun
		willRetry bool
		want      string
	}{
		{
			name: "failed without retry",
			run:  sampleRun(store.RunStatusFailed),
			want: EventFailed,
		},
		{
			name:      "failed with scheduled retry",
			run:       sampleRun(store.RunStatusFailed),
			willRetry: true,
			want:      "",
		},
		{
			name: "canceled",
			run:  sampleRun(store.RunStatusCanceled),
			want: EventCanceled,
		},
		{
			name: "succeeded",
			run:  sampleRun(store.RunStatusSucceeded),
			want: EventSucceeded,
		},
		{
			name: "recovered by retry",
			run: func() store.JobRun {
				run := sampleRun(store.RunStatusSucceeded)
				run.Trigger = store.RunTriggerRetry
				run.RetryAttempt = 1
				return run
			}(),
			want: EventRecovered,
		},
		{
			name: "running",
			run:  sampleRun(store.RunStatusRunning),
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EventFor(tt.run, tt.willRetry); got != tt.want {
				t.Fatalf("EventFor() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatRunText(t *testing.T) {
	text := FormatRunText(EventFailed, sampleRun(store.RunStatusFailed))
	for _, want := range []string{
		"【go-datax】任务失败",
		"任务：nightly-sync",
		"运行：#42 · 手动触发",
		"退出码：3",
		"错误：boom at line 1",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("FormatRunText() = %q, want it to contain %q", text, want)
		}
	}

	run := sampleRun(store.RunStatusSucceeded)
	run.Trigger = store.RunTriggerRetry
	run.RetryAttempt = 2
	text = FormatRunText(EventRecovered, run)
	if !strings.Contains(text, "任务已恢复") || !strings.Contains(text, "自动重试 #2") {
		t.Errorf("recovered text = %q", text)
	}
}

func TestRunPayload(t *testing.T) {
	var payload struct {
		Event           string  `json:"event"`
		Status          string  `json:"status"`
		JobID           int64   `json:"jobId"`
		JobName         string  `json:"jobName"`
		RunID           int64   `json:"runId"`
		DurationSeconds float64 `json:"durationSeconds"`
		ExitCode        *int    `json:"exitCode"`
		ErrorSummary    string  `json:"errorSummary"`
	}
	data, err := RunPayload(EventFailed, sampleRun(store.RunStatusFailed))
	if err != nil {
		t.Fatalf("RunPayload() failed: %v", err)
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if payload.Event != EventFailed || payload.Status != store.RunStatusFailed {
		t.Errorf("event/status = %q/%q", payload.Event, payload.Status)
	}
	if payload.JobID != 7 || payload.JobName != "nightly-sync" || payload.RunID != 42 {
		t.Errorf("identity = %+v", payload)
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

func TestTextPayloadTypes(t *testing.T) {
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
			body, err := TextPayload(tt.typ, FormatRunText(EventFailed, sampleRun(store.RunStatusFailed)))
			if err != nil {
				t.Fatalf("TextPayload() failed: %v", err)
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

	if _, err := TextPayload("sms", "hello"); err == nil {
		t.Fatal("TextPayload() error = nil, want unsupported type error")
	}
}

func TestPost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}
		_, _ = w.Write([]byte(`{"errcode":0}`))
	}))
	defer server.Close()

	if err := Post(server.URL, 5*time.Second, []byte(`{"ok":true}`)); err != nil {
		t.Fatalf("Post() failed: %v", err)
	}
}

func TestPostFailure(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer failing.Close()

	err := Post(failing.URL, 5*time.Second, []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("Post() error = %v, want HTTP 500", err)
	}

	invalid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":310000,"errmsg":"invalid token"}`))
	}))
	defer invalid.Close()

	err = Post(invalid.URL, 5*time.Second, []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "invalid token") {
		t.Fatalf("Post() error = %v, want webhook error message", err)
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := TruncateRunes("你好世界", 2); got != "你好..." {
		t.Errorf("TruncateRunes() = %q, want 你好...", got)
	}
	if got := TruncateRunes("ok", 10); got != "ok" {
		t.Errorf("TruncateRunes() = %q, want ok", got)
	}
}
