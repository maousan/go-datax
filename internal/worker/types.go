package worker

import (
	"time"

	"github.com/longkeyy/go-datax/internal/connector"
	"github.com/longkeyy/go-datax/internal/transform"
)

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
const ProtocolVersion = 2

// ListenAddressPrefix is printed by etl-worker and parsed by etl-server.
const ListenAddressPrefix = "etl-worker listening on "

// IsActive reports whether a run is still queued or running.
func (s RunStatus) IsActive() bool {
	return s == StatusQueued || s == StatusRunning
}

// RunSpec describes work accepted by the worker. Pipeline is mutually exclusive
// with the diagnostic Rows fields.
type RunSpec struct {
	Rows            int           `json:"rows,omitempty"`
	BatchSize       int           `json:"batchSize,omitempty"`
	Splits          int           `json:"splits,omitempty"`
	MaxBytes        int64         `json:"maxBytes,omitempty"`
	DelayPerBatchMS int           `json:"delayPerBatchMs,omitempty"`
	Pipeline        *PipelineSpec `json:"pipeline,omitempty"`
}

// PipelineSpec is one resolved linear source → transforms → sink execution.
type PipelineSpec struct {
	Source                  EndpointSpec     `json:"source"`
	Transforms              []transform.Step `json:"transforms,omitempty"`
	TransformLibraryVersion int              `json:"transformLibraryVersion,omitempty"`
	Sink                    EndpointSpec     `json:"sink"`
}

// EndpointSpec is a resolved Source/Sink definition, including the DataSource
// fields required by the worker. It carries no database identifiers. Username
// and Password must never be logged or persisted.
type EndpointSpec struct {
	Name                 string            `json:"name"`
	Connector            string            `json:"connector"`
	DataSourceType       string            `json:"dataSourceType"`
	JDBCUrl              string            `json:"jdbcUrl,omitempty"`
	Username             string            `json:"username,omitempty"`
	Password             string            `json:"password,omitempty"`
	DataSourceProperties map[string]string `json:"dataSourceProperties,omitempty"`
	Schema               connector.Schema  `json:"schema"`
	Options              map[string]string `json:"options,omitempty"`
}

// Config converts the endpoint snapshot into a connector config.
func (e EndpointSpec) Config() connector.Config {
	properties := map[string]string{}
	for key, value := range e.DataSourceProperties {
		properties[key] = value
	}
	options := map[string]string{}
	for key, value := range e.Options {
		options[key] = value
	}
	return connector.Config{
		Connector:            e.Connector,
		DataSourceType:       e.DataSourceType,
		JDBCUrl:              e.JDBCUrl,
		Username:             e.Username,
		Password:             e.Password,
		DataSourceProperties: properties,
		Schema:               e.Schema,
		Options:              options,
	}
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
	Type         string      `json:"type"`
	RunID        string      `json:"runId,omitempty"`
	Message      string      `json:"message,omitempty"`
	Status       RunStatus   `json:"status,omitempty"`
	Percent      float64     `json:"percent,omitempty"`
	ReadRecords  int64       `json:"readRecords,omitempty"`
	WriteRecords int64       `json:"writeRecords,omitempty"`
	ReadBytes    int64       `json:"readBytes,omitempty"`
	Error        string      `json:"error,omitempty"`
	Tasks        []TaskStats `json:"tasks,omitempty"`
}

// RunState is the queryable state of a run.
type RunState struct {
	ID           string      `json:"id"`
	Status       RunStatus   `json:"status"`
	StartedAt    time.Time   `json:"startedAt"`
	FinishedAt   *time.Time  `json:"finishedAt,omitempty"`
	ReadRecords  int64       `json:"readRecords"`
	WriteRecords int64       `json:"writeRecords"`
	ReadBytes    int64       `json:"readBytes"`
	Error        string      `json:"error,omitempty"`
	Tasks        []TaskStats `json:"tasks,omitempty"`
}
