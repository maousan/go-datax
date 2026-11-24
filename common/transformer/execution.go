package transformer

import (
	"fmt"
	"strconv"
	"time"

	"github.com/longkeyy/go-datax/common/config"
	"github.com/longkeyy/go-datax/common/element"
)

// Execution binds one configured transformer to its parameters and keeps
// per-instance statistics. It is not safe for concurrent use; a task group
// evaluates records from a single reader goroutine.
type Execution struct {
	transformer Transformer
	columnIndex int

	successRecords int64
	failedRecords  int64
	filterRecords  int64

	usedNanos     int64
	lastUsedNanos int64
}

// NewExecution builds an Execution from a job.content[0].transformer entry.
func NewExecution(cfg config.Configuration) (*Execution, error) {
	name := cfg.GetString("name")
	if name == "" {
		return nil, fmt.Errorf("transformer name is required")
	}

	paramConfig := cfg.GetConfiguration("parameter")

	// Record-level transformers (e.g. dx_script) own their parameter block and
	// do not use the columnIndex/paras convention.
	if recordFactory, exists := lookupRecord(name); exists {
		implementation, err := recordFactory(paramConfig)
		if err != nil {
			return nil, err
		}
		return &Execution{
			transformer: implementation,
			columnIndex: -1,
		}, nil
	}

	// Resolve the implementation first so unsupported names (e.g. dx_groovy)
	// produce their dedicated error instead of a parameter error.
	factory, err := lookup(name)
	if err != nil {
		return nil, err
	}

	if !paramConfig.IsExists("columnIndex") {
		return nil, fmt.Errorf("transformer %q: parameter.columnIndex is required", name)
	}
	columnIndex := paramConfig.GetInt("columnIndex")
	if columnIndex < 0 {
		return nil, fmt.Errorf("transformer %q: columnIndex must be non-negative, got %d", name, columnIndex)
	}

	paras, err := stringParas(paramConfig.GetList("paras"))
	if err != nil {
		return nil, fmt.Errorf("transformer %q: %v", name, err)
	}

	implementation, err := factory(columnIndex, paras)
	if err != nil {
		return nil, err
	}

	return &Execution{
		transformer: implementation,
		columnIndex: columnIndex,
	}, nil
}

// BuildExecutions converts all configured transformer entries into executions.
func BuildExecutions(configs []config.Configuration) ([]*Execution, error) {
	executions := make([]*Execution, 0, len(configs))
	for i, transformerConfig := range configs {
		execution, err := NewExecution(transformerConfig)
		if err != nil {
			return nil, fmt.Errorf("transformer[%d]: %w", i, err)
		}
		executions = append(executions, execution)
	}
	return executions, nil
}

// stringParas normalises scalar JSON values into strings so numeric
// configuration (e.g. "paras": [0, 3]) behaves like its string form.
func stringParas(values []interface{}) ([]string, error) {
	paras := make([]string, 0, len(values))
	for i, raw := range values {
		switch value := raw.(type) {
		case string:
			paras = append(paras, value)
		case float64:
			paras = append(paras, strconv.FormatFloat(value, 'f', -1, 64))
		case int:
			paras = append(paras, strconv.Itoa(value))
		case int64:
			paras = append(paras, strconv.FormatInt(value, 10))
		case bool:
			paras = append(paras, strconv.FormatBool(value))
		case nil:
			return nil, fmt.Errorf("paras[%d] must not be null", i)
		default:
			return nil, fmt.Errorf("paras[%d] must be a scalar value, got %T", i, raw)
		}
	}
	return paras, nil
}

// Evaluate applies the transformer, updating counters and timing. Panics in a
// transformer are converted into errors so one malformed record cannot crash
// the whole pipeline; the record is then accounted as dirty data.
func (e *Execution) Evaluate(record element.Record) (result element.Record, err error) {
	start := time.Now()
	defer func() {
		elapsed := time.Since(start).Nanoseconds()
		e.lastUsedNanos = elapsed
		e.usedNanos += elapsed

		if recovered := recover(); recovered != nil {
			result = nil
			err = fmt.Errorf("%s panicked: %v", e.transformer.Name(), recovered)
		}
		if err != nil {
			e.failedRecords++
			result = nil
			return
		}
		if result == nil {
			e.filterRecords++
			return
		}
		e.successRecords++
	}()

	return e.transformer.Evaluate(record)
}

// Name returns the transformer name.
func (e *Execution) Name() string { return e.transformer.Name() }

// ColumnIndex returns the configured column index.
func (e *Execution) ColumnIndex() int { return e.columnIndex }

// SuccessRecords returns the number of records that passed this transformer.
func (e *Execution) SuccessRecords() int64 { return e.successRecords }

// FailedRecords returns the number of dirty records produced by this transformer.
func (e *Execution) FailedRecords() int64 { return e.failedRecords }

// FilterRecords returns the number of records filtered out by this transformer.
func (e *Execution) FilterRecords() int64 { return e.filterRecords }

// UsedNanos returns the cumulative evaluation time in nanoseconds.
func (e *Execution) UsedNanos() int64 { return e.usedNanos }

// LastUsedNanos returns the evaluation time of the most recent record.
func (e *Execution) LastUsedNanos() int64 { return e.lastUsedNanos }

// Close releases resources held by the underlying transformer, if any.
func (e *Execution) Close() {
	if closer, ok := e.transformer.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}

// CloseExecutions releases all transformer executions.
func CloseExecutions(executions []*Execution) {
	for _, execution := range executions {
		execution.Close()
	}
}
