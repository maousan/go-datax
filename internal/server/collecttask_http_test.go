package server

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/longkeyy/go-datax/internal/store"
)

func createCollectTaskBody(jobID int64) map[string]any {
	return map[string]any{
		"name":                 "minute-sync",
		"syncJobId":            jobID,
		"cronExpr":             "0 0/5 * * * ?",
		"enabled":              true,
		"blockingPolicy":       store.BlockingDiscard,
		"retryMax":             2,
		"retryIntervalSeconds": 30,
		"timeoutSeconds":       60,
	}
}

func TestCollectTaskCRUD(t *testing.T) {
	dataStore := openTestStore(t)
	_, handler := newTestServerWithStore(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	}, testAuthConfig(t), dataStore)
	token := loginToken(t, handler, "admin", "secret")
	job := createRawJobForRuns(t, dataStore, "collect-definition")

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/datax/collect-tasks", token, createCollectTaskBody(job.ID))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var created collectTaskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("create response is not JSON: %v", err)
	}
	if created.Name != "minute-sync" || created.SyncJobName != job.Name {
		t.Errorf("created = %+v", created)
	}
	if created.NextRunAt == nil {
		t.Errorf("nextRunAt is empty")
	}
	if created.RetryMax != 2 || created.RetryIntervalSeconds != 30 || created.TimeoutSeconds != 60 {
		t.Errorf("retry/timeout = %+v", created)
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/datax/collect-tasks?query=minute", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	var list []collectTaskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("list is not JSON: %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Errorf("list = %+v", list)
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/datax/collect-tasks/"+itoa(created.ID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d", rec.Code)
	}

	body := createCollectTaskBody(job.ID)
	body["name"] = "minute-sync-updated"
	body["cronExpr"] = "0 0 2 * * ?"
	body["blockingPolicy"] = store.BlockingOverwrite
	body["retryMax"] = 0
	body["retryIntervalSeconds"] = 0
	body["timeoutSeconds"] = 0
	rec = authedJSONRequest(t, handler, http.MethodPut, "/api/v1/datax/collect-tasks/"+itoa(created.ID), token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var updated collectTaskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("update response is not JSON: %v", err)
	}
	if updated.Name != "minute-sync-updated" || updated.CronExpr != "0 0 2 * * ?" ||
		updated.BlockingPolicy != store.BlockingOverwrite {
		t.Errorf("updated = %+v", updated)
	}

	// 任务定义被采集任务引用时不能删除。
	rec = authedJSONRequest(t, handler, http.MethodDelete, "/api/v1/datax/sync-jobs/"+itoa(job.ID), token, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete referenced job status = %d, want 409 (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "采集任务") {
		t.Errorf("delete job message = %s", rec.Body.String())
	}

	rec = authedJSONRequest(t, handler, http.MethodDelete, "/api/v1/datax/collect-tasks/"+itoa(created.ID), token, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", rec.Code)
	}
	rec = authedJSONRequest(t, handler, http.MethodDelete, "/api/v1/datax/sync-jobs/"+itoa(job.ID), token, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete job after task removed status = %d", rec.Code)
	}
}

func TestCollectTaskValidation(t *testing.T) {
	dataStore := openTestStore(t)
	_, handler := newTestServerWithStore(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	}, testAuthConfig(t), dataStore)
	token := loginToken(t, handler, "admin", "secret")
	job := createRawJobForRuns(t, dataStore, "collect-validation")

	invalidCron := createCollectTaskBody(job.ID)
	invalidCron["cronExpr"] = "0 0 * * *"
	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/datax/collect-tasks", token, invalidCron)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid cron status = %d, want 400", rec.Code)
	}

	invalidPolicy := createCollectTaskBody(job.ID)
	invalidPolicy["blockingPolicy"] = "queue"
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/datax/collect-tasks", token, invalidPolicy)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid policy status = %d, want 400", rec.Code)
	}

	invalidTimeout := createCollectTaskBody(job.ID)
	invalidTimeout["timeoutSeconds"] = -1
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/datax/collect-tasks", token, invalidTimeout)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid timeout status = %d, want 400", rec.Code)
	}

	body := createCollectTaskBody(job.ID)
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/datax/collect-tasks", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/datax/collect-tasks", token, body)
	if rec.Code != http.StatusConflict {
		t.Errorf("duplicate name status = %d, want 409", rec.Code)
	}
}

func TestCollectTaskSchedulerRuns(t *testing.T) {
	dataStore := openTestStore(t)
	srv, handler := newRunTestServer(t, dataStore, func(string) *exec.Cmd { return successCommand() }, 2)
	token := loginToken(t, handler, "admin", "secret")
	job := prepareRunnableJob(t, handler, token)

	body := createCollectTaskBody(job.ID)
	body["name"] = "every-second"
	body["cronExpr"] = "* * * * * ?"
	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/datax/collect-tasks", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var task collectTaskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &task); err != nil {
		t.Fatalf("create response is not JSON: %v", err)
	}

	srv.StartScheduler()
	defer srv.StopScheduler()

	var run store.JobRun
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		candidate, found, err := dataStore.LatestRunForCollectTask(task.ID)
		if err != nil {
			t.Fatalf("LatestRunForCollectTask() failed: %v", err)
		}
		if found {
			run = candidate
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if run.ID == 0 {
		t.Fatal("scheduler did not create a run")
	}
	if run.Trigger != store.RunTriggerSchedule {
		t.Errorf("trigger = %q, want schedule", run.Trigger)
	}
	if run.CollectTaskID == nil || *run.CollectTaskID != task.ID {
		t.Errorf("collect task id = %v, want %d", run.CollectTaskID, task.ID)
	}
	if run.CollectTaskName != task.Name {
		t.Errorf("collect task name = %q, want %q", run.CollectTaskName, task.Name)
	}

	waitForRunStatus(t, dataStore, run.ID, store.RunStatusSucceeded)
}

func TestCollectTaskTimeout(t *testing.T) {
	dataStore := openTestStore(t)
	srv, handler := newRunTestServer(t, dataStore, func(string) *exec.Cmd { return slowCommand() }, 2)
	token := loginToken(t, handler, "admin", "secret")
	job := prepareRunnableJob(t, handler, token)

	task := store.CollectTask{
		Name:           "timeout-task",
		SyncJobID:      job.ID,
		CronExpr:       "0 0 0 1 1 ?",
		Enabled:        true,
		BlockingPolicy: store.BlockingDiscard,
		TimeoutSeconds: 1,
	}
	if err := dataStore.CreateCollectTask(&task); err != nil {
		t.Fatalf("CreateCollectTask() failed: %v", err)
	}
	jobDef, err := dataStore.GetSyncJob(job.ID)
	if err != nil {
		t.Fatalf("GetSyncJob() failed: %v", err)
	}
	config, err := srv.exportConfig(jobDef)
	if err != nil {
		t.Fatalf("exportConfig() failed: %v", err)
	}

	run, err := srv.runCollectTask(task, jobDef, config, store.RunTriggerManual)
	if err != nil {
		t.Fatalf("runCollectTask() failed: %v", err)
	}
	finished := waitForRunStatus(t, dataStore, run.ID, store.RunStatusFailed)
	if !strings.Contains(finished.ErrorSummary, "执行超时") {
		t.Errorf("error summary = %q, want timeout message", finished.ErrorSummary)
	}
}

func TestCollectTaskRetry(t *testing.T) {
	dataStore := openTestStore(t)
	srv, handler := newRunTestServer(t, dataStore, func(string) *exec.Cmd { return failingCommand() }, 2)
	token := loginToken(t, handler, "admin", "secret")
	job := prepareRunnableJob(t, handler, token)

	task := store.CollectTask{
		Name:                 "retry-task",
		SyncJobID:            job.ID,
		CronExpr:             "0 0 0 1 1 ?",
		Enabled:              true,
		BlockingPolicy:       store.BlockingDiscard,
		RetryMax:             1,
		RetryIntervalSeconds: 0,
	}
	if err := dataStore.CreateCollectTask(&task); err != nil {
		t.Fatalf("CreateCollectTask() failed: %v", err)
	}
	jobDef, err := dataStore.GetSyncJob(job.ID)
	if err != nil {
		t.Fatalf("GetSyncJob() failed: %v", err)
	}
	config, err := srv.exportConfig(jobDef)
	if err != nil {
		t.Fatalf("exportConfig() failed: %v", err)
	}

	first, err := srv.runCollectTask(task, jobDef, config, store.RunTriggerManual)
	if err != nil {
		t.Fatalf("runCollectTask() failed: %v", err)
	}
	waitForRunStatus(t, dataStore, first.ID, store.RunStatusFailed)
	var runs []store.JobRun
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runs, _, _ = dataStore.ListJobRuns(store.RunFilter{JobID: job.ID, Page: 1, PageSize: 10})
		if len(runs) >= 2 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(runs) < 2 {
		dbTask, _, _ := dataStore.LatestRunForCollectTask(task.ID)
		stored, _ := dataStore.GetCollectTask(task.ID)
		t.Fatalf("runs = %+v, task = %+v, collect latest = %+v, first = %+v", runs, stored, dbTask, first)
	}
	var retryRun store.JobRun
	for _, run := range runs {
		if run.Trigger == store.RunTriggerRetry {
			retryRun = run
		}
	}
	if retryRun.ID == 0 {
		t.Fatalf("no retry run in %+v", runs)
	}
	if retryRun.CollectTaskID == nil || *retryRun.CollectTaskID != task.ID {
		t.Errorf("retry collect task id = %v", retryRun.CollectTaskID)
	}
	if retryRun.RetryAttempt != 1 {
		t.Errorf("retry attempt = %d, want 1", retryRun.RetryAttempt)
	}
	waitForRunStatus(t, dataStore, retryRun.ID, store.RunStatusFailed)
}

func TestCollectTaskBlockingDiscard(t *testing.T) {
	dataStore := openTestStore(t)
	srv, handler := newRunTestServer(t, dataStore, func(string) *exec.Cmd { return slowCommand() }, 2)
	token := loginToken(t, handler, "admin", "secret")
	job := prepareRunnableJob(t, handler, token)

	task := store.CollectTask{
		Name:           "discard-task",
		SyncJobID:      job.ID,
		CronExpr:       "0 0 0 1 1 ?",
		Enabled:        true,
		BlockingPolicy: store.BlockingDiscard,
	}
	if err := dataStore.CreateCollectTask(&task); err != nil {
		t.Fatalf("CreateCollectTask() failed: %v", err)
	}
	jobDef, err := dataStore.GetSyncJob(job.ID)
	if err != nil {
		t.Fatalf("GetSyncJob() failed: %v", err)
	}
	config, err := srv.exportConfig(jobDef)
	if err != nil {
		t.Fatalf("exportConfig() failed: %v", err)
	}

	first, err := srv.runCollectTask(task, jobDef, config, store.RunTriggerSchedule)
	if err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	if _, err := srv.runCollectTask(task, jobDef, config, store.RunTriggerSchedule); err != errCollectTaskBlocked {
		t.Fatalf("second run error = %v, want errCollectTaskBlocked", err)
	}

	if err := srv.executor.Cancel(first.ID); err != nil {
		t.Fatalf("Cancel() failed: %v", err)
	}
	waitForRunStatus(t, dataStore, first.ID, store.RunStatusCanceled)
}
