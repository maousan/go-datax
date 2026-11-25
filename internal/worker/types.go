package worker

import "time"

// RunStatus is the lifecycle state of a worker run.
type RunStatus string

const (
	StatusQueued    RunStatus = "queued"
	StatusRunning   RunStatus = "running"
	StatusSucceeded RunStatus = "succeeded"
	StatusFailed    RunStatus = "failed"
	StatusCanceled  RunStatus = "canceled"
)

// ProtocolVersion is bumped when the worker HTTP protocol changes incompatibly.
const ProtocolVersion = 1

// ListenAddressPrefix is printed by etl-worker and parsed by etl-server.
const ListenAddressPrefix = "etl-worker listening on "

// IsActive reports whether a run is still queued or running.
func (s RunStatus) IsActive() bool {
	return s == StatusQueued || s == StatusRunning
}

// RunSpec describes a diagnostic run accepted by the worker.
type RunSpec struct {
	Rows            int   `json:"rows"`
	BatchSize       int   `json:"batchSize,omitempty"`
	Splits          int   `json:"splits,omitempty"`
	MaxBytes        int64 `json:"maxBytes,omitempty"`
	DelayPerBatchMS int   `json:"delayPerBatchMs,omitempty"`
}

// Batch is a unit of records moving from a producer to a sink.
type Batch struct {
	Rows  int
	Bytes int64
	Split int
}

// TaskStats is the per-split counter snapshot.
type TaskStats struct {
	Task    int   `json:"task"`
	Records int64 `json:"records"`
	Bytes   int64 `json:"bytes"`
}

// Event is one SSE/JSON message emitted by a run.
type Event struct {
	Type        string      `json:"type"`
	RunID       string      `json:"runId,omitempty"`
	Message     string      `json:"message,omitempty"`
	Status      RunStatus   `json:"status,omitempty"`
	Percent     float64     `json:"percent,omitempty"`
	ReadRecords int64       `json:"readRecords,omitempty"`
	ReadBytes   int64       `json:"readBytes,omitempty"`
	Error       string      `json:"error,omitempty"`
	Tasks       []TaskStats `json:"tasks,omitempty"`
}

// RunState is the queryable state of a run.
type RunState struct {
	ID          string      `json:"id"`
	Status      RunStatus   `json:"status"`
	StartedAt   time.Time   `json:"startedAt"`
	FinishedAt  *time.Time  `json:"finishedAt,omitempty"`
	ReadRecords int64       `json:"readRecords"`
	ReadBytes   int64       `json:"readBytes"`
	Error       string      `json:"error,omitempty"`
	Tasks       []TaskStats `json:"tasks,omitempty"`
}
