package worker

import (
	"context"
	"sync"
	"testing"
	"time"
)

type recordingSink struct {
	mu      sync.Mutex
	batches []Batch
}

func (s *recordingSink) SendBatch(ctx context.Context, batch Batch) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	s.batches = append(s.batches, batch)
	s.mu.Unlock()
	return nil
}

func (s *recordingSink) snapshot() []Batch {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Batch(nil), s.batches...)
}

func TestRunDiagnosticCompletes(t *testing.T) {
	sink := &recordingSink{}
	var events []Event
	status, err := RunDiagnostic(context.Background(), RunSpec{
		Rows:      1024,
		BatchSize: 256,
		Splits:    2,
		MaxBytes:  51200,
	}, sink, func(event Event) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatalf("RunDiagnostic() failed: %v", err)
	}
	if status != StatusSucceeded {
		t.Fatalf("status = %q, want succeeded", status)
	}

	batches := sink.snapshot()
	if len(batches) != 4 {
		t.Fatalf("batches = %d, want 4", len(batches))
	}
	var rows int
	for _, batch := range batches {
		rows += batch.Rows
	}
	if rows != 1024 {
		t.Fatalf("rows = %d, want 1024", rows)
	}

	if len(events) == 0 {
		t.Fatal("expected progress/done events")
	}
	last := events[len(events)-1]
	if last.Type != "done" || last.Status != StatusSucceeded || last.ReadRecords != 1024 {
		t.Fatalf("last event = %+v, want done/succeeded/1024", last)
	}
	if len(last.Tasks) != 2 {
		t.Fatalf("task stats = %d, want 2", len(last.Tasks))
	}
	var taskRows int64
	for _, task := range last.Tasks {
		taskRows += task.Records
	}
	if taskRows != 1024 {
		t.Fatalf("task rows = %d, want 1024", taskRows)
	}
}

func TestRunDiagnosticCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	statusCh := make(chan RunStatus, 1)
	go func() {
		status, _ := RunDiagnostic(ctx, RunSpec{
			Rows:            1000000,
			BatchSize:       256,
			Splits:          2,
			MaxBytes:        51200,
			DelayPerBatchMS: 20,
		}, &recordingSink{}, nil)
		statusCh <- status
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case status := <-statusCh:
		if status != StatusCanceled {
			t.Fatalf("status = %q, want canceled", status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunDiagnostic() did not stop after cancel")
	}
}

func TestByteBudgetBlocksUntilRelease(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	budget := newByteBudget(ctx, 100)

	if err := budget.acquire(ctx, 100); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	acquired := make(chan error, 1)
	go func() {
		acquired <- budget.acquire(ctx, 100)
	}()

	select {
	case err := <-acquired:
		t.Fatalf("second acquire returned early: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	budget.release(100)
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatalf("second acquire after release: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("second acquire stayed blocked after release")
	}
}

func TestByteBudgetRejectsOversizedBatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	budget := newByteBudget(ctx, 100)
	if err := budget.acquire(ctx, 101); err == nil {
		t.Fatal("expected oversized batch to be rejected")
	}
}
