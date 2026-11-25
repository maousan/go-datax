package server

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/longkeyy/go-datax/internal/store"
	"github.com/longkeyy/go-datax/internal/worker"
)

const maxPipelineRunEvents = 2000

// pipelineStreamHub keeps live events for runs started by this server process.
// Completed runs fall back to their persisted log file.
type pipelineStreamHub struct {
	mu      sync.Mutex
	streams map[int64]*pipelineStream
}

type pipelineStream struct {
	mu      sync.Mutex
	history []worker.Event
	subs    map[chan worker.Event]struct{}
	last    worker.Event
	done    bool
}

func newPipelineStreamHub() *pipelineStreamHub {
	return &pipelineStreamHub{streams: map[int64]*pipelineStream{}}
}

func (h *pipelineStreamHub) start(id int64) {
	stream := &pipelineStream{subs: map[chan worker.Event]struct{}{}}
	h.mu.Lock()
	h.streams[id] = stream
	h.mu.Unlock()
}

func (h *pipelineStreamHub) publish(id int64, event worker.Event) {
	h.mu.Lock()
	stream := h.streams[id]
	h.mu.Unlock()
	if stream == nil {
		return
	}
	stream.publish(event)
}

func (h *pipelineStreamHub) finish(id int64) {
	h.mu.Lock()
	stream := h.streams[id]
	delete(h.streams, id)
	h.mu.Unlock()
	if stream != nil {
		stream.close()
	}
}

// snapshot returns the latest live event for a running stream.
func (h *pipelineStreamHub) snapshot(id int64) (worker.Event, bool) {
	h.mu.Lock()
	stream := h.streams[id]
	h.mu.Unlock()
	if stream == nil {
		return worker.Event{}, false
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	return stream.last, true
}

func (h *pipelineStreamHub) subscribe(id int64) ([]worker.Event, <-chan worker.Event, bool, func(), bool) {
	h.mu.Lock()
	stream := h.streams[id]
	h.mu.Unlock()
	if stream == nil {
		return nil, nil, true, func() {}, false
	}
	return stream.subscribe()
}

func (s *pipelineStream) publish(event worker.Event) {
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return
	}
	s.history = append(s.history, event)
	if len(s.history) > maxPipelineRunEvents {
		s.history = append([]worker.Event(nil), s.history[len(s.history)-maxPipelineRunEvents:]...)
	}
	if event.Type == "progress" || event.Type == "done" {
		s.last = event
	}
	if event.Type == "done" {
		s.done = true
		for ch := range s.subs {
			select {
			case ch <- event:
			default:
				// Make room for the terminal event; dropping one progress event is
				// safer than closing the stream without a done event.
				select {
				case <-ch:
				default:
				}
				select {
				case ch <- event:
				default:
				}
			}
			close(ch)
		}
		s.subs = nil
		s.mu.Unlock()
		return
	}
	for ch := range s.subs {
		select {
		case ch <- event:
		default:
			close(ch)
			delete(s.subs, ch)
		}
	}
	s.mu.Unlock()
}

func (s *pipelineStream) subscribe() ([]worker.Event, <-chan worker.Event, bool, func(), bool) {
	s.mu.Lock()
	history := append([]worker.Event(nil), s.history...)
	if s.done {
		s.mu.Unlock()
		return history, nil, true, func() {}, true
	}
	ch := make(chan worker.Event, 64)
	s.subs[ch] = struct{}{}
	s.mu.Unlock()

	cancel := func() {
		s.mu.Lock()
		if _, ok := s.subs[ch]; ok {
			delete(s.subs, ch)
			close(ch)
		}
		s.mu.Unlock()
	}
	return history, ch, false, cancel, true
}

func (s *pipelineStream) close() {
	s.mu.Lock()
	if !s.done {
		s.done = true
		for ch := range s.subs {
			close(ch)
		}
		s.subs = nil
	}
	s.mu.Unlock()
}

// executePipelineRun consumes worker events, persists logs, and fans out live events.
func (s *Server) executePipelineRun(workerRunID string, run store.PipelineRun, opts pipelineStartOptions) {
	stopTimeout, timedOut := s.applyPipelineTimeout(workerRunID, run.ID, opts.Timeout)
	defer stopTimeout()
	stream := s.pipelineStreams
	logFile, err := os.OpenFile(run.LogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		s.pipelineStreams.publish(run.ID, worker.Event{
			Type:   "done",
			Status: worker.StatusFailed,
			Error:  err.Error(),
		})
		s.pipelineStreams.finish(run.ID)
		_ = s.dataStore.FinishPipelineRun(run.ID, store.PipelineRunStatusFailed, err.Error())
		cancelCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = s.worker.CancelRun(cancelCtx, workerRunID)
		cancel()
		willRetry := s.maybeRetryPipelineRun(run.ID)
		s.notifyPipelineRun(run.ID, willRetry)
		if run.PipelineID != nil {
			s.drainPipelineQueue(*run.PipelineID)
		}
		return
	}
	defer logFile.Close()
	logWriter := newPipelineLogWriter(logFile, s.runtimeSettings().MaxLogBytes)
	writeLog := logWriter.writeLog

	writeLog("pipeline run #%d started (worker run %s)", run.ID, workerRunID)
	gotDone := false
	_, _, waitErr := s.worker.WaitRunEvents(context.Background(), workerRunID, func(event worker.Event) {
		stream.publish(run.ID, event)
		switch event.Type {
		case "log":
			writeLog("%s", event.Message)
		case "done":
			gotDone = true
			if event.Error != "" {
				writeLog("done status=%s read=%d write=%d error=%s", event.Status, event.ReadRecords, event.WriteRecords, event.Error)
			} else {
				writeLog("done status=%s read=%d write=%d", event.Status, event.ReadRecords, event.WriteRecords)
			}
		}
	})
	if waitErr != nil && !gotDone {
		stream.publish(run.ID, worker.Event{
			Type:   "done",
			Status: worker.StatusFailed,
			Error:  waitErr.Error(),
		})
		writeLog("runner error: %v", waitErr)
	}
	if timedOut.Load() {
		summary := fmt.Sprintf("运行超时（%s）", opts.Timeout)
		_ = s.dataStore.FinishPipelineRun(run.ID, store.PipelineRunStatusFailed, summary)
		writeLog("timeout: %s", summary)
	}
	stream.finish(run.ID)
	s.prunePipelineRuns()
	willRetry := s.maybeRetryPipelineRun(run.ID)
	s.notifyPipelineRun(run.ID, willRetry)
	if run.PipelineID != nil {
		s.drainPipelineQueue(*run.PipelineID)
	}
}

// prunePipelineRuns applies runtime retention settings to PipelineRun rows and logs.
func (s *Server) prunePipelineRuns() {
	settings := s.runtimeSettings()
	runs, err := s.dataStore.ListPipelineRunsForPrune(settings.MaxRuns, settings.MaxAgeDays)
	if err != nil {
		return
	}
	for _, run := range runs {
		if err := s.dataStore.DeletePipelineRun(run.ID); err != nil {
			continue
		}
		if run.LogPath != "" {
			_ = os.Remove(run.LogPath)
		}
	}
}

// pipelineLogWriter writes timestamped run logs while enforcing maxLogBytes.
type pipelineLogWriter struct {
	writer    *bufio.Writer
	limit     int64
	written   int64
	truncated bool
}

func newPipelineLogWriter(writer io.Writer, limit int64) *pipelineLogWriter {
	return &pipelineLogWriter{writer: bufio.NewWriter(writer), limit: limit}
}

func (w *pipelineLogWriter) writeLog(format string, args ...any) {
	if w.truncated {
		return
	}
	line := fmt.Sprintf("[%s] %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
	if w.limit > 0 && w.written+int64(len(line)) > w.limit {
		marker := "[log truncated]\n"
		remaining := w.limit - w.written
		if remaining > 0 {
			if int64(len(marker)) > remaining {
				marker = marker[:remaining]
			}
			_, _ = w.writer.WriteString(marker)
		}
		w.truncated = true
		_ = w.writer.Flush()
		return
	}
	_, _ = w.writer.WriteString(line)
	w.written += int64(len(line))
	_ = w.writer.Flush()
}
