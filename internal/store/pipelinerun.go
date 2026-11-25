package store

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

// PipelineRun 状态。
const (
	PipelineRunStatusQueued    = "queued"
	PipelineRunStatusRunning   = "running"
	PipelineRunStatusSucceeded = "succeeded"
	PipelineRunStatusFailed    = "failed"
	PipelineRunStatusCanceled  = "canceled"
)

// PipelineRun 触发来源。
const (
	PipelineRunTriggerDiagnostic = "diagnostic"
	PipelineRunTriggerManual     = "manual"
	PipelineRunTriggerRerun      = "rerun"
	PipelineRunTriggerSchedule   = "schedule"
	PipelineRunTriggerRetry      = "retry"
)

// ErrPipelineRunNotFound 表示 PipelineRun 不存在。
var ErrPipelineRunNotFound = errors.New("pipeline run not found")

// PipelineRun 是一次 Pipeline 执行实例。诊断运行只用于验证 worker 协议。
type PipelineRun struct {
	ID             int64     `gorm:"primaryKey"`
	PipelineID     *int64    `gorm:"index"`
	VersionID      *int64    `gorm:"index"`
	ScheduleID     *int64    `gorm:"index"`
	VersionNumber  int       `gorm:"not null;default:0"`
	PipelineName   string    `gorm:"size:100"`
	Status         string    `gorm:"size:20;index;not null"`
	Trigger        string    `gorm:"size:20;not null"`
	StartedAt      time.Time `gorm:"index"`
	FinishedAt     *time.Time
	ReadRecords    int64  `gorm:"not null;default:0"`
	WriteRecords   int64  `gorm:"not null;default:0"`
	ReadBytes      int64  `gorm:"not null;default:0"`
	ErrorSummary   string `gorm:"size:1000"`
	LogPath        string `gorm:"size:500"`
	BlockingPolicy string `gorm:"size:20"`
	TimeoutSeconds int    `gorm:"not null;default:0"`
	RetryAttempt   int    `gorm:"not null;default:0"`
	CreatedAt      time.Time
}

// PipelineRunFilter 是运行历史筛选条件。
type PipelineRunFilter struct {
	PipelineID int64
	Status     string
	Page       int
	PageSize   int
}

// CreatePipelineRun 写入一次运行记录。
func (s *Store) CreatePipelineRun(run *PipelineRun) error {
	return s.db.Create(run).Error
}

// GetPipelineRun 按 id 查询运行记录。
func (s *Store) GetPipelineRun(id int64) (PipelineRun, error) {
	var run PipelineRun
	err := s.db.First(&run, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return run, ErrPipelineRunNotFound
	}
	if err != nil {
		return run, err
	}
	return run, nil
}

// FinishPipelineRun 更新运行的终态。
func (s *Store) FinishPipelineRun(id int64, status, errorSummary string) error {
	return s.FinishPipelineRunDetails(id, status, errorSummary, 0, 0, 0)
}

// FinishPipelineRunDetails 更新终态与计数。
func (s *Store) FinishPipelineRunDetails(id int64, status, errorSummary string, readRecords, writeRecords, readBytes int64) error {
	now := time.Now()
	return s.db.Model(&PipelineRun{}).Where("id = ?", id).Updates(map[string]any{
		"status":        status,
		"error_summary": errorSummary,
		"finished_at":   now,
		"read_records":  readRecords,
		"write_records": writeRecords,
		"read_bytes":    readBytes,
	}).Error
}

// UpdatePipelineRunLogPath 记录运行日志文件位置。
func (s *Store) UpdatePipelineRunLogPath(id int64, path string) error {
	return s.db.Model(&PipelineRun{}).Where("id = ?", id).Update("log_path", path).Error
}

// ListPipelineRuns 返回运行历史与总数。
func (s *Store) ListPipelineRuns(filter PipelineRunFilter) ([]PipelineRun, int64, error) {
	query := s.db.Model(&PipelineRun{})
	if filter.PipelineID > 0 {
		query = query.Where("pipeline_id = ?", filter.PipelineID)
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
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
	var result []PipelineRun
	err := query.Order("id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&result).Error
	if err != nil {
		return nil, 0, err
	}
	return result, total, nil
}

// HasActivePipelineRun reports whether a pipeline already has a running run.
func (s *Store) HasActivePipelineRun(pipelineID int64) (bool, error) {
	var count int64
	err := s.db.Model(&PipelineRun{}).
		Where("pipeline_id = ? AND status = ?", pipelineID, PipelineRunStatusRunning).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// HasQueuedPipelineRun reports whether a pipeline has a queued run.
func (s *Store) HasQueuedPipelineRun(pipelineID int64) (bool, error) {
	var count int64
	err := s.db.Model(&PipelineRun{}).
		Where("pipeline_id = ? AND status = ?", pipelineID, PipelineRunStatusQueued).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// ActivePipelineRun returns the running run for a pipeline.
func (s *Store) ActivePipelineRun(pipelineID int64) (PipelineRun, bool, error) {
	var run PipelineRun
	err := s.db.Where("pipeline_id = ? AND status = ?", pipelineID, PipelineRunStatusRunning).
		Order("id asc").
		First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return run, false, nil
	}
	if err != nil {
		return run, false, err
	}
	return run, true, nil
}

// FirstQueuedPipelineRun returns the oldest queued run for a pipeline.
func (s *Store) FirstQueuedPipelineRun(pipelineID int64) (PipelineRun, bool, error) {
	var run PipelineRun
	err := s.db.Where("pipeline_id = ? AND status = ?", pipelineID, PipelineRunStatusQueued).
		Order("id asc").
		First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return run, false, nil
	}
	if err != nil {
		return run, false, err
	}
	return run, true, nil
}

// ListQueuedPipelineIDs returns distinct pipeline ids with queued runs.
func (s *Store) ListQueuedPipelineIDs() ([]int64, error) {
	var ids []int64
	err := s.db.Model(&PipelineRun{}).
		Where("status = ? AND pipeline_id IS NOT NULL", PipelineRunStatusQueued).
		Distinct("pipeline_id").
		Pluck("pipeline_id", &ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// CancelQueuedPipelineRun cancels one run if it is still queued.
func (s *Store) CancelQueuedPipelineRun(id int64, summary string) (bool, error) {
	now := time.Now()
	result := s.db.Model(&PipelineRun{}).
		Where("id = ? AND status = ?", id, PipelineRunStatusQueued).
		Updates(map[string]any{
			"status":        PipelineRunStatusCanceled,
			"error_summary": summary,
			"finished_at":   now,
		})
	return result.RowsAffected > 0, result.Error
}

// MarkPipelineRunQueued reverts a promoted run to queued after a transient
// worker start failure.
func (s *Store) MarkPipelineRunQueued(id int64) error {
	return s.db.Model(&PipelineRun{}).Where("id = ?", id).Updates(map[string]any{
		"status":        PipelineRunStatusQueued,
		"finished_at":   nil,
		"error_summary": "",
	}).Error
}

// CancelQueuedPipelineRuns cancels all queued runs for a pipeline.
func (s *Store) CancelQueuedPipelineRuns(pipelineID int64, summary string) (int64, error) {
	now := time.Now()
	result := s.db.Model(&PipelineRun{}).
		Where("pipeline_id = ? AND status = ?", pipelineID, PipelineRunStatusQueued).
		Updates(map[string]any{
			"status":        PipelineRunStatusCanceled,
			"error_summary": summary,
			"finished_at":   now,
		})
	return result.RowsAffected, result.Error
}

// MarkQueuedPipelineRunStarted promotes a queued run to running and reports
// whether the row was still queued.
func (s *Store) MarkQueuedPipelineRunStarted(id int64, startedAt time.Time) (bool, error) {
	result := s.db.Model(&PipelineRun{}).Where("id = ? AND status = ?", id, PipelineRunStatusQueued).
		Updates(map[string]any{
			"status":     PipelineRunStatusRunning,
			"started_at": startedAt,
		})
	return result.RowsAffected > 0, result.Error
}

// MarkRunningPipelineRunsFailed 把服务重启后残留的运行标记为失败。
func (s *Store) MarkRunningPipelineRunsFailed(summary string) (int64, error) {
	now := time.Now()
	result := s.db.Model(&PipelineRun{}).Where("status = ?", PipelineRunStatusRunning).Updates(map[string]any{
		"status":        PipelineRunStatusFailed,
		"error_summary": summary,
		"finished_at":   now,
	})
	return result.RowsAffected, result.Error
}

// ListPipelineRunsForPrune returns runs outside the retention policy.
func (s *Store) ListPipelineRunsForPrune(maxRuns, maxAgeDays int) ([]PipelineRun, error) {
	var pruned []PipelineRun
	if maxAgeDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -maxAgeDays)
		var expired []PipelineRun
		if err := s.db.Where("started_at < ? AND status NOT IN ?", cutoff, []string{PipelineRunStatusRunning, PipelineRunStatusQueued}).
			Find(&expired).Error; err != nil {
			return nil, err
		}
		pruned = append(pruned, expired...)
	}
	if maxRuns > 0 {
		var ids []int64
		if err := s.db.Model(&PipelineRun{}).
			Where("status NOT IN ?", []string{PipelineRunStatusRunning, PipelineRunStatusQueued}).
			Order("id desc").
			Pluck("id", &ids).Error; err != nil {
			return nil, err
		}
		if len(ids) > maxRuns {
			var overflow []PipelineRun
			if err := s.db.Where("id IN ?", ids[maxRuns:]).Find(&overflow).Error; err != nil {
				return nil, err
			}
			pruned = append(pruned, overflow...)
		}
	}
	seen := map[int64]bool{}
	result := make([]PipelineRun, 0, len(pruned))
	for _, run := range pruned {
		if seen[run.ID] {
			continue
		}
		seen[run.ID] = true
		result = append(result, run)
	}
	return result, nil
}

// DeletePipelineRun removes a run record.
func (s *Store) DeletePipelineRun(id int64) error {
	return s.db.Delete(&PipelineRun{}, id).Error
}
