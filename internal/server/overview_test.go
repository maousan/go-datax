package server

import (
	"encoding/json"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/longkeyy/go-datax/internal/store"
)

func createOverviewJob(t *testing.T, dataStore *store.Store, name string) store.SyncJob {
	t.Helper()

	job := store.SyncJob{
		Name:          name,
		AuthoringMode: "raw",
		Definition:    `{"job":{"content":[{"reader":{"name":"streamreader","parameter":{}},"writer":{"name":"streamwriter","parameter":{}}}]}}`,
	}
	if err := dataStore.CreateSyncJob(&job); err != nil {
		t.Fatalf("CreateSyncJob() failed: %v", err)
	}
	return job
}

func seedOverviewRun(t *testing.T, dataStore *store.Store, job store.SyncJob, status string, started time.Time, progress *store.RunProgress) store.JobRun {
	t.Helper()

	jobID := job.ID
	finished := started.Add(time.Minute)
	run := store.JobRun{
		SyncJobID:   &jobID,
		SyncJobName: job.Name,
		Status:      status,
		Trigger:     store.RunTriggerManual,
		StartedAt:   started,
		FinishedAt:  &finished,
	}
	if progress != nil {
		data, err := json.Marshal(progress)
		if err != nil {
			t.Fatalf("marshal progress: %v", err)
		}
		run.Progress = string(data)
	}
	if err := dataStore.CreateJobRun(&run); err != nil {
		t.Fatalf("CreateJobRun() failed: %v", err)
	}
	return run
}

func TestOverviewAggregation(t *testing.T) {
	dataStore := openTestStore(t)
	_, handler := newTestServerWithStore(t, map[string]string{
		"index.html": "<html><body>app-shell</body></html>",
	}, testAuthConfig(t), dataStore)
	token := loginToken(t, handler, "admin", "secret")

	job1 := createOverviewJob(t, dataStore, "job-one")
	job2 := createOverviewJob(t, dataStore, "job-two")

	// Keep the fixture away from the local midnight boundary so the 24-hour
	// window and daily buckets stay stable even when the test crosses midnight.
	now := time.Now().Add(-2 * time.Hour)
	seedOverviewRun(t, dataStore, job1, store.RunStatusSucceeded, now, &store.RunProgress{ReadRecords: 100, ReadBytes: 1000})
	seedOverviewRun(t, dataStore, job1, store.RunStatusFailed, now.Add(-time.Minute), &store.RunProgress{ReadRecords: 30, ReadBytes: 300})
	seedOverviewRun(t, dataStore, job2, store.RunStatusFailed, now.Add(-2*time.Minute), nil)
	seedOverviewRun(t, dataStore, job2, store.RunStatusSucceeded, now.AddDate(0, 0, -1), &store.RunProgress{ReadRecords: 50, ReadBytes: 500})
	seedOverviewRun(t, dataStore, job1, store.RunStatusFailed, now.AddDate(0, 0, -20), nil)

	rec := authedJSONRequest(t, handler, http.MethodGet, "/api/v1/overview?hours=24", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("overview status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var overview struct {
		DataSourceCount int64            `json:"dataSourceCount"`
		SyncJobCount    int64            `json:"syncJobCount"`
		TotalRuns       int64            `json:"totalRuns"`
		SuccessRate     float64          `json:"successRate"`
		StatusCounts    map[string]int64 `json:"statusCounts"`
		RunningRuns     int64            `json:"runningRuns"`
		FailedRuns      int64            `json:"failedRuns"`
		ReadRecords     int64            `json:"readRecords"`
		ReadBytes       int64            `json:"readBytes"`
		DailyStats      []struct {
			Date        string `json:"date"`
			Total       int64  `json:"total"`
			Succeeded   int64  `json:"succeeded"`
			Failed      int64  `json:"failed"`
			Canceled    int64  `json:"canceled"`
			ReadRecords int64  `json:"readRecords"`
		} `json:"dailyStats"`
		FailedJobs []struct {
			JobID    int64  `json:"jobId"`
			JobName  string `json:"jobName"`
			Failures int64  `json:"failures"`
		} `json:"failedJobs"`
		RecentRuns []runResponse `json:"recentRuns"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &overview); err != nil {
		t.Fatalf("overview is not JSON: %v", err)
	}

	if overview.DataSourceCount != 0 || overview.SyncJobCount != 2 {
		t.Errorf("counts = %+v", overview)
	}
	if overview.TotalRuns != 3 {
		t.Errorf("totalRuns = %d, want 3 (24h window)", overview.TotalRuns)
	}
	if overview.StatusCounts[store.RunStatusSucceeded] != 1 ||
		overview.StatusCounts[store.RunStatusFailed] != 2 ||
		overview.StatusCounts[store.RunStatusCanceled] != 0 ||
		overview.StatusCounts[store.RunStatusRunning] != 0 {
		t.Errorf("statusCounts = %+v", overview.StatusCounts)
	}
	if overview.RunningRuns != 0 || overview.FailedRuns != 2 {
		t.Errorf("running/failed = %d/%d, want 0/2", overview.RunningRuns, overview.FailedRuns)
	}
	if diff := overview.SuccessRate - 1.0/3.0; diff > 0.001 || diff < -0.001 {
		t.Errorf("successRate = %v, want 1/3", overview.SuccessRate)
	}
	if overview.ReadRecords != 130 || overview.ReadBytes != 1300 {
		t.Errorf("window volume = %d records / %d bytes, want 130/1300", overview.ReadRecords, overview.ReadBytes)
	}

	if len(overview.DailyStats) != 14 {
		t.Fatalf("dailyStats length = %d, want 14", len(overview.DailyStats))
	}
	today := now.Local().Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Local().Format("2006-01-02")
	var todayStats, yesterdayStats *struct {
		Date        string `json:"date"`
		Total       int64  `json:"total"`
		Succeeded   int64  `json:"succeeded"`
		Failed      int64  `json:"failed"`
		Canceled    int64  `json:"canceled"`
		ReadRecords int64  `json:"readRecords"`
	}
	for i := range overview.DailyStats {
		switch overview.DailyStats[i].Date {
		case today:
			todayStats = &overview.DailyStats[i]
		case yesterday:
			yesterdayStats = &overview.DailyStats[i]
		}
	}
	if todayStats == nil || todayStats.Total != 3 || todayStats.Succeeded != 1 || todayStats.Failed != 2 || todayStats.ReadRecords != 130 {
		t.Errorf("today stats = %+v", todayStats)
	}
	if yesterdayStats == nil || yesterdayStats.Total != 1 || yesterdayStats.Succeeded != 1 || yesterdayStats.ReadRecords != 50 {
		t.Errorf("yesterday stats = %+v", yesterdayStats)
	}

	if len(overview.FailedJobs) != 2 {
		t.Fatalf("failedJobs = %+v, want 2 jobs", overview.FailedJobs)
	}
	failures := map[string]int64{}
	for _, job := range overview.FailedJobs {
		failures[job.JobName] = job.Failures
	}
	if failures["job-one"] != 1 || failures["job-two"] != 1 {
		t.Errorf("failedJobs = %+v, want one failure each", overview.FailedJobs)
	}
	if len(overview.RecentRuns) != 5 {
		t.Errorf("recentRuns = %d, want 5", len(overview.RecentRuns))
	}

	// 数据量与成功率应使用同一个时间窗口。
	if math.Abs(float64(overview.ReadRecords)-130) > 0.001 {
		t.Errorf("readRecords = %d", overview.ReadRecords)
	}
}
