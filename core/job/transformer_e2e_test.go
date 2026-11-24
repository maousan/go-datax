package job_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longkeyy/go-datax/common/config"
	"github.com/longkeyy/go-datax/core/job"
	_ "github.com/longkeyy/go-datax/plugins/reader/streamreader"
	_ "github.com/longkeyy/go-datax/plugins/transformer/script"
	_ "github.com/longkeyy/go-datax/plugins/writer/streamwriter"
)

// runStreamJob executes a deterministic streamreader -> transformer ->
// streamwriter job and returns the finished job plus the output file path.
func runStreamJob(t *testing.T, settingJSON, transformerJSON string) (*job.Job, string, error) {
	t.Helper()

	dir := t.TempDir()
	outputPath := filepath.Join(dir, "out.txt")

	raw := fmt.Sprintf(`{
		"job": {
			"setting": {"speed": {"channel": 1}%s},
			"content": [{
				"reader": {
					"name": "streamreader",
					"parameter": {
						"sliceRecordCount": 3,
						"column": [
							{"type": "string", "value": "abcdef"},
							{"type": "long", "value": "7"}
						]
					}
				},
				"transformer": %s,
				"writer": {
					"name": "streamwriter",
					"parameter": {
						"print": true,
						"path": %q,
						"fileName": "out.txt",
						"fieldDelimiter": "|"
					}
				}
			}]
		}
	}`, settingJSON, transformerJSON, dir)

	cfg, err := config.FromJSON(raw)
	if err != nil {
		t.Fatalf("invalid test JSON: %v", err)
	}

	j := job.NewJob(cfg)
	return j, outputPath, j.Run(context.Background())
}

func TestTransformerEndToEnd(t *testing.T) {
	j, outputPath, err := runStreamJob(t, "",
		`[
			{"name": "dx_substr", "parameter": {"columnIndex": 0, "paras": ["0", "3"]}},
			{"name": "dx_pad", "parameter": {"columnIndex": 1, "paras": ["l", "2", "0"]}}
		]`)
	if err != nil {
		t.Fatalf("job failed: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output failed: %v", err)
	}
	want := strings.Repeat("abc|07\n", 3)
	if string(data) != want {
		t.Fatalf("output = %q, want %q", string(data), want)
	}

	stats := j.Status().Stats
	if stats == nil {
		t.Fatal("missing final stats")
	}
	if stats.TotalRecords != 3 {
		t.Fatalf("totalRecords = %d, want 3", stats.TotalRecords)
	}
	if stats.SucceedRecords != 3 {
		t.Fatalf("succeedRecords = %d, want 3", stats.SucceedRecords)
	}
	if stats.FilterRecords != 0 {
		t.Fatalf("filterRecords = %d, want 0", stats.FilterRecords)
	}
	if stats.Transformer.InputRecords != 3 {
		t.Fatalf("transformer.inputRecords = %d, want 3", stats.Transformer.InputRecords)
	}
	if stats.Transformer.OutputRecords != 3 {
		t.Fatalf("transformer.outputRecords = %d, want 3", stats.Transformer.OutputRecords)
	}
	if stats.Transformer.DirtyRecords != 0 {
		t.Fatalf("transformer.dirtyRecords = %d, want 0", stats.Transformer.DirtyRecords)
	}
}

func TestTransformerEndToEndFilter(t *testing.T) {
	j, outputPath, err := runStreamJob(t, "",
		`[{"name": "dx_filter", "parameter": {"columnIndex": 1, "paras": ["<", "100"]}}]`)
	if err != nil {
		t.Fatalf("job failed: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output failed: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("output = %q, want empty", string(data))
	}

	stats := j.Status().Stats
	if stats == nil {
		t.Fatal("missing final stats")
	}
	if stats.FilterRecords != 3 {
		t.Fatalf("filterRecords = %d, want 3", stats.FilterRecords)
	}
	if stats.SucceedRecords != 0 {
		t.Fatalf("succeedRecords = %d, want 0", stats.SucceedRecords)
	}
	if stats.Transformer.InputRecords != 3 || stats.Transformer.OutputRecords != 0 {
		t.Fatalf("transformer in/out = %d/%d, want 3/0",
			stats.Transformer.InputRecords, stats.Transformer.OutputRecords)
	}
}

func TestTransformerEndToEndDirty(t *testing.T) {
	j, outputPath, err := runStreamJob(t, "",
		`[{"name": "dx_substr", "parameter": {"columnIndex": 0, "paras": ["99", "1"]}}]`)
	if err != nil {
		t.Fatalf("dirty records without an error limit must not fail the job: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output failed: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("output = %q, want empty", string(data))
	}

	stats := j.Status().Stats
	if stats == nil {
		t.Fatal("missing final stats")
	}
	if stats.Transformer.DirtyRecords != 3 {
		t.Fatalf("transformer.dirtyRecords = %d, want 3", stats.Transformer.DirtyRecords)
	}
	if stats.Transformer.OutputRecords != 0 {
		t.Fatalf("transformer.outputRecords = %d, want 0", stats.Transformer.OutputRecords)
	}
}

func TestScriptTransformerEndToEnd(t *testing.T) {
	code := `
function transform() {
  set_string(0, get_string(0).substring(0, 3));
  set_long(1, get_long(1) + 1);
  return true;
}
`
	transformerJSON := fmt.Sprintf(
		`[{"name": "dx_script", "parameter": {"language": "javascript", "code": %q}}]`, code)

	j, outputPath, err := runStreamJob(t, "", transformerJSON)
	if err != nil {
		t.Fatalf("job failed: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output failed: %v", err)
	}
	want := strings.Repeat("abc|8\n", 3)
	if string(data) != want {
		t.Fatalf("output = %q, want %q", string(data), want)
	}

	stats := j.Status().Stats
	if stats == nil {
		t.Fatal("missing final stats")
	}
	if stats.Transformer.InputRecords != 3 || stats.Transformer.OutputRecords != 3 {
		t.Fatalf("transformer in/out = %d/%d, want 3/3",
			stats.Transformer.InputRecords, stats.Transformer.OutputRecords)
	}
	if stats.Transformer.DirtyRecords != 0 {
		t.Fatalf("transformer.dirtyRecords = %d, want 0", stats.Transformer.DirtyRecords)
	}
}

func TestScriptTransformerFilterEndToEnd(t *testing.T) {
	code := `function transform() { return get_long(1) > 100; }`
	transformerJSON := fmt.Sprintf(
		`[{"name": "dx_script", "parameter": {"language": "javascript", "code": %q}}]`, code)

	j, outputPath, err := runStreamJob(t, "", transformerJSON)
	if err != nil {
		t.Fatalf("job failed: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output failed: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("output = %q, want empty", string(data))
	}

	stats := j.Status().Stats
	if stats == nil {
		t.Fatal("missing final stats")
	}
	if stats.FilterRecords != 3 {
		t.Fatalf("filterRecords = %d, want 3", stats.FilterRecords)
	}
	if stats.Transformer.OutputRecords != 0 {
		t.Fatalf("transformer.outputRecords = %d, want 0", stats.Transformer.OutputRecords)
	}
}

func TestTransformerEndToEndErrorLimit(t *testing.T) {
	_, _, err := runStreamJob(t, `, "errorLimit": {"record": 1, "percentage": 100}`,
		`[{"name": "dx_substr", "parameter": {"columnIndex": 0, "paras": ["99", "1"]}}]`)
	if err == nil {
		t.Fatal("expected the job to fail after exceeding errorLimit")
	}
	if !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("error = %v, want an error limit message", err)
	}
}
