package store

import (
	"time"

	"gorm.io/gorm"
)

// 告警发送状态。
const (
	AlertStatusSuccess = "success"
	AlertStatusFailed  = "failed"
)

// 告警来源。
const (
	AlertSourceDataX    = "datax"
	AlertSourcePipeline = "pipeline"
)

// AlertLog 记录一次告警投递的结果。
type AlertLog struct {
	ID              int64  `gorm:"primaryKey"`
	SourceKind      string `gorm:"size:20;index;not null;default:datax"`
	Event           string `gorm:"size:20;index"`
	Status          string `gorm:"size:20;index"`
	ChannelID       string `gorm:"size:40;index"`
	ChannelName     string `gorm:"size:100"`
	ChannelType     string `gorm:"size:20;index"`
	RunID           int64  `gorm:"index"`
	SyncJobID       *int64 `gorm:"index"`
	SyncJobName     string `gorm:"size:100"`
	CollectTaskID   *int64 `gorm:"index"`
	CollectTaskName string `gorm:"size:100"`
	PipelineID      *int64 `gorm:"index"`
	PipelineName    string `gorm:"size:100"`
	PipelineRunID   *int64 `gorm:"index"`
	VersionNumber   int    `gorm:"column:pipeline_version_number;not null;default:0"`
	Error           string `gorm:"size:1000"`
	DurationMS      int64
	CreatedAt       time.Time `gorm:"index"`
}

// AlertLogFilter 是告警日志的查询条件。
type AlertLogFilter struct {
	SourceKind  string
	Event       string
	Status      string
	ChannelType string
	Query       string
	From        *time.Time
	To          *time.Time
	Page        int
	PageSize    int
}

// CreateAlertLog 写入一条告警日志。
func (s *Store) CreateAlertLog(entry *AlertLog) error {
	return s.db.Create(entry).Error
}

// ListAlertLogs 分页查询告警日志，返回当前页与总数。
func (s *Store) ListAlertLogs(filter AlertLogFilter) ([]AlertLog, int64, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}

	tx := s.db.Model(&AlertLog{})
	switch filter.SourceKind {
	case AlertSourceDataX:
		// 旧日志没有 source_kind，仍按 DataX 处理。
		tx = tx.Where("(source_kind = ? OR source_kind = '')", AlertSourceDataX)
	case AlertSourcePipeline:
		tx = tx.Where("source_kind = ?", AlertSourcePipeline)
	}
	if filter.Event != "" {
		tx = tx.Where("event = ?", filter.Event)
	}
	if filter.Status != "" {
		tx = tx.Where("status = ?", filter.Status)
	}
	if filter.ChannelType != "" {
		tx = tx.Where("channel_type = ?", filter.ChannelType)
	}
	if filter.Query != "" {
		like := "%" + filter.Query + "%"
		tx = tx.Where(
			"(channel_name LIKE ? OR sync_job_name LIKE ? OR collect_task_name LIKE ? OR pipeline_name LIKE ?)",
			like, like, like, like,
		)
	}
	if filter.From != nil {
		tx = tx.Where("created_at >= ?", *filter.From)
	}
	if filter.To != nil {
		tx = tx.Where("created_at <= ?", *filter.To)
	}

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var logs []AlertLog
	err := tx.Order("created_at DESC, id DESC").
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Find(&logs).Error
	if err != nil {
		return nil, 0, err
	}
	return logs, total, nil
}

// DeleteAlertLogs 删除 before 之前的日志；before 为 nil 时清空全部日志。
func (s *Store) DeleteAlertLogs(before *time.Time) (int64, error) {
	tx := s.db.Session(&gorm.Session{AllowGlobalUpdate: true})
	if before != nil {
		tx = tx.Where("created_at < ?", *before)
	}
	result := tx.Delete(&AlertLog{})
	return result.RowsAffected, result.Error
}
