package server

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/longkeyy/go-datax/internal/store"
	"github.com/longkeyy/go-datax/internal/worker"
)

type pipelineRunResponse struct {
	ID            int64   `json:"id"`
	PipelineID    *int64  `json:"pipelineId"`
	VersionID     *int64  `json:"versionId"`
	VersionNumber int     `json:"versionNumber"`
	PipelineName  string  `json:"pipelineName"`
	Status        string  `json:"status"`
	Trigger       string  `json:"trigger"`
	StartedAt     string  `json:"startedAt"`
	FinishedAt    *string `json:"finishedAt"`
	ReadRecords   int64   `json:"readRecords"`
	WriteRecords  int64   `json:"writeRecords"`
	ReadBytes     int64   `json:"readBytes"`
	ErrorSummary  string  `json:"errorSummary"`
	CreatedAt     string  `json:"createdAt"`
}

func toPipelineRunResponse(run store.PipelineRun) pipelineRunResponse {
	response := pipelineRunResponse{
		ID:            run.ID,
		PipelineID:    run.PipelineID,
		VersionID:     run.VersionID,
		VersionNumber: run.VersionNumber,
		PipelineName:  run.PipelineName,
		Status:        run.Status,
		Trigger:       run.Trigger,
		StartedAt:     run.StartedAt.UTC().Format(time.RFC3339),
		ReadRecords:   run.ReadRecords,
		WriteRecords:  run.WriteRecords,
		ReadBytes:     run.ReadBytes,
		ErrorSummary:  run.ErrorSummary,
		CreatedAt:     run.CreatedAt.UTC().Format(time.RFC3339),
	}
	if run.FinishedAt != nil {
		finished := run.FinishedAt.UTC().Format(time.RFC3339)
		response.FinishedAt = &finished
	}
	return response
}

func (s *Server) mergePipelineRunLive(response *pipelineRunResponse) {
	if response.Status != store.PipelineRunStatusRunning {
		return
	}
	event, ok := s.pipelineStreams.snapshot(response.ID)
	if !ok {
		return
	}
	response.ReadRecords = event.ReadRecords
	response.WriteRecords = event.WriteRecords
	response.ReadBytes = event.ReadBytes
	if event.Type == "done" {
		response.Status = string(event.Status)
		response.ErrorSummary = event.Error
	}
}

func (s *Server) handleListPipelineRuns(w http.ResponseWriter, r *http.Request) {
	filter := parsePipelineRunFilter(r)
	runs, total, err := s.dataStore.ListPipelineRuns(filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取运行记录失败")
		return
	}
	items := make([]pipelineRunResponse, 0, len(runs))
	for _, run := range runs {
		item := toPipelineRunResponse(run)
		s.mergePipelineRunLive(&item)
		items = append(items, item)
	}
	page := filter.Page
	if page <= 0 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":    items,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

func (s *Server) handleGetPipelineRun(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	run, err := s.dataStore.GetPipelineRun(id)
	if errors.Is(err, store.ErrPipelineRunNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "运行记录不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取运行记录失败")
		return
	}
	response := toPipelineRunResponse(run)
	s.mergePipelineRunLive(&response)
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handlePipelineRunEvents(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	run, err := s.dataStore.GetPipelineRun(id)
	if errors.Is(err, store.ErrPipelineRunNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "运行记录不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取运行记录失败")
		return
	}
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

	history, live, done, cancel, active := s.pipelineStreams.subscribe(id)
	offset := 0
	if value := r.URL.Query().Get("offset"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed >= 0 {
			offset = parsed
		}
	}
	if !active {
		s.streamPipelineRunLogFile(w, flusher, run, offset)
		return
	}
	defer cancel()

	for index, event := range history {
		if index < offset {
			continue
		}
		if writePipelineSSE(w, flusher, event) != nil {
			return
		}
	}
	if done {
		if offset >= len(history) && len(history) > 0 {
			_ = writePipelineSSE(w, flusher, history[len(history)-1])
		}
		return
	}

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-live:
			if !ok {
				return
			}
			if err := writePipelineSSE(w, flusher, event); err != nil {
				return
			}
			if event.Type == "done" {
				return
			}
		case <-heartbeat.C:
			if err := writePipelineSSE(w, flusher, worker.Event{Type: "ping"}); err != nil {
				return
			}
		}
	}
}

func writePipelineSSE(w http.ResponseWriter, flusher http.Flusher, event worker.Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func (s *Server) streamPipelineRunLogFile(w http.ResponseWriter, flusher http.Flusher, run store.PipelineRun, offset int) {
	if run.LogPath != "" {
		file, err := os.Open(run.LogPath)
		if err == nil {
			scanner := bufio.NewScanner(file)
			line := 0
			for scanner.Scan() {
				line++
				if line <= offset {
					continue
				}
				if writePipelineSSE(w, flusher, worker.Event{Type: "log", Message: scanner.Text()}) != nil {
					_ = file.Close()
					return
				}
			}
			_ = file.Close()
		}
	}
	_ = writePipelineSSE(w, flusher, worker.Event{
		Type:         "done",
		Status:       worker.RunStatus(run.Status),
		ReadRecords:  run.ReadRecords,
		WriteRecords: run.WriteRecords,
		ReadBytes:    run.ReadBytes,
		Error:        run.ErrorSummary,
	})
}

func (s *Server) handlePipelineRunLog(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	run, err := s.dataStore.GetPipelineRun(id)
	if errors.Is(err, store.ErrPipelineRunNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "运行记录不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取运行记录失败")
		return
	}
	if run.LogPath == "" {
		writeError(w, http.StatusNotFound, "not_found", "日志文件不存在")
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fmt.Sprintf("pipeline-run-%d.log", run.ID)))
	http.ServeFile(w, r, run.LogPath)
}
