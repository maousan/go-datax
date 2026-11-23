package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/longkeyy/go-datax/internal/execution"
	"github.com/longkeyy/go-datax/internal/store"
)

func shellCommand(scriptWindows, scriptUnix string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/c", scriptWindows)
	}
	return exec.Command("sh", "-c", scriptUnix)
}

func successCommand() *exec.Cmd {
	return shellCommand("echo hello-run", "echo hello-run")
}

func failingCommand() *exec.Cmd {
	return shellCommand("echo boom & exit 3", "echo boom; exit 3")
}

func streamingCommand() *exec.Cmd {
	return shellCommand(
		"echo first & ping -n 3 127.0.0.1 >nul & echo second",
		"echo first; sleep 2; echo second",
	)
}

func slowCommand() *exec.Cmd {
	return shellCommand("echo started & ping -n 30 127.0.0.1 >nul", "echo started; sleep 30")
}

func newRunTestServer(t *testing.T, dataStore *store.Store, builder func(string) *exec.Cmd, maxConcurrent int) (*Server, http.Handler) {
	t.Helper()

	manager, err := execution.New(execution.Config{
		DataXBin:      "fake-datax",
		DataXVersion:  "fake-1.0",
		LogsDir:       t.TempDir(),
		SnapshotsDir:  t.TempDir(),
		MaxConcurrent: maxConcurrent,
		MaxRuns:       100,
		MaxAgeDays:    30,
		MaxLogBytes:   1 << 20,
	}, dataStore, execution.WithCommandBuilder(builder))
	if err != nil {
		t.Fatalf("execution.New() failed: %v", err)
	}
	return newTestServerWithExecutor(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	}, testAuthConfig(t), dataStore, manager)
}

func prepareRunnableJob(t *testing.T, handler http.Handler, token string) syncJobResponse {
	t.Helper()

	dir := t.TempDir()
	reader := createSQLiteDataSourceForJob(t, handler, token, "reader", dir)
	writer := createSQLiteDataSourceForJob(t, handler, token, "writer", dir)

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs", token,
		formJobBody("runnable", reader.ID, writer.ID))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create job status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var job syncJobResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatalf("job response is not JSON: %v", err)
	}
	return job
}

func startRun(t *testing.T, handler http.Handler, token string, jobID int64) runResponse {
	t.Helper()

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs/"+itoa(jobID)+"/runs", token, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("start run status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var run runResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
		t.Fatalf("run response is not JSON: %v", err)
	}
	return run
}

func waitForRunStatus(t *testing.T, dataStore *store.Store, runID int64, want string) store.JobRun {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	var run store.JobRun
	var err error
	for time.Now().Before(deadline) {
		run, err = dataStore.GetJobRun(runID)
		if err != nil {
			t.Fatalf("GetJobRun() failed: %v", err)
		}
		if run.Status == want {
			return run
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("run %d status = %q, want %q", runID, run.Status, want)
	return run
}

func TestRunLifecycleSuccess(t *testing.T) {
	dataStore := openTestStore(t)
	_, handler := newRunTestServer(t, dataStore, func(string) *exec.Cmd { return successCommand() }, 2)
	token := loginToken(t, handler, "admin", "secret")
	job := prepareRunnableJob(t, handler, token)

	run := startRun(t, handler, token, job.ID)
	finished := waitForRunStatus(t, dataStore, run.ID, store.RunStatusSucceeded)

	if finished.ExitCode == nil || *finished.ExitCode != 0 {
		t.Errorf("exit code = %v, want 0", finished.ExitCode)
	}

	rec := authedJSONRequest(t, handler, http.MethodGet, "/api/v1/runs/"+itoa(run.ID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get run status = %d", rec.Code)
	}
	var detail runResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("run detail is not JSON: %v", err)
	}
	if detail.Status != store.RunStatusSucceeded {
		t.Errorf("status = %q, want succeeded", detail.Status)
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/runs/"+itoa(run.ID)+"/log", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("log download status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "hello-run") {
		t.Errorf("log does not contain the process output: %q", rec.Body.String())
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/runs/"+itoa(run.ID)+"/snapshot", token, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"job"`) {
		t.Errorf("snapshot response is wrong: %d %q", rec.Code, rec.Body.String())
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/runs?jobId="+itoa(job.ID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list runs status = %d", rec.Code)
	}
	var list struct {
		Items []runResponse `json:"items"`
		Total int64         `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("list runs is not JSON: %v", err)
	}
	if list.Total != 1 || len(list.Items) != 1 {
		t.Errorf("list = %+v, want one run", list)
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/overview", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("overview status = %d", rec.Code)
	}
	var overview struct {
		DataSourceCount int64         `json:"dataSourceCount"`
		SyncJobCount    int64         `json:"syncJobCount"`
		TotalRuns       int64         `json:"totalRuns"`
		SuccessRate     float64       `json:"successRate"`
		RecentRuns      []runResponse `json:"recentRuns"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &overview); err != nil {
		t.Fatalf("overview is not JSON: %v", err)
	}
	if overview.DataSourceCount != 2 || overview.SyncJobCount != 1 {
		t.Errorf("overview counts = %+v", overview)
	}
	if overview.TotalRuns != 1 || overview.SuccessRate != 1 || len(overview.RecentRuns) != 1 {
		t.Errorf("overview runs = %+v", overview)
	}
}

func TestRunFailureRecordsSummary(t *testing.T) {
	dataStore := openTestStore(t)
	_, handler := newRunTestServer(t, dataStore, func(string) *exec.Cmd { return failingCommand() }, 2)
	token := loginToken(t, handler, "admin", "secret")
	job := prepareRunnableJob(t, handler, token)

	run := startRun(t, handler, token, job.ID)
	finished := waitForRunStatus(t, dataStore, run.ID, store.RunStatusFailed)

	if finished.ExitCode == nil || *finished.ExitCode != 3 {
		t.Errorf("exit code = %v, want 3", finished.ExitCode)
	}
	if !strings.Contains(finished.ErrorSummary, "boom") {
		t.Errorf("error summary = %q, want the last log line", finished.ErrorSummary)
	}
}

func TestRunCancel(t *testing.T) {
	dataStore := openTestStore(t)
	_, handler := newRunTestServer(t, dataStore, func(string) *exec.Cmd { return slowCommand() }, 2)
	token := loginToken(t, handler, "admin", "secret")
	job := prepareRunnableJob(t, handler, token)

	run := startRun(t, handler, token, job.ID)

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/runs/"+itoa(run.ID)+"/cancel", token, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("cancel status = %d, body = %s", rec.Code, rec.Body.String())
	}
	waitForRunStatus(t, dataStore, run.ID, store.RunStatusCanceled)

	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/runs/"+itoa(run.ID)+"/cancel", token, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("second cancel status = %d, want 200", rec.Code)
	}
}

func TestRunSingleFlight(t *testing.T) {
	dataStore := openTestStore(t)
	_, handler := newRunTestServer(t, dataStore, func(string) *exec.Cmd { return slowCommand() }, 2)
	token := loginToken(t, handler, "admin", "secret")
	job := prepareRunnableJob(t, handler, token)

	run := startRun(t, handler, token, job.ID)

	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs/"+itoa(job.ID)+"/runs", token, nil)
	if rec.Code != http.StatusConflict {
		t.Errorf("second start status = %d, want 409 (body=%s)", rec.Code, rec.Body.String())
	}

	_ = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/runs/"+itoa(run.ID)+"/cancel", token, nil)
	waitForRunStatus(t, dataStore, run.ID, store.RunStatusCanceled)
}

func TestRunGlobalConcurrencyLimit(t *testing.T) {
	dataStore := openTestStore(t)
	_, handler := newRunTestServer(t, dataStore, func(string) *exec.Cmd { return slowCommand() }, 1)
	token := loginToken(t, handler, "admin", "secret")
	firstJob := prepareRunnableJob(t, handler, token)

	run := startRun(t, handler, token, firstJob.ID)

	second := formJobBody("second", firstJob.Form.Reader.DataSourceID, firstJob.Form.Writer.DataSourceID)
	rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs", token, second)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create second job status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var secondJob syncJobResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &secondJob); err != nil {
		t.Fatalf("second job response is not JSON: %v", err)
	}

	rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/sync-jobs/"+itoa(secondJob.ID)+"/runs", token, nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("second job start status = %d, want 429 (body=%s)", rec.Code, rec.Body.String())
	}

	_ = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/runs/"+itoa(run.ID)+"/cancel", token, nil)
	waitForRunStatus(t, dataStore, run.ID, store.RunStatusCanceled)
}

func TestRunLogStreamReplay(t *testing.T) {
	dataStore := openTestStore(t)
	_, handler := newRunTestServer(t, dataStore, func(string) *exec.Cmd { return successCommand() }, 2)
	token := loginToken(t, handler, "admin", "secret")
	job := prepareRunnableJob(t, handler, token)

	run := startRun(t, handler, token, job.ID)
	waitForRunStatus(t, dataStore, run.ID, store.RunStatusSucceeded)

	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	request, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/api/v1/runs/"+itoa(run.ID)+"/logs/stream", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("stream request failed: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read stream failed: %v", err)
	}

	stream := string(body)
	if !strings.Contains(stream, `"type":"log"`) || !strings.Contains(stream, "hello-run") {
		t.Errorf("stream has no log event: %q", stream)
	}
	if !strings.Contains(stream, `"type":"status"`) || !strings.Contains(stream, `"type":"end"`) {
		t.Errorf("stream has no completion events: %q", stream)
	}
}

func TestRunHistorySurvivesJobDeletion(t *testing.T) {
	dataStore := openTestStore(t)
	_, handler := newRunTestServer(t, dataStore, func(string) *exec.Cmd { return successCommand() }, 2)
	token := loginToken(t, handler, "admin", "secret")
	job := prepareRunnableJob(t, handler, token)

	run := startRun(t, handler, token, job.ID)
	waitForRunStatus(t, dataStore, run.ID, store.RunStatusSucceeded)

	rec := authedJSONRequest(t, handler, http.MethodDelete, "/api/v1/sync-jobs/"+itoa(job.ID), token, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete job status = %d", rec.Code)
	}

	rec = authedJSONRequest(t, handler, http.MethodGet, "/api/v1/runs", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list runs status = %d", rec.Code)
	}
	var list struct {
		Items []runResponse `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("list runs is not JSON: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].SyncJobName != job.Name {
		t.Errorf("history after deletion = %+v, want job name %q preserved", list.Items, job.Name)
	}
}

func TestMarkRunningRunsFailed(t *testing.T) {
	dataStore := openTestStore(t)
	jobID := int64(1)
	run := &store.JobRun{
		SyncJobID:   &jobID,
		SyncJobName: "orphan",
		Status:      store.RunStatusRunning,
		Trigger:     store.RunTriggerManual,
		StartedAt:   time.Now(),
	}
	if err := dataStore.CreateJobRun(run); err != nil {
		t.Fatalf("CreateJobRun() failed: %v", err)
	}

	affected, err := dataStore.MarkRunningRunsFailed("服务重启导致运行中断")
	if err != nil {
		t.Fatalf("MarkRunningRunsFailed() failed: %v", err)
	}
	if affected != 1 {
		t.Errorf("affected = %d, want 1", affected)
	}

	stored, _ := dataStore.GetJobRun(run.ID)
	if stored.Status != store.RunStatusFailed {
		t.Errorf("status = %q, want failed", stored.Status)
	}
}
