package store

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

// 采集任务的阻塞策略。
const (
	BlockingDiscard   = "discard"
	BlockingOverwrite = "overwrite"
)

// 采集任务相关错误。
var (
	ErrCollectTaskNotFound      = errors.New("collect task not found")
	ErrDuplicateCollectTaskName = errors.New("collect task name already exists")
)

// CollectTask 是一条定时采集任务，归属于一个同步任务定义。
type CollectTask struct {
	ID                   int64  `gorm:"primaryKey"`
	Name                 string `gorm:"size:100;uniqueIndex;not null"`
	SyncJobID            int64  `gorm:"index;not null"`
	CronExpr             string `gorm:"size:100;not null"`
	Enabled              bool   `gorm:"not null"`
	BlockingPolicy       string `gorm:"size:20;not null;default:discard"`
	RetryMax             int    `gorm:"not null;default:0"`
	RetryIntervalSeconds int    `gorm:"not null;default:0"`
	TimeoutSeconds       int    `gorm:"not null;default:0"`
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// ListCollectTasks 返回采集任务列表，可按名称模糊搜索和任务定义过滤。
func (s *Store) ListCollectTasks(query string, syncJobID int64) ([]CollectTask, error) {
	var result []CollectTask
	tx := s.db.Order("lower(name) asc")
	if trimmed := strings.TrimSpace(query); trimmed != "" {
		tx = tx.Where("lower(name) LIKE ?", "%"+strings.ToLower(trimmed)+"%")
	}
	if syncJobID > 0 {
		tx = tx.Where("sync_job_id = ?", syncJobID)
	}
	if err := tx.Find(&result).Error; err != nil {
		return nil, err
	}
	return result, nil
}

// ListEnabledCollectTasks 返回所有启用中的采集任务，供调度器加载。
func (s *Store) ListEnabledCollectTasks() ([]CollectTask, error) {
	var result []CollectTask
	if err := s.db.Where("enabled = ?", true).Order("id asc").Find(&result).Error; err != nil {
		return nil, err
	}
	return result, nil
}

// GetCollectTask 按 id 查询采集任务。
func (s *Store) GetCollectTask(id int64) (CollectTask, error) {
	var task CollectTask
	err := s.db.First(&task, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return task, ErrCollectTaskNotFound
	}
	if err != nil {
		return task, err
	}
	return task, nil
}

// CreateCollectTask 创建采集任务。
func (s *Store) CreateCollectTask(task *CollectTask) error {
	exists, err := s.collectTaskNameExists(task.Name, 0)
	if err != nil {
		return err
	}
	if exists {
		return ErrDuplicateCollectTaskName
	}
	if err := s.db.Create(task).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrDuplicateCollectTaskName
		}
		return err
	}
	return nil
}

// UpdateCollectTask 全量保存采集任务。
func (s *Store) UpdateCollectTask(task *CollectTask) error {
	exists, err := s.collectTaskNameExists(task.Name, task.ID)
	if err != nil {
		return err
	}
	if exists {
		return ErrDuplicateCollectTaskName
	}
	if err := s.db.Save(task).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrDuplicateCollectTaskName
		}
		return err
	}
	return nil
}

// DeleteCollectTask 删除采集任务，历史运行记录保留。
func (s *Store) DeleteCollectTask(id int64) error {
	result := s.db.Delete(&CollectTask{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrCollectTaskNotFound
	}
	return nil
}

// CountCollectTasksForJob 返回某个同步任务定义下的采集任务数量。
func (s *Store) CountCollectTasksForJob(jobID int64) (int64, error) {
	var count int64
	err := s.db.Model(&CollectTask{}).Where("sync_job_id = ?", jobID).Count(&count).Error
	return count, err
}

// CountCollectTasks 返回采集任务总数。
func (s *Store) CountCollectTasks() (int64, error) {
	var count int64
	err := s.db.Model(&CollectTask{}).Count(&count).Error
	return count, err
}

func (s *Store) collectTaskNameExists(name string, excludeID int64) (bool, error) {
	var count int64
	tx := s.db.Model(&CollectTask{}).Where("lower(name) = ?", strings.ToLower(strings.TrimSpace(name)))
	if excludeID != 0 {
		tx = tx.Where("id <> ?", excludeID)
	}
	if err := tx.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
