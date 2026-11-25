package server

import (
	"context"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/longkeyy/go-datax/internal/store"
	"github.com/longkeyy/go-datax/internal/worker"
)

func TestWorkerSupervisorExternal(t *testing.T) {
	token := "external-worker-token"
	ts := httptest.NewServer(worker.NewServer(token).Handler())
	t.Cleanup(ts.Close)

	supervisor := newWorkerSupervisor(WorkerConfig{URL: ts.URL, Token: token}, nil)
	if err := supervisor.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	t.Cleanup(supervisor.Stop)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	id, err := supervisor.StartDiagnosticRun(ctx, worker.RunSpec{
		Rows:      512,
		BatchSize: 256,
		Splits:    1,
		MaxBytes:  51200,
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

func TestWorkerSupervisorExternalDetectsDisconnect(t *testing.T) {
	token := "external-worker-token"
	ts := httptest.NewServer(worker.NewServer(token).Handler())

	supervisor := newWorkerSupervisor(WorkerConfig{URL: ts.URL, Token: token}, nil)
	supervisor.healthInterval = 50 * time.Millisecond
	if err := supervisor.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	t.Cleanup(supervisor.Stop)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id, err := supervisor.StartDiagnosticRun(ctx, worker.RunSpec{
		Rows:            1024,
		BatchSize:       256,
		Splits:          2,
		MaxBytes:        51200,
		DelayPerBatchMS: 200,
	})
	if err != nil {
		t.Fatalf("StartDiagnosticRun() failed: %v", err)
	}

	ts.Close()

	deadline := time.Now().Add(5 * time.Second)
	for supervisor.RunStatus(id) != worker.StatusFailed {
		if time.Now().After(deadline) {
			t.Fatalf("run status = %q, want failed after external worker disconnect", supervisor.RunStatus(id))
		}
		time.Sleep(20 * time.Millisecond)
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

	dataStore, err := store.Open(filepath.Join(t.TempDir(), "admin.db"))
	if err != nil {
		t.Fatalf("store.Open() failed: %v", err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })

	supervisor := newWorkerSupervisor(WorkerConfig{Bin: bin, Listen: "127.0.0.1:0"}, dataStore)
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
		BatchSize:       256,
		Splits:          2,
		MaxBytes:        51200,
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
	pipelineRunID := supervisor.PipelineRunID(id)
	if pipelineRunID == 0 {
		t.Fatal("interrupted run has no PipelineRun record")
	}
	record, err := dataStore.GetPipelineRun(pipelineRunID)
	if err != nil {
		t.Fatalf("GetPipelineRun() failed: %v", err)
	}
	if record.Status != store.PipelineRunStatusFailed {
		t.Fatalf("persisted status = %q, want failed", record.Status)
	}

	for supervisor.URL() == "" {
		if time.Now().After(deadline) {
			t.Fatal("restarted worker did not report a URL")
		}
		time.Sleep(20 * time.Millisecond)
	}

	nextID, err := supervisor.StartDiagnosticRun(ctx, worker.RunSpec{
		Rows:      512,
		BatchSize: 256,
		Splits:    1,
		MaxBytes:  51200,
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
	secondPipelineRunID := supervisor.PipelineRunID(nextID)
	record, err = dataStore.GetPipelineRun(secondPipelineRunID)
	if err != nil {
		t.Fatalf("GetPipelineRun() after restart failed: %v", err)
	}
	if record.Status != store.PipelineRunStatusSucceeded {
		t.Fatalf("persisted status after restart = %q, want succeeded", record.Status)
	}
}
