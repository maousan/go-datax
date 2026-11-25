package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/longkeyy/go-datax/internal/store"
	"github.com/longkeyy/go-datax/internal/worker"
)

const pipelineOverwriteWait = 10 * time.Second

// pipelineStartError carries the HTTP status/code for a failed run admission.
type pipelineStartError struct {
	Status  int
	Code    string
	Message string
}

func (e *pipelineStartError) Error() string { return e.Message }

func pipelineStartFailure(status int, code, message string) error {
	return &pipelineStartError{Status: status, Code: code, Message: message}
}

type pipelineStartOptions struct {
	Trigger      string
	Blocking     string
	ScheduleID   *int64
	RetryAttempt int
	Timeout      time.Duration
}

// startPipelineRun applies admission, then either starts or queues the run.
func (s *Server) startPipelineRun(pipeline store.Pipeline, version store.PipelineVersion, opts pipelineStartOptions) (store.PipelineRun, error) {
	if s.worker == nil {
		return store.PipelineRun{}, pipelineStartFailure(503, "worker_unavailable", "etl-worker 未启用")
	}
	if version.PipelineID != pipeline.ID {
		return store.PipelineRun{}, pipelineStartFailure(409, "version_mismatch", "版本不属于该 Pipeline")
	}
	lock := s.pipelineRunLock(pipeline.ID)
	lock.Lock()
	defer lock.Unlock()

	active, err := s.dataStore.HasActivePipelineRun(pipeline.ID)
	if err != nil {
		return store.PipelineRun{}, pipelineStartFailure(500, "internal_error", "检查运行状态失败")
	}
	queued, err := s.dataStore.HasQueuedPipelineRun(pipeline.ID)
	if err != nil {
		return store.PipelineRun{}, pipelineStartFailure(500, "internal_error", "检查排队状态失败")
	}

	switch opts.Blocking {
	case store.BlockingOverwrite:
		if active {
			if err := s.cancelActivePipelineRunLocked(pipeline.ID); err != nil {
				return store.PipelineRun{}, err
			}
		}
		if queued {
			if _, err := s.dataStore.CancelQueuedPipelineRuns(pipeline.ID, "被覆盖运行取代"); err != nil {
				return store.PipelineRun{}, pipelineStartFailure(500, "internal_error", "取消排队运行失败")
			}
		}
	case store.BlockingQueue:
		if active || queued {
			run, err := s.enqueuePipelineRunLocked(pipeline, version, opts)
			if err == nil && !active {
				go s.drainPipelineQueue(pipeline.ID)
			}
			return run, err
		}
	default:
		if active || queued {
			return store.PipelineRun{}, pipelineStartFailure(409, "pipeline_running", "该 Pipeline 已有运行中或排队中的实例")
		}
	}
	return s.launchPipelineRunLocked(pipeline, version, opts, nil)
}

func (s *Server) cancelActivePipelineRunLocked(pipelineID int64) error {
	active, found, err := s.dataStore.ActivePipelineRun(pipelineID)
	if err != nil {
		return pipelineStartFailure(500, "internal_error", "读取运行状态失败")
	}
	if !found {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	err = s.worker.CancelPipelineRun(ctx, active.ID)
	cancel()
	if errors.Is(err, errPipelineRunNotActive) {
		_ = s.dataStore.FinishPipelineRun(active.ID, store.PipelineRunStatusFailed, "运行进程已不可用，无法取消")
	} else if err != nil {
		return pipelineStartFailure(409, "cancel_failed", "取消当前运行失败："+err.Error())
	}
	deadline := time.Now().Add(pipelineOverwriteWait)
	for time.Now().Before(deadline) {
		running, err := s.dataStore.HasActivePipelineRun(pipelineID)
		if err != nil {
			return pipelineStartFailure(500, "internal_error", "检查运行状态失败")
		}
		if !running {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return pipelineStartFailure(409, "cancel_timeout", "等待上一次运行结束超时")
}

func (s *Server) enqueuePipelineRunLocked(pipeline store.Pipeline, version store.PipelineVersion, opts pipelineStartOptions) (store.PipelineRun, error) {
	pipelineID := pipeline.ID
	versionID := version.ID
	run := &store.PipelineRun{
		PipelineID:     &pipelineID,
		VersionID:      &versionID,
		ScheduleID:     opts.ScheduleID,
		VersionNumber:  version.VersionNumber,
		PipelineName:   pipeline.Name,
		Status:         store.PipelineRunStatusQueued,
		Trigger:        opts.Trigger,
		StartedAt:      time.Now(),
		BlockingPolicy: opts.Blocking,
		TimeoutSeconds: int(opts.Timeout / time.Second),
		RetryAttempt:   opts.RetryAttempt,
	}
	if err := s.dataStore.CreatePipelineRun(run); err != nil {
		return store.PipelineRun{}, pipelineStartFailure(500, "internal_error", "创建排队运行失败")
	}
	if err := s.preparePipelineRunLog(run, "queued"); err != nil {
		_ = s.dataStore.FinishPipelineRun(run.ID, store.PipelineRunStatusFailed, err.Error())
		return store.PipelineRun{}, pipelineStartFailure(500, "internal_error", "创建日志文件失败")
	}
	return *run, nil
}

func (s *Server) launchPipelineRunLocked(pipeline store.Pipeline, version store.PipelineVersion, opts pipelineStartOptions, queued *store.PipelineRun) (store.PipelineRun, error) {
	spec, err := s.pipelineRunSpec(version)
	if err != nil {
		return store.PipelineRun{}, err
	}

	var run store.PipelineRun
	if queued != nil {
		started, err := s.dataStore.MarkQueuedPipelineRunStarted(queued.ID, time.Now())
		if err != nil {
			return store.PipelineRun{}, pipelineStartFailure(500, "internal_error", "启动排队运行失败")
		}
		if !started {
			return store.PipelineRun{}, pipelineStartFailure(409, "queue_canceled", "排队运行已被取消")
		}
		run = *queued
		run.Status = store.PipelineRunStatusRunning
		run.StartedAt = time.Now()
		if err := s.preparePipelineRunLog(&run, ""); err != nil {
			_ = s.dataStore.FinishPipelineRun(run.ID, store.PipelineRunStatusFailed, err.Error())
			return store.PipelineRun{}, pipelineStartFailure(500, "internal_error", "创建日志文件失败")
		}
	} else {
		pipelineID := pipeline.ID
		versionID := version.ID
		run = store.PipelineRun{
			PipelineID:     &pipelineID,
			VersionID:      &versionID,
			ScheduleID:     opts.ScheduleID,
			VersionNumber:  version.VersionNumber,
			PipelineName:   pipeline.Name,
			Status:         store.PipelineRunStatusRunning,
			Trigger:        opts.Trigger,
			StartedAt:      time.Now(),
			BlockingPolicy: opts.Blocking,
			TimeoutSeconds: int(opts.Timeout / time.Second),
			RetryAttempt:   opts.RetryAttempt,
		}
		if err := s.dataStore.CreatePipelineRun(&run); err != nil {
			return store.PipelineRun{}, pipelineStartFailure(500, "internal_error", "创建运行记录失败")
		}
		if err := s.preparePipelineRunLog(&run, ""); err != nil {
			_ = s.dataStore.FinishPipelineRun(run.ID, store.PipelineRunStatusFailed, err.Error())
			return store.PipelineRun{}, pipelineStartFailure(500, "internal_error", "创建日志文件失败")
		}
	}

	s.pipelineStreams.start(run.ID)
	startCtx, cancelStart := context.WithTimeout(context.Background(), 10*time.Second)
	workerRunID, err := s.worker.StartTrackedRun(startCtx, spec, run.ID)
	cancelStart()
	if err != nil {
		s.pipelineStreams.finish(run.ID)
		if queued != nil {
			_ = s.dataStore.MarkPipelineRunQueued(run.ID)
			_ = s.preparePipelineRunLog(&run, "queued")
		} else {
			_ = s.dataStore.FinishPipelineRun(run.ID, store.PipelineRunStatusFailed, err.Error())
		}
		return store.PipelineRun{}, pipelineStartFailure(503, "worker_unavailable", "启动 etl-worker 失败："+err.Error())
	}
	go s.executePipelineRun(workerRunID, run, opts)
	return run, nil
}

func (s *Server) preparePipelineRunLog(run *store.PipelineRun, firstLine string) error {
	logDir := s.config.LogsDir()
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return err
	}
	logPath := filepath.Join(logDir, fmt.Sprintf("pipeline-run-%d.log", run.ID))
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if firstLine != "" {
		if _, err := fmt.Fprintf(file, "[%s] pipeline run #%d %s\n", time.Now().Format("15:04:05"), run.ID, firstLine); err != nil {
			_ = file.Close()
			return err
		}
	}
	if err := file.Close(); err != nil {
		return err
	}
	run.LogPath = logPath
	return s.dataStore.UpdatePipelineRunLogPath(run.ID, logPath)
}

func (s *Server) pipelineRunSpec(version store.PipelineVersion) (worker.RunSpec, error) {
	var source, sink pipelineEndpointSnapshot
	if err := json.Unmarshal([]byte(version.SourceJSON), &source); err != nil {
		return worker.RunSpec{}, pipelineStartFailure(500, "internal_error", "读取版本快照失败")
	}
	if err := json.Unmarshal([]byte(version.SinkJSON), &sink); err != nil {
		return worker.RunSpec{}, pipelineStartFailure(500, "internal_error", "读取版本快照失败")
	}
	sourceDS, err := s.dataStore.GetDataSource(source.DataSourceID)
	if errors.Is(err, store.ErrDataSourceNotFound) {
		return worker.RunSpec{}, pipelineStartFailure(409, "data_source_missing", "版本引用的源数据源已删除")
	}
	if err != nil {
		return worker.RunSpec{}, pipelineStartFailure(500, "internal_error", "读取源数据源失败")
	}
	sinkDS, err := s.dataStore.GetDataSource(sink.DataSourceID)
	if errors.Is(err, store.ErrDataSourceNotFound) {
		return worker.RunSpec{}, pipelineStartFailure(409, "data_source_missing", "版本引用的目标数据源已删除")
	}
	if err != nil {
		return worker.RunSpec{}, pipelineStartFailure(500, "internal_error", "读取目标数据源失败")
	}
	transforms, err := decodeTransforms(version.TransformsJSON)
	if err != nil {
		return worker.RunSpec{}, pipelineStartFailure(500, "internal_error", "读取版本 Transform 失败")
	}
	return worker.RunSpec{Pipeline: &worker.PipelineSpec{
		Source:                  source.workerEndpoint(sourceDS),
		Transforms:              transforms,
		TransformLibraryVersion: version.TransformLibraryVersion,
		Sink:                    sink.workerEndpoint(sinkDS),
	}}, nil
}

// drainPipelineQueue starts the oldest queued run once the pipeline is idle.
func (s *Server) drainPipelineQueue(pipelineID int64) {
	lock := s.pipelineRunLock(pipelineID)
	lock.Lock()
	defer lock.Unlock()

	for {
		active, err := s.dataStore.HasActivePipelineRun(pipelineID)
		if err != nil || active {
			return
		}
		queued, found, err := s.dataStore.FirstQueuedPipelineRun(pipelineID)
		if err != nil || !found {
			return
		}
		pipeline, err := s.dataStore.GetPipeline(pipelineID)
		if err != nil {
			_ = s.dataStore.FinishPipelineRun(queued.ID, store.PipelineRunStatusFailed, "排队运行所属 Pipeline 已删除")
			s.notifyPipelineRun(queued.ID, false)
			continue
		}
		if queued.VersionID == nil {
			_ = s.dataStore.FinishPipelineRun(queued.ID, store.PipelineRunStatusFailed, "排队运行缺少版本信息")
			s.notifyPipelineRun(queued.ID, false)
			continue
		}
		version, err := s.dataStore.GetPipelineVersion(*queued.VersionID)
		if err != nil {
			_ = s.dataStore.FinishPipelineRun(queued.ID, store.PipelineRunStatusFailed, "排队运行的版本已不存在")
			s.notifyPipelineRun(queued.ID, false)
			continue
		}
		opts := pipelineStartOptions{
			Trigger:      queued.Trigger,
			ScheduleID:   queued.ScheduleID,
			RetryAttempt: queued.RetryAttempt,
			Blocking:     queued.BlockingPolicy,
			Timeout:      time.Duration(queued.TimeoutSeconds) * time.Second,
		}
		if queued.ScheduleID != nil {
			schedule, err := s.dataStore.GetPipelineSchedule(*queued.ScheduleID)
			if err != nil || !schedule.Enabled {
				_ = s.dataStore.FinishPipelineRun(queued.ID, store.PipelineRunStatusCanceled, "调度已停用或删除")
				s.notifyPipelineRun(queued.ID, false)
				continue
			}
		}
		if _, err := s.launchPipelineRunLocked(pipeline, version, opts, &queued); err != nil {
			var startErr *pipelineStartError
			if errors.As(err, &startErr) {
				if startErr.Status == 503 {
					// Worker unavailable: keep the row queued and try again later.
					return
				}
				if startErr.Code == "queue_canceled" {
					continue
				}
			}
			_ = s.dataStore.FinishPipelineRun(queued.ID, store.PipelineRunStatusFailed, err.Error())
			s.notifyPipelineRun(queued.ID, false)
			continue
		}
		return
	}
}

// resumePipelineQueues restarts queued work after a server/worker restart.
func (s *Server) resumePipelineQueues() {
	ids, err := s.dataStore.ListQueuedPipelineIDs()
	if err != nil {
		log.Printf("读取排队运行失败: %v", err)
		return
	}
	for _, id := range ids {
		go s.drainPipelineQueue(id)
	}
}

// maybeRetryPipelineRun schedules the next retry for a failed scheduled run.
// It reports whether a retry was arranged.
func (s *Server) maybeRetryPipelineRun(runID int64) bool {
	run, err := s.dataStore.GetPipelineRun(runID)
	if err != nil || run.Status != store.PipelineRunStatusFailed || run.ScheduleID == nil {
		return false
	}
	schedule, err := s.dataStore.GetPipelineSchedule(*run.ScheduleID)
	if err != nil || !schedule.Enabled || run.RetryAttempt >= schedule.RetryMax {
		return false
	}
	scheduleID := schedule.ID
	interval := time.Duration(schedule.RetryIntervalSeconds) * time.Second
	go func() {
		if interval > 0 {
			time.Sleep(interval)
		}
		current, err := s.dataStore.GetPipelineSchedule(scheduleID)
		if err != nil || !current.Enabled {
			return
		}
		if run.PipelineID == nil {
			return
		}
		pipeline, err := s.dataStore.GetPipeline(*run.PipelineID)
		if err != nil {
			return
		}
		version, err := resolvePipelineScheduleVersion(s.dataStore, current, pipeline.ID)
		if err != nil {
			return
		}
		_, err = s.startPipelineRun(pipeline, version, pipelineStartOptions{
			Trigger:      store.PipelineRunTriggerRetry,
			Blocking:     current.BlockingPolicy,
			ScheduleID:   &current.ID,
			RetryAttempt: run.RetryAttempt + 1,
			Timeout:      time.Duration(current.TimeoutSeconds) * time.Second,
		})
		if err != nil {
			log.Printf("Pipeline 运行 #%d 重试失败: %v", run.ID, err)
			s.notifyPipelineRun(run.ID, false)
		}
	}()
	return true
}

func (s *Server) notifyPipelineRun(runID int64, willRetry bool) {
	final, err := s.dataStore.GetPipelineRun(runID)
	if err != nil {
		log.Printf("读取 Pipeline 运行 #%d 以发送告警失败: %v", runID, err)
		return
	}
	s.alerts.NotifyPipeline(final, willRetry)
}

// applyPipelineTimeout cancels a run and marks it timed out when its deadline passes.
func (s *Server) applyPipelineTimeout(workerRunID string, runID int64, timeout time.Duration) (stop func(), timedOut *atomic.Bool) {
	timedOut = &atomic.Bool{}
	if timeout <= 0 {
		return func() {}, timedOut
	}
	timer := time.AfterFunc(timeout, func() {
		current, err := s.dataStore.GetPipelineRun(runID)
		if err != nil || current.Status != store.PipelineRunStatusRunning {
			return
		}
		timedOut.Store(true)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.worker.CancelRun(ctx, workerRunID)
	})
	return func() { timer.Stop() }, timedOut
}
