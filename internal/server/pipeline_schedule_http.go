package server

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/longkeyy/go-datax/internal/quartzcron"
	"github.com/longkeyy/go-datax/internal/store"
)

type pipelineScheduleRequest struct {
	Name                 string `json:"name"`
	CronExpr             string `json:"cronExpr"`
	Timezone             string `json:"timezone"`
	Enabled              bool   `json:"enabled"`
	BlockingPolicy       string `json:"blockingPolicy"`
	RetryMax             int    `json:"retryMax"`
	RetryIntervalSeconds int    `json:"retryIntervalSeconds"`
	TimeoutSeconds       int    `json:"timeoutSeconds"`
	VersionID            int64  `json:"versionId"`
}

type pipelineScheduleResponse struct {
	ID                   int64                `json:"id"`
	PipelineID           int64                `json:"pipelineId"`
	PipelineName         string               `json:"pipelineName"`
	Name                 string               `json:"name"`
	CronExpr             string               `json:"cronExpr"`
	Timezone             string               `json:"timezone"`
	Enabled              bool                 `json:"enabled"`
	BlockingPolicy       string               `json:"blockingPolicy"`
	RetryMax             int                  `json:"retryMax"`
	RetryIntervalSeconds int                  `json:"retryIntervalSeconds"`
	TimeoutSeconds       int                  `json:"timeoutSeconds"`
	PinnedVersionID      *int64               `json:"pinnedVersionId"`
	PinnedVersionNumber  int                  `json:"pinnedVersionNumber"`
	NextRunAt            *string              `json:"nextRunAt"`
	LastRun              *pipelineRunResponse `json:"lastRun"`
	CreatedAt            string               `json:"createdAt"`
	UpdatedAt            string               `json:"updatedAt"`
}

func (req pipelineScheduleRequest) normalized() (pipelineScheduleRequest, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return req, fmt.Errorf("名称不能为空")
	}
	if len([]rune(req.Name)) > 100 {
		return req, fmt.Errorf("名称不能超过 100 个字符")
	}
	req.CronExpr = strings.TrimSpace(req.CronExpr)
	if req.CronExpr == "" {
		return req, fmt.Errorf("cron 表达式不能为空")
	}
	if len([]rune(req.CronExpr)) > 100 {
		return req, fmt.Errorf("cron 表达式不能超过 100 个字符")
	}
	req.Timezone = strings.TrimSpace(req.Timezone)
	if req.Timezone == "" {
		req.Timezone = time.Local.String()
		if _, fallbackErr := time.LoadLocation(req.Timezone); fallbackErr != nil {
			req.Timezone = "UTC"
		}
	}
	timing, err := parsePipelineScheduleTiming(req.CronExpr, req.Timezone)
	if err != nil {
		return req, err
	}
	if timing.next(time.Now()).IsZero() {
		return req, fmt.Errorf("cron 表达式在未来 %d 年内没有可执行时间", quartzcron.MaxLookaheadYears)
	}

	req.BlockingPolicy = strings.ToLower(strings.TrimSpace(req.BlockingPolicy))
	if req.BlockingPolicy == "" {
		req.BlockingPolicy = store.BlockingDiscard
	}
	switch req.BlockingPolicy {
	case store.BlockingDiscard, store.BlockingOverwrite, store.BlockingQueue:
	default:
		return req, fmt.Errorf("阻塞策略必须是 discard、overwrite 或 queue")
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
	if req.VersionID < 0 {
		return req, fmt.Errorf("pin 版本不正确")
	}
	return req, nil
}

func (s *Server) handleListPipelineSchedules(w http.ResponseWriter, r *http.Request) {
	pipelineID, _ := strconv.ParseInt(r.URL.Query().Get("pipelineId"), 10, 64)
	schedules, err := s.dataStore.ListPipelineSchedules(r.URL.Query().Get("query"), pipelineID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取调度失败")
		return
	}
	pipelines, err := s.dataStore.ListPipelines("")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取 Pipeline 失败")
		return
	}
	names := make(map[int64]string, len(pipelines))
	for _, pipeline := range pipelines {
		names[pipeline.ID] = pipeline.Name
	}
	ids := make([]int64, 0, len(schedules))
	for _, schedule := range schedules {
		ids = append(ids, schedule.ID)
	}
	latestRuns, err := s.dataStore.LatestPipelineRunsByScheduleIDs(ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取最近运行失败")
		return
	}
	lastBySchedule := make(map[int64]store.PipelineRun, len(latestRuns))
	for _, run := range latestRuns {
		if run.ScheduleID != nil {
			lastBySchedule[*run.ScheduleID] = run
		}
	}
	response := make([]pipelineScheduleResponse, 0, len(schedules))
	for _, schedule := range schedules {
		var last *store.PipelineRun
		if run, ok := lastBySchedule[schedule.ID]; ok {
			last = &run
		}
		item, err := s.toPipelineScheduleResponse(schedule, names[schedule.PipelineID], last)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "读取调度失败")
			return
		}
		response = append(response, item)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleGetPipelineSchedule(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	schedule, err := s.dataStore.GetPipelineSchedule(id)
	if errors.Is(err, store.ErrPipelineScheduleNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "调度不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取调度失败")
		return
	}
	pipeline, err := s.dataStore.GetPipeline(schedule.PipelineID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取 Pipeline 失败")
		return
	}
	last, found, err := s.dataStore.LatestPipelineRunForSchedule(schedule.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取最近运行失败")
		return
	}
	var lastPtr *store.PipelineRun
	if found {
		lastPtr = &last
	}
	response, err := s.toPipelineScheduleResponse(schedule, pipeline.Name, lastPtr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取调度失败")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleCreatePipelineSchedule(w http.ResponseWriter, r *http.Request) {
	pipelineID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	pipeline, err := s.dataStore.GetPipeline(pipelineID)
	if errors.Is(err, store.ErrPipelineNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Pipeline 不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取 Pipeline 失败")
		return
	}
	var req pipelineScheduleRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}
	normalized, err := req.normalized()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	pinnedVersionID, err := s.validateScheduleVersion(pipeline.ID, normalized.VersionID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	schedule := &store.PipelineSchedule{
		PipelineID:           pipeline.ID,
		Name:                 normalized.Name,
		CronExpr:             normalized.CronExpr,
		Timezone:             normalized.Timezone,
		Enabled:              normalized.Enabled,
		BlockingPolicy:       normalized.BlockingPolicy,
		RetryMax:             normalized.RetryMax,
		RetryIntervalSeconds: normalized.RetryIntervalSeconds,
		TimeoutSeconds:       normalized.TimeoutSeconds,
		PinnedVersionID:      pinnedVersionID,
	}
	if err := s.dataStore.CreatePipelineSchedule(schedule); err != nil {
		if errors.Is(err, store.ErrDuplicatePipelineScheduleName) {
			writeError(w, http.StatusConflict, "duplicate_name", "该 Pipeline 下调度名称已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "创建调度失败")
		return
	}
	s.reloadPipelineScheduler()
	response, err := s.toPipelineScheduleResponse(*schedule, pipeline.Name, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取调度失败")
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Server) handleUpdatePipelineSchedule(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	schedule, err := s.dataStore.GetPipelineSchedule(id)
	if errors.Is(err, store.ErrPipelineScheduleNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "调度不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取调度失败")
		return
	}
	pipeline, err := s.dataStore.GetPipeline(schedule.PipelineID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取 Pipeline 失败")
		return
	}
	var req pipelineScheduleRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}
	normalized, err := req.normalized()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	pinnedVersionID, err := s.validateScheduleVersion(schedule.PipelineID, normalized.VersionID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	schedule.Name = normalized.Name
	schedule.CronExpr = normalized.CronExpr
	schedule.Timezone = normalized.Timezone
	schedule.Enabled = normalized.Enabled
	schedule.BlockingPolicy = normalized.BlockingPolicy
	schedule.RetryMax = normalized.RetryMax
	schedule.RetryIntervalSeconds = normalized.RetryIntervalSeconds
	schedule.TimeoutSeconds = normalized.TimeoutSeconds
	schedule.PinnedVersionID = pinnedVersionID
	if err := s.dataStore.UpdatePipelineSchedule(&schedule); err != nil {
		if errors.Is(err, store.ErrDuplicatePipelineScheduleName) {
			writeError(w, http.StatusConflict, "duplicate_name", "该 Pipeline 下调度名称已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "更新调度失败")
		return
	}
	s.reloadPipelineScheduler()
	last, found, err := s.dataStore.LatestPipelineRunForSchedule(schedule.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取最近运行失败")
		return
	}
	var lastPtr *store.PipelineRun
	if found {
		lastPtr = &last
	}
	response, err := s.toPipelineScheduleResponse(schedule, pipeline.Name, lastPtr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取调度失败")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleDeletePipelineSchedule(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := s.dataStore.DeletePipelineSchedule(id); errors.Is(err, store.ErrPipelineScheduleNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "调度不存在")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "删除调度失败")
		return
	}
	s.reloadPipelineScheduler()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) validateScheduleVersion(pipelineID, versionID int64) (*int64, error) {
	if versionID == 0 {
		return nil, nil
	}
	version, err := s.dataStore.GetPipelineVersion(versionID)
	if errors.Is(err, store.ErrPipelineVersionNotFound) {
		return nil, fmt.Errorf("pin 版本不存在")
	}
	if err != nil {
		return nil, fmt.Errorf("读取 pin 版本失败")
	}
	if version.PipelineID != pipelineID {
		return nil, fmt.Errorf("pin 版本不属于该 Pipeline")
	}
	return &versionID, nil
}

func (s *Server) toPipelineScheduleResponse(schedule store.PipelineSchedule, pipelineName string, last *store.PipelineRun) (pipelineScheduleResponse, error) {
	response := pipelineScheduleResponse{
		ID:                   schedule.ID,
		PipelineID:           schedule.PipelineID,
		PipelineName:         pipelineName,
		Name:                 schedule.Name,
		CronExpr:             schedule.CronExpr,
		Timezone:             schedule.Timezone,
		Enabled:              schedule.Enabled,
		BlockingPolicy:       schedule.BlockingPolicy,
		RetryMax:             schedule.RetryMax,
		RetryIntervalSeconds: schedule.RetryIntervalSeconds,
		TimeoutSeconds:       schedule.TimeoutSeconds,
		PinnedVersionID:      schedule.PinnedVersionID,
		CreatedAt:            schedule.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:            schedule.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if schedule.PinnedVersionID != nil {
		if version, err := s.dataStore.GetPipelineVersion(*schedule.PinnedVersionID); err == nil {
			response.PinnedVersionNumber = version.VersionNumber
		}
	}
	if schedule.Enabled {
		if timing, err := parsePipelineScheduleTiming(schedule.CronExpr, schedule.Timezone); err == nil {
			if next := timing.next(time.Now()); !next.IsZero() {
				formatted := next.UTC().Format(time.RFC3339)
				response.NextRunAt = &formatted
			}
		}
	}
	if last != nil {
		run := toPipelineRunResponse(*last)
		response.LastRun = &run
	}
	return response, nil
}

func (s *Server) reloadPipelineScheduler() {
	if s.pipelineScheduler != nil {
		s.pipelineScheduler.Reload()
	}
}
