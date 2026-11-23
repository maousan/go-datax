package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/longkeyy/go-datax/internal/store"
)

func (s *Server) handleListSyncJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.dataStore.ListSyncJobs(r.URL.Query().Get("query"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取同步任务失败")
		return
	}

	jobIDs := make([]int64, 0, len(jobs))
	for _, job := range jobs {
		jobIDs = append(jobIDs, job.ID)
	}
	latestRuns, err := s.dataStore.LatestRunsForJobs(jobIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取最近运行失败")
		return
	}

	response := make([]syncJobResponse, 0, len(jobs))
	for _, job := range jobs {
		item, err := toSyncJobResponse(job)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "解析同步任务失败")
			return
		}
		if run, ok := latestRuns[job.ID]; ok {
			item.LastRun = runResponsePtr(run)
		}
		response = append(response, item)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleGetSyncJob(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
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

	response, err := toSyncJobResponse(job)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	lastRun, found, err := s.dataStore.LatestRunForJob(job.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取最近运行失败")
		return
	}
	if found {
		response.LastRun = runResponsePtr(lastRun)
	}
	writeJSON(w, http.StatusOK, response)
}

// handleSyncJobStats 返回任务在最近若干天内的运行统计。
func (s *Server) handleSyncJobStats(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if _, err := s.dataStore.GetSyncJob(id); err != nil {
		if errors.Is(err, store.ErrSyncJobNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "同步任务不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "读取同步任务失败")
		return
	}

	days := 30
	if value := r.URL.Query().Get("days"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 && parsed <= 365 {
			days = parsed
		}
	}

	stats, err := s.dataStore.JobRunStats(id, time.Now().AddDate(0, 0, -days))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "统计运行数据失败")
		return
	}

	// 运行中的记录还没有结果，不计入成功率与平均耗时。
	finished := stats.Total - stats.Running
	successRate := 0.0
	if finished > 0 {
		successRate = float64(stats.Succeeded) / float64(finished)
	}

	var avgDuration any
	if finished > 0 {
		avgDuration = stats.AverageSeconds
	}

	response := map[string]any{
		"jobId":              id,
		"windowDays":         days,
		"total":              stats.Total,
		"succeeded":          stats.Succeeded,
		"failed":             stats.Failed,
		"canceled":           stats.Canceled,
		"running":            stats.Running,
		"successRate":        successRate,
		"avgDurationSeconds": avgDuration,
	}
	if stats.LastFailed != nil {
		response["lastFailedRun"] = toRunResponse(*stats.LastFailed)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleCreateSyncJob(w http.ResponseWriter, r *http.Request) {
	var req syncJobRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}
	s.createSyncJob(w, req)
}

func (s *Server) createSyncJob(w http.ResponseWriter, req syncJobRequest) {
	normalized, err := req.normalized()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	definition, err := s.encodeDefinition(normalized)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	job := &store.SyncJob{
		Name:                 normalized.Name,
		AuthoringMode:        normalized.AuthoringMode,
		Definition:           definition,
		RetryMax:             normalized.RetryMax,
		RetryIntervalSeconds: normalized.RetryIntervalSeconds,
	}
	if err := s.dataStore.CreateSyncJob(job); err != nil {
		if errors.Is(err, store.ErrDuplicateSyncJobName) {
			writeError(w, http.StatusConflict, "duplicate_name", "同步任务名称已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "创建同步任务失败")
		return
	}

	response, err := toSyncJobResponse(*job)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Server) handleUpdateSyncJob(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	existing, err := s.dataStore.GetSyncJob(id)
	if errors.Is(err, store.ErrSyncJobNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "同步任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取同步任务失败")
		return
	}

	var req syncJobRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}

	normalized, err := req.normalized()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if normalized.AuthoringMode != existing.AuthoringMode {
		writeError(w, http.StatusBadRequest, "invalid_request", "任务创建后不能切换编写模式")
		return
	}

	definition, err := s.encodeDefinition(normalized)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	existing.Name = normalized.Name
	existing.Definition = definition
	existing.RetryMax = normalized.RetryMax
	existing.RetryIntervalSeconds = normalized.RetryIntervalSeconds
	if err := s.dataStore.UpdateSyncJob(&existing); err != nil {
		if errors.Is(err, store.ErrDuplicateSyncJobName) {
			writeError(w, http.StatusConflict, "duplicate_name", "同步任务名称已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "更新同步任务失败")
		return
	}

	response, err := toSyncJobResponse(existing)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleDeleteSyncJob(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if s.executor != nil {
		running, err := s.dataStore.HasRunningRun(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "检查运行状态失败")
			return
		}
		if running {
			writeError(w, http.StatusConflict, "job_running", "任务正在运行，无法删除")
			return
		}
	}

	if err := s.dataStore.DeleteSyncJob(id); err != nil {
		if errors.Is(err, store.ErrSyncJobNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "同步任务不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "删除同步任务失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDuplicateSyncJob(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	original, err := s.dataStore.GetSyncJob(id)
	if errors.Is(err, store.ErrSyncJobNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "同步任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取同步任务失败")
		return
	}

	copyJob := &store.SyncJob{
		Name:                 uniqueCopyName(original.Name),
		AuthoringMode:        original.AuthoringMode,
		Definition:           original.Definition,
		RetryMax:             original.RetryMax,
		RetryIntervalSeconds: original.RetryIntervalSeconds,
	}
	if err := s.dataStore.CreateSyncJob(copyJob); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "复制同步任务失败")
		return
	}

	response, err := toSyncJobResponse(*copyJob)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

type importSyncJobRequest struct {
	Name   string          `json:"name"`
	Config json.RawMessage `json:"config"`
}

func (s *Server) handleImportSyncJob(w http.ResponseWriter, r *http.Request) {
	var req importSyncJobRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}
	s.createSyncJob(w, syncJobRequest{
		Name:          req.Name,
		AuthoringMode: "raw",
		Raw:           req.Config,
	})
}

func (s *Server) handleExportSyncJob(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
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
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "导出配置失败")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", job.Name+".json"))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// encodeDefinition 校验数据源并把请求转换为存储用的 JSON。
func (s *Server) encodeDefinition(req syncJobRequest) (string, error) {
	if req.AuthoringMode == "raw" {
		return string(req.Raw), nil
	}

	dataSources, err := s.dataSourcesForForm(*req.Form)
	if err != nil {
		return "", err
	}
	if err := validateFormWriteMode(*req.Form, dataSources, true); err != nil {
		return "", err
	}
	if _, err := materializeForm(*req.Form, dataSources); err != nil {
		return "", err
	}

	data, err := json.Marshal(req.Form)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Server) dataSourcesForForm(def formJobDefinition) (map[int64]store.DataSource, error) {
	result := map[int64]store.DataSource{}
	for _, id := range []int64{def.Reader.DataSourceID, def.Writer.DataSourceID} {
		if _, exists := result[id]; exists {
			continue
		}
		ds, err := s.dataStore.GetDataSource(id)
		if errors.Is(err, store.ErrDataSourceNotFound) {
			return nil, fmt.Errorf("数据源 %d 不存在", id)
		}
		if err != nil {
			return nil, err
		}
		result[id] = ds
	}
	return result, nil
}

func (s *Server) exportConfig(job store.SyncJob) (map[string]any, error) {
	if job.AuthoringMode == "raw" {
		if err := validateRawConfig(json.RawMessage(job.Definition)); err != nil {
			return nil, err
		}
		var config map[string]any
		if err := json.Unmarshal([]byte(job.Definition), &config); err != nil {
			return nil, fmt.Errorf("解析任务配置失败: %v", err)
		}
		return config, nil
	}

	var def formJobDefinition
	if err := json.Unmarshal([]byte(job.Definition), &def); err != nil {
		return nil, fmt.Errorf("解析任务配置失败: %v", err)
	}
	dataSources, err := s.dataSourcesForForm(def)
	if err != nil {
		return nil, err
	}
	if err := validateFormWriteMode(def, dataSources, false); err != nil {
		return nil, err
	}
	return materializeForm(def, dataSources)
}

func (s *Server) jobNamesReferencingDataSource(dataSourceID int64) ([]string, error) {
	jobs, err := s.dataStore.ListSyncJobs("")
	if err != nil {
		return nil, err
	}

	var names []string
	for _, job := range jobs {
		if job.AuthoringMode != "form" {
			continue
		}
		var def formJobDefinition
		if err := json.Unmarshal([]byte(job.Definition), &def); err != nil {
			continue
		}
		if def.Reader.DataSourceID == dataSourceID || def.Writer.DataSourceID == dataSourceID {
			names = append(names, job.Name)
		}
	}
	return names, nil
}

func pathID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("无效的 ID")
	}
	return id, nil
}

func uniqueCopyName(name string) string {
	base := strings.TrimSpace(name) + " (副本)"
	if len([]rune(base)) > 100 {
		base = string([]rune(strings.TrimSpace(name))[:90]) + " (副本)"
	}
	return base
}
