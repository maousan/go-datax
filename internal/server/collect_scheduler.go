package server

import (
	"errors"
	"log"
	"sync"
	"time"

	"github.com/longkeyy/go-datax/internal/quartzcron"
	"github.com/longkeyy/go-datax/internal/store"
)

type collectScheduleEntry struct {
	spec *quartzcron.Schedule
	next time.Time
}

// collectScheduler 在服务进程内按 Quartz cron 触发采集任务。
type collectScheduler struct {
	server *Server

	mu      sync.Mutex
	entries map[int64]*collectScheduleEntry
	stop    chan struct{}
	started bool
}

func newCollectScheduler(server *Server) *collectScheduler {
	return &collectScheduler{
		server:  server,
		entries: map[int64]*collectScheduleEntry{},
		stop:    make(chan struct{}),
	}
}

// Start 加载启用中的采集任务并开始每秒检查。
func (s *collectScheduler) Start() {
	if s.server.executor == nil {
		return
	}
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

// Stop 停止调度循环。
func (s *collectScheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return
	}
	s.started = false
	close(s.stop)
	s.stop = make(chan struct{})
}

// Reload 重新读取启用中的采集任务并计算下一次触发时间。
func (s *collectScheduler) Reload() {
	tasks, err := s.server.dataStore.ListEnabledCollectTasks()
	if err != nil {
		log.Printf("加载采集任务失败: %v", err)
		return
	}

	now := time.Now()
	entries := make(map[int64]*collectScheduleEntry, len(tasks))
	for _, task := range tasks {
		spec, err := quartzcron.Parse(task.CronExpr)
		if err != nil {
			log.Printf("采集任务 %s 的 cron 表达式无效: %v", task.Name, err)
			continue
		}
		next := spec.Next(now)
		if next.IsZero() {
			log.Printf("采集任务 %s 的 cron 表达式没有未来执行时间", task.Name)
			continue
		}
		entries[task.ID] = &collectScheduleEntry{spec: spec, next: next}
	}

	s.mu.Lock()
	s.entries = entries
	s.mu.Unlock()
}

func (s *collectScheduler) loop(stop <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			s.tick(now)
		case <-stop:
			return
		}
	}
}

func (s *collectScheduler) tick(now time.Time) {
	var due []int64

	s.mu.Lock()
	for id, entry := range s.entries {
		if entry.next.After(now) {
			continue
		}
		next := entry.spec.Next(now)
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

func (s *collectScheduler) dispatch(taskID int64) {
	task, err := s.server.dataStore.GetCollectTask(taskID)
	if err != nil || !task.Enabled {
		return
	}
	job, err := s.server.dataStore.GetSyncJob(task.SyncJobID)
	if err != nil {
		log.Printf("采集任务 %s 的datax任务定义不存在: %v", task.Name, err)
		return
	}
	config, err := s.server.exportConfig(job)
	if err != nil {
		log.Printf("采集任务 %s 的配置无效: %v", task.Name, err)
		return
	}

	if _, err := s.server.runCollectTask(task, job, config, store.RunTriggerSchedule); err != nil {
		if errors.Is(err, errCollectTaskBlocked) {
			log.Printf("采集任务 %s 已跳过：上一次运行尚未结束", task.Name)
			return
		}
		log.Printf("采集任务 %s 启动失败: %v", task.Name, err)
	}
}
