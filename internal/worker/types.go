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

// RunSpec describes a diagnostic run accepted by the worker.
type RunSpec struct {
	Kind            string `json:"kind,omitempty"`
	Rows            int    `json:"rows"`
	BatchSize       int    `json:"batchSize,omitempty"`
	Splits          int    `json:"splits,omitempty"`
	MaxBytes        int64  `json:"maxBytes,omitempty"`
	DelayPerBatchMS int    `json:"delayPerBatchMs,omitempty"`
}

// Batch is a unit of records moving from a producer to a sink.
type Batch struct {
	Rows     int
	Bytes    int64
	Sequence int
}

// Event is one SSE/JSON message emitted by a run.
type Event struct {
	Type        string    `json:"type"`
	RunID       string    `json:"runId,omitempty"`
	Message     string    `json:"message,omitempty"`
	Status      RunStatus `json:"status,omitempty"`
	Percent     float64   `json:"percent,omitempty"`
	ReadRecords int64     `json:"readRecords,omitempty"`
	ReadBytes   int64     `json:"readBytes,omitempty"`
	Error       string    `json:"error,omitempty"`
}

// RunState is the queryable state of a run.
type RunState struct {
	ID          string     `json:"id"`
	Status      RunStatus  `json:"status"`
	StartedAt   time.Time  `json:"startedAt"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
	ReadRecords int64      `json:"readRecords"`
	ReadBytes   int64      `json:"readBytes"`
	Error       string     `json:"error,omitempty"`
}
