package worker

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/longkeyy/go-datax/internal/connector"
)

const defaultPipelineMaxBytes = 16 << 20

// normalizePipelineSpec validates the resolved endpoint snapshots and the
// source/sink schema contract before the worker opens any connection.
func normalizePipelineSpec(spec PipelineSpec) (PipelineSpec, error) {
	if err := connector.ValidateSource(spec.Source.Config()); err != nil {
		return spec, fmt.Errorf("source %q: %w", spec.Source.Name, err)
	}
	if err := connector.ValidateSink(spec.Sink.Config()); err != nil {
		return spec, fmt.Errorf("sink %q: %w", spec.Sink.Name, err)
	}
	if !connector.SchemasEqual(spec.Source.Schema, spec.Sink.Schema) {
		return spec, fmt.Errorf("source and sink schemas do not match")
	}
	return spec, nil
}

// RunPipeline executes one linear source → sink pipeline and reports progress
// through emit. The first read or write error cancels the other splits.
func RunPipeline(ctx context.Context, spec PipelineSpec, emit func(Event)) (RunStatus, error) {
	var emitMu sync.Mutex
	safeEmit := func(event Event) {
		if emit == nil {
			return
		}
		emitMu.Lock()
		emit(event)
		emitMu.Unlock()
	}
	logf := func(format string, args ...any) {
		safeEmit(Event{Type: "log", Message: fmt.Sprintf(format, args...)})
	}
	fail := func(err error) (RunStatus, error) {
		if ctx.Err() != nil {
			logf("已取消")
			safeEmit(Event{Type: "done", Status: StatusCanceled, Error: ctx.Err().Error()})
			return StatusCanceled, ctx.Err()
		}
		logf("运行失败：%v", err)
		safeEmit(Event{Type: "done", Status: StatusFailed, Error: err.Error()})
		return StatusFailed, err
	}

	spec, err := normalizePipelineSpec(spec)
	if err != nil {
		return fail(err)
	}
	source, err := connector.NewSource(spec.Source.Config())
	if err != nil {
		return fail(fmt.Errorf("open source: %w", err))
	}
	sink, err := connector.NewSink(spec.Sink.Config())
	if err != nil {
		_ = source.Close()
		return fail(fmt.Errorf("open sink: %w", err))
	}

	logf("源 %s（%s）", spec.Source.Name, spec.Source.Connector)
	logf("目标 %s（%s）", spec.Sink.Name, spec.Sink.Connector)
	splits, err := source.Splits(ctx)
	if err != nil {
		_ = source.Close()
		_ = sink.Close()
		return fail(fmt.Errorf("list source splits: %w", err))
	}
	logf("读取分片数：%d", len(splits))

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	budget := newByteBudget(runCtx, defaultPipelineMaxBytes)

	type pipelineBatch struct {
		index int
		split string
		batch connector.Batch
		bytes int64
	}
	var readRecords, writeRecords atomic.Int64
	taskRecords := make([]atomic.Int64, len(splits))
	errCh := make(chan error, len(splits)+1)
	// Bound in-flight work to two connector batches per split. Connector batches
	// are already capped (see connector.defaultBatchSize), so this also bounds
	// memory without a separate byte budget.
	batches := make(chan pipelineBatch, len(splits)*2)

	var producers sync.WaitGroup
	for index, split := range splits {
		producers.Add(1)
		go func(index int, split connector.Split) {
			defer producers.Done()
			for {
				if runCtx.Err() != nil {
					return
				}
				batch, err := source.ReadBatch(runCtx, split)
				if err == io.EOF {
					return
				}
				if err != nil {
					errCh <- fmt.Errorf("split %q read: %w", split.ID, err)
					cancel()
					return
				}
				batchBytes := batch.Bytes
				if batchBytes <= 0 {
					batchBytes = int64(len(batch.Rows)) * 100
				}
				if err := budget.acquire(runCtx, batchBytes); err != nil {
					errCh <- fmt.Errorf("split %q backpressure: %w", split.ID, err)
					cancel()
					return
				}
				select {
				case batches <- pipelineBatch{index: index, split: split.ID, batch: batch, bytes: batchBytes}:
				case <-runCtx.Done():
					budget.release(batchBytes)
					return
				}
			}
		}(index, split)
	}
	go func() {
		producers.Wait()
		close(batches)
	}()

	writerFailed := false
	for item := range batches {
		if writerFailed || runCtx.Err() != nil {
			budget.release(item.bytes)
			continue
		}
		if err := sink.WriteBatch(runCtx, item.batch); err != nil {
			budget.release(item.bytes)
			errCh <- fmt.Errorf("split %q write: %w", item.split, err)
			writerFailed = true
			cancel()
			continue
		}
		budget.release(item.bytes)
		rows := int64(len(item.batch.Rows))
		total := readRecords.Add(rows)
		written := writeRecords.Add(rows)
		taskRecords[item.index].Add(rows)
		safeEmit(Event{
			Type:         "progress",
			ReadRecords:  total,
			WriteRecords: written,
		})
	}
	producers.Wait()
	cancel()

	close(errCh)
	var firstErr error
	for err := range errCh {
		if firstErr == nil {
			firstErr = err
		}
	}

	sourceCloseErr := source.Close()
	sinkCloseErr := sink.Close()
	if firstErr == nil {
		if sinkCloseErr != nil {
			firstErr = fmt.Errorf("close sink: %w", sinkCloseErr)
		} else if sourceCloseErr != nil {
			firstErr = fmt.Errorf("close source: %w", sourceCloseErr)
		}
	}

	status := StatusSucceeded
	if ctx.Err() != nil {
		status = StatusCanceled
		firstErr = ctx.Err()
	} else if firstErr != nil {
		status = StatusFailed
	}

	tasks := make([]TaskStats, 0, len(splits))
	for index := range taskRecords {
		tasks = append(tasks, TaskStats{Task: index, Records: taskRecords[index].Load()})
	}
	event := Event{
		Type:         "done",
		Status:       status,
		ReadRecords:  readRecords.Load(),
		WriteRecords: writeRecords.Load(),
		Tasks:        tasks,
	}
	if firstErr != nil {
		event.Error = firstErr.Error()
	}
	if status == StatusSucceeded {
		logf("完成：读取 %d 行，写入 %d 行", event.ReadRecords, event.WriteRecords)
	} else if status == StatusCanceled {
		logf("已取消")
	} else {
		logf("运行失败：%v", firstErr)
	}
	safeEmit(event)
	if status == StatusSucceeded {
		return status, nil
	}
	return status, firstErr
}
