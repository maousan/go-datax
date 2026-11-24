package transformer

import (
	"strings"
	"testing"

	"github.com/longkeyy/go-datax/common/config"
	"github.com/longkeyy/go-datax/common/element"
)

func buildExecutionsFromJSON(t *testing.T, raw string) []*Execution {
	t.Helper()

	root, err := config.FromJSON(raw)
	if err != nil {
		t.Fatalf("invalid test JSON: %v", err)
	}
	executions, err := BuildExecutions(root.GetListConfiguration("transformer"))
	if err != nil {
		t.Fatalf("BuildExecutions failed: %v", err)
	}
	return executions
}

func TestBuildExecutionsNormalisesNumericParas(t *testing.T) {
	executions := buildExecutionsFromJSON(t, `{
		"transformer": [
			{"name": "dx_substr", "parameter": {"columnIndex": 0, "paras": [0, 2]}}
		]
	}`)

	if len(executions) != 1 {
		t.Fatalf("got %d executions, want 1", len(executions))
	}

	record := newTestRecord(element.NewStringColumn("abcd"))
	if _, err := executions[0].Evaluate(record); err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if got := record.GetColumn(0).GetAsString(); got != "ab" {
		t.Fatalf("value = %q, want %q", got, "ab")
	}
}

func TestBuildExecutionsChainOrder(t *testing.T) {
	executions := buildExecutionsFromJSON(t, `{
		"transformer": [
			{"name": "dx_substr", "parameter": {"columnIndex": 0, "paras": ["0", "4"]}},
			{"name": "dx_replace", "parameter": {"columnIndex": 0, "paras": ["ab", "AB"]}}
		]
	}`)

	record := newTestRecord(element.NewStringColumn("abcdef"))
	for _, execution := range executions {
		result, err := execution.Evaluate(record)
		if err != nil {
			t.Fatalf("%s failed: %v", execution.Name(), err)
		}
		if result == nil {
			t.Fatalf("%s unexpectedly filtered the record", execution.Name())
		}
	}

	if got := record.GetColumn(0).GetAsString(); got != "ABcd" {
		t.Fatalf("value = %q, want %q", got, "ABcd")
	}
}

func TestBuildExecutionsErrors(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{
			name: "unknown transformer",
			json: `{"transformer": [{"name": "nope", "parameter": {"columnIndex": 0}}]}`,
			want: "unknown transformer",
		},
		{
			name: "groovy unsupported",
			json: `{"transformer": [{"name": "dx_groovy", "parameter": {"code": "return record"}}]}`,
			want: "not supported",
		},
		{
			name: "missing column index",
			json: `{"transformer": [{"name": "dx_substr", "parameter": {"paras": ["0", "2"]}}]}`,
			want: "columnIndex is required",
		},
		{
			name: "negative column index",
			json: `{"transformer": [{"name": "dx_substr", "parameter": {"columnIndex": -1, "paras": ["0", "2"]}}]}`,
			want: "non-negative",
		},
		{
			name: "invalid paras",
			json: `{"transformer": [{"name": "dx_substr", "parameter": {"columnIndex": 0, "paras": ["0"]}}]}`,
			want: "requires 2 parameters",
		},
		{
			name: "nested para",
			json: `{"transformer": [{"name": "dx_substr", "parameter": {"columnIndex": 0, "paras": [["x"], "2"]}}]}`,
			want: "scalar",
		},
		{
			name: "null para",
			json: `{"transformer": [{"name": "dx_substr", "parameter": {"columnIndex": 0, "paras": [null, "2"]}}]}`,
			want: "must not be null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, err := config.FromJSON(tt.json)
			if err != nil {
				t.Fatalf("invalid test JSON: %v", err)
			}
			_, err = BuildExecutions(root.GetListConfiguration("transformer"))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

func TestExecutionStatistics(t *testing.T) {
	execution, err := NewExecution(mustConfig(t, `{"name": "dx_substr", "parameter": {"columnIndex": 0, "paras": ["0", "1"]}}`))
	if err != nil {
		t.Fatalf("NewExecution failed: %v", err)
	}

	// success
	record := newTestRecord(element.NewStringColumn("abc"))
	if _, err := execution.Evaluate(record); err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	// failure: record has no column 0
	if _, err := execution.Evaluate(newTestRecord()); err == nil {
		t.Fatal("expected out-of-range column to fail")
	}

	if got := execution.SuccessRecords(); got != 1 {
		t.Fatalf("successRecords = %d, want 1", got)
	}
	if got := execution.FailedRecords(); got != 1 {
		t.Fatalf("failedRecords = %d, want 1", got)
	}
	if got := execution.FilterRecords(); got != 0 {
		t.Fatalf("filterRecords = %d, want 0", got)
	}

	filter, err := NewExecution(mustConfig(t, `{"name": "dx_filter", "parameter": {"columnIndex": 0, "paras": ["=", "drop"]}}`))
	if err != nil {
		t.Fatalf("NewExecution failed: %v", err)
	}
	result, err := filter.Evaluate(newTestRecord(element.NewStringColumn("drop")))
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if result != nil {
		t.Fatal("expected record to be filtered")
	}
	if got := filter.FilterRecords(); got != 1 {
		t.Fatalf("filterRecords = %d, want 1", got)
	}
}

type panicTransformer struct{}

func (panicTransformer) Name() string { return "panic_transformer" }

func (panicTransformer) Evaluate(element.Record) (element.Record, error) {
	panic("boom")
}

func TestExecutionConvertsPanicToDirtyData(t *testing.T) {
	execution := &Execution{transformer: panicTransformer{}}

	result, err := execution.Evaluate(newTestRecord(element.NewStringColumn("x")))
	if err == nil {
		t.Fatal("expected panic to be converted into an error")
	}
	if result != nil {
		t.Fatal("expected no result for a panicking transformer")
	}
	if got := execution.FailedRecords(); got != 1 {
		t.Fatalf("failedRecords = %d, want 1", got)
	}
	if !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("error = %v, want a panic message", err)
	}
}

func mustConfig(t *testing.T, raw string) config.Configuration {
	t.Helper()

	cfg, err := config.FromJSON(raw)
	if err != nil {
		t.Fatalf("invalid test JSON: %v", err)
	}
	return cfg
}
