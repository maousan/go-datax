package store

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Pipeline Schedule 阻塞策略。
const (
	BlockingQueue = "queue"
)

var (
	ErrPipelineScheduleNotFound      = errors.New("pipeline schedule not found")
	ErrDuplicatePipelineScheduleName = errors.New("pipeline schedule name already exists")
)

// PipelineSchedule 是绑定 Pipeline 的定时触发器。
type PipelineSchedule struct {
	ID                   int64  `gorm:"primaryKey"`
	PipelineID           int64  `gorm:"not null;index;uniqueIndex:idx_pipeline_schedule_name"`
	Name                 string `gorm:"size:100;not null;uniqueIndex:idx_pipeline_schedule_name"`
	CronExpr             string `gorm:"size:100;not null"`
	Timezone             string `gorm:"size:64;not null"`
	Enabled              bool   `gorm:"not null"`
	BlockingPolicy       string `gorm:"size:20;not null;default:discard"`
	RetryMax             int    `gorm:"not null;default:0"`
	RetryIntervalSeconds int    `gorm:"not null;default:0"`
	TimeoutSeconds       int    `gorm:"not null;default:0"`
	PinnedVersionID      *int64 `gorm:"column:pinned_version_id;index"`
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// ListPipelineSchedules returns schedules ordered by pipeline and name.
func (s *Store) ListPipelineSchedules(query string, pipelineID int64) ([]PipelineSchedule, error) {
	var result []PipelineSchedule
	tx := s.db.Order("pipeline_id asc, lower(name) asc")
	if trimmed := strings.TrimSpace(query); trimmed != "" {
		tx = tx.Where("lower(name) LIKE ?", "%"+strings.ToLower(trimmed)+"%")
	}
	if pipelineID > 0 {
		tx = tx.Where("pipeline_id = ?", pipelineID)
	}
	if err := tx.Find(&result).Error; err != nil {
		return nil, err
	}
	return result, nil
}

// ListEnabledPipelineSchedules returns enabled schedules for the scheduler.
func (s *Store) ListEnabledPipelineSchedules() ([]PipelineSchedule, error) {
	var result []PipelineSchedule
	if err := s.db.Where("enabled = ?", true).Order("id asc").Find(&result).Error; err != nil {
		return nil, err
	}
	return result, nil
}

// GetPipelineSchedule returns one schedule by id.
func (s *Store) GetPipelineSchedule(id int64) (PipelineSchedule, error) {
	var schedule PipelineSchedule
	err := s.db.First(&schedule, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return schedule, ErrPipelineScheduleNotFound
	}
	if err != nil {
		return schedule, err
	}
	return schedule, nil
}

// CreatePipelineSchedule inserts a schedule.
func (s *Store) CreatePipelineSchedule(schedule *PipelineSchedule) error {
	exists, err := s.pipelineScheduleNameExists(schedule.PipelineID, schedule.Name, 0)
	if err != nil {
		return err
	}
	if exists {
		return ErrDuplicatePipelineScheduleName
	}
	if err := s.db.Create(schedule).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrDuplicatePipelineScheduleName
		}
		return err
	}
	return nil
}

// UpdatePipelineSchedule saves an existing schedule.
func (s *Store) UpdatePipelineSchedule(schedule *PipelineSchedule) error {
	exists, err := s.pipelineScheduleNameExists(schedule.PipelineID, schedule.Name, schedule.ID)
	if err != nil {
		return err
	}
	if exists {
		return ErrDuplicatePipelineScheduleName
	}
	if err := s.db.Save(schedule).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrDuplicatePipelineScheduleName
		}
		return err
	}
	return nil
}

// DeletePipelineSchedule removes a schedule; history is retained.
func (s *Store) DeletePipelineSchedule(id int64) error {
	result := s.db.Delete(&PipelineSchedule{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrPipelineScheduleNotFound
	}
	return nil
}

// CountPipelineSchedules returns the number of schedules for a pipeline.
func (s *Store) CountPipelineSchedules(pipelineID int64) (int64, error) {
	var count int64
	err := s.db.Model(&PipelineSchedule{}).Where("pipeline_id = ?", pipelineID).Count(&count).Error
	return count, err
}

// LatestPipelineRunForSchedule returns the latest run for one schedule.
func (s *Store) LatestPipelineRunForSchedule(scheduleID int64) (PipelineRun, bool, error) {
	var run PipelineRun
	err := s.db.Where("schedule_id = ?", scheduleID).Order("id desc").First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return run, false, nil
	}
	if err != nil {
		return run, false, err
	}
	return run, true, nil
}

// LatestPipelineRunsByScheduleIDs returns the latest run per schedule in one query.
func (s *Store) LatestPipelineRunsByScheduleIDs(ids []int64) ([]PipelineRun, error) {
	if len(ids) == 0 {
		return []PipelineRun{}, nil
	}
	var result []PipelineRun
	subquery := s.db.Model(&PipelineRun{}).
		Select("MAX(id)").
		Where("schedule_id IN ?", ids).
		Group("schedule_id")
	err := s.db.Where("id IN (?)", subquery).Find(&result).Error
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) pipelineScheduleNameExists(pipelineID int64, name string, excludeID int64) (bool, error) {
	var count int64
	tx := s.db.Model(&PipelineSchedule{}).
		Where("pipeline_id = ? AND lower(name) = ?", pipelineID, strings.ToLower(strings.TrimSpace(name)))
	if excludeID != 0 {
		tx = tx.Where("id <> ?", excludeID)
	}
	if err := tx.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
