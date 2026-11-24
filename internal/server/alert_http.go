package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/longkeyy/go-datax/internal/alert"
	"github.com/longkeyy/go-datax/internal/store"
)

type alertTestRequest struct {
	Channel        alert.Channel `json:"channel"`
	TimeoutSeconds int           `json:"timeoutSeconds"`
}

type alertLogResponse struct {
	ID              int64  `json:"id"`
	Event           string `json:"event"`
	Status          string `json:"status"`
	ChannelID       string `json:"channelId"`
	ChannelName     string `json:"channelName"`
	ChannelType     string `json:"channelType"`
	RunID           int64  `json:"runId"`
	SyncJobID       *int64 `json:"syncJobId"`
	SyncJobName     string `json:"syncJobName"`
	CollectTaskID   *int64 `json:"collectTaskId"`
	CollectTaskName string `json:"collectTaskName"`
	Error           string `json:"error"`
	DurationMS      int64  `json:"durationMs"`
	CreatedAt       string `json:"createdAt"`
}

func toAlertLogResponse(entry store.AlertLog) alertLogResponse {
	return alertLogResponse{
		ID:              entry.ID,
		Event:           entry.Event,
		Status:          entry.Status,
		ChannelID:       entry.ChannelID,
		ChannelName:     entry.ChannelName,
		ChannelType:     entry.ChannelType,
		RunID:           entry.RunID,
		SyncJobID:       entry.SyncJobID,
		SyncJobName:     entry.SyncJobName,
		CollectTaskID:   entry.CollectTaskID,
		CollectTaskName: entry.CollectTaskName,
		Error:           entry.Error,
		DurationMS:      entry.DurationMS,
		CreatedAt:       entry.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func (s *Server) handleGetAlertConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.alerts.Config())
}

func (s *Server) handleUpdateAlertConfig(w http.ResponseWriter, r *http.Request) {
	var cfg alert.Config
	if err := decodeJSON(w, r, &cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}
	if err := s.alerts.Update(cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_settings", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.alerts.Config())
}

func (s *Server) handleTestAlertChannel(w http.ResponseWriter, r *http.Request) {
	var req alertTestRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}
	if err := s.alerts.Test(req.Channel, req.TimeoutSeconds); err != nil {
		writeError(w, http.StatusBadGateway, "alert_failed", "测试告警发送失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleListAlertLogs(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	filter := store.AlertLogFilter{
		Event:       query.Get("event"),
		Status:      query.Get("status"),
		ChannelType: query.Get("channelType"),
		Query:       query.Get("query"),
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

	logs, total, err := s.dataStore.ListAlertLogs(filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取告警日志失败")
		return
	}

	items := make([]alertLogResponse, 0, len(logs))
	for _, entry := range logs {
		items = append(items, toAlertLogResponse(entry))
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

func (s *Server) handleClearAlertLogs(w http.ResponseWriter, r *http.Request) {
	var before *time.Time
	if value := r.URL.Query().Get("before"); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "before 必须是 RFC3339 时间")
			return
		}
		before = &parsed
	}

	deleted, err := s.dataStore.DeleteAlertLogs(before)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "清理告警日志失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}
