package worker

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// BatchSink consumes a batch. Implementations must respect ctx.
type BatchSink interface {
	SendBatch(ctx context.Context, batch Batch) error
}

// normalizeRunSpec fills defaults and validates a diagnostic or pipeline run.
func normalizeRunSpec(spec RunSpec) (RunSpec, error) {
	if spec.Pipeline != nil {
		_, err := normalizePipelineSpec(*spec.Pipeline)
		return spec, err
	}
	if spec.Rows <= 0 {
		return spec, fmt.Errorf("rows must be positive")
	}
	if spec.BatchSize == 0 {
		spec.BatchSize = 512
	}
	if spec.BatchSize < 256 || spec.BatchSize > 1024 {
		return spec, fmt.Errorf("batchSize must be between 256 and 1024")
	}
	if spec.Splits == 0 {
		spec.Splits = 1
	}
	if spec.Splits < 1 || spec.Splits > 64 {
		return spec, fmt.Errorf("splits must be between 1 and 64")
	}
	if spec.MaxBytes == 0 {
		spec.MaxBytes = 1 << 20
	}
	if spec.MaxBytes < int64(spec.BatchSize)*100 {
		return spec, fmt.Errorf("maxBytes must be at least batchSize*100")
	}
	if spec.DelayPerBatchMS < 0 {
		return spec, fmt.Errorf("delayPerBatchMs must not be negative")
	}
	return spec, nil
}

// RunDiagnostic exercises the batch pipeline with synthetic records.
// Each record costs a fixed 100 bytes; it is a protocol/backpressure test, not a benchmark.
func RunDiagnostic(ctx context.Context, spec RunSpec, sink BatchSink, emit func(Event)) (RunStatus, error) {
	// Validate again here: the manager validates for a fast 400, but this is also a direct entry point.
	spec, err := normalizeRunSpec(spec)
	if err != nil {
		return StatusFailed, err
	}
	if sink == nil {
		return StatusFailed, fmt.Errorf("sink is required")
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	budget := newByteBudget(runCtx, spec.MaxBytes)
	batches := make(chan Batch, spec.Splits*2)
	var produced sync.WaitGroup
	var readRecords, readBytes atomic.Int64
	taskRecords := make([]atomic.Int64, spec.Splits)
	taskBytes := make([]atomic.Int64, spec.Splits)

	remaining := spec.Rows
	for split := 0; split < spec.Splits; split++ {
		rows := remaining / (spec.Splits - split)
		remaining -= rows
		if rows <= 0 {
			continue
		}
		produced.Add(1)
		go func(split, rows int) {
			defer produced.Done()
			for rows > 0 {
				size := spec.BatchSize
				if size > rows {
					size = rows
				}
				batch := Batch{Rows: size, Bytes: int64(size) * 100, Split: split}
				if err := budget.acquire(runCtx, batch.Bytes); err != nil {
					return
				}
				select {
				case batches <- batch:
					rows -= size
				case <-runCtx.Done():
					budget.release(batch.Bytes)
					return
				}
			}
		}(split, rows)
	}
	go func() {
		produced.Wait()
		close(batches)
	}()

	status := StatusSucceeded
	var runErr error
	aborted := false
	for batch := range batches {
		if spec.DelayPerBatchMS > 0 {
			timer := time.NewTimer(time.Duration(spec.DelayPerBatchMS) * time.Millisecond)
			select {
			case <-timer.C:
			case <-runCtx.Done():
				timer.Stop()
				runErr = runCtx.Err()
				aborted = true
				cancel()
			}
		}
		if aborted {
			budget.release(batch.Bytes)
			break
		}
		if err := sink.SendBatch(runCtx, batch); err != nil {
			runErr = err
			aborted = true
			cancel()
			budget.release(batch.Bytes)
			break
		}
		budget.release(batch.Bytes)
		records := readRecords.Add(int64(batch.Rows))
		bytes := readBytes.Add(batch.Bytes)
		taskRecords[batch.Split].Add(int64(batch.Rows))
		taskBytes[batch.Split].Add(batch.Bytes)
		if emit != nil {
			emit(Event{
				Type:        "progress",
				Percent:     float64(records) * 100 / float64(spec.Rows),
				ReadRecords: records,
				ReadBytes:   bytes,
			})
		}
	}
	if aborted {
		for pending := range batches {
			budget.release(pending.Bytes)
		}
	}

	if ctx.Err() != nil {
		status = StatusCanceled
		runErr = ctx.Err()
	} else if runErr != nil {
		status = StatusFailed
	}
	if emit != nil {
		tasks := make([]TaskStats, 0, spec.Splits)
		for index := range taskRecords {
			tasks = append(tasks, TaskStats{
				Task:    index,
				Records: taskRecords[index].Load(),
				Bytes:   taskBytes[index].Load(),
			})
		}
		event := Event{
			Type:         "done",
			Status:       status,
			ReadRecords:  readRecords.Load(),
			WriteRecords: readRecords.Load(),
			ReadBytes:    readBytes.Load(),
			Tasks:        tasks,
		}
		if runErr != nil && status == StatusFailed {
			event.Error = runErr.Error()
		}
		emit(event)
	}
	if status == StatusSucceeded {
		return status, nil
	}
	return status, runErr
}

// byteBudget bounds the bytes in flight between producers and the sink.
type byteBudget struct {
	limit int64

	mu   sync.Mutex
	cond *sync.Cond
	used int64
}

func newByteBudget(ctx context.Context, limit int64) *byteBudget {
	budget := &byteBudget{limit: limit}
	budget.cond = sync.NewCond(&budget.mu)
	go func() {
		<-ctx.Done()
		budget.mu.Lock()
		budget.cond.Broadcast()
		budget.mu.Unlock()
	}()
	return budget
}

func (b *byteBudget) acquire(ctx context.Context, n int64) error {
	if n > b.limit {
		return fmt.Errorf("batch of %d bytes exceeds budget %d", n, b.limit)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for b.used+n > b.limit {
		if err := ctx.Err(); err != nil {
			return err
		}
		b.cond.Wait()
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	b.used += n
	return nil
}

func (b *byteBudget) release(n int64) {
	b.mu.Lock()
	b.used -= n
	if b.used < 0 {
		b.used = 0
	}
	b.mu.Unlock()
	b.cond.Broadcast()
}
