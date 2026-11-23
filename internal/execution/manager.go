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
	DataXBin     string
	DataXVersion string
	LogsDir      string
	SnapshotsDir string
	MaxConcurrent int
	MaxRuns      int
	MaxAgeDays   int
	MaxLogBytes  int64
}

// Event 是推送给 SSE 订阅者的事件。
type Event struct {
	Type       string `json:"type"`
	Line       string `json:"line,omitempty"`
	Seq        int64  `json:"seq,omitempty"`
	Status     string `json:"status,omitempty"`
	ExitCode   *int   `json:"exitCode,omitempty"`
	FinishedAt string `json:"finishedAt,omitempty"`
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

// Start 启动一次运行。config 是已展开的 DataX 配置。
func (m *Manager) Start(job store.SyncJob, config map[string]any) (store.JobRun, error) {
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
		SyncJobID:   &jobID,
		SyncJobName: job.Name,
		Status:      store.RunStatusRunning,
		Trigger:     store.RunTriggerManual,
		StartedAt:   now,
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

	go m.execute(entry, releaseLock)
	return run, nil
}

func (m *Manager) failBeforeStart(run *store.JobRun, summary string) {
	now := time.Now()
	run.Status = store.RunStatusFailed
	run.FinishedAt = &now
	run.ErrorSummary = summary
	_ = m.store.UpdateJobRun(run)
}

func (m *Manager) execute(entry *runningRun, releaseLock func()) {
	logFile, err := os.OpenFile(entry.run.LogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		m.finish(entry, 1, fmt.Sprintf("打开日志文件失败: %v", err), false)
		releaseLock()
		return
	}

	writer := newLogWriter(logFile, m.cfg.MaxLogBytes, func(line string, seq int64) {
		m.broadcast(entry.run.ID, Event{Type: "log", Line: line, Seq: seq})
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
	m.prune()
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
	pruned, err := m.store.ListRunsForPrune(m.cfg.MaxRuns, m.cfg.MaxAgeDays)
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
