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

func samplePipelineRun(status string) store.PipelineRun {
	pipelineID := int64(11)
	versionID := int64(22)
	scheduleID := int64(33)
	started := time.Date(2026, 11, 24, 10, 0, 0, 0, time.Local)
	finished := started.Add(90 * time.Second)
	return store.PipelineRun{
		ID: 44, PipelineID: &pipelineID, VersionID: &versionID,
		ScheduleID: &scheduleID, VersionNumber: 3, PipelineName: "orders-etl",
		Status: status, Trigger: store.PipelineRunTriggerSchedule,
		StartedAt: started, FinishedAt: &finished,
		ReadRecords: 100, WriteRecords: 98, ErrorSummary: "boom\nat line 1",
	}
}

func TestPipelineEventFor(t *testing.T) {
	tests := []struct {
		name      string
		run       store.PipelineRun
		willRetry bool
		want      string
	}{
		{name: "failed", run: samplePipelineRun(store.PipelineRunStatusFailed), want: EventFailed},
		{name: "failed retry pending", run: samplePipelineRun(store.PipelineRunStatusFailed), willRetry: true, want: ""},
		{name: "canceled", run: samplePipelineRun(store.PipelineRunStatusCanceled), want: EventCanceled},
		{name: "succeeded", run: samplePipelineRun(store.PipelineRunStatusSucceeded), want: EventSucceeded},
		{
			name: "recovered",
			run: func() store.PipelineRun {
				run := samplePipelineRun(store.PipelineRunStatusSucceeded)
				run.Trigger = store.PipelineRunTriggerRetry
				run.RetryAttempt = 1
				return run
			}(),
			want: EventRecovered,
		},
		{name: "running", run: samplePipelineRun(store.PipelineRunStatusRunning), want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PipelineEventFor(tt.run, tt.willRetry); got != tt.want {
				t.Fatalf("PipelineEventFor() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPipelineRunPayload(t *testing.T) {
	var payload struct {
		SourceKind      string  `json:"sourceKind"`
		Event           string  `json:"event"`
		Status          string  `json:"status"`
		PipelineID      int64   `json:"pipelineId"`
		PipelineName    string  `json:"pipelineName"`
		VersionID       *int64  `json:"versionId"`
		VersionNumber   int     `json:"versionNumber"`
		RunID           int64   `json:"runId"`
		ScheduleID      *int64  `json:"scheduleId"`
		Trigger         string  `json:"trigger"`
		ReadRecords     int64   `json:"readRecords"`
		WriteRecords    int64   `json:"writeRecords"`
		DurationSeconds float64 `json:"durationSeconds"`
		ErrorSummary    string  `json:"errorSummary"`
	}
	data, err := PipelineRunPayload(EventFailed, samplePipelineRun(store.PipelineRunStatusFailed))
	if err != nil {
		t.Fatalf("PipelineRunPayload() failed: %v", err)
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if payload.SourceKind != store.AlertSourcePipeline || payload.Event != EventFailed ||
		payload.Status != store.PipelineRunStatusFailed {
		t.Fatalf("source/event/status = %+v", payload)
	}
	if payload.PipelineID != 11 || payload.PipelineName != "orders-etl" ||
		payload.VersionID == nil || *payload.VersionID != 22 || payload.VersionNumber != 3 ||
		payload.RunID != 44 || payload.ScheduleID == nil || *payload.ScheduleID != 33 ||
		payload.Trigger != store.PipelineRunTriggerSchedule {
		t.Errorf("identity = %+v", payload)
	}
	if payload.ReadRecords != 100 || payload.WriteRecords != 98 || payload.DurationSeconds != 90 {
		t.Errorf("metrics = %+v", payload)
	}
	if !strings.Contains(payload.ErrorSummary, "boom at line 1") {
		t.Errorf("errorSummary = %q", payload.ErrorSummary)
	}
}

func TestFormatPipelineRunText(t *testing.T) {
	text := FormatPipelineRunText(EventFailed, samplePipelineRun(store.PipelineRunStatusFailed))
	for _, want := range []string{
		"【go-datax】任务失败",
		"Pipeline：orders-etl",
		"版本：v3",
		"运行：#44 · 定时调度",
		"调度：#33",
		"读取/写入：100 / 98 行",
		"错误：boom at line 1",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("FormatPipelineRunText() = %q, want it to contain %q", text, want)
		}
	}
}
