package store

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// ErrJobRunNotFound 表示运行记录不存在。
var ErrJobRunNotFound = errors.New("job run not found")

// 运行状态。
const (
	RunStatusRunning   = "running"
	RunStatusSucceeded = "succeeded"
	RunStatusFailed    = "failed"
	RunStatusCanceled  = "canceled"
)

// 触发来源。
const (
	RunTriggerManual = "manual"
)

// JobRun 是一次同步任务的执行实例。
type JobRun struct {
	ID           int64      `gorm:"primaryKey"`
	SyncJobID    *int64     `gorm:"index"`
	SyncJobName  string     `gorm:"size:100"`
	Status       string     `gorm:"size:20;index;not null"`
	Trigger      string     `gorm:"size:20;not null"`
	StartedAt    time.Time  `gorm:"index"`
	FinishedAt   *time.Time
	ExitCode     *int
	ErrorSummary string `gorm:"size:1000"`
	SnapshotPath string `gorm:"size:500"`
	LogPath      string `gorm:"size:500"`
	LogSize      int64
	CreatedAt    time.Time
}

// RunFilter 是运行历史的查询条件。
type RunFilter struct {
	JobID    int64
	Status   string
	From     *time.Time
	To       *time.Time
	Page     int
	PageSize int
}

// CreateJobRun 创建运行记录。
func (s *Store) CreateJobRun(run *JobRun) error {
	return s.db.Create(run).Error
}

// UpdateJobRun 保存运行记录。
func (s *Store) UpdateJobRun(run *JobRun) error {
	return s.db.Save(run).Error
}

// GetJobRun 按 id 查询运行记录。
func (s *Store) GetJobRun(id int64) (JobRun, error) {
	var run JobRun
	err := s.db.First(&run, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return run, ErrJobRunNotFound
	}
	if err != nil {
		return run, err
	}
	return run, nil
}

// ListJobRuns 分页查询运行记录，返回当前页与总数。
func (s *Store) ListJobRuns(filter RunFilter) ([]JobRun, int64, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}

	tx := s.db.Model(&JobRun{})
	if filter.JobID > 0 {
		tx = tx.Where("sync_job_id = ?", filter.JobID)
	}
	if filter.Status != "" {
		tx = tx.Where("status = ?", filter.Status)
	}
	if filter.From != nil {
		tx = tx.Where("started_at >= ?", *filter.From)
	}
	if filter.To != nil {
		tx = tx.Where("started_at <= ?", *filter.To)
	}

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var runs []JobRun
	if err := tx.Order("id desc").
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Find(&runs).Error; err != nil {
		return nil, 0, err
	}
	return runs, total, nil
}

// HasRunningRun 判断任务是否已有运行中的记录。
func (s *Store) HasRunningRun(jobID int64) (bool, error) {
	var count int64
	err := s.db.Model(&JobRun{}).
		Where("sync_job_id = ? AND status = ?", jobID, RunStatusRunning).
		Count(&count).Error
	return count > 0, err
}

// CountRunningRuns 返回当前运行中的记录数。
func (s *Store) CountRunningRuns() (int64, error) {
	var count int64
	err := s.db.Model(&JobRun{}).Where("status = ?", RunStatusRunning).Count(&count).Error
	return count, err
}

// MarkRunningRunsFailed 把残留的运行中记录标记为失败。
func (s *Store) MarkRunningRunsFailed(summary string) (int64, error) {
	now := time.Now()
	result := s.db.Model(&JobRun{}).
		Where("status = ?", RunStatusRunning).
		Updates(map[string]any{
			"status":        RunStatusFailed,
			"finished_at":   now,
			"error_summary": summary,
		})
	return result.RowsAffected, result.Error
}

// ListRecentRuns 返回最近的运行记录。
func (s *Store) ListRecentRuns(limit int) ([]JobRun, error) {
	var runs []JobRun
	if err := s.db.Order("id desc").Limit(limit).Find(&runs).Error; err != nil {
		return nil, err
	}
	return runs, nil
}

// CountRunsSince 统计指定时间之后指定状态的运行数量。
func (s *Store) CountRunsSince(status string, since time.Time) (int64, error) {
	var count int64
	tx := s.db.Model(&JobRun{}).Where("started_at >= ?", since)
	if status != "" {
		tx = tx.Where("status = ?", status)
	}
	if err := tx.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// ListRunsForPrune 返回超出保留策略、需要删除的运行记录。
func (s *Store) ListRunsForPrune(maxRuns, maxAgeDays int) ([]JobRun, error) {
	var pruned []JobRun

	if maxAgeDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -maxAgeDays)
		var expired []JobRun
		if err := s.db.Where("started_at < ? AND status <> ?", cutoff, RunStatusRunning).
			Find(&expired).Error; err != nil {
			return nil, err
		}
		pruned = append(pruned, expired...)
	}

	if maxRuns > 0 {
		var keepIDs []int64
		if err := s.db.Model(&JobRun{}).
			Where("status <> ?", RunStatusRunning).
			Order("id desc").
			Limit(maxRuns).
			Pluck("id", &keepIDs).Error; err != nil {
			return nil, err
		}

		tx := s.db.Where("status <> ?", RunStatusRunning)
		if len(keepIDs) > 0 {
			tx = tx.Where("id NOT IN ?", keepIDs)
		}
		var overflow []JobRun
		if err := tx.Find(&overflow).Error; err != nil {
			return nil, err
		}
		pruned = append(pruned, overflow...)
	}

	return dedupeRuns(pruned), nil
}

// DeleteJobRun 删除运行记录。
func (s *Store) DeleteJobRun(id int64) error {
	return s.db.Delete(&JobRun{}, id).Error
}

func dedupeRuns(runs []JobRun) []JobRun {
	seen := make(map[int64]bool, len(runs))
	result := make([]JobRun, 0, len(runs))
	for _, run := range runs {
		if seen[run.ID] {
			continue
		}
		seen[run.ID] = true
		result = append(result, run)
	}
	return result
}
