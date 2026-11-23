package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/longkeyy/go-datax/internal/execution"
	"github.com/longkeyy/go-datax/internal/store"
)

type runResponse struct {
	ID           int64   `json:"id"`
	SyncJobID    *int64  `json:"syncJobId"`
	SyncJobName  string  `json:"syncJobName"`
	Status       string  `json:"status"`
	Trigger      string  `json:"trigger"`
	StartedAt    string  `json:"startedAt"`
	FinishedAt   *string `json:"finishedAt"`
	ExitCode     *int    `json:"exitCode"`
	ErrorSummary string  `json:"errorSummary"`
	LogSize      int64   `json:"logSize"`
	CreatedAt    string  `json:"createdAt"`
}

func toRunResponse(run store.JobRun) runResponse {
	response := runResponse{
		ID:           run.ID,
		SyncJobID:    run.SyncJobID,
		SyncJobName:  run.SyncJobName,
		Status:       run.Status,
		Trigger:      run.Trigger,
		StartedAt:    run.StartedAt.UTC().Format(time.RFC3339),
		ExitCode:     run.ExitCode,
		ErrorSummary: run.ErrorSummary,
		LogSize:      run.LogSize,
		CreatedAt:    run.CreatedAt.UTC().Format(time.RFC3339),
	}
	if run.FinishedAt != nil {
		finished := run.FinishedAt.UTC().Format(time.RFC3339)
		response.FinishedAt = &finished
	}
	return response
}

func (s *Server) handleStartRun(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if s.executor == nil {
		writeError(w, http.StatusServiceUnavailable, "executor_unavailable", "执行器未启用")
		return
	}

	job, err := s.dataStore.GetSyncJob(id)
	if errors.Is(err, store.ErrSyncJobNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "同步任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取同步任务失败")
		return
	}

	config, err := s.exportConfig(job)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_job", err.Error())
		return
	}

	run, err := s.executor.Start(job, config)
	switch {
	case errors.Is(err, execution.ErrJobRunning):
		writeError(w, http.StatusConflict, "job_running", "该任务正在运行")
		return
	case errors.Is(err, execution.ErrTooManyRuns):
		writeError(w, http.StatusTooManyRequests, "too_many_runs", "并发运行数已达上限，请稍后重试")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal_error", "启动运行失败："+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, toRunResponse(run))
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	filter := store.RunFilter{
		Status: query.Get("status"),
	}
	if value := query.Get("jobId"); value != "" {
		if id, err := strconv.ParseInt(value, 10, 64); err == nil {
			filter.JobID = id
		}
	}
	if value := query.Get("page"); value != "" {
		if page, err := strconv.Atoi(value); err == nil {
			filter.Page = page
		}
	}
	if value := query.Get("pageSize"); value != "" {
		if size, err := strconv.Atoi(value); err == nil && size <= 100 {
			filter.PageSize = size
		}
	}
	if value := query.Get("from"); value != "" {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			filter.From = &parsed
		}
	}
	if value := query.Get("to"); value != "" {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			filter.To = &parsed
		}
	}

	runs, total, err := s.dataStore.ListJobRuns(filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取运行历史失败")
		return
	}

	items := make([]runResponse, 0, len(runs))
	for _, run := range runs {
		items = append(items, toRunResponse(run))
	}

	page := filter.Page
	if page <= 0 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":    items,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	run, err := s.dataStore.GetJobRun(id)
	if errors.Is(err, store.ErrJobRunNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "运行记录不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取运行记录失败")
		return
	}
	writeJSON(w, http.StatusOK, toRunResponse(run))
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	run, err := s.dataStore.GetJobRun(id)
	if errors.Is(err, store.ErrJobRunNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "运行记录不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取运行记录失败")
		return
	}

	if run.Status != store.RunStatusRunning {
		// 幂等：已经结束的运行直接返回当前状态。
		writeJSON(w, http.StatusOK, toRunResponse(run))
		return
	}
	if s.executor == nil {
		writeError(w, http.StatusServiceUnavailable, "executor_unavailable", "执行器未启用")
		return
	}

	_ = s.executor.Cancel(id)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "canceling"})
}

func (s *Server) handleRunLogStream(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if s.executor == nil {
		writeError(w, http.StatusServiceUnavailable, "executor_unavailable", "执行器未启用")
		return
	}

	offset := int64(0)
	if value := r.URL.Query().Get("offset"); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	replay, events, cancel, err := s.executor.Subscribe(id, offset)
	if errors.Is(err, store.ErrJobRunNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "运行记录不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "订阅日志失败")
		return
	}
	defer cancel()

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal_error", "当前连接不支持流式输出")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	for _, event := range replay {
		writeSSEEvent(w, flusher, event)
	}
	if events == nil {
		return
	}

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			writeSSEEvent(w, flusher, event)
			if event.Type == "end" {
				return
			}
		case <-heartbeat.C:
			writeSSEEvent(w, flusher, execution.Event{Type: "ping"})
		}
	}
}

func writeSSEEvent(w io.Writer, flusher http.Flusher, event execution.Event) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

func (s *Server) handleRunLogDownload(w http.ResponseWriter, r *http.Request) {
	run, ok := s.loadRunForDownload(w, r)
	if !ok {
		return
	}
	if run.LogPath == "" {
		writeError(w, http.StatusNotFound, "not_found", "日志文件不存在")
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fmt.Sprintf("run-%d.log", run.ID)))
	http.ServeFile(w, r, run.LogPath)
}

func (s *Server) handleRunSnapshot(w http.ResponseWriter, r *http.Request) {
	run, ok := s.loadRunForDownload(w, r)
	if !ok {
		return
	}
	if run.SnapshotPath == "" {
		writeError(w, http.StatusNotFound, "not_found", "快照文件不存在")
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(run.SnapshotPath)))
	http.ServeFile(w, r, run.SnapshotPath)
}

func (s *Server) loadRunForDownload(w http.ResponseWriter, r *http.Request) (store.JobRun, bool) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return store.JobRun{}, false
	}
	run, err := s.dataStore.GetJobRun(id)
	if errors.Is(err, store.ErrJobRunNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "运行记录不存在")
		return store.JobRun{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取运行记录失败")
		return store.JobRun{}, false
	}
	if _, err := os.Stat(run.LogPath); err != nil && run.LogPath != "" {
		// 文件缺失时继续走到 ServeFile，由它返回 404。
		_ = err
	}
	return run, true
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	hours := 24
	if value := r.URL.Query().Get("hours"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 && parsed <= 168 {
			hours = parsed
		}
	}
	since := time.Now().Add(-time.Duration(hours) * time.Hour)

	dataSourceCount, err := s.dataStore.CountDataSources()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "统计数据源失败")
		return
	}
	syncJobCount, err := s.dataStore.CountSyncJobs()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "统计同步任务失败")
		return
	}
	totalRuns, err := s.dataStore.CountRunsSince("", since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "统计运行记录失败")
		return
	}
	succeededRuns, err := s.dataStore.CountRunsSince(store.RunStatusSucceeded, since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "统计运行记录失败")
		return
	}
	recent, err := s.dataStore.ListRecentRuns(10)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取最近运行失败")
		return
	}

	successRate := 0.0
	if totalRuns > 0 {
		successRate = float64(succeededRuns) / float64(totalRuns)
	}

	recentItems := make([]runResponse, 0, len(recent))
	for _, run := range recent {
		recentItems = append(recentItems, toRunResponse(run))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"dataSourceCount": dataSourceCount,
		"syncJobCount":    syncJobCount,
		"windowHours":     hours,
		"totalRuns":       totalRuns,
		"successRate":     successRate,
		"recentRuns":      recentItems,
	})
}
