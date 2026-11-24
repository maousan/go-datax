package monitor

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestParseGoroutineStates(t *testing.T) {
	stack := strings.Join([]string{
		"goroutine 1 [running]:",
		"main.main()",
		"\t/app/main.go:10 +0x1",
		"goroutine 2 [runnable]:",
		"runtime.doInit1()",
		"goroutine 3 [chan receive, 3 minutes]:",
		"goroutine 4 [chan send]:",
		"goroutine 5 [select, 1 minutes]:",
		"goroutine 6 [semacquire]:",
		"goroutine 7 [sync.Mutex.Lock]:",
		"goroutine 8 [IO wait]:",
		"goroutine 9 [sleep, 5 minutes]:",
		"goroutine 10 [syscall, 10 minutes]:",
		"goroutine 11 [GC worker (idle)]:",
		"goroutine 12 []:",
		"goroutine 13",
		"",
	}, "\n")

	got := parseGoroutineStates([]byte(stack))
	want := GoroutineStates{
		Running: 2,
		Blocked: 5,
		Waiting: 3,
		Syscall: 1,
		Other:   2,
	}
	if got != want {
		t.Fatalf("parseGoroutineStates() = %+v, want %+v", got, want)
	}
}

func TestSamplerCollectsWindow(t *testing.T) {
	s := New(10*time.Second, 30*time.Second)
	base := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	current := base
	cpuSeconds := 10.0

	s.startedAt = base
	s.now = func() time.Time { return current }
	s.readCPU = func() (float64, error) { return cpuSeconds, nil }
	s.readStates = func() GoroutineStates {
		return GoroutineStates{Running: 1, Waiting: 2}
	}

	for i := 0; i < 5; i++ {
		s.collect()
		current = current.Add(10 * time.Second)
		cpuSeconds += 0.5
	}

	snapshot := s.Snapshot()
	if len(snapshot.Samples) != 3 {
		t.Fatalf("len(Samples) = %d, want 3", len(snapshot.Samples))
	}
	if !snapshot.Samples[0].Time.Equal(base.Add(20 * time.Second)) {
		t.Fatalf("first retained sample time = %v, want %v", snapshot.Samples[0].Time, base.Add(20*time.Second))
	}
	if !snapshot.Current.Time.Equal(base.Add(40 * time.Second)) {
		t.Fatalf("current sample time = %v, want %v", snapshot.Current.Time, base.Add(40*time.Second))
	}

	cores := float64(runtime.NumCPU())
	wantCPU := round2(0.5 / 10 / cores * 100)
	if snapshot.Current.CPUPercent != wantCPU {
		t.Fatalf("current CPU = %v, want %v", snapshot.Current.CPUPercent, wantCPU)
	}
	if snapshot.Current.States != (GoroutineStates{Running: 1, Waiting: 2}) {
		t.Fatalf("current states = %+v", snapshot.Current.States)
	}
	if snapshot.Current.PeakGoroutines < snapshot.Current.Goroutines {
		t.Fatalf("peak %d < current %d", snapshot.Current.PeakGoroutines, snapshot.Current.Goroutines)
	}
	for _, sample := range snapshot.Samples {
		if sample.PeakGoroutines < sample.Goroutines {
			t.Fatalf("sample peak %d < goroutines %d", sample.PeakGoroutines, sample.Goroutines)
		}
	}
	if snapshot.IntervalSeconds != 10 || snapshot.RetentionSeconds != 30 {
		t.Fatalf("interval/retention = %d/%d", snapshot.IntervalSeconds, snapshot.RetentionSeconds)
	}
	if snapshot.UptimeSeconds != 50 {
		t.Fatalf("uptime = %d, want 50", snapshot.UptimeSeconds)
	}
}

func TestSnapshotBeforeSampling(t *testing.T) {
	s := New(0, 0)
	snapshot := s.Snapshot()

	if snapshot.Current.Cores != runtime.NumCPU() {
		t.Fatalf("cores = %d, want %d", snapshot.Current.Cores, runtime.NumCPU())
	}
	if len(snapshot.Samples) != 0 {
		t.Fatalf("len(Samples) = %d, want 0", len(snapshot.Samples))
	}
	if snapshot.IntervalSeconds != int(DefaultInterval/time.Second) {
		t.Fatalf("interval = %d", snapshot.IntervalSeconds)
	}
	if snapshot.RetentionSeconds != int(DefaultRetention/time.Second) {
		t.Fatalf("retention = %d", snapshot.RetentionSeconds)
	}
	if !strings.HasPrefix(snapshot.GoVersion, "go") {
		t.Fatalf("go version = %q", snapshot.GoVersion)
	}
}

func TestSamplerStartCollectsImmediatelyAndStops(t *testing.T) {
	s := New(time.Hour, time.Hour)
	s.readCPU = func() (float64, error) { return 0, nil }
	s.readStates = func() GoroutineStates { return GoroutineStates{} }

	s.Start()
	snapshot := s.Snapshot()
	if len(snapshot.Samples) != 1 {
		t.Fatalf("len(Samples) = %d, want 1", len(snapshot.Samples))
	}
	s.Stop()
	s.Stop() // 重复调用应安全
}
