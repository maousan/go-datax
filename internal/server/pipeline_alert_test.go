package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/longkeyy/go-datax/internal/alert"
	"github.com/longkeyy/go-datax/internal/notify"
	"github.com/longkeyy/go-datax/internal/store"
)

func TestPipelineRunSendsAlert(t *testing.T) {
	srv, handler, dataStore := newPipelineTestServer(t)
	token := loginToken(t, handler, "admin", "secret")
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "source.csv"), []byte("id,name\n1,a\n2,b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sinkPath := filepath.Join(t.TempDir(), "sink.db")
	createPipelineTestTarget(t, sinkPath)
	sourceID, sinkID := seedPipelineSourceAndSink(t, dataStore, base, "source.csv", sinkPath)
	pipeline := createScheduleTestPipeline(t, handler, token, "alert-pipeline", sourceID, sinkID)
	publishScheduleTestPipeline(t, handler, token, pipeline.ID)

	payloads := make(chan map[string]any, 1)
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		payloads <- payload
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer webhook.Close()
	if err := srv.alerts.Update(alert.Config{
		Enabled: true, TimeoutSeconds: 5, RetentionDays: 30,
		Channels: []alert.Channel{{
			Name: "pipeline-hook", Type: alert.TypeWebhook, Enabled: true,
			Events: []string{notify.EventSucceeded}, URL: webhook.URL,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines/"+itoa(pipeline.ID)+"/runs", token, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("run status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var run pipelineRunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	run = waitPipelineRun(t, handler, token, run.ID)
	if run.Status != store.PipelineRunStatusSucceeded {
		t.Fatalf("run = %+v", run)
	}

	select {
	case payload := <-payloads:
		if payload["sourceKind"] != "pipeline" || payload["event"] != "succeeded" ||
			payload["pipelineName"] != pipeline.Name || payload["versionNumber"] != float64(1) ||
			payload["runId"] != float64(run.ID) || payload["trigger"] != store.PipelineRunTriggerManual {
			t.Fatalf("pipeline alert payload = %+v", payload)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pipeline alert webhook was not called")
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		logs, total, err := dataStore.ListAlertLogs(store.AlertLogFilter{
			SourceKind: store.AlertSourcePipeline, Page: 1, PageSize: 10,
		})
		if err != nil {
			t.Fatal(err)
		}
		if total == 1 {
			entry := logs[0]
			if entry.SourceKind != store.AlertSourcePipeline || entry.PipelineID == nil ||
				*entry.PipelineID != pipeline.ID || entry.PipelineRunID == nil ||
				*entry.PipelineRunID != run.ID || entry.VersionNumber != 1 || entry.PipelineName != pipeline.Name {
				t.Fatalf("pipeline alert log = %+v", entry)
			}
			if entry.SyncJobName != "" || entry.RunID != 0 {
				t.Fatalf("pipeline alert log has DataX fields: %+v", entry)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pipeline alert log was not written")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
