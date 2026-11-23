package execution

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/longkeyy/go-datax/internal/store"
)

// ErrJobRunning 表示同一个任务已有运行中的实例。
var ErrJobRunning = errors.New("sync job is already running")

// ErrTooManyRuns 表示达到全局并发上限。
var ErrTooManyRuns = errors.New("too many running jobs")

// Config 是执行器配置。
type Config struct {
	DataXBin      string
	DataXVersion  string
	LogsDir       string
	SnapshotsDir  string
	MaxConcurrent int
	MaxRuns       int
	MaxAgeDays    int
	MaxLogBytes   int64
}

// RuntimeSettings 是可在管理台热更新的运行参数。
type RuntimeSettings struct {
	MaxConcurrent int   `json:"maxConcurrent"`
	MaxRuns       int   `json:"maxRuns"`
	MaxAgeDays    int   `json:"maxAgeDays"`
	MaxLogBytes   int64 `json:"maxLogBytes"`
}

// Event 是推送给 SSE 订阅者的事件。
type Event struct {
	Type       string             `json:"type"`
	Line       string             `json:"line,omitempty"`
	Seq        int64              `json:"seq,omitempty"`
	Status     string             `json:"status,omitempty"`
	ExitCode   *int               `json:"exitCode,omitempty"`
	FinishedAt string             `json:"finishedAt,omitempty"`
	Progress   *store.RunProgress `json:"progress,omitempty"`
}

type runningRun struct {
	run      store.JobRun
	cmd      *exec.Cmd
	canceled bool
}

// Manager 负责子进程生命周期、日志与状态流转。
type Manager struct {
	cfg   Config
	store *store.Store

	mu          sync.Mutex
	running     map[int64]*runningRun
	jobLocks    map[int64]bool
	subscribers map[int64]map[chan Event]struct{}

	command func(snapshotPath string) *exec.Cmd

	onFinish func(store.JobRun, bool)
}

// Option 调整执行器行为（测试可注入命令）。
type Option func(*Manager)

// WithCommandBuilder 替换实际的 datax 命令构造。
func WithCommandBuilder(builder func(snapshotPath string) *exec.Cmd) Option {
	return func(m *Manager) { m.command = builder }
}

// New 创建执行器。
func New(cfg Config, dataStore *store.Store, options ...Option) (*Manager, error) {
	if dataStore == nil {
		return nil, fmt.Errorf("store is required")
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 2
	}
	if cfg.MaxLogBytes <= 0 {
		cfg.MaxLogBytes = 10 * 1024 * 1024
	}

	m := &Manager{
		cfg:         cfg,
		store:       dataStore,
		running:     map[int64]*runningRun{},
		jobLocks:    map[int64]bool{},
		subscribers: map[int64]map[chan Event]struct{}{},
	}
	for _, option := range options {
		option(m)
	}
	if m.command == nil {
		bin := cfg.DataXBin
		m.command = func(snapshotPath string) *exec.Cmd {
			return exec.Command(bin, "-job", snapshotPath)
		}
	}
	return m, nil
}

// DataXVersion 返回启动时解析到的 datax 版本。
func (m *Manager) DataXVersion() string { return m.cfg.DataXVersion }

// Prune 按保留策略清理运行记录与文件。
func (m *Manager) Prune() { m.prune() }

// Runtime 返回当前运行参数。
func (m *Manager) Runtime() RuntimeSettings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return RuntimeSettings{
		MaxConcurrent: m.cfg.MaxConcurrent,
		MaxRuns:       m.cfg.MaxRuns,
		MaxAgeDays:    m.cfg.MaxAgeDays,
		MaxLogBytes:   m.cfg.MaxLogBytes,
	}
}

// UpdateRuntime 热更新运行参数，并按新的保留策略立即清理。
func (m *Manager) UpdateRuntime(settings RuntimeSettings) {
	m.mu.Lock()
	if settings.MaxConcurrent > 0 {
		m.cfg.MaxConcurrent = settings.MaxConcurrent
	}
	if settings.MaxRuns > 0 {
		m.cfg.MaxRuns = settings.MaxRuns
	}
	if settings.MaxAgeDays > 0 {
		m.cfg.MaxAgeDays = settings.MaxAgeDays
	}
	if settings.MaxLogBytes > 0 {
		m.cfg.MaxLogBytes = settings.MaxLogBytes
	}
	m.mu.Unlock()
	m.prune()
}

// SetOnFinish 设置运行结束后的回调。willRetry 表示失败后是否会安排自动重试。
func (m *Manager) SetOnFinish(fn func(store.JobRun, bool)) {
	m.mu.Lock()
	m.onFinish = fn
	m.mu.Unlock()
}

func (m *Manager) notifyFinish(run store.JobRun, willRetry bool) {
	m.mu.Lock()
	fn := m.onFinish
	m.mu.Unlock()
	if fn != nil {
		fn(run, willRetry)
	}
}

// Start 启动一次手动运行。config 是已展开的 DataX 配置。
func (m *Manager) Start(job store.SyncJob, config map[string]any) (store.JobRun, error) {
	return m.start(job, config, store.RunTriggerManual, 0)
}

// Rerun 使用任务当前配置重新执行一次。
func (m *Manager) Rerun(job store.SyncJob, config map[string]any) (store.JobRun, error) {
	return m.start(job, config, store.RunTriggerRerun, 0)
}

func (m *Manager) start(job store.SyncJob, config map[string]any, trigger string, retryAttempt int) (store.JobRun, error) {
	m.mu.Lock()
	if m.jobLocks[job.ID] {
		m.mu.Unlock()
		return store.JobRun{}, ErrJobRunning
	}
	if len(m.jobLocks) >= m.cfg.MaxConcurrent {
		m.mu.Unlock()
		return store.JobRun{}, ErrTooManyRuns
	}
	m.jobLocks[job.ID] = true
	m.mu.Unlock()

	releaseLock := func() {
		m.mu.Lock()
		delete(m.jobLocks, job.ID)
		m.mu.Unlock()
	}

	now := time.Now()
	jobID := job.ID
	run := store.JobRun{
		SyncJobID:    &jobID,
		SyncJobName:  job.Name,
		Status:       store.RunStatusRunning,
		Trigger:      trigger,
		StartedAt:    now,
		RetryAttempt: retryAttempt,
	}
	if err := m.store.CreateJobRun(&run); err != nil {
		releaseLock()
		return store.JobRun{}, err
	}

	snapshotPath := filepath.Join(m.cfg.SnapshotsDir, fmt.Sprintf("%d.json", run.ID))
	logPath := filepath.Join(m.cfg.LogsDir, fmt.Sprintf("%d.log", run.ID))

	snapshot, err := json.MarshalIndent(config, "", "  ")
	if err == nil {
		err = os.WriteFile(snapshotPath, snapshot, 0o600)
	}
	if err != nil {
		m.failBeforeStart(&run, fmt.Sprintf("写入运行快照失败: %v", err))
		releaseLock()
		return run, err
	}

	run.SnapshotPath = snapshotPath
	run.LogPath = logPath
	if err := m.store.UpdateJobRun(&run); err != nil {
		m.failBeforeStart(&run, "更新运行记录失败")
		releaseLock()
		return run, err
	}

	entry := &runningRun{run: run}
	m.mu.Lock()
	m.running[run.ID] = entry
	m.mu.Unlock()

	go m.execute(entry, job, releaseLock)
	return run, nil
}

func (m *Manager) failBeforeStart(run *store.JobRun, summary string) {
	now := time.Now()
	run.Status = store.RunStatusFailed
	run.FinishedAt = &now
	run.ErrorSummary = summary
	_ = m.store.UpdateJobRun(run)
}

func (m *Manager) execute(entry *runningRun, job store.SyncJob, releaseLock func()) {
	logFile, err := os.OpenFile(entry.run.LogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		m.finish(entry, 1, fmt.Sprintf("打开日志文件失败: %v", err), false)
		releaseLock()
		scheduled := m.maybeScheduleRetry(job, entry.run)
		m.notifyFinish(entry.run, scheduled)
		return
	}

	m.mu.Lock()
	maxLogBytes := m.cfg.MaxLogBytes
	m.mu.Unlock()

	writer := newLogWriter(logFile, maxLogBytes, func(line string, seq int64) {
		m.broadcast(entry.run.ID, Event{Type: "log", Line: line, Seq: seq})
		m.observeProgress(entry, line)
	})
	defer writer.Close()

	cmd := m.command(entry.run.SnapshotPath)
	cmd.Stdout = writer
	cmd.Stderr = writer
	configureProcess(cmd)

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		m.finish(entry, 1, fmt.Sprintf("启动 datax 进程失败: %v", err), false)
		releaseLock()
		scheduled := m.maybeScheduleRetry(job, entry.run)
		m.notifyFinish(entry.run, scheduled)
		return
	}

	m.mu.Lock()
	entry.cmd = cmd
	canceledEarly := entry.canceled
	m.mu.Unlock()

	if canceledEarly {
		// 取消请求可能早于进程启动完成，这里补一次终止。
		_ = killProcessTree(cmd)
	}

	waitErr := cmd.Wait()
	_ = logFile.Close()
	writer.Close()

	exitCode := 0
	if waitErr != nil {
		exitCode = 1
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}

	m.mu.Lock()
	canceled := entry.canceled
	m.mu.Unlock()

	summary := ""
	if canceled {
		summary = "运行已取消"
	} else if exitCode != 0 {
		summary = writer.LastLine()
		if summary == "" {
			summary = waitErr.Error()
		}
	}

	m.finish(entry, exitCode, summary, canceled)
	releaseLock()
	scheduled := m.maybeScheduleRetry(job, entry.run)
	m.notifyFinish(entry.run, scheduled)
	m.prune()
}

// maybeScheduleRetry 在运行失败后按任务策略安排一次自动重试，返回是否已安排。
func (m *Manager) maybeScheduleRetry(job store.SyncJob, failed store.JobRun) bool {
	if failed.Status != store.RunStatusFailed {
		return false
	}
	if job.RetryMax <= 0 || failed.RetryAttempt >= job.RetryMax {
		return false
	}

	interval := time.Duration(job.RetryIntervalSeconds) * time.Second
	if interval < 0 {
		interval = 0
	}

	go func() {
		if interval > 0 {
			time.Sleep(interval)
		}

		// 延迟期间任务可能被删除，或重试策略被改小。
		current, err := m.store.GetSyncJob(job.ID)
		if err != nil || current.RetryMax <= failed.RetryAttempt {
			return
		}

		config, err := readRunSnapshot(failed.SnapshotPath)
		if err != nil {
			return
		}
		_, _ = m.start(current, config, store.RunTriggerRetry, failed.RetryAttempt+1)
	}()
	return true
}

func readRunSnapshot(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return config, nil
}

// observeProgress 解析引擎上报的进度快照，持久化并推送给订阅者。
func (m *Manager) observeProgress(entry *runningRun, line string) {
	progress, ok := parseProgressLine(line)
	if !ok {
		return
	}
	data, err := json.Marshal(progress)
	if err != nil {
		return
	}

	m.mu.Lock()
	entry.run.Progress = string(data)
	m.mu.Unlock()

	_ = m.store.UpdateJobRunProgress(entry.run.ID, string(data))
	snapshot := progress
	m.broadcast(entry.run.ID, Event{Type: "progress", Progress: &snapshot})
}

func (m *Manager) finish(entry *runningRun, exitCode int, summary string, canceled bool) {
	now := time.Now()
	entry.run.FinishedAt = &now
	entry.run.ExitCode = &exitCode
	entry.run.ErrorSummary = truncate(summary, 900)
	if info, err := os.Stat(entry.run.LogPath); err == nil {
		entry.run.LogSize = info.Size()
	}

	switch {
	case canceled:
		entry.run.Status = store.RunStatusCanceled
	case exitCode == 0:
		entry.run.Status = store.RunStatusSucceeded
	default:
		entry.run.Status = store.RunStatusFailed
	}

	_ = m.store.UpdateJobRun(&entry.run)

	m.mu.Lock()
	delete(m.running, entry.run.ID)
	subs := m.subscribers[entry.run.ID]
	delete(m.subscribers, entry.run.ID)
	m.mu.Unlock()

	statusEvent := Event{
		Type:       "status",
		Status:     entry.run.Status,
		ExitCode:   entry.run.ExitCode,
		FinishedAt: now.UTC().Format(time.RFC3339),
	}
	endEvent := Event{Type: "end"}
	for ch := range subs {
		select {
		case ch <- statusEvent:
		default:
		}
		select {
		case ch <- endEvent:
		default:
		}
		close(ch)
	}
}

// Cancel 终止运行中的子进程树。
func (m *Manager) Cancel(runID int64) error {
	m.mu.Lock()
	entry, ok := m.running[runID]
	if ok {
		entry.canceled = true
	}
	cmd := (*exec.Cmd)(nil)
	if ok {
		cmd = entry.cmd
	}
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("运行不在进行中")
	}
	if cmd != nil && cmd.Process != nil {
		return killProcessTree(cmd)
	}
	return nil
}

// Subscribe 返回回放事件与实时事件通道。offset 为日志字节偏移。
func (m *Manager) Subscribe(runID int64, offset int64) ([]Event, <-chan Event, func(), error) {
	run, err := m.store.GetJobRun(runID)
	if err != nil {
		return nil, nil, nil, err
	}

	replay, err := replayLog(run.LogPath, offset)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil, err
	}

	// 订阅时先补一条当前进度，避免中途接入的客户端要等到下一次上报。
	if run.Progress != "" {
		var progress store.RunProgress
		if err := json.Unmarshal([]byte(run.Progress), &progress); err == nil {
			replay = append(replay, Event{Type: "progress", Progress: &progress})
		}
	}

	if run.Status != store.RunStatusRunning {
		if run.FinishedAt != nil {
			replay = append(replay, Event{
				Type:       "status",
				Status:     run.Status,
				ExitCode:   run.ExitCode,
				FinishedAt: run.FinishedAt.UTC().Format(time.RFC3339),
			})
		}
		replay = append(replay, Event{Type: "end"})
		return replay, nil, func() {}, nil
	}

	ch := make(chan Event, 128)
	m.mu.Lock()
	if m.subscribers[runID] == nil {
		m.subscribers[runID] = map[chan Event]struct{}{}
	}
	m.subscribers[runID][ch] = struct{}{}
	m.mu.Unlock()

	cancel := func() {
		m.mu.Lock()
		if subs, ok := m.subscribers[runID]; ok {
			if _, exists := subs[ch]; exists {
				delete(subs, ch)
				close(ch)
			}
		}
		m.mu.Unlock()
	}
	return replay, ch, cancel, nil
}

func (m *Manager) broadcast(runID int64, event Event) {
	m.mu.Lock()
	subs := m.subscribers[runID]
	m.mu.Unlock()

	for ch := range subs {
		select {
		case ch <- event:
		default:
		}
	}
}

func (m *Manager) prune() {
	m.mu.Lock()
	maxRuns, maxAgeDays := m.cfg.MaxRuns, m.cfg.MaxAgeDays
	m.mu.Unlock()

	pruned, err := m.store.ListRunsForPrune(maxRuns, maxAgeDays)
	if err != nil {
		return
	}
	for _, run := range pruned {
		_ = m.store.DeleteJobRun(run.ID)
		if run.LogPath != "" {
			_ = os.Remove(run.LogPath)
		}
		if run.SnapshotPath != "" {
			_ = os.Remove(run.SnapshotPath)
		}
	}
}

func replayLog(path string, offset int64) ([]Event, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	if offset > 0 {
		if _, err := file.Seek(offset, 0); err != nil {
			return nil, err
		}
	}

	var events []Event
	reader := newLineReader(file)
	seq := offset
	for {
		line, read, err := reader.Next()
		if read > 0 {
			seq += int64(read)
			events = append(events, Event{Type: "log", Line: line, Seq: seq})
		}
		if err != nil {
			break
		}
	}
	return events, nil
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
