package transform

import (
	"errors"
	"testing"
	"time"

	"github.com/longkeyy/go-datax/internal/connector"
)

func TestFiveStepChain(t *testing.T) {
	input := connector.Schema{
		{Name: "id", Type: connector.TypeInt64},
		{Name: "name", Type: connector.TypeString},
		{Name: "amount", Type: connector.TypeString},
		{Name: "email", Type: connector.TypeString},
		{Name: "created", Type: connector.TypeString},
	}
	steps := []Step{
		{Type: StepCast, Cast: []CastColumn{
			{Column: "amount", Type: connector.TypeFloat64},
			{Column: "created", Type: connector.TypeTimestamp},
		}},
		{Type: StepFilter, Condition: "amount >= 10"},
		{Type: StepProject, Project: []ProjectColumn{
			{Name: "id", Type: connector.TypeInt64, Expr: "id"},
			{Name: "customer", Type: connector.TypeString, Expr: "upper(trim(name))"},
			{Name: "total", Type: connector.TypeFloat64, Expr: "round_to(amount, 2)"},
			{Name: "email", Type: connector.TypeString, Expr: "mask_email(email)"},
			{Name: "created", Type: connector.TypeTimestamp, Expr: "created"},
		}},
		{Type: StepRename, Rename: []Rename{
			{From: "customer", To: "customer_name"},
			{From: "total", To: "amount"},
		}},
		{Type: StepDrop, Drop: []string{"email", "created"}},
	}
	pipeline, preview, err := Prepare(input, steps)
	if err != nil {
		t.Fatalf("Prepare() failed: %v", err)
	}
	if len(preview.Steps) != 5 {
		t.Fatalf("preview steps = %d, want 5", len(preview.Steps))
	}
	if preview.OutputSchema[0].Name != "id" || preview.OutputSchema[1].Name != "customer_name" ||
		preview.OutputSchema[2].Name != "amount" || preview.OutputSchema[2].Type != connector.TypeFloat64 {
		t.Fatalf("output schema = %+v", preview.OutputSchema)
	}

	batch := connector.Batch{
		Schema: input,
		Rows: [][]any{
			{int64(1), " alice ", "10.5", "alice@example.com", "2024-01-02T03:04:05Z"},
			{int64(2), "bob", "5", "bob@example.com", "2024-01-03T03:04:05Z"},
			{int64(3), "carol", "20", "carol@example.com", "2024-01-04T03:04:05Z"},
		},
	}
	out, err := pipeline.Transform(batch)
	if err != nil {
		t.Fatalf("Transform() failed: %v", err)
	}
	if len(out.Rows) != 2 {
		t.Fatalf("rows = %d, want 2: %#v", len(out.Rows), out.Rows)
	}
	if out.Rows[0][1] != "ALICE" || out.Rows[0][2] != 10.5 {
		t.Fatalf("first row = %#v", out.Rows[0])
	}
	if out.Rows[1][1] != "CAROL" || out.Rows[1][2] != 20.0 {
		t.Fatalf("second row = %#v", out.Rows[1])
	}
}

func TestFunctionLibrary(t *testing.T) {
	input := connector.Schema{{Name: "s", Type: connector.TypeString}}
	steps := []Step{{Type: StepProject, Project: []ProjectColumn{
		{Name: "digest", Type: connector.TypeString, Expr: `sha256("abc")`},
		{Name: "part", Type: connector.TypeString, Expr: "substr(s, 1, 2)"},
		{Name: "date", Type: connector.TypeString, Expr: `format_time(parse_time("2024-01-02", "2006-01-02"), "2006/01/02")`},
		{Name: "phone", Type: connector.TypeString, Expr: "mask_phone(s)"},
		{Name: "count", Type: connector.TypeInt64, Expr: `to_int("12")`},
	}}}
	pipeline, _, err := Prepare(input, steps)
	if err != nil {
		t.Fatalf("Prepare() failed: %v", err)
	}
	out, err := pipeline.Transform(connector.Batch{
		Schema: input,
		Rows:   [][]any{{"13800138000"}},
	})
	if err != nil {
		t.Fatalf("Transform() failed: %v", err)
	}
	row := out.Rows[0]
	if row[0] != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("sha256 = %v", row[0])
	}
	if row[1] != "38" {
		t.Fatalf("substr = %v", row[1])
	}
	if row[2] != "2024/01/02" {
		t.Fatalf("date = %v", row[2])
	}
	if row[3] != "138****8000" {
		t.Fatalf("masked phone = %v", row[3])
	}
	if row[4] != int64(12) {
		t.Fatalf("to_int = %v", row[4])
	}
}

func TestTransformErrorHasStepAndRow(t *testing.T) {
	input := connector.Schema{{Name: "name", Type: connector.TypeString}}
	pipeline, _, err := Prepare(input, []Step{{
		Type: StepProject,
		Project: []ProjectColumn{{
			Name: "value", Type: connector.TypeInt64, Expr: "to_int(name)",
		}},
	}})
	if err != nil {
		t.Fatalf("Prepare() failed: %v", err)
	}
	_, err = pipeline.Transform(connector.Batch{
		Schema: input,
		Rows:   [][]any{{"12"}, {"not-a-number"}},
	})
	var stepErr *StepError
	if !errors.As(err, &stepErr) {
		t.Fatalf("expected StepError, got %v", err)
	}
	if stepErr.Step != 0 || stepErr.Row != 1 {
		t.Fatalf("StepError = %+v, want step 0 row 1", stepErr)
	}
}

func TestPrepareRejectsInvalidExpressions(t *testing.T) {
	input := connector.Schema{{Name: "id", Type: connector.TypeInt64}}
	cases := []Step{
		{Type: StepProject, Project: []ProjectColumn{{Name: "x", Type: connector.TypeInt64, Expr: "missing + 1"}}},
		{Type: StepFilter, Condition: "id +"},
		{Type: StepCast, Cast: []CastColumn{{Column: "missing", Type: connector.TypeString}}},
		{Type: StepRename, Rename: []Rename{{From: "missing", To: "x"}}},
		{Type: StepDrop, Drop: []string{"missing"}},
	}
	for index, step := range cases {
		if _, _, err := Prepare(input, []Step{step}); err == nil {
			t.Fatalf("case %d should be rejected", index)
		}
	}
}

func TestTransformByColumnNameMap(t *testing.T) {
	input := connector.Schema{{Name: "full name", Type: connector.TypeString}}
	pipeline, _, err := Prepare(input, []Step{{
		Type: StepProject,
		Project: []ProjectColumn{{
			Name: "name", Type: connector.TypeString, Expr: `_row["full name"]`,
		}},
	}})
	if err != nil {
		t.Fatalf("Prepare() failed: %v", err)
	}
	out, err := pipeline.Transform(connector.Batch{
		Schema: input,
		Rows:   [][]any{{"Alice"}},
	})
	if err != nil || len(out.Rows) != 1 || out.Rows[0][0] != "Alice" {
		t.Fatalf("row = %#v, err = %v", out.Rows, err)
	}
}

func TestCastTimestamp(t *testing.T) {
	input := connector.Schema{{Name: "created", Type: connector.TypeString}}
	pipeline, preview, err := Prepare(input, []Step{{
		Type: StepCast,
		Cast: []CastColumn{{Column: "created", Type: connector.TypeTimestamp}},
	}})
	if err != nil {
		t.Fatalf("Prepare() failed: %v", err)
	}
	if preview.OutputSchema[0].Type != connector.TypeTimestamp {
		t.Fatalf("schema = %+v", preview.OutputSchema)
	}
	out, err := pipeline.Transform(connector.Batch{
		Schema: input,
		Rows:   [][]any{{"2024-01-02T03:04:05Z"}},
	})
	if err != nil {
		t.Fatalf("Transform() failed: %v", err)
	}
	if _, ok := out.Rows[0][0].(time.Time); !ok {
		t.Fatalf("timestamp = %#v", out.Rows[0][0])
	}
}

func TestFunctionLibraryPropagatesNull(t *testing.T) {
	input := connector.Schema{
		{Name: "s", Type: connector.TypeString},
		{Name: "f", Type: connector.TypeFloat64},
		{Name: "ts", Type: connector.TypeTimestamp},
	}
	steps := []Step{{Type: StepProject, Project: []ProjectColumn{
		{Name: "part", Type: connector.TypeString, Expr: "substr(s, 0, 2)"},
		{Name: "int", Type: connector.TypeInt64, Expr: "to_int(s)"},
		{Name: "float", Type: connector.TypeFloat64, Expr: "to_float(s)"},
		{Name: "bool", Type: connector.TypeBool, Expr: "to_bool(s)"},
		{Name: "str", Type: connector.TypeString, Expr: "to_string(s)"},
		{Name: "time", Type: connector.TypeTimestamp, Expr: "to_time(s)"},
		{Name: "added", Type: connector.TypeTimestamp, Expr: `date_add(ts, "24h")`},
		{Name: "rounded", Type: connector.TypeFloat64, Expr: "round_to(f, 2)"},
		{Name: "digest", Type: connector.TypeString, Expr: "md5(s)"},
		{Name: "masked", Type: connector.TypeString, Expr: "mask_phone(s)"},
	}}}
	pipeline, _, err := Prepare(input, steps)
	if err != nil {
		t.Fatalf("Prepare() failed: %v", err)
	}
	out, err := pipeline.Transform(connector.Batch{
		Schema: input,
		Rows:   [][]any{{nil, nil, nil}},
	})
	if err != nil {
		t.Fatalf("Transform() failed: %v", err)
	}
	if len(out.Rows) != 1 || len(out.Rows[0]) != len(steps[0].Project) {
		t.Fatalf("rows = %#v", out.Rows)
	}
	for index, value := range out.Rows[0] {
		if value != nil {
			t.Fatalf("column %d = %#v, want nil", index, value)
		}
	}
}
