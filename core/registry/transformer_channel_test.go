package plugin

import (
	"testing"

	"github.com/longkeyy/go-datax/common/config"
	"github.com/longkeyy/go-datax/common/element"
	"github.com/longkeyy/go-datax/common/statistics"
	"github.com/longkeyy/go-datax/common/transformer"
)

func buildExecutions(t *testing.T, cfg config.Configuration) []*transformer.Execution {
	t.Helper()

	executions, err := transformer.BuildExecutions(cfg.GetListConfiguration("transformer"))
	if err != nil {
		t.Fatalf("BuildExecutions failed: %v", err)
	}
	return executions
}

func parseConfig(t *testing.T, raw string) config.Configuration {
	t.Helper()

	cfg, err := config.FromJSON(raw)
	if err != nil {
		t.Fatalf("invalid test JSON: %v", err)
	}
	return cfg
}

func newTestRecord(value string) element.Record {
	record := element.NewRecord()
	record.AddColumn(element.NewStringColumn(value))
	return record
}

func TestTransformerChannelMetrics(t *testing.T) {
	cfg := parseConfig(t, `{
		"transformer": [
			{"name": "dx_substr", "parameter": {"columnIndex": 0, "paras": ["0", "2"]}},
			{"name": "dx_replace", "parameter": {"columnIndex": 0, "paras": ["ab", "AB"]}}
		]
	}`)

	base := NewChannel(4)
	communication := statistics.NewCommunication()
	executions := buildExecutions(t, cfg)
	channel := NewTransformerChannel(base, executions, communication, nil)

	if err := channel.Push(newTestRecord("abcd")); err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	record, err := channel.Pull()
	if err != nil {
		t.Fatalf("Pull failed: %v", err)
	}
	if got := record.GetColumn(0).GetAsString(); got != "AB" {
		t.Fatalf("pushed value = %q, want %q", got, "AB")
	}

	assertCounter(t, communication, statistics.TRANSFORMER_INPUT_RECORDS, 1)
	assertCounter(t, communication, statistics.TRANSFORMER_INPUT_BYTES, 4)
	assertCounter(t, communication, statistics.TRANSFORMER_OUTPUT_RECORDS, 1)
	assertCounter(t, communication, statistics.TRANSFORMER_OUTPUT_BYTES, 2)
	assertCounter(t, communication, statistics.TRANSFORMER_SUCCEED_RECORDS, 1)
	assertCounter(t, communication, statistics.TRANSFORMER_FAILED_RECORDS, 0)
	assertCounter(t, communication, statistics.TRANSFORMER_DIRTY_RECORDS, 0)
	assertCounter(t, communication, statistics.TRANSFORMER_FILTER_RECORDS, 0)

	wantUsed := executions[0].UsedNanos() + executions[1].UsedNanos()
	assertCounter(t, communication, statistics.TRANSFORMER_USED_TIME, wantUsed)
	assertCounter(t, communication, statistics.TRANSFORMER_NAME_PREFIX+"dx_substr", executions[0].UsedNanos())
	assertCounter(t, communication, statistics.TRANSFORMER_NAME_PREFIX+"dx_replace", executions[1].UsedNanos())
}

func TestTransformerChannelFilter(t *testing.T) {
	cfg := parseConfig(t, `{
		"transformer": [
			{"name": "dx_filter", "parameter": {"columnIndex": 0, "paras": ["=", "drop"]}}
		]
	}`)

	base := NewChannel(4)
	communication := statistics.NewCommunication()
	channel := NewTransformerChannel(base, buildExecutions(t, cfg), communication, nil)

	if err := channel.Push(newTestRecord("drop")); err != nil {
		t.Fatalf("Push failed: %v", err)
	}
	if got := base.Size(); got != 0 {
		t.Fatalf("filtered record reached the base channel (size %d)", got)
	}

	assertCounter(t, communication, statistics.TRANSFORMER_INPUT_RECORDS, 1)
	assertCounter(t, communication, statistics.TRANSFORMER_OUTPUT_RECORDS, 0)
	assertCounter(t, communication, statistics.TRANSFORMER_SUCCEED_RECORDS, 0)
	assertCounter(t, communication, statistics.TRANSFORMER_FILTER_RECORDS, 1)
	assertCounter(t, communication, statistics.TRANSFORMER_DIRTY_RECORDS, 0)
}

func TestTransformerChannelDirtySkipsRemaining(t *testing.T) {
	cfg := parseConfig(t, `{
		"transformer": [
			{"name": "dx_substr", "parameter": {"columnIndex": 0, "paras": ["10", "1"]}},
			{"name": "dx_replace", "parameter": {"columnIndex": 0, "paras": ["a", "b"]}}
		]
	}`)

	base := NewChannel(4)
	communication := statistics.NewCommunication()
	executions := buildExecutions(t, cfg)
	channel := NewTransformerChannel(base, executions, communication, nil)

	if err := channel.Push(newTestRecord("abc")); err != nil {
		t.Fatalf("dirty record should be dropped without failing the task, got: %v", err)
	}
	if got := base.Size(); got != 0 {
		t.Fatalf("dirty record reached the base channel (size %d)", got)
	}

	assertCounter(t, communication, statistics.TRANSFORMER_INPUT_RECORDS, 1)
	assertCounter(t, communication, statistics.TRANSFORMER_OUTPUT_RECORDS, 0)
	assertCounter(t, communication, statistics.TRANSFORMER_SUCCEED_RECORDS, 0)
	assertCounter(t, communication, statistics.TRANSFORMER_FAILED_RECORDS, 1)
	assertCounter(t, communication, statistics.TRANSFORMER_DIRTY_RECORDS, 1)
	assertCounter(t, communication, statistics.TRANSFORMER_DIRTY_BYTES, 3)
	assertCounter(t, communication, statistics.TRANSFORMER_FILTER_RECORDS, 0)

	if executions[0].FailedRecords() != 1 {
		t.Fatalf("first transformer failedRecords = %d, want 1", executions[0].FailedRecords())
	}
	if executions[1].SuccessRecords() != 0 || executions[1].UsedNanos() != 0 {
		t.Fatal("second transformer should be skipped after a dirty record")
	}
}

func TestTransformerChannelErrorLimit(t *testing.T) {
	cfg := parseConfig(t, `{
		"job": {
			"setting": {"errorLimit": {"record": 1, "percentage": 100}}
		},
		"transformer": [
			{"name": "dx_substr", "parameter": {"columnIndex": 0, "paras": ["10", "1"]}}
		]
	}`)

	base := NewChannel(4)
	communication := statistics.NewCommunication()
	limiter := statistics.NewErrorLimiter(cfg)
	channel := NewTransformerChannel(base, buildExecutions(t, cfg), communication, limiter)

	if err := channel.Push(newTestRecord("abc")); err != nil {
		t.Fatalf("first dirty record should be tolerated: %v", err)
	}
	if err := channel.Push(newTestRecord("abc")); err == nil {
		t.Fatal("second dirty record should exceed the error limit")
	}

	assertCounter(t, communication, statistics.TRANSFORMER_DIRTY_RECORDS, 2)
	assertCounter(t, communication, statistics.TRANSFORMER_FAILED_RECORDS, 2)
}

func assertCounter(t *testing.T, communication *statistics.Communication, key string, want int64) {
	t.Helper()

	if got := communication.GetLongCounter(key); got != want {
		t.Fatalf("counter %s = %d, want %d", key, got, want)
	}
}
