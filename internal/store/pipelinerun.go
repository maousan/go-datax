package store

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// PipelineRun 状态。
const (
	PipelineRunStatusRunning   = "running"
	PipelineRunStatusSucceeded = "succeeded"
	PipelineRunStatusFailed    = "failed"
	PipelineRunStatusCanceled  = "canceled"
)

// PipelineRun 触发来源。
const (
	PipelineRunTriggerDiagnostic = "diagnostic"
)

// ErrPipelineRunNotFound 表示 PipelineRun 不存在。
var ErrPipelineRunNotFound = errors.New("pipeline run not found")

// PipelineRun 是一次 Pipeline 执行实例。PipelineID/VersionID/ScheduleID 在 v1
// 控制面接入前可为空；诊断运行只用于验证 worker 协议。
type PipelineRun struct {
	ID           int64     `gorm:"primaryKey"`
	PipelineID   *int64    `gorm:"index"`
	VersionID    *int64    `gorm:"index"`
	ScheduleID   *int64    `gorm:"index"`
	PipelineName string    `gorm:"size:100"`
	Status       string    `gorm:"size:20;index;not null"`
	Trigger      string    `gorm:"size:20;not null"`
	StartedAt    time.Time `gorm:"index"`
	FinishedAt   *time.Time
	ErrorSummary string `gorm:"size:1000"`
	LogPath      string `gorm:"size:500"`
	RetryAttempt int    `gorm:"not null;default:0"`
	CreatedAt    time.Time
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
	now := time.Now()
	return s.db.Model(&PipelineRun{}).Where("id = ?", id).Updates(map[string]any{
		"status":        status,
		"error_summary": errorSummary,
		"finished_at":   now,
	}).Error
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
