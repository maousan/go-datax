package server

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/longkeyy/go-datax/internal/execution"
	"github.com/longkeyy/go-datax/internal/quartzcron"
	"github.com/longkeyy/go-datax/internal/store"
)

var errCollectTaskBlocked = errors.New("collect task is already running")

type collectTaskRequest struct {
	Name                 string `json:"name"`
	SyncJobID            int64  `json:"syncJobId"`
	CronExpr             string `json:"cronExpr"`
	Enabled              bool   `json:"enabled"`
	BlockingPolicy       string `json:"blockingPolicy"`
	RetryMax             int    `json:"retryMax"`
	RetryIntervalSeconds int    `json:"retryIntervalSeconds"`
	TimeoutSeconds       int    `json:"timeoutSeconds"`
}

type collectTaskResponse struct {
	ID                   int64        `json:"id"`
	Name                 string       `json:"name"`
	SyncJobID            int64        `json:"syncJobId"`
	SyncJobName          string       `json:"syncJobName"`
	CronExpr             string       `json:"cronExpr"`
	Enabled              bool         `json:"enabled"`
	BlockingPolicy       string       `json:"blockingPolicy"`
	RetryMax             int          `json:"retryMax"`
	RetryIntervalSeconds int          `json:"retryIntervalSeconds"`
	TimeoutSeconds       int          `json:"timeoutSeconds"`
	NextRunAt            *string      `json:"nextRunAt"`
	LastRun              *runResponse `json:"lastRun,omitempty"`
	CreatedAt            string       `json:"createdAt"`
	UpdatedAt            string       `json:"updatedAt"`
}

func (req collectTaskRequest) normalized() (collectTaskRequest, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return req, fmt.Errorf("名称不能为空")
	}
	if len([]rune(req.Name)) > 100 {
		return req, fmt.Errorf("名称不能超过 100 个字符")
	}
	if req.SyncJobID <= 0 {
		return req, fmt.Errorf("必须选择datax任务定义")
	}
	req.CronExpr = strings.TrimSpace(req.CronExpr)
	if req.CronExpr == "" {
		return req, fmt.Errorf("cron 表达式不能为空")
	}
	if len([]rune(req.CronExpr)) > 100 {
		return req, fmt.Errorf("cron 表达式不能超过 100 个字符")
	}
	schedule, err := quartzcron.Parse(req.CronExpr)
	if err != nil {
		return req, fmt.Errorf("cron 表达式不正确: %v", err)
	}
	if schedule.Next(time.Now()).IsZero() {
		return req, fmt.Errorf("cron 表达式在未来 8 年内没有可执行时间")
	}

	req.BlockingPolicy = strings.ToLower(strings.TrimSpace(req.BlockingPolicy))
	if req.BlockingPolicy == "" {
		req.BlockingPolicy = store.BlockingDiscard
	}
	if req.BlockingPolicy != store.BlockingDiscard && req.BlockingPolicy != store.BlockingOverwrite {
		return req, fmt.Errorf("阻塞策略必须是 discard 或 overwrite")
	}
	if req.RetryMax < 0 || req.RetryMax > 10 {
		return req, fmt.Errorf("失败重试次数必须在 0 到 10 之间")
	}
	if req.RetryIntervalSeconds < 0 || req.RetryIntervalSeconds > 86400 {
		return req, fmt.Errorf("重试间隔必须在 0 到 86400 秒之间")
	}
	if req.TimeoutSeconds < 0 || req.TimeoutSeconds > 86400 {
		return req, fmt.Errorf("超时时间必须在 0 到 86400 秒之间")
	}
	return req, nil
}

func (s *Server) handleListCollectTasks(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	var syncJobID int64
	if value := query.Get("syncJobId"); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil && parsed > 0 {
			syncJobID = parsed
		}
	}

	tasks, err := s.dataStore.ListCollectTasks(query.Get("query"), syncJobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取采集任务失败")
		return
	}

	jobNames := map[int64]string{}
	jobs, err := s.dataStore.ListSyncJobs("")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取datax任务失败")
		return
	}
	for _, job := range jobs {
		jobNames[job.ID] = job.Name
	}
	taskIDs := make([]int64, 0, len(tasks))
	for _, task := range tasks {
		taskIDs = append(taskIDs, task.ID)
	}
	latestRuns, err := s.dataStore.LatestRunsForCollectTasks(taskIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取最近运行失败")
		return
	}

	response := make([]collectTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		var lastRun *store.JobRun
		if run, ok := latestRuns[task.ID]; ok {
			lastRun = &run
		}
		response = append(response, toCollectTaskResponse(task, jobNames[task.SyncJobID], lastRun))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleGetCollectTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	task, err := s.dataStore.GetCollectTask(id)
	if errors.Is(err, store.ErrCollectTaskNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "采集任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取采集任务失败")
		return
	}
	jobName := ""
	if job, err := s.dataStore.GetSyncJob(task.SyncJobID); err == nil {
		jobName = job.Name
	}
	lastRun, found, err := s.dataStore.LatestRunForCollectTask(task.ID)
	var lastRunPtr *store.JobRun
	if err == nil && found {
		lastRunPtr = &lastRun
	}
	writeJSON(w, http.StatusOK, toCollectTaskResponse(task, jobName, lastRunPtr))
}

func (s *Server) handleCreateCollectTask(w http.ResponseWriter, r *http.Request) {
	var req collectTaskRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}
	normalized, err := req.normalized()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if _, err := s.dataStore.GetSyncJob(normalized.SyncJobID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "datax任务定义不存在")
		return
	}

	task := &store.CollectTask{
		Name:                 normalized.Name,
		SyncJobID:            normalized.SyncJobID,
		CronExpr:             normalized.CronExpr,
		Enabled:              normalized.Enabled,
		BlockingPolicy:       normalized.BlockingPolicy,
		RetryMax:             normalized.RetryMax,
		RetryIntervalSeconds: normalized.RetryIntervalSeconds,
		TimeoutSeconds:       normalized.TimeoutSeconds,
	}
	if err := s.dataStore.CreateCollectTask(task); err != nil {
		if errors.Is(err, store.ErrDuplicateCollectTaskName) {
			writeError(w, http.StatusConflict, "duplicate_name", "采集任务名称已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "创建采集任务失败")
		return
	}
	s.reloadScheduler()

	job, _ := s.dataStore.GetSyncJob(task.SyncJobID)
	writeJSON(w, http.StatusCreated, toCollectTaskResponse(*task, job.Name, nil))
}

func (s *Server) handleUpdateCollectTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	existing, err := s.dataStore.GetCollectTask(id)
	if errors.Is(err, store.ErrCollectTaskNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "采集任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取采集任务失败")
		return
	}

	var req collectTaskRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}
	normalized, err := req.normalized()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if _, err := s.dataStore.GetSyncJob(normalized.SyncJobID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "datax任务定义不存在")
		return
	}

	existing.Name = normalized.Name
	existing.SyncJobID = normalized.SyncJobID
	existing.CronExpr = normalized.CronExpr
	existing.Enabled = normalized.Enabled
	existing.BlockingPolicy = normalized.BlockingPolicy
	existing.RetryMax = normalized.RetryMax
	existing.RetryIntervalSeconds = normalized.RetryIntervalSeconds
	existing.TimeoutSeconds = normalized.TimeoutSeconds
	if err := s.dataStore.UpdateCollectTask(&existing); err != nil {
		if errors.Is(err, store.ErrDuplicateCollectTaskName) {
			writeError(w, http.StatusConflict, "duplicate_name", "采集任务名称已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "更新采集任务失败")
		return
	}
	s.reloadScheduler()

	job, _ := s.dataStore.GetSyncJob(existing.SyncJobID)
	lastRun, found, _ := s.dataStore.LatestRunForCollectTask(existing.ID)
	var lastRunPtr *store.JobRun
	if found {
		lastRunPtr = &lastRun
	}
	writeJSON(w, http.StatusOK, toCollectTaskResponse(existing, job.Name, lastRunPtr))
}

func (s *Server) handleDeleteCollectTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := s.dataStore.DeleteCollectTask(id); err != nil {
		if errors.Is(err, store.ErrCollectTaskNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "采集任务不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "删除采集任务失败")
		return
	}
	s.reloadScheduler()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRunCollectTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if s.executor == nil {
		writeError(w, http.StatusServiceUnavailable, "executor_unavailable", "执行器未启用")
		return
	}
	task, err := s.dataStore.GetCollectTask(id)
	if errors.Is(err, store.ErrCollectTaskNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "采集任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取采集任务失败")
		return
	}
	job, err := s.dataStore.GetSyncJob(task.SyncJobID)
	if errors.Is(err, store.ErrSyncJobNotFound) {
		writeError(w, http.StatusConflict, "job_deleted", "datax任务定义已删除")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取datax任务失败")
		return
	}
	config, err := s.exportConfig(job)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_job", err.Error())
		return
	}

	run, err := s.runCollectTask(task, job, config, store.RunTriggerManual)
	switch {
	case errors.Is(err, errCollectTaskBlocked):
		writeError(w, http.StatusConflict, "task_running", "上一次运行尚未结束，已按丢弃策略跳过")
		return
	case errors.Is(err, execution.ErrJobRunning):
		writeError(w, http.StatusConflict, "job_running", "该任务定义正在运行")
		return
	case errors.Is(err, execution.ErrTooManyRuns):
		writeError(w, http.StatusTooManyRequests, "too_many_runs", "并发运行数已达上限，请稍后重试")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal_error", "启动采集任务失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toRunResponse(run))
}

// runCollectTask 按采集任务的阻塞策略启动一次运行。
func (s *Server) runCollectTask(task store.CollectTask, job store.SyncJob, config map[string]any, trigger string) (store.JobRun, error) {
	if s.executor == nil {
		return store.JobRun{}, fmt.Errorf("执行器未启用")
	}
	if runID, running := s.executor.RunningRunIDForJob(job.ID); running {
		switch task.BlockingPolicy {
		case store.BlockingOverwrite:
			_ = s.executor.Cancel(runID)
			if !s.executor.WaitForJobIdle(job.ID, 5*time.Second) {
				return store.JobRun{}, fmt.Errorf("等待上一次运行结束超时")
			}
		default:
			return store.JobRun{}, errCollectTaskBlocked
		}
	}

	taskID := task.ID
	return s.executor.StartScheduled(job, config, execution.RunOptions{
		Trigger:         trigger,
		RetryMax:        task.RetryMax,
		RetryInterval:   time.Duration(task.RetryIntervalSeconds) * time.Second,
		Timeout:         time.Duration(task.TimeoutSeconds) * time.Second,
		CollectTaskID:   &taskID,
		CollectTaskName: task.Name,
	})
}

func (s *Server) reloadScheduler() {
	if s.scheduler != nil {
		s.scheduler.Reload()
	}
}

func toCollectTaskResponse(task store.CollectTask, jobName string, lastRun *store.JobRun) collectTaskResponse {
	response := collectTaskResponse{
		ID:                   task.ID,
		Name:                 task.Name,
		SyncJobID:            task.SyncJobID,
		SyncJobName:          jobName,
		CronExpr:             task.CronExpr,
		Enabled:              task.Enabled,
		BlockingPolicy:       task.BlockingPolicy,
		RetryMax:             task.RetryMax,
		RetryIntervalSeconds: task.RetryIntervalSeconds,
		TimeoutSeconds:       task.TimeoutSeconds,
		CreatedAt:            task.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:            task.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if task.Enabled {
		if schedule, err := quartzcron.Parse(task.CronExpr); err == nil {
			if next := schedule.Next(time.Now()); !next.IsZero() {
				formatted := next.UTC().Format(time.RFC3339)
				response.NextRunAt = &formatted
			}
		}
	}
	if lastRun != nil {
		run := toRunResponse(*lastRun)
		response.LastRun = &run
	}
	return response
}
