package server

import (
	"errors"
	"log"
	"sync"
	"time"

	"github.com/longkeyy/go-datax/internal/store"
)

type pipelineScheduleEntry struct {
	timing *pipelineScheduleTiming
	next   time.Time
}

// pipelineScheduler triggers Pipeline runs from enabled schedules.
type pipelineScheduler struct {
	server *Server

	mu      sync.Mutex
	entries map[int64]*pipelineScheduleEntry
	stop    chan struct{}
	started bool
}

func newPipelineScheduler(server *Server) *pipelineScheduler {
	return &pipelineScheduler{
		server:  server,
		entries: map[int64]*pipelineScheduleEntry{},
		stop:    make(chan struct{}),
	}
}

// Start loads enabled schedules and checks them once per second.
func (s *pipelineScheduler) Start() {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	stop := s.stop
	s.mu.Unlock()

	s.Reload()
	go s.loop(stop)
}

// Stop stops the scheduler loop.
func (s *pipelineScheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return
	}
	s.started = false
	close(s.stop)
	s.stop = make(chan struct{})
}

// Reload recalculates the next trigger for every enabled schedule.
func (s *pipelineScheduler) Reload() {
	schedules, err := s.server.dataStore.ListEnabledPipelineSchedules()
	if err != nil {
		log.Printf("加载 Pipeline 调度失败: %v", err)
		return
	}
	now := time.Now()
	entries := make(map[int64]*pipelineScheduleEntry, len(schedules))
	for _, schedule := range schedules {
		timing, err := parsePipelineScheduleTiming(schedule.CronExpr, schedule.Timezone)
		if err != nil {
			log.Printf("Pipeline 调度 %s 配置无效: %v", schedule.Name, err)
			continue
		}
		next := timing.next(now)
		if next.IsZero() {
			log.Printf("Pipeline 调度 %s 的 cron 表达式没有未来执行时间", schedule.Name)
			continue
		}
		entries[schedule.ID] = &pipelineScheduleEntry{timing: timing, next: next}
	}
	s.mu.Lock()
	s.entries = entries
	s.mu.Unlock()
}

func (s *pipelineScheduler) loop(stop <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	queueTicker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	defer queueTicker.Stop()
	for {
		select {
		case now := <-ticker.C:
			s.tick(now)
		case <-queueTicker.C:
			s.server.resumePipelineQueues()
		case <-stop:
			return
		}
	}
}

func (s *pipelineScheduler) tick(now time.Time) {
	var due []int64
	s.mu.Lock()
	for id, entry := range s.entries {
		if entry.next.After(now) {
			continue
		}
		next := entry.timing.next(now)
		if next.IsZero() {
			delete(s.entries, id)
		} else {
			entry.next = next
		}
		due = append(due, id)
	}
	s.mu.Unlock()

	for _, id := range due {
		go s.dispatch(id)
	}
}

func (s *pipelineScheduler) dispatch(scheduleID int64) {
	schedule, err := s.server.dataStore.GetPipelineSchedule(scheduleID)
	if err != nil || !schedule.Enabled {
		return
	}
	pipeline, err := s.server.dataStore.GetPipeline(schedule.PipelineID)
	if err != nil {
		log.Printf("Pipeline 调度 %s 的 Pipeline 不存在: %v", schedule.Name, err)
		return
	}
	version, err := resolvePipelineScheduleVersion(s.server.dataStore, schedule, pipeline.ID)
	if err != nil {
		log.Printf("Pipeline 调度 %s 无可用版本: %v", schedule.Name, err)
		return
	}
	_, err = s.server.startPipelineRun(pipeline, version, pipelineStartOptions{
		Trigger:    store.PipelineRunTriggerSchedule,
		Blocking:   schedule.BlockingPolicy,
		ScheduleID: &schedule.ID,
		Timeout:    time.Duration(schedule.TimeoutSeconds) * time.Second,
	})
	if err != nil {
		var startErr *pipelineStartError
		if errors.As(err, &startErr) && startErr.Code == "pipeline_running" {
			log.Printf("Pipeline 调度 %s 已跳过：上一次运行尚未结束", schedule.Name)
			return
		}
		log.Printf("Pipeline 调度 %s 启动失败: %v", schedule.Name, err)
	}
}

func resolvePipelineScheduleVersion(dataStore *store.Store, schedule store.PipelineSchedule, pipelineID int64) (store.PipelineVersion, error) {
	if schedule.PinnedVersionID != nil {
		version, err := dataStore.GetPipelineVersion(*schedule.PinnedVersionID)
		if err != nil {
			return store.PipelineVersion{}, err
		}
		if version.PipelineID != pipelineID {
			return store.PipelineVersion{}, errors.New("pin 版本不属于该 Pipeline")
		}
		return version, nil
	}
	return dataStore.LatestPipelineVersion(pipelineID)
}
