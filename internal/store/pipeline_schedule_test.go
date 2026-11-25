package store

import (
	"errors"
	"testing"
	"time"
)

func TestPipelineScheduleCRUD(t *testing.T) {
	dataStore := openTestStore(t, t.TempDir()+"/admin.db")
	pipeline := &Pipeline{Name: "scheduled", SourceID: 1, SinkID: 2}
	if err := dataStore.CreatePipeline(pipeline); err != nil {
		t.Fatal(err)
	}
	other := &Pipeline{Name: "other", SourceID: 1, SinkID: 2}
	if err := dataStore.CreatePipeline(other); err != nil {
		t.Fatal(err)
	}

	schedule := &PipelineSchedule{
		PipelineID:     pipeline.ID,
		Name:           "Every Five Minutes",
		CronExpr:       "0 0/5 * * * ?",
		Timezone:       "Asia/Shanghai",
		Enabled:        true,
		BlockingPolicy: BlockingDiscard,
		RetryMax:       2,
	}
	if err := dataStore.CreatePipelineSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	if err := dataStore.CreatePipelineSchedule(&PipelineSchedule{
		PipelineID: pipeline.ID,
		Name:       "every five minutes",
		CronExpr:   "0 0/10 * * * ?",
		Timezone:   "UTC",
	}); !errors.Is(err, ErrDuplicatePipelineScheduleName) {
		t.Fatalf("duplicate error = %v", err)
	}
	if err := dataStore.CreatePipelineSchedule(&PipelineSchedule{
		PipelineID: other.ID,
		Name:       "Every Five Minutes",
		CronExpr:   "0 0/10 * * * ?",
		Timezone:   "UTC",
	}); err != nil {
		t.Fatalf("same name on another pipeline: %v", err)
	}

	schedules, err := dataStore.ListPipelineSchedules("", pipeline.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(schedules) != 1 || schedules[0].ID != schedule.ID {
		t.Fatalf("schedules = %+v", schedules)
	}
	if enabled, err := dataStore.ListEnabledPipelineSchedules(); err != nil || len(enabled) != 1 {
		t.Fatalf("enabled = %+v, err = %v", enabled, err)
	}

	schedule.Enabled = false
	schedule.RetryMax = 3
	if err := dataStore.UpdatePipelineSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	loaded, err := dataStore.GetPipelineSchedule(schedule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Enabled || loaded.RetryMax != 3 {
		t.Fatalf("updated schedule = %+v", loaded)
	}
	if enabled, err := dataStore.ListEnabledPipelineSchedules(); err != nil || len(enabled) != 0 {
		t.Fatalf("enabled after disable = %+v, err = %v", enabled, err)
	}
	if err := dataStore.DeletePipelineSchedule(schedule.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := dataStore.GetPipelineSchedule(schedule.ID); !errors.Is(err, ErrPipelineScheduleNotFound) {
		t.Fatalf("missing schedule error = %v", err)
	}
}

func TestPipelineRunQueueHelpers(t *testing.T) {
	dataStore := openTestStore(t, t.TempDir()+"/admin.db")
	pipeline := &Pipeline{Name: "queued", SourceID: 1, SinkID: 2}
	if err := dataStore.CreatePipeline(pipeline); err != nil {
		t.Fatal(err)
	}
	pipelineID := pipeline.ID
	queued := &PipelineRun{
		PipelineID: &pipelineID, PipelineName: pipeline.Name,
		Status: PipelineRunStatusQueued, Trigger: PipelineRunTriggerManual, StartedAt: time.Now(),
	}
	running := &PipelineRun{
		PipelineID: &pipelineID, PipelineName: pipeline.Name,
		Status: PipelineRunStatusRunning, Trigger: PipelineRunTriggerManual, StartedAt: time.Now(),
	}
	if err := dataStore.CreatePipelineRun(queued); err != nil {
		t.Fatal(err)
	}
	if err := dataStore.CreatePipelineRun(running); err != nil {
		t.Fatal(err)
	}

	if ok, err := dataStore.HasQueuedPipelineRun(pipeline.ID); err != nil || !ok {
		t.Fatalf("HasQueuedPipelineRun = %v, %v", ok, err)
	}
	active, ok, err := dataStore.ActivePipelineRun(pipeline.ID)
	if err != nil || !ok || active.ID != running.ID {
		t.Fatalf("ActivePipelineRun = %+v, %v, %v", active, ok, err)
	}
	first, ok, err := dataStore.FirstQueuedPipelineRun(pipeline.ID)
	if err != nil || !ok || first.ID != queued.ID {
		t.Fatalf("FirstQueuedPipelineRun = %+v, %v, %v", first, ok, err)
	}
	ids, err := dataStore.ListQueuedPipelineIDs()
	if err != nil || len(ids) != 1 || ids[0] != pipeline.ID {
		t.Fatalf("ListQueuedPipelineIDs = %v, %v", ids, err)
	}
	if started, err := dataStore.MarkQueuedPipelineRunStarted(queued.ID, time.Now()); err != nil || !started {
		t.Fatalf("MarkQueuedPipelineRunStarted = %v, %v", started, err)
	}
	if canceled, err := dataStore.CancelQueuedPipelineRun(queued.ID, "too late"); err != nil || canceled {
		t.Fatalf("CancelQueuedPipelineRun on running = %v, %v", canceled, err)
	}
	if err := dataStore.MarkPipelineRunQueued(queued.ID); err != nil {
		t.Fatal(err)
	}
	requeued, err := dataStore.GetPipelineRun(queued.ID)
	if err != nil || requeued.Status != PipelineRunStatusQueued || requeued.FinishedAt != nil {
		t.Fatalf("requeued run = %+v, %v", requeued, err)
	}
	if canceled, err := dataStore.CancelQueuedPipelineRun(queued.ID, "canceled"); err != nil || !canceled {
		t.Fatalf("CancelQueuedPipelineRun = %v, %v", canceled, err)
	}
	if canceled, err := dataStore.CancelQueuedPipelineRuns(pipeline.ID, "overwritten"); err != nil || canceled != 0 {
		t.Fatalf("CancelQueuedPipelineRuns = %d, %v", canceled, err)
	}
}
