package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
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
	RunTriggerRerun  = "rerun"
	RunTriggerRetry  = "retry"
)

// JobRun 是一次同步任务的执行实例。
type JobRun struct {
	ID           int64     `gorm:"primaryKey"`
	SyncJobID    *int64    `gorm:"index"`
	SyncJobName  string    `gorm:"size:100"`
	Status       string    `gorm:"size:20;index;not null"`
	Trigger      string    `gorm:"size:20;not null"`
	StartedAt    time.Time `gorm:"index"`
	FinishedAt   *time.Time
	ExitCode     *int
	ErrorSummary string `gorm:"size:1000"`
	SnapshotPath string `gorm:"size:500"`
	LogPath      string `gorm:"size:500"`
	LogSize      int64
	Progress     string `gorm:"size:1000"`
	RetryAttempt int    `gorm:"not null;default:0"`
	CreatedAt    time.Time
}

// RunProgress 是引擎上报的进度快照，序列化后保存在 JobRun.Progress。
type RunProgress struct {
	Percent            float64 `json:"percent"`
	ReadRecords        int64   `json:"readRecords"`
	ReadBytes          int64   `json:"readBytes"`
	SpeedBytesPerSec   int64   `json:"speedBytesPerSec"`
	SpeedRecordsPerSec int64   `json:"speedRecordsPerSec"`
	ErrorRecords       int64   `json:"errorRecords"`
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

// UpdateJobRunProgress 只更新进度字段，避免整行覆盖。
func (s *Store) UpdateJobRunProgress(id int64, progress string) error {
	return s.db.Model(&JobRun{}).Where("id = ?", id).Update("progress", progress).Error
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

// LatestRunForJob 返回任务最近一次运行；没有运行时 found 为 false。
func (s *Store) LatestRunForJob(jobID int64) (JobRun, bool, error) {
	var run JobRun
	err := s.db.Where("sync_job_id = ?", jobID).Order("id desc").First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return run, false, nil
	}
	if err != nil {
		return run, false, err
	}
	return run, true, nil
}

// LatestRunsForJobs 批量返回每个任务最近一次运行，避免列表接口逐条查询。
func (s *Store) LatestRunsForJobs(jobIDs []int64) (map[int64]JobRun, error) {
	result := make(map[int64]JobRun, len(jobIDs))
	if len(jobIDs) == 0 {
		return result, nil
	}

	var runs []JobRun
	err := s.db.Raw(
		`SELECT * FROM job_runs
		 WHERE id IN (
		   SELECT MAX(id) FROM job_runs WHERE sync_job_id IN ? GROUP BY sync_job_id
		 )`,
		jobIDs,
	).Scan(&runs).Error
	if err != nil {
		return nil, err
	}
	for _, run := range runs {
		if run.SyncJobID != nil {
			result[*run.SyncJobID] = run
		}
	}
	return result, nil
}

// RunStats 汇总一个任务在时间窗口内的运行情况。
type RunStats struct {
	Total          int64
	Succeeded      int64
	Failed         int64
	Canceled       int64
	Running        int64
	AverageSeconds float64
	LastFailed     *JobRun
}

// JobRunStats 统计任务在 since 之后的运行状态与平均耗时。
func (s *Store) JobRunStats(jobID int64, since time.Time) (RunStats, error) {
	var stats RunStats

	type statusCount struct {
		Status string
		Count  int64
	}
	var counts []statusCount
	if err := s.db.Model(&JobRun{}).
		Select("status, count(*) as count").
		Where("sync_job_id = ? AND started_at >= ?", jobID, since).
		Group("status").
		Scan(&counts).Error; err != nil {
		return stats, err
	}
	for _, item := range counts {
		stats.Total += item.Count
		switch item.Status {
		case RunStatusSucceeded:
			stats.Succeeded = item.Count
		case RunStatusFailed:
			stats.Failed = item.Count
		case RunStatusCanceled:
			stats.Canceled = item.Count
		case RunStatusRunning:
			stats.Running = item.Count
		}
	}

	var average struct {
		Seconds sql.NullFloat64
	}
	if err := s.db.Model(&JobRun{}).
		Select("AVG((julianday(finished_at) - julianday(started_at)) * 86400) AS seconds").
		Where("sync_job_id = ? AND started_at >= ? AND finished_at IS NOT NULL", jobID, since).
		Scan(&average).Error; err != nil {
		return stats, err
	}
	if average.Seconds.Valid {
		stats.AverageSeconds = average.Seconds.Float64
	}

	var lastFailed JobRun
	result := s.db.Where("sync_job_id = ? AND status = ? AND started_at >= ?", jobID, RunStatusFailed, since).
		Order("id desc").
		Limit(1).
		Find(&lastFailed)
	if result.Error != nil {
		return stats, result.Error
	}
	if result.RowsAffected > 0 {
		stats.LastFailed = &lastFailed
	}

	return stats, nil
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

// RunStatusCountsSince 返回时间窗口内各状态的运行数量。
func (s *Store) RunStatusCountsSince(since time.Time) (map[string]int64, error) {
	type statusCount struct {
		Status string
		Count  int64
	}
	var rows []statusCount
	if err := s.db.Model(&JobRun{}).
		Select("status, count(*) as count").
		Where("started_at >= ?", since).
		Group("status").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	result := map[string]int64{
		RunStatusRunning:   0,
		RunStatusSucceeded: 0,
		RunStatusFailed:    0,
		RunStatusCanceled:  0,
	}
	for _, row := range rows {
		result[row.Status] = row.Count
	}
	return result, nil
}

// DailyRunStats 是按天聚合的运行统计。
type DailyRunStats struct {
	Date        string
	Total       int64
	Succeeded   int64
	Failed      int64
	Canceled    int64
	ReadRecords int64
	ReadBytes   int64
}

// RunDailyStatsSince 按本地日期聚合运行数量与进度数据。
func (s *Store) RunDailyStatsSince(since time.Time) ([]DailyRunStats, error) {
	type row struct {
		StartedAt time.Time
		Status    string
		Progress  string
	}
	var rows []row
	if err := s.db.Model(&JobRun{}).
		Select("started_at, status, progress").
		Where("started_at >= ?", since).
		Order("started_at asc").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	byDate := make(map[string]*DailyRunStats)
	for _, row := range rows {
		date := row.StartedAt.Local().Format("2006-01-02")
		stats := byDate[date]
		if stats == nil {
			stats = &DailyRunStats{Date: date}
			byDate[date] = stats
		}
		stats.Total++
		switch row.Status {
		case RunStatusSucceeded:
			stats.Succeeded++
		case RunStatusFailed:
			stats.Failed++
		case RunStatusCanceled:
			stats.Canceled++
		}
		if row.Progress != "" {
			var progress RunProgress
			if err := json.Unmarshal([]byte(row.Progress), &progress); err == nil {
				stats.ReadRecords += progress.ReadRecords
				stats.ReadBytes += progress.ReadBytes
			}
		}
	}

	result := make([]DailyRunStats, 0, len(byDate))
	for _, stats := range byDate {
		result = append(result, *stats)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Date < result[j].Date })
	return result, nil
}

// FailedJobStats 是一个任务在时间窗口内的失败聚合。
type FailedJobStats struct {
	JobID    int64
	JobName  string
	Failures int64
}

// FailedJobsSince 返回时间窗口内失败次数最多的任务，仅包含仍然存在的任务。
func (s *Store) FailedJobsSince(since time.Time, limit int) ([]FailedJobStats, error) {
	if limit <= 0 {
		limit = 5
	}
	result := make([]FailedJobStats, 0, limit)
	err := s.db.Table("job_runs").
		Select("job_runs.sync_job_id AS job_id, job_runs.sync_job_name AS job_name, count(*) AS failures").
		Joins("JOIN sync_jobs ON sync_jobs.id = job_runs.sync_job_id").
		Where("job_runs.status = ? AND job_runs.started_at >= ?", RunStatusFailed, since).
		Group("job_runs.sync_job_id, job_runs.sync_job_name").
		Order("failures DESC").
		Limit(limit).
		Scan(&result).Error
	if err != nil {
		return nil, err
	}
	return result, nil
}

// RunProgressTotalsSince 汇总时间窗口内的读取记录与字节数。
func (s *Store) RunProgressTotalsSince(since time.Time) (int64, int64, error) {
	var rows []struct {
		Progress string
	}
	if err := s.db.Model(&JobRun{}).
		Select("progress").
		Where("started_at >= ? AND progress <> ''", since).
		Scan(&rows).Error; err != nil {
		return 0, 0, err
	}

	var records, bytes int64
	for _, row := range rows {
		var progress RunProgress
		if err := json.Unmarshal([]byte(row.Progress), &progress); err == nil {
			records += progress.ReadRecords
			bytes += progress.ReadBytes
		}
	}
	return records, bytes, nil
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
