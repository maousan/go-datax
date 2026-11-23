package store

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

// ErrSyncJobNotFound 表示同步任务不存在。
var ErrSyncJobNotFound = errors.New("sync job not found")

// ErrDuplicateSyncJobName 表示同步任务名称重复。
var ErrDuplicateSyncJobName = errors.New("sync job name already exists")

// SyncJob 是一条同步任务定义。
type SyncJob struct {
	ID            int64  `gorm:"primaryKey"`
	Name          string `gorm:"size:100;uniqueIndex;not null"`
	AuthoringMode string `gorm:"size:10;not null"` // form | raw
	Definition    string `gorm:"type:text;not null"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ListSyncJobs 返回任务列表，query 非空时按名称不区分大小写模糊匹配。
func (s *Store) ListSyncJobs(query string) ([]SyncJob, error) {
	var result []SyncJob
	tx := s.db.Order("lower(name) asc")
	if trimmed := strings.TrimSpace(query); trimmed != "" {
		tx = tx.Where("lower(name) LIKE ?", "%"+strings.ToLower(trimmed)+"%")
	}
	if err := tx.Find(&result).Error; err != nil {
		return nil, err
	}
	return result, nil
}

// GetSyncJob 按 id 查询任务。
func (s *Store) GetSyncJob(id int64) (SyncJob, error) {
	var job SyncJob
	err := s.db.First(&job, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return job, ErrSyncJobNotFound
	}
	if err != nil {
		return job, err
	}
	return job, nil
}

// CreateSyncJob 创建任务。
func (s *Store) CreateSyncJob(job *SyncJob) error {
	exists, err := s.syncJobNameExists(job.Name, 0)
	if err != nil {
		return err
	}
	if exists {
		return ErrDuplicateSyncJobName
	}
	if err := s.db.Create(job).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrDuplicateSyncJobName
		}
		return err
	}
	return nil
}

// UpdateSyncJob 全量保存任务。
func (s *Store) UpdateSyncJob(job *SyncJob) error {
	exists, err := s.syncJobNameExists(job.Name, job.ID)
	if err != nil {
		return err
	}
	if exists {
		return ErrDuplicateSyncJobName
	}
	if err := s.db.Save(job).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrDuplicateSyncJobName
		}
		return err
	}
	return nil
}

// DeleteSyncJob 删除任务，历史运行记录保留。
func (s *Store) DeleteSyncJob(id int64) error {
	result := s.db.Delete(&SyncJob{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrSyncJobNotFound
	}
	return nil
}

// CountSyncJobs 返回任务总数。
func (s *Store) CountSyncJobs() (int64, error) {
	var count int64
	if err := s.db.Model(&SyncJob{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Store) syncJobNameExists(name string, excludeID int64) (bool, error) {
	var count int64
	tx := s.db.Model(&SyncJob{}).Where("lower(name) = ?", strings.ToLower(strings.TrimSpace(name)))
	if excludeID != 0 {
		tx = tx.Where("id <> ?", excludeID)
	}
	if err := tx.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
