// Package monitor 采集进程运行时指标，供管理台的「服务监控」页面展示。
//
// 采样结果保存在一个有界环形窗口内，只用于展示趋势，不落盘。
package monitor

import (
	"bytes"
	"math"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultInterval 是默认采样间隔。
	DefaultInterval = 10 * time.Second
	// DefaultRetention 是默认保留的采样总时长。
	DefaultRetention = 15 * time.Minute

	// maxStackBytes 限制解析协程状态时抓取的堆栈大小，避免极端情况下占用过多内存。
	maxStackBytes = 32 << 20
)

// GoroutineStates 是按状态归类的协程数量。
type GoroutineStates struct {
	Running int `json:"running"`
	Blocked int `json:"blocked"`
	Waiting int `json:"waiting"`
	Syscall int `json:"syscall"`
	Other   int `json:"other"`
}

// Sample 是一次运行时采样。
type Sample struct {
	Time           time.Time       `json:"time"`
	CPUPercent     float64         `json:"cpuPercent"`
	Cores          int             `json:"cores"`
	Goroutines     int             `json:"goroutines"`
	PeakGoroutines int             `json:"peakGoroutines"`
	States         GoroutineStates `json:"states"`
	HeapAlloc      uint64          `json:"heapAlloc"`
	HeapSys        uint64          `json:"heapSys"`
	Sys            uint64          `json:"sys"`
	GCCount        uint32          `json:"gcCount"`
	GCPauseTotalMs float64         `json:"gcPauseTotalMs"`
}

// Snapshot 是服务监控接口返回的完整快照。
type Snapshot struct {
	IntervalSeconds  int       `json:"intervalSeconds"`
	RetentionSeconds int       `json:"retentionSeconds"`
	StartedAt        time.Time `json:"startedAt"`
	UptimeSeconds    int64     `json:"uptimeSeconds"`
	GoVersion        string    `json:"goVersion"`
	Current          Sample    `json:"current"`
	Samples          []Sample  `json:"samples"`
}

// Sampler 周期性采集进程指标并保留最近一段时间的历史。
type Sampler struct {
	mu        sync.Mutex
	interval  time.Duration
	retention time.Duration
	maxItems  int
	startedAt time.Time
	samples   []Sample
	peak      int
	cpuTotal  float64
	cpuAt     time.Time

	started  bool
	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}

	now        func() time.Time
	readCPU    func() (float64, error)
	readStates func() GoroutineStates
}

// New 创建一个采样器。interval 或 retention 非正时使用默认值。
func New(interval, retention time.Duration) *Sampler {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if retention <= 0 {
		retention = DefaultRetention
	}
	maxItems := int(retention / interval)
	if maxItems < 2 {
		maxItems = 2
	}
	return &Sampler{
		interval:   interval,
		retention:  retention,
		maxItems:   maxItems,
		startedAt:  time.Now(),
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
		now:        time.Now,
		readCPU:    processCPUSeconds,
		readStates: collectGoroutineStates,
	}
}

// Start 启动采样循环，并立即采集第一份数据。
func (s *Sampler) Start() {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.mu.Unlock()

	s.collect()

	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		defer close(s.doneCh)
		for {
			select {
			case <-ticker.C:
				s.collect()
			case <-s.stopCh:
				return
			}
		}
	}()
}

// Stop 停止采样循环。未启动时直接返回。
func (s *Sampler) Stop() {
	s.mu.Lock()
	started := s.started
	s.mu.Unlock()
	if !started {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
		<-s.doneCh
	})
}

// Snapshot 返回当前指标和窗口内的历史采样。
func (s *Sampler) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	samples := make([]Sample, len(s.samples))
	copy(samples, s.samples)

	snapshot := Snapshot{
		IntervalSeconds:  int(s.interval / time.Second),
		RetentionSeconds: int(s.retention / time.Second),
		StartedAt:        s.startedAt,
		UptimeSeconds:    int64(s.now().Sub(s.startedAt).Seconds()),
		GoVersion:        runtime.Version(),
		Current:          Sample{Cores: runtime.NumCPU()},
		Samples:          samples,
	}
	if len(samples) > 0 {
		snapshot.Current = samples[len(samples)-1]
	}
	return snapshot
}

func (s *Sampler) collect() {
	now := s.now()

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	states := s.readStates()
	goroutines := runtime.NumGoroutine()
	cpuTotal, cpuErr := s.readCPU()

	s.mu.Lock()
	defer s.mu.Unlock()

	cpuPercent := 0.0
	if cpuErr == nil {
		if !s.cpuAt.IsZero() {
			elapsed := now.Sub(s.cpuAt).Seconds()
			delta := cpuTotal - s.cpuTotal
			if elapsed > 0 && delta >= 0 {
				// 进程 CPU 占整机全部核心的百分比，与任务管理器/监控面板口径一致。
				cpuPercent = round2(delta / elapsed / float64(runtime.NumCPU()) * 100)
			}
		}
		s.cpuTotal = cpuTotal
		s.cpuAt = now
	}

	if goroutines > s.peak {
		s.peak = goroutines
	}

	s.samples = append(s.samples, Sample{
		Time:           now,
		CPUPercent:     cpuPercent,
		Cores:          runtime.NumCPU(),
		Goroutines:     goroutines,
		PeakGoroutines: s.peak,
		States:         states,
		HeapAlloc:      mem.HeapAlloc,
		HeapSys:        mem.HeapSys,
		Sys:            mem.Sys,
		GCCount:        mem.NumGC,
		GCPauseTotalMs: round2(float64(mem.PauseTotalNs) / 1e6),
	})
	if len(s.samples) > s.maxItems {
		// 只保留最近窗口内的采样，并让底层数组跟随迁移，避免长期占用旧容量。
		trimmed := make([]Sample, s.maxItems)
		copy(trimmed, s.samples[len(s.samples)-s.maxItems:])
		s.samples = trimmed
	}
}

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}

// collectGoroutineStates 通过抓取全部协程堆栈统计运行状态分布。
// runtime 没有直接暴露状态计数，这是唯一无需额外依赖的可行方式；
// 采样间隔为秒级，短暂停顿可以接受。
func collectGoroutineStates() GoroutineStates {
	buf := make([]byte, 64<<10)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) || len(buf) >= maxStackBytes {
			buf = buf[:n]
			break
		}
		buf = make([]byte, len(buf)*2)
	}
	return parseGoroutineStates(buf)
}

func parseGoroutineStates(stack []byte) GoroutineStates {
	var states GoroutineStates
	for _, line := range bytes.Split(stack, []byte("\n")) {
		if !bytes.HasPrefix(line, []byte("goroutine ")) {
			continue
		}
		open := bytes.IndexByte(line, '[')
		if open < 0 {
			states.Other++
			continue
		}
		rest := line[open+1:]
		end := bytes.IndexAny(rest, ",]")
		if end < 0 {
			states.Other++
			continue
		}
		state := strings.TrimSpace(string(rest[:end]))
		switch {
		case state == "":
			states.Other++
		case state == "running" || state == "runnable":
			states.Running++
		case state == "syscall":
			states.Syscall++
		case state == "select", strings.HasPrefix(state, "chan "),
			state == "semacquire", strings.HasPrefix(state, "sync."):
			states.Blocked++
		default:
			states.Waiting++
		}
	}
	return states
}
