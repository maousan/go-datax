package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/longkeyy/go-datax/internal/execution"
)

const runtimeSettingKey = "runtime"

const megabyte = int64(1024 * 1024)

func (s *Server) savedRuntimeSettings() (execution.RuntimeSettings, bool) {
	raw, found, err := s.dataStore.GetSetting(runtimeSettingKey)
	if err != nil || !found || strings.TrimSpace(raw) == "" {
		return execution.RuntimeSettings{}, false
	}
	var settings execution.RuntimeSettings
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		log.Printf("忽略无法解析的运行参数设置: %v", err)
		return execution.RuntimeSettings{}, false
	}
	if err := validateRuntimeSettings(settings); err != nil {
		log.Printf("忽略无效的运行参数设置: %v", err)
		return execution.RuntimeSettings{}, false
	}
	return settings, true
}

func (s *Server) runtimeSettings() execution.RuntimeSettings {
	if s.executor != nil {
		return s.executor.Runtime()
	}
	if saved, ok := s.savedRuntimeSettings(); ok {
		return saved
	}
	return execution.RuntimeSettings{
		MaxConcurrent: s.config.Runtime.MaxConcurrent,
		MaxRuns:       s.config.Retention.MaxRuns,
		MaxAgeDays:    s.config.Retention.MaxAgeDays,
		MaxLogBytes:   s.config.Retention.MaxLogBytes,
	}
}

func (s *Server) handleGetRuntimeSettings(w http.ResponseWriter, r *http.Request) {
	if s.executor != nil {
		writeJSON(w, http.StatusOK, s.executor.Runtime())
		return
	}
	writeJSON(w, http.StatusOK, execution.RuntimeSettings{
		MaxConcurrent: s.config.Runtime.MaxConcurrent,
		MaxRuns:       s.config.Retention.MaxRuns,
		MaxAgeDays:    s.config.Retention.MaxAgeDays,
		MaxLogBytes:   s.config.Retention.MaxLogBytes,
	})
}

func (s *Server) handleUpdateRuntimeSettings(w http.ResponseWriter, r *http.Request) {
	var settings execution.RuntimeSettings
	if err := decodeJSON(w, r, &settings); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}
	if err := validateRuntimeSettings(settings); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_settings", err.Error())
		return
	}

	data, err := json.Marshal(settings)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "编码运行参数失败")
		return
	}
	if err := s.dataStore.SaveSetting(runtimeSettingKey, string(data)); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "保存运行参数失败")
		return
	}

	if s.executor != nil {
		s.executor.UpdateRuntime(settings)
	}
	s.prunePipelineRuns()
	writeJSON(w, http.StatusOK, s.runtimeSettings())
}

func validateRuntimeSettings(settings execution.RuntimeSettings) error {
	if settings.MaxConcurrent < 1 || settings.MaxConcurrent > 16 {
		return fmt.Errorf("并发上限必须在 1 到 16 之间")
	}
	if settings.MaxRuns < 1 || settings.MaxRuns > 100000 {
		return fmt.Errorf("保留运行数必须在 1 到 100000 之间")
	}
	if settings.MaxAgeDays < 1 || settings.MaxAgeDays > 3650 {
		return fmt.Errorf("保留天数必须在 1 到 3650 之间")
	}
	if settings.MaxLogBytes < megabyte || settings.MaxLogBytes > 1024*megabyte {
		return fmt.Errorf("单次日志上限必须在 1MB 到 1024MB 之间")
	}
	return nil
}
