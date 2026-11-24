package script

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longkeyy/go-datax/common/config"
	"github.com/longkeyy/go-datax/common/element"
	"github.com/longkeyy/go-datax/common/transformer"
)

func newScriptExecution(t *testing.T, params string) *transformer.Execution {
	t.Helper()

	raw := fmt.Sprintf(`{"name": "dx_script", "parameter": %s}`, params)
	cfg, err := config.FromJSON(raw)
	if err != nil {
		t.Fatalf("invalid test JSON: %v", err)
	}

	execution, err := transformer.NewExecution(cfg)
	if err != nil {
		t.Fatalf("NewExecution failed: %v", err)
	}
	t.Cleanup(execution.Close)
	return execution
}

func newScriptRecord(columns ...element.Column) element.Record {
	record := element.NewRecord()
	for _, column := range columns {
		record.AddColumn(column)
	}
	return record
}

func TestJavaScriptScript(t *testing.T) {
	code := `
function transform() {
  set_string(0, get_string(0).toUpperCase());
  set_long(1, get_long(1) + 1);
  set_double(2, get_double(2) * 2);
  set_bool(3, !get_bool(3));
  set_null(4);
  return true;
}
`
	execution := newScriptExecution(t, fmt.Sprintf(`{"language": "javascript", "code": %q}`, code))

	record := newScriptRecord(
		element.NewStringColumn("abc"),
		element.NewLongColumn(5),
		element.NewDoubleColumn(1.5),
		element.NewBoolColumn(true),
		element.NewStringColumn("drop-me"),
	)
	result, err := execution.Evaluate(record)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if result == nil {
		t.Fatal("record was unexpectedly filtered")
	}

	if got := record.GetColumn(0).GetAsString(); got != "ABC" {
		t.Fatalf("column 0 = %q, want ABC", got)
	}
	if got, _ := record.GetColumn(1).GetAsLong(); got != 6 {
		t.Fatalf("column 1 = %d, want 6", got)
	}
	if got, _ := record.GetColumn(2).GetAsDouble(); got != 3 {
		t.Fatalf("column 2 = %v, want 3", got)
	}
	if got, _ := record.GetColumn(3).GetAsBool(); got {
		t.Fatal("column 3 = true, want false")
	}
	if !record.GetColumn(4).IsNull() {
		t.Fatal("column 4 should be null")
	}
}

func TestJavaScriptFilter(t *testing.T) {
	code := `function transform() { return get_long(0) < 10; }`
	execution := newScriptExecution(t, fmt.Sprintf(`{"language": "javascript", "code": %q}`, code))

	kept, err := execution.Evaluate(newScriptRecord(element.NewLongColumn(5)))
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if kept == nil {
		t.Fatal("record with value 5 should be kept")
	}

	filtered, err := execution.Evaluate(newScriptRecord(element.NewLongColumn(20)))
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if filtered != nil {
		t.Fatal("record with value 20 should be filtered")
	}
}

func TestJavaScriptConversionError(t *testing.T) {
	code := `function transform() { return get_long(0) > 0; }`
	execution := newScriptExecution(t, fmt.Sprintf(`{"language": "javascript", "code": %q}`, code))

	_, err := execution.Evaluate(newScriptRecord(element.NewStringColumn("abc")))
	if err == nil {
		t.Fatal("expected a conversion error")
	}
	if !strings.Contains(err.Error(), "cannot read as long") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestJavaScriptTimeout(t *testing.T) {
	code := `function transform() { while (true) {} }`
	execution := newScriptExecution(t, fmt.Sprintf(`{"language": "javascript", "code": %q, "timeoutMs": 50}`, code))

	_, err := execution.Evaluate(newScriptRecord(element.NewLongColumn(1)))
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestJavaScriptTopLevelTimeout(t *testing.T) {
	params := fmt.Sprintf(`{"language": "javascript", "code": %q, "timeoutMs": 50}`, `while (true) {}`)
	raw := fmt.Sprintf(`{"name": "dx_script", "parameter": %s}`, params)
	cfg, err := config.FromJSON(raw)
	if err != nil {
		t.Fatalf("invalid test JSON: %v", err)
	}

	_, err = transformer.NewExecution(cfg)
	if err == nil {
		t.Fatal("expected a top-level timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStarlarkScript(t *testing.T) {
	code := `
def transform():
    set_string(0, get_string(0) + "-star")
    set_long(1, get_long(1) * 2)
    return True
`
	execution := newScriptExecution(t, fmt.Sprintf(`{"language": "starlark", "code": %q}`, code))

	record := newScriptRecord(element.NewStringColumn("abc"), element.NewLongColumn(4))
	result, err := execution.Evaluate(record)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if result == nil {
		t.Fatal("record was unexpectedly filtered")
	}
	if got := record.GetColumn(0).GetAsString(); got != "abc-star" {
		t.Fatalf("column 0 = %q, want abc-star", got)
	}
	if got, _ := record.GetColumn(1).GetAsLong(); got != 8 {
		t.Fatalf("column 1 = %d, want 8", got)
	}
}

func TestStarlarkFilter(t *testing.T) {
	code := `
def transform():
    return get_long(0) > 5
`
	execution := newScriptExecution(t, fmt.Sprintf(`{"language": "starlark", "code": %q}`, code))

	kept, err := execution.Evaluate(newScriptRecord(element.NewLongColumn(10)))
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if kept == nil {
		t.Fatal("record with value 10 should be kept")
	}

	filtered, err := execution.Evaluate(newScriptRecord(element.NewLongColumn(2)))
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if filtered != nil {
		t.Fatal("record with value 2 should be filtered")
	}
}

func TestExprScript(t *testing.T) {
	execution := newScriptExecution(t, `{"language": "expr", "code": "set_string(0, get_string(0) + \"-expr\")"}`)

	record := newScriptRecord(element.NewStringColumn("abc"))
	result, err := execution.Evaluate(record)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if result == nil {
		t.Fatal("record was unexpectedly filtered")
	}
	if got := record.GetColumn(0).GetAsString(); got != "abc-expr" {
		t.Fatalf("column 0 = %q, want abc-expr", got)
	}
}

func TestExprFilter(t *testing.T) {
	execution := newScriptExecution(t, `{"language": "expr", "code": "get_long(0) > 5"}`)

	kept, err := execution.Evaluate(newScriptRecord(element.NewLongColumn(10)))
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if kept == nil {
		t.Fatal("record with value 10 should be kept")
	}

	filtered, err := execution.Evaluate(newScriptRecord(element.NewLongColumn(2)))
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if filtered != nil {
		t.Fatal("record with value 2 should be filtered")
	}
}

func TestExprColumnError(t *testing.T) {
	execution := newScriptExecution(t, `{"language": "expr", "code": "get_string(9)"}`)

	_, err := execution.Evaluate(newScriptRecord(element.NewStringColumn("abc")))
	if err == nil {
		t.Fatal("expected an out-of-range column error")
	}
}

func TestCodeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transform.js")
	code := `function transform() { set_string(0, "from-file"); return true; }`
	if err := os.WriteFile(path, []byte(code), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}

	execution := newScriptExecution(t, fmt.Sprintf(`{"language": "javascript", "codeFile": %q}`, path))
	record := newScriptRecord(element.NewStringColumn("abc"))
	if _, err := execution.Evaluate(record); err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if got := record.GetColumn(0).GetAsString(); got != "from-file" {
		t.Fatalf("column 0 = %q, want from-file", got)
	}
}

func TestCodeFileAddaxHomeFallback(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "transform.js"),
		[]byte(`function transform() { set_string(0, "from-addax-home"); return true; }`), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv("GO_DATAX_HOME", "")
	t.Setenv("ADDAX_HOME", dir)

	execution := newScriptExecution(t, `{"language": "javascript", "codeFile": "transform.js"}`)
	record := newScriptRecord(element.NewStringColumn("abc"))
	if _, err := execution.Evaluate(record); err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if got := record.GetColumn(0).GetAsString(); got != "from-addax-home" {
		t.Fatalf("column 0 = %q, want from-addax-home", got)
	}
}

func TestConfigValidation(t *testing.T) {
	t.Setenv("GO_DATAX_HOME", "")
	t.Setenv("ADDAX_HOME", "")

	tests := []struct {
		name   string
		params string
		want   string
	}{
		{name: "missing language", params: `{"code": "x"}`, want: "language is required"},
		{name: "unsupported language", params: `{"language": "groovy", "code": "x"}`, want: "unsupported language"},
		{name: "code and codeFile", params: `{"language": "javascript", "code": "x", "codeFile": "y"}`, want: "mutually exclusive"},
		{name: "missing code", params: `{"language": "javascript"}`, want: "code or parameter.codeFile"},
		{name: "starlark timeout", params: `{"language": "starlark", "code": "def transform():\n    return True", "timeoutMs": 10}`, want: "only supported for javascript"},
		{name: "relative codeFile", params: `{"language": "javascript", "codeFile": "relative.js"}`, want: "GO_DATAX_HOME"},
		{name: "negative timeout", params: `{"language": "javascript", "code": "x", "timeoutMs": -1}`, want: "non-negative"},
		{name: "compile error", params: `{"language": "javascript", "code": "function transform( {"}`, want: "compile script"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := fmt.Sprintf(`{"name": "dx_script", "parameter": %s}`, tt.params)
			cfg, err := config.FromJSON(raw)
			if err != nil {
				t.Fatalf("invalid test JSON: %v", err)
			}
			_, err = transformer.NewExecution(cfg)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want substring %q", err, tt.want)
			}
		})
	}
}
