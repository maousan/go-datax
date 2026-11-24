package transformer

import (
	"strings"
	"testing"

	"github.com/longkeyy/go-datax/common/element"
)

func newTestRecord(columns ...element.Column) element.Record {
	record := element.NewRecord()
	for _, column := range columns {
		record.AddColumn(column)
	}
	return record
}

func mustTransformer(t *testing.T, name string, columnIndex int, paras ...string) Transformer {
	t.Helper()

	transformer, err := New(name, columnIndex, paras)
	if err != nil {
		t.Fatalf("New(%q, %d, %v) failed: %v", name, columnIndex, paras, err)
	}
	return transformer
}

func TestSubstrTransformer(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		pos     string
		length  string
		want    string
		wantErr bool
	}{
		{name: "ascii", value: "abcdef", pos: "1", length: "3", want: "bcd"},
		{name: "cjk", value: "你好世界", pos: "1", length: "2", want: "好世"},
		{name: "beyond end is clamped", value: "abc", pos: "1", length: "10", want: "bc"},
		{name: "empty result", value: "abc", pos: "3", length: "1", want: ""},
		{name: "start out of range", value: "abc", pos: "5", length: "1", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transformer := mustTransformer(t, "dx_substr", 0, tt.pos, tt.length)
			record := newTestRecord(element.NewStringColumn(tt.value))

			_, err := transformer.Evaluate(record)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Evaluate failed: %v", err)
			}
			if got := record.GetColumn(0).GetAsString(); got != tt.want {
				t.Fatalf("value = %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("null passes through", func(t *testing.T) {
		transformer := mustTransformer(t, "dx_substr", 0, "0", "3")
		record := newTestRecord(element.NewStringColumn(""))
		if _, err := transformer.Evaluate(record); err != nil {
			t.Fatalf("Evaluate failed: %v", err)
		}
		if !record.GetColumn(0).IsNull() {
			t.Fatal("expected null column to stay null")
		}
	})

	t.Run("invalid params", func(t *testing.T) {
		if _, err := New("dx_substr", 0, []string{"-1", "3"}); err == nil {
			t.Fatal("expected negative pos to be rejected")
		}
		if _, err := New("dx_substr", 0, []string{"x", "3"}); err == nil {
			t.Fatal("expected non-numeric pos to be rejected")
		}
		if _, err := New("dx_substr", 0, []string{"0"}); err == nil {
			t.Fatal("expected missing length to be rejected")
		}
	})
}

func TestPadTransformer(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		flag   string
		length string
		chr    string
		want   string
	}{
		{name: "left short flag", value: "xyz", flag: "l", length: "4", chr: "A", want: "Axyz"},
		{name: "right short flag", value: "xyz", flag: "r", length: "4", chr: "A", want: "xyzA"},
		{name: "left long flag", value: "xyz", flag: "left", length: "6", chr: "A", want: "AAAxyz"},
		{name: "right long flag", value: "xyz", flag: "right", length: "5", chr: "0", want: "xyz00"},
		{name: "over length truncates right", value: "xyzzzzz", flag: "l", length: "4", chr: "A", want: "xyzz"},
		{name: "multi rune pad", value: "x", flag: "l", length: "5", chr: "ab", want: "ababx"},
		{name: "zero length", value: "abc", flag: "l", length: "0", chr: "A", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transformer := mustTransformer(t, "dx_pad", 0, tt.flag, tt.length, tt.chr)
			record := newTestRecord(element.NewStringColumn(tt.value))

			if _, err := transformer.Evaluate(record); err != nil {
				t.Fatalf("Evaluate failed: %v", err)
			}
			if got := record.GetColumn(0).GetAsString(); got != tt.want {
				t.Fatalf("value = %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("null becomes padding", func(t *testing.T) {
		transformer := mustTransformer(t, "dx_pad", 0, "l", "4", "A")
		record := newTestRecord(element.NewStringColumn(""))
		if _, err := transformer.Evaluate(record); err != nil {
			t.Fatalf("Evaluate failed: %v", err)
		}
		if got := record.GetColumn(0).GetAsString(); got != "AAAA" {
			t.Fatalf("value = %q, want %q", got, "AAAA")
		}
	})

	t.Run("invalid params", func(t *testing.T) {
		if _, err := New("dx_pad", 0, []string{"middle", "4", "A"}); err == nil {
			t.Fatal("expected invalid flag to be rejected")
		}
		if _, err := New("dx_pad", 0, []string{"l", "4", ""}); err == nil {
			t.Fatal("expected empty chr to be rejected")
		}
	})
}

func TestReplaceTransformer(t *testing.T) {
	t.Run("addax positional", func(t *testing.T) {
		tests := []struct {
			value  string
			pos    string
			length string
			str    string
			want   string
		}{
			// Addax/DataX documentation examples (0-based positions).
			{value: "dataxTest", pos: "2", length: "4", str: "****", want: "da****est"},
			{value: "dataxTest", pos: "5", length: "10", str: "****", want: "datax****"},
			{value: "addaxTest", pos: "2", length: "4", str: "****", want: "ad****est"},
			{value: "你好世界", pos: "1", length: "1", str: "X", want: "你X世界"},
			{value: "abc", pos: "3", length: "2", str: "Z", want: "abcZ"},
		}
		for _, tt := range tests {
			transformer := mustTransformer(t, "dx_replace", 0, tt.pos, tt.length, tt.str)
			record := newTestRecord(element.NewStringColumn(tt.value))
			if _, err := transformer.Evaluate(record); err != nil {
				t.Fatalf("Evaluate failed: %v", err)
			}
			if got := record.GetColumn(0).GetAsString(); got != tt.want {
				t.Fatalf("dx_replace(%q, %s, %s, %q) = %q, want %q", tt.value, tt.pos, tt.length, tt.str, got, tt.want)
			}
		}

		transformer := mustTransformer(t, "dx_replace", 0, "5", "1", "X")
		record := newTestRecord(element.NewStringColumn("abc"))
		if _, err := transformer.Evaluate(record); err == nil {
			t.Fatal("expected out-of-range pos to be rejected")
		}
	})

	t.Run("legacy global replace", func(t *testing.T) {
		transformer := mustTransformer(t, "dx_replace", 0, "@", "_")
		record := newTestRecord(element.NewStringColumn("a@b@c"))
		if _, err := transformer.Evaluate(record); err != nil {
			t.Fatalf("Evaluate failed: %v", err)
		}
		if got := record.GetColumn(0).GetAsString(); got != "a_b_c" {
			t.Fatalf("value = %q, want %q", got, "a_b_c")
		}
	})

	t.Run("invalid params", func(t *testing.T) {
		if _, err := New("dx_replace", 0, []string{"only-one"}); err == nil {
			t.Fatal("expected 1 parameter to be rejected")
		}
		if _, err := New("dx_replace", 0, []string{"a", "b", "c", "d"}); err == nil {
			t.Fatal("expected 4 parameters to be rejected")
		}
		if _, err := New("dx_replace", 0, []string{"", "x"}); err == nil {
			t.Fatal("expected empty search string to be rejected")
		}
	})
}

func TestFilterTransformer(t *testing.T) {
	tests := []struct {
		name       string
		column     element.Column
		operator   string
		expr       string
		wantFilter bool
		wantErr    bool
	}{
		{name: "long greater", column: element.NewLongColumn(20), operator: ">", expr: "18", wantFilter: true},
		{name: "long not greater", column: element.NewLongColumn(10), operator: ">", expr: "18"},
		{name: "long greater equal", column: element.NewLongColumn(18), operator: ">=", expr: "18", wantFilter: true},
		{name: "long not equal", column: element.NewLongColumn(18), operator: "!=", expr: "20", wantFilter: true},
		{name: "long equal", column: element.NewLongColumn(18), operator: "=", expr: "18", wantFilter: true},
		{name: "double less", column: element.NewDoubleColumn(1.5), operator: "<", expr: "1.6", wantFilter: true},
		{name: "string equal", column: element.NewStringColumn("dataTest"), operator: "=", expr: "dataTest", wantFilter: true},
		{name: "string lexicographic", column: element.NewStringColumn("b"), operator: ">", expr: "a", wantFilter: true},
		{name: "bool lexicographic", column: element.NewBoolColumn(true), operator: ">", expr: "false", wantFilter: true},
		{name: "like full regex match", column: element.NewStringColumn("dataTest"), operator: "like", expr: "data.*", wantFilter: true},
		{name: "like requires full match", column: element.NewStringColumn("dataTest"), operator: "like", expr: "data"},
		{name: "not like non-match", column: element.NewStringColumn("dataTest"), operator: "not like", expr: "xyz.*", wantFilter: true},
		{name: "not like match", column: element.NewStringColumn("dataTest"), operator: "not like", expr: "data.*"},
		{name: "null equals null", column: element.NewStringColumn(""), operator: "=", expr: "null", wantFilter: true},
		{name: "null not equal null", column: element.NewStringColumn(""), operator: "!=", expr: "null"},
		{name: "null equals value", column: element.NewStringColumn(""), operator: "=", expr: "x"},
		{name: "null not equal value", column: element.NewStringColumn(""), operator: "!=", expr: "x", wantFilter: true},
		{name: "null comparison", column: element.NewStringColumn(""), operator: ">", expr: "5"},
		{name: "null like", column: element.NewStringColumn(""), operator: "like", expr: ".*"},
		{name: "null not like", column: element.NewStringColumn(""), operator: "not like", expr: ".*", wantFilter: true},
		{name: "numeric parse error", column: element.NewLongColumn(10), operator: ">", expr: "abc", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transformer := mustTransformer(t, "dx_filter", 0, tt.operator, tt.expr)
			record := newTestRecord(tt.column)

			result, err := transformer.Evaluate(record)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Evaluate failed: %v", err)
			}

			filtered := result == nil
			if filtered != tt.wantFilter {
				t.Fatalf("filtered = %v, want %v", filtered, tt.wantFilter)
			}
		})
	}

	t.Run("operator normalization", func(t *testing.T) {
		if _, err := New("dx_filter", 0, []string{"NOT   LIKE", "x.*"}); err != nil {
			t.Fatalf("expected spaced NOT LIKE to be accepted: %v", err)
		}
	})

	t.Run("invalid params", func(t *testing.T) {
		if _, err := New("dx_filter", 0, []string{"~", "1"}); err == nil {
			t.Fatal("expected invalid operator to be rejected")
		}
		if _, err := New("dx_filter", 0, []string{"like", "("}); err == nil {
			t.Fatal("expected invalid regex to be rejected")
		}
		if _, err := New("dx_filter", 0, []string{"=", ""}); err == nil {
			t.Fatal("expected empty expr to be rejected")
		}
	})
}

func TestDigestTransformer(t *testing.T) {
	transformer := mustTransformer(t, "dx_digest", 0, "md5")
	record := newTestRecord(element.NewStringColumn("hello"))
	if _, err := transformer.Evaluate(record); err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	want := "5d41402abc4b2a76b9719d911017c592"
	if got := record.GetColumn(0).GetAsString(); got != want {
		t.Fatalf("md5 = %q, want %q", got, want)
	}

	for _, algorithm := range []string{"sha1", "sha256"} {
		transformer := mustTransformer(t, "dx_digest", 0, algorithm)
		record := newTestRecord(element.NewStringColumn("hello"))
		if _, err := transformer.Evaluate(record); err != nil {
			t.Fatalf("%s Evaluate failed: %v", algorithm, err)
		}
		if got := record.GetColumn(0).GetAsString(); len(got) == 0 {
			t.Fatalf("%s produced empty digest", algorithm)
		}
	}

	if _, err := New("dx_digest", 0, []string{"crc32"}); err == nil {
		t.Fatal("expected unsupported algorithm to be rejected")
	}
}

func TestTransformerErrorsIncludeSupportedNames(t *testing.T) {
	_, err := New("dx_groovy", 0, nil)
	if err == nil {
		t.Fatal("expected dx_groovy to be rejected")
	}
	if !strings.Contains(err.Error(), "not supported") || !strings.Contains(err.Error(), "dx_substr") {
		t.Fatalf("unexpected dx_groovy error: %v", err)
	}

	_, err = New("nope", 0, nil)
	if err == nil {
		t.Fatal("expected unknown transformer to be rejected")
	}
	if !strings.Contains(err.Error(), "unknown transformer") {
		t.Fatalf("unexpected unknown transformer error: %v", err)
	}
}
