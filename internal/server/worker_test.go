package server

import (
	"context"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/longkeyy/go-datax/internal/worker"
)

func TestWorkerSupervisorExternal(t *testing.T) {
	token := "external-worker-token"
	ts := httptest.NewServer(worker.NewServer(token).Handler())
	t.Cleanup(ts.Close)

	supervisor := newWorkerSupervisor(WorkerConfig{URL: ts.URL, Token: token})
	if err := supervisor.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	t.Cleanup(supervisor.Stop)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	id, err := supervisor.StartDiagnosticRun(ctx, worker.RunSpec{
		Rows:      500,
		BatchSize: 100,
		Splits:    2,
		MaxBytes:  20000,
	})
	if err != nil {
		t.Fatalf("StartDiagnosticRun() failed: %v", err)
	}
	status, err := supervisor.WaitRun(ctx, id)
	if err != nil {
		t.Fatalf("WaitRun() failed: %v", err)
	}
	if status != worker.StatusSucceeded {
		t.Fatalf("status = %q, want succeeded", status)
	}
	if got := supervisor.RunStatus(id); got != worker.StatusSucceeded {
		t.Fatalf("tracked status = %q, want succeeded", got)
	}
}

func TestWorkerSupervisorSpawnsAndRestarts(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and spawns etl-worker")
	}

	bin := filepath.Join(t.TempDir(), "etl-worker")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, "github.com/longkeyy/go-datax/cmd/etl-worker")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build etl-worker: %v\n%s", err, output)
	}

	supervisor := newWorkerSupervisor(WorkerConfig{Bin: bin, Listen: "127.0.0.1:0"})
	supervisor.backoff = 50 * time.Millisecond
	if err := supervisor.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	t.Cleanup(supervisor.Stop)
	if supervisor.URL() == "" {
		t.Fatal("worker URL is empty after start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	id, err := supervisor.StartDiagnosticRun(ctx, worker.RunSpec{
		Rows:            1000000,
		BatchSize:       100,
		Splits:          2,
		MaxBytes:        20000,
		DelayPerBatchMS: 10,
	})
	if err != nil {
		t.Fatalf("StartDiagnosticRun() failed: %v", err)
	}

	supervisor.killProcess()
	deadline := time.Now().Add(10 * time.Second)
	for supervisor.RestartCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("worker did not restart after being killed")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := supervisor.RunStatus(id); got != worker.StatusFailed {
		t.Fatalf("interrupted run status = %q, want failed", got)
	}

	for supervisor.URL() == "" {
		if time.Now().After(deadline) {
			t.Fatal("restarted worker did not report a URL")
		}
		time.Sleep(20 * time.Millisecond)
	}

	nextID, err := supervisor.StartDiagnosticRun(ctx, worker.RunSpec{
		Rows:      100,
		BatchSize: 50,
		Splits:    1,
		MaxBytes:  10000,
	})
	if err != nil {
		t.Fatalf("StartDiagnosticRun() after restart failed: %v", err)
	}
	status, err := supervisor.WaitRun(ctx, nextID)
	if err != nil {
		t.Fatalf("WaitRun() after restart failed: %v", err)
	}
	if status != worker.StatusSucceeded {
		t.Fatalf("status after restart = %q, want succeeded", status)
	}
}
