package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/longkeyy/go-datax/internal/store"
)

func createScheduleTestPipeline(t *testing.T, handler http.Handler, token, name string, sourceID, sinkID int64) pipelineResponse {
	t.Helper()
	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines", token, map[string]any{
		"name": name, "sourceId": sourceID, "sinkId": sinkID,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create pipeline status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var pipeline pipelineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &pipeline); err != nil {
		t.Fatal(err)
	}
	return pipeline
}

func publishScheduleTestPipeline(t *testing.T, handler http.Handler, token string, pipelineID int64) pipelineVersionResponse {
	t.Helper()
	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines/"+itoa(pipelineID)+"/publish", token, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("publish status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var version pipelineVersionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &version); err != nil {
		t.Fatal(err)
	}
	return version
}

func cloneScheduleBody(body map[string]any) map[string]any {
	copy := make(map[string]any, len(body))
	for key, value := range body {
		copy[key] = value
	}
	return copy
}

func TestPipelineScheduleCRUD(t *testing.T) {
	_, handler, dataStore := newPipelineTestServer(t)
	token := loginToken(t, handler, "admin", "secret")
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "source.csv"), []byte("id,name\n1,a\n2,b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sinkPath := filepath.Join(t.TempDir(), "sink.db")
	createPipelineTestTarget(t, sinkPath)
	sourceID, sinkID := seedPipelineSourceAndSink(t, dataStore, base, "source.csv", sinkPath)
	pipeline := createScheduleTestPipeline(t, handler, token, "scheduled-pipeline", sourceID, sinkID)
	version := publishScheduleTestPipeline(t, handler, token, pipeline.ID)
	otherPipeline := createScheduleTestPipeline(t, handler, token, "other-scheduled", sourceID, sinkID)
	otherVersion := publishScheduleTestPipeline(t, handler, token, otherPipeline.ID)

	body := map[string]any{
		"name":                 "Daily at two",
		"cronExpr":             "0 0 2 * * ?",
		"timezone":             "Asia/Shanghai",
		"enabled":              true,
		"blockingPolicy":       "queue",
		"retryMax":             2,
		"retryIntervalSeconds": 30,
		"timeoutSeconds":       600,
		"versionId":            version.ID,
	}
	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines/"+itoa(pipeline.ID)+"/schedules", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create schedule status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var schedule pipelineScheduleResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &schedule); err != nil {
		t.Fatal(err)
	}
	if schedule.PipelineID != pipeline.ID || !schedule.Enabled || schedule.BlockingPolicy != "queue" ||
		schedule.PinnedVersionID == nil || *schedule.PinnedVersionID != version.ID ||
		schedule.PinnedVersionNumber != 1 || schedule.NextRunAt == nil {
		t.Fatalf("schedule = %+v", schedule)
	}

	duplicate := cloneScheduleBody(body)
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines/"+itoa(pipeline.ID)+"/schedules", token, duplicate)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate schedule status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/pipeline-schedules?pipelineId="+itoa(pipeline.ID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list schedules status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var list []pipelineScheduleResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != schedule.ID || list[0].PipelineName != pipeline.Name {
		t.Fatalf("schedules = %+v", list)
	}

	body["name"] = "Daily at two disabled"
	body["enabled"] = false
	body["versionId"] = 0
	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/pipeline-schedules/"+itoa(schedule.ID), token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("update schedule status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var updated pipelineScheduleResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Enabled || updated.NextRunAt != nil || updated.PinnedVersionID != nil {
		t.Fatalf("updated schedule = %+v", updated)
	}

	invalidTimezone := cloneScheduleBody(body)
	invalidTimezone["timezone"] = "Mars/Olympus"
	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/pipeline-schedules/"+itoa(schedule.ID), token, invalidTimezone)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "时区") {
		t.Fatalf("invalid timezone status = %d, body = %s", rec.Code, rec.Body.String())
	}
	invalidBlocking := cloneScheduleBody(body)
	invalidBlocking["blockingPolicy"] = "replace"
	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/pipeline-schedules/"+itoa(schedule.ID), token, invalidBlocking)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "阻塞策略") {
		t.Fatalf("invalid blocking status = %d, body = %s", rec.Code, rec.Body.String())
	}
	otherPin := cloneScheduleBody(body)
	otherPin["versionId"] = otherVersion.ID
	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/pipeline-schedules/"+itoa(schedule.ID), token, otherPin)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "不属于") {
		t.Fatalf("foreign pin status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = authedJSONRequest(t, handler, http.MethodDelete, "/api/v1/pipeline-schedules/"+itoa(schedule.ID), token, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete schedule status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if _, err := dataStore.GetPipelineSchedule(schedule.ID); err == nil {
		t.Fatal("schedule still exists after delete")
	}
}

func TestPipelineSchedulerTriggersPinnedVersion(t *testing.T) {
	srv, handler, dataStore := newPipelineTestServer(t)
	token := loginToken(t, handler, "admin", "secret")
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "source.csv"), []byte("id,name\n1,a\n2,b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sinkPath := filepath.Join(t.TempDir(), "sink.db")
	createPipelineTestTarget(t, sinkPath)
	sourceID, sinkID := seedPipelineSourceAndSink(t, dataStore, base, "source.csv", sinkPath)
	pipeline := createScheduleTestPipeline(t, handler, token, "scheduler-pipeline", sourceID, sinkID)
	first := publishScheduleTestPipeline(t, handler, token, pipeline.ID)
	second := publishScheduleTestPipeline(t, handler, token, pipeline.ID)
	if first.ID == second.ID || second.VersionNumber != 2 {
		t.Fatalf("versions = %+v / %+v", first, second)
	}

	body := map[string]any{
		"name":           "every second",
		"cronExpr":       "* * * * * ?",
		"timezone":       "UTC",
		"enabled":        false,
		"blockingPolicy": "discard",
		"versionId":      first.ID,
	}
	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines/"+itoa(pipeline.ID)+"/schedules", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create schedule status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var schedule pipelineScheduleResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &schedule); err != nil {
		t.Fatal(err)
	}

	srv.StartScheduler()
	defer srv.StopScheduler()
	time.Sleep(1500 * time.Millisecond)
	if runs := pipelineRunsForTest(t, handler, token, pipeline.ID); len(runs) != 0 {
		t.Fatalf("disabled schedule created runs: %+v", runs)
	}

	body["enabled"] = true
	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/pipeline-schedules/"+itoa(schedule.ID), token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable schedule status = %d, body = %s", rec.Code, rec.Body.String())
	}

	deadline := time.Now().Add(8 * time.Second)
	var runs []pipelineRunResponse
	for time.Now().Before(deadline) {
		runs = pipelineRunsForTest(t, handler, token, pipeline.ID)
		if len(runs) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(runs) == 0 {
		t.Fatal("enabled schedule did not trigger")
	}
	if runs[0].VersionNumber != 1 || runs[0].Trigger != store.PipelineRunTriggerSchedule {
		t.Fatalf("scheduled run = %+v", runs[0])
	}
}

func pipelineRunsForTest(t *testing.T, handler http.Handler, token string, pipelineID int64) []pipelineRunResponse {
	t.Helper()
	rec := authedJSONRequest(t, handler, http.MethodGet, "/api/v1/pipeline-runs?pipelineId="+itoa(pipelineID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list runs status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Items []pipelineRunResponse `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Items
}

func TestPipelineManualQueueAndDrain(t *testing.T) {
	srv, handler, dataStore := newPipelineTestServer(t)
	token := loginToken(t, handler, "admin", "secret")
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "source.csv"), []byte("id,name\n1,a\n2,b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sinkPath := filepath.Join(t.TempDir(), "sink.db")
	createPipelineTestTarget(t, sinkPath)
	sourceID, sinkID := seedPipelineSourceAndSink(t, dataStore, base, "source.csv", sinkPath)
	pipeline := createScheduleTestPipeline(t, handler, token, "queue-pipeline", sourceID, sinkID)
	publishScheduleTestPipeline(t, handler, token, pipeline.ID)

	active := &store.PipelineRun{
		PipelineID: &pipeline.ID, PipelineName: pipeline.Name,
		Status: store.PipelineRunStatusRunning, Trigger: store.PipelineRunTriggerManual, StartedAt: time.Now(),
	}
	if err := dataStore.CreatePipelineRun(active); err != nil {
		t.Fatal(err)
	}

	rec := authedJSONRequest(t, handler, http.MethodPost,
		"/api/v1/pipelines/"+itoa(pipeline.ID)+"/runs?blocking=queue", token, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("queue run status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var queued pipelineRunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &queued); err != nil {
		t.Fatal(err)
	}
	if queued.Status != store.PipelineRunStatusQueued {
		t.Fatalf("queued run = %+v", queued)
	}

	if err := dataStore.FinishPipelineRun(active.ID, store.PipelineRunStatusFailed, "test release"); err != nil {
		t.Fatal(err)
	}
	srv.drainPipelineQueue(pipeline.ID)
	queued = waitPipelineRun(t, handler, token, queued.ID)
	if queued.Status != store.PipelineRunStatusSucceeded || queued.WriteRecords != 2 {
		t.Fatalf("drained run = %+v", queued)
	}
}

func TestPipelineOverwriteCancelsStaleActiveRun(t *testing.T) {
	srv, handler, dataStore := newPipelineTestServer(t)
	token := loginToken(t, handler, "admin", "secret")
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "source.csv"), []byte("id,name\n1,a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sinkPath := filepath.Join(t.TempDir(), "sink.db")
	createPipelineTestTarget(t, sinkPath)
	sourceID, sinkID := seedPipelineSourceAndSink(t, dataStore, base, "source.csv", sinkPath)
	pipeline := createScheduleTestPipeline(t, handler, token, "overwrite-pipeline", sourceID, sinkID)
	version := publishScheduleTestPipeline(t, handler, token, pipeline.ID)

	active := &store.PipelineRun{
		PipelineID: &pipeline.ID, PipelineName: pipeline.Name,
		Status: store.PipelineRunStatusRunning, Trigger: store.PipelineRunTriggerManual, StartedAt: time.Now(),
	}
	if err := dataStore.CreatePipelineRun(active); err != nil {
		t.Fatal(err)
	}
	pending := &store.PipelineRun{
		PipelineID: &pipeline.ID, PipelineName: pipeline.Name,
		Status: store.PipelineRunStatusQueued, Trigger: store.PipelineRunTriggerSchedule, StartedAt: time.Now(),
	}
	if err := dataStore.CreatePipelineRun(pending); err != nil {
		t.Fatal(err)
	}
	pipelineModel, err := dataStore.GetPipeline(pipeline.ID)
	if err != nil {
		t.Fatal(err)
	}
	versionModel, err := dataStore.GetPipelineVersion(version.ID)
	if err != nil {
		t.Fatal(err)
	}
	run, err := srv.startPipelineRun(pipelineModel, versionModel, pipelineStartOptions{
		Trigger: store.PipelineRunTriggerSchedule, Blocking: store.BlockingOverwrite,
	})
	if err != nil {
		t.Fatalf("overwrite start failed: %v", err)
	}
	stale, err := dataStore.GetPipelineRun(active.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stale.Status != store.PipelineRunStatusFailed {
		t.Fatalf("stale run status = %s", stale.Status)
	}
	queued, err := dataStore.GetPipelineRun(pending.ID)
	if err != nil {
		t.Fatal(err)
	}
	if queued.Status != store.PipelineRunStatusCanceled {
		t.Fatalf("pending run status = %s", queued.Status)
	}
	finished := waitPipelineRun(t, handler, token, run.ID)
	if finished.Status != store.PipelineRunStatusSucceeded {
		t.Fatalf("overwrite run = %+v", finished)
	}
}

func TestPipelineScheduleRetriesFailedRun(t *testing.T) {
	srv, handler, dataStore := newPipelineTestServer(t)
	token := loginToken(t, handler, "admin", "secret")
	base := t.TempDir()
	sourcePath := filepath.Join(base, "source.csv")
	if err := os.WriteFile(sourcePath, []byte("id,name\n1,a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sinkPath := filepath.Join(t.TempDir(), "sink.db")
	createPipelineTestTarget(t, sinkPath)
	sourceID, sinkID := seedPipelineSourceAndSink(t, dataStore, base, "source.csv", sinkPath)
	pipeline := createScheduleTestPipeline(t, handler, token, "retry-pipeline", sourceID, sinkID)
	version := publishScheduleTestPipeline(t, handler, token, pipeline.ID)

	body := map[string]any{
		"name": "retry every second", "cronExpr": "* * * * * ?", "timezone": "UTC",
		"enabled": true, "blockingPolicy": "discard", "retryMax": 1,
		"retryIntervalSeconds": 0, "timeoutSeconds": 0, "versionId": version.ID,
	}
	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines/"+itoa(pipeline.ID)+"/schedules", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create schedule status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var schedule pipelineScheduleResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &schedule); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(sourcePath); err != nil {
		t.Fatal(err)
	}

	pipelineModel, err := dataStore.GetPipeline(pipeline.ID)
	if err != nil {
		t.Fatal(err)
	}
	versionModel, err := dataStore.GetPipelineVersion(version.ID)
	if err != nil {
		t.Fatal(err)
	}
	scheduleID := schedule.ID
	run, err := srv.startPipelineRun(pipelineModel, versionModel, pipelineStartOptions{
		Trigger: store.PipelineRunTriggerSchedule, Blocking: store.BlockingDiscard,
		ScheduleID: &scheduleID,
	})
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if finished := waitPipelineRun(t, handler, token, run.ID); finished.Status != store.PipelineRunStatusFailed {
		t.Fatalf("first run = %+v", finished)
	}

	deadline := time.Now().Add(10 * time.Second)
	var retried *pipelineRunResponse
	for time.Now().Before(deadline) {
		for _, candidate := range pipelineRunsForTest(t, handler, token, pipeline.ID) {
			if candidate.Trigger == store.PipelineRunTriggerRetry {
				retried = &candidate
				break
			}
		}
		if retried != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if retried == nil {
		t.Fatal("schedule retry did not start")
	}
	if retried.RetryAttempt != 1 {
		t.Fatalf("retry attempt = %d, run = %+v", retried.RetryAttempt, retried)
	}
}

func TestPipelineTimeoutTimer(t *testing.T) {
	dataStore := openTestStore(t)
	pipelineID := int64(1)
	run := &store.PipelineRun{
		PipelineID: &pipelineID, PipelineName: "timeout-timer",
		Status: store.PipelineRunStatusRunning, Trigger: store.PipelineRunTriggerManual, StartedAt: time.Now(),
	}
	if err := dataStore.CreatePipelineRun(run); err != nil {
		t.Fatal(err)
	}
	srv := &Server{worker: newWorkerSupervisor(WorkerConfig{}, nil), dataStore: dataStore}
	stop, timedOut := srv.applyPipelineTimeout("missing-worker-run", run.ID, 30*time.Millisecond)
	defer stop()
	time.Sleep(80 * time.Millisecond)
	if !timedOut.Load() {
		t.Fatal("timeout timer did not fire")
	}
}

func TestPipelineScheduleTimeoutMarksRunFailed(t *testing.T) {
	srv, handler, dataStore := newPipelineTestServer(t)
	token := loginToken(t, handler, "admin", "secret")
	base := t.TempDir()
	var source strings.Builder
	source.WriteString("id,name\n")
	for i := 0; i < 500000; i++ {
		fmt.Fprintf(&source, "%d,name\n", i)
	}
	if err := os.WriteFile(filepath.Join(base, "source.csv"), []byte(source.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	sinkPath := filepath.Join(t.TempDir(), "sink.db")
	createPipelineTestTarget(t, sinkPath)
	sourceID, sinkID := seedPipelineSourceAndSink(t, dataStore, base, "source.csv", sinkPath)
	pipeline := createScheduleTestPipeline(t, handler, token, "timeout-pipeline", sourceID, sinkID)
	version := publishScheduleTestPipeline(t, handler, token, pipeline.ID)
	body := map[string]any{
		"name": "one second timeout", "cronExpr": "0 0 2 * * ?", "timezone": "UTC",
		"enabled": false, "blockingPolicy": "discard", "retryMax": 0,
		"retryIntervalSeconds": 0, "timeoutSeconds": 1, "versionId": version.ID,
	}
	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines/"+itoa(pipeline.ID)+"/schedules", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create schedule status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var schedule pipelineScheduleResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &schedule); err != nil {
		t.Fatal(err)
	}
	pipelineModel, err := dataStore.GetPipeline(pipeline.ID)
	if err != nil {
		t.Fatal(err)
	}
	versionModel, err := dataStore.GetPipelineVersion(version.ID)
	if err != nil {
		t.Fatal(err)
	}
	scheduleID := schedule.ID
	run, err := srv.startPipelineRun(pipelineModel, versionModel, pipelineStartOptions{
		Trigger: store.PipelineRunTriggerSchedule, Blocking: store.BlockingDiscard,
		ScheduleID: &scheduleID, Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	var finished pipelineRunResponse
	for {
		finished = pipelineRunByID(t, handler, token, run.ID)
		if finished.Status == store.PipelineRunStatusFailed && strings.Contains(finished.ErrorSummary, "超时") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed-out run = %+v", finished)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestPipelineSchedulerUsesLatestVersion(t *testing.T) {
	srv, handler, dataStore := newPipelineTestServer(t)
	token := loginToken(t, handler, "admin", "secret")
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "source.csv"), []byte("id,name\n1,a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sinkPath := filepath.Join(t.TempDir(), "sink.db")
	createPipelineTestTarget(t, sinkPath)
	sourceID, sinkID := seedPipelineSourceAndSink(t, dataStore, base, "source.csv", sinkPath)
	pipeline := createScheduleTestPipeline(t, handler, token, "latest-pipeline", sourceID, sinkID)
	publishScheduleTestPipeline(t, handler, token, pipeline.ID)
	publishScheduleTestPipeline(t, handler, token, pipeline.ID)

	body := map[string]any{
		"name": "latest every second", "cronExpr": "* * * * * ?", "timezone": "UTC",
		"enabled": true, "blockingPolicy": "discard", "versionId": 0,
	}
	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines/"+itoa(pipeline.ID)+"/schedules", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create schedule status = %d, body = %s", rec.Code, rec.Body.String())
	}
	srv.StartScheduler()
	defer srv.StopScheduler()

	deadline := time.Now().Add(8 * time.Second)
	var runs []pipelineRunResponse
	for time.Now().Before(deadline) {
		runs = pipelineRunsForTest(t, handler, token, pipeline.ID)
		if len(runs) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(runs) == 0 {
		t.Fatal("schedule did not trigger")
	}
	if runs[0].VersionNumber != 2 {
		t.Fatalf("latest schedule run = %+v", runs[0])
	}
}
