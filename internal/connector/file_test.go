package connector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCSVSourceAndDiscovery(t *testing.T) {
	base := t.TempDir()
	writeFixture(t, base, "input.csv", "id,name,score,active,created\n"+
		"1,alice,9.5,true,2024-01-02T03:04:05Z\n"+
		"2,\"bob, jr\",8,false,2024-01-03 04:05:06\n")

	cfg := filesystemConfig(base, "csv", map[string]string{"path": "input.csv"})
	cfg.Schema = sampleSchema()
	source, err := NewSource(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()

	rows := readAll(t, source)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0][0] != int64(1) || rows[0][1] != "alice" || rows[0][2] != 9.5 || rows[0][3] != true {
		t.Fatalf("unexpected first row: %#v", rows[0])
	}
	if rows[1][1] != "bob, jr" || rows[1][3] != false {
		t.Fatalf("unexpected second row: %#v", rows[1])
	}
	created, ok := rows[0][4].(time.Time)
	if !ok || !created.Equal(time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("unexpected timestamp: %#v", rows[0][4])
	}

	discovered, err := Discover(context.Background(), filesystemConfig(base, "csv", map[string]string{"path": "input.csv"}))
	if err != nil {
		t.Fatal(err)
	}
	expected := sampleSchema()
	if !schemaEqual(discovered, expected) {
		t.Fatalf("discovered %#v, want %#v", discovered, expected)
	}
}

func TestCSVBatchBoundaryAndEOF(t *testing.T) {
	base := t.TempDir()
	var builder strings.Builder
	builder.WriteString("id,name\n")
	const total = defaultBatchSize*2 + 88
	for index := 0; index < total; index++ {
		fmt.Fprintf(&builder, "%d,name-%d\n", index+1, index+1)
	}
	writeFixture(t, base, "many.csv", builder.String())

	cfg := filesystemConfig(base, "csv", map[string]string{"path": "many.csv"})
	cfg.Schema = Schema{{Name: "id", Type: TypeInt64}, {Name: "name", Type: TypeString}}
	source, err := NewSource(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()

	ctx := context.Background()
	split := Split{ID: "0"}
	for _, size := range []int{defaultBatchSize, defaultBatchSize, 88} {
		batch, err := source.ReadBatch(ctx, split)
		if err != nil {
			t.Fatal(err)
		}
		if len(batch.Rows) != size {
			t.Fatalf("batch size = %d, want %d", len(batch.Rows), size)
		}
	}
	if _, err := source.ReadBatch(ctx, split); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
	if _, err := source.ReadBatch(ctx, split); err != io.EOF {
		t.Fatalf("expected sticky EOF, got %v", err)
	}
}

func TestFileAppendAddsMissingNewline(t *testing.T) {
	base := t.TempDir()
	writeFixture(t, base, "append.txt", "1\told")
	schema := Schema{{Name: "id", Type: TypeInt64}, {Name: "name", Type: TypeString}}
	cfg := filesystemConfig(base, "txt", map[string]string{"path": "append.txt", "writeMode": "insert"})
	cfg.Schema = schema
	sink, err := NewSink(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.WriteBatch(context.Background(), Batch{Schema: schema, Rows: [][]any{{int64(2), "new"}}}); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(base, "append.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "1\told\n2\tnew\n" {
		t.Fatalf("unexpected content: %q", content)
	}

	sourceCfg := filesystemConfig(base, "txt", map[string]string{"path": "append.txt"})
	sourceCfg.Schema = schema
	source, err := NewSource(sourceCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if rows := readAll(t, source); len(rows) != 2 {
		t.Fatalf("got %d rows, want 2: %#v", len(rows), rows)
	}
}

func TestFileSinkRejectsValueTypeMismatch(t *testing.T) {
	base := t.TempDir()
	schema := Schema{{Name: "id", Type: TypeInt64}, {Name: "name", Type: TypeString}}
	cfg := filesystemConfig(base, "csv", map[string]string{"path": "bad-out.csv", "writeMode": "truncate"})
	cfg.Schema = schema
	sink, err := NewSink(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	err = sink.WriteBatch(context.Background(), Batch{
		Schema: schema,
		Rows:   [][]any{{"not-an-int", "alice"}},
	})
	var rowErr *RowError
	if !errors.As(err, &rowErr) || rowErr.Row != 0 {
		t.Fatalf("expected RowError at row 0, got %v", err)
	}
}

func TestJSONLBytesRoundTrip(t *testing.T) {
	base := t.TempDir()
	schema := Schema{{Name: "id", Type: TypeInt64}, {Name: "payload", Type: TypeBytes}}
	cfg := filesystemConfig(base, "jsonl", map[string]string{"path": "bytes.jsonl", "writeMode": "truncate"})
	cfg.Schema = schema
	sink, err := NewSink(cfg)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte{0x00, 0x01, 0xff, 'a'}
	if err := sink.WriteBatch(context.Background(), Batch{Schema: schema, Rows: [][]any{{int64(1), payload}}}); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}

	sourceCfg := filesystemConfig(base, "jsonl", map[string]string{"path": "bytes.jsonl"})
	sourceCfg.Schema = schema
	source, err := NewSource(sourceCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	rows := readAll(t, source)
	got, ok := rows[0][1].([]byte)
	if !ok || !bytes.Equal(got, payload) {
		t.Fatalf("payload = %#v, want %#v", rows[0][1], payload)
	}
}

func TestFileDelimiterValidation(t *testing.T) {
	base := t.TempDir()
	writeFixture(t, base, "delimiter.txt", "1|alice\n")
	schema := Schema{{Name: "id", Type: TypeInt64}, {Name: "name", Type: TypeString}}
	for name, tc := range map[string]struct {
		connector string
		delimiter string
		ok        bool
	}{
		"tab escape":      {connector: "txt", delimiter: `\t`, ok: true},
		"unicode escape":  {connector: "txt", delimiter: `\u0001`, ok: true},
		"literal pipe":    {connector: "txt", delimiter: "|", ok: true},
		"newline escape":  {connector: "txt", delimiter: `\n`},
		"unicode newline": {connector: "txt", delimiter: `\u000a`},
		"two characters":  {connector: "txt", delimiter: "||"},
		"csv quote":       {connector: "csv", delimiter: `"`},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := filesystemConfig(base, tc.connector, map[string]string{
				"path":      "delimiter.txt",
				"delimiter": tc.delimiter,
			})
			cfg.Schema = schema
			source, err := NewSource(cfg)
			if tc.ok {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				_ = source.Close()
				return
			}
			if err == nil {
				_ = source.Close()
				t.Fatal("expected delimiter validation error")
			}
		})
	}
}

func TestCSVSinkRoundTrip(t *testing.T) {
	base := t.TempDir()
	schema := sampleSchema()
	cfg := filesystemConfig(base, "csv", map[string]string{"path": "out.csv", "writeMode": "truncate"})
	cfg.Schema = schema
	sink, err := NewSink(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rows := [][]any{
		{int64(1), "alice, inc", 9.5, true, time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)},
		{int64(2), "bob\nsmith", 8.0, false, time.Date(2024, 1, 3, 4, 5, 6, 0, time.UTC)},
	}
	for index, row := range rows {
		if err := sink.WriteBatch(context.Background(), Batch{Schema: schema, Rows: [][]any{row}}); err != nil {
			t.Fatalf("write row %d: %v", index, err)
		}
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(filepath.Join(base, "out.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(content), "id,name,score,active,created\n") {
		t.Fatalf("missing header: %q", content)
	}

	sourceCfg := filesystemConfig(base, "csv", map[string]string{"path": "out.csv"})
	sourceCfg.Schema = schema
	source, err := NewSource(sourceCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	got := readAll(t, source)
	if len(got) != 2 || got[0][1] != "alice, inc" || got[1][1] != "bob\nsmith" {
		t.Fatalf("round trip mismatch: %#v", got)
	}
}

func TestCSVAppendDoesNotRepeatHeader(t *testing.T) {
	base := t.TempDir()
	schema := Schema{{Name: "id", Type: TypeInt64}, {Name: "name", Type: TypeString}}
	for index, mode := range []string{"truncate", "insert"} {
		cfg := filesystemConfig(base, "csv", map[string]string{"path": "append.csv", "writeMode": mode})
		cfg.Schema = schema
		sink, err := NewSink(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := sink.WriteBatch(context.Background(), Batch{Schema: schema, Rows: [][]any{{int64(index + 1), fmt.Sprintf("row-%d", index+1)}}}); err != nil {
			t.Fatal(err)
		}
		if err := sink.Close(); err != nil {
			t.Fatal(err)
		}
	}
	content, err := os.ReadFile(filepath.Join(base, "append.csv"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 3 || lines[0] != "id,name" {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestCSVNullFormat(t *testing.T) {
	base := t.TempDir()
	writeFixture(t, base, "nulls.csv", "id,name\n1,\n2,NULL\n")
	cfg := filesystemConfig(base, "csv", map[string]string{"path": "nulls.csv", "nullFormat": "NULL"})
	cfg.Schema = Schema{{Name: "id", Type: TypeInt64}, {Name: "name", Type: TypeString}}
	source, err := NewSource(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	rows := readAll(t, source)
	if rows[0][1] != "" {
		t.Fatalf("empty string should stay empty, got %#v", rows[0][1])
	}
	if rows[1][1] != nil {
		t.Fatalf("NULL should map to nil, got %#v", rows[1][1])
	}
}

func TestCSVDefaultEmptyBecomesNull(t *testing.T) {
	base := t.TempDir()
	writeFixture(t, base, "empty.csv", "id,name\n1,\n")
	cfg := filesystemConfig(base, "csv", map[string]string{"path": "empty.csv"})
	cfg.Schema = Schema{{Name: "id", Type: TypeInt64}, {Name: "name", Type: TypeString}}
	source, err := NewSource(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	rows := readAll(t, source)
	if rows[0][1] != nil {
		t.Fatalf("empty field should default to nil, got %#v", rows[0][1])
	}
}

func TestCSVSinkSchemaMismatch(t *testing.T) {
	base := t.TempDir()
	cfg := filesystemConfig(base, "csv", map[string]string{"path": "mismatch.csv"})
	cfg.Schema = Schema{{Name: "id", Type: TypeInt64}}
	sink, err := NewSink(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	err = sink.WriteBatch(context.Background(), Batch{
		Schema: Schema{{Name: "other", Type: TypeInt64}},
		Rows:   [][]any{{int64(1)}},
	})
	if err == nil {
		t.Fatal("expected schema mismatch")
	}
}

func TestCSVValidationRejectsBadPaths(t *testing.T) {
	base := t.TempDir()
	for _, path := range []string{"../escape.csv", filepath.Join(base, "absolute.csv")} {
		cfg := filesystemConfig(base, "csv", map[string]string{"path": path})
		cfg.Schema = sampleSchema()
		if _, err := NewSource(cfg); err == nil {
			t.Fatalf("expected path %q to be rejected", path)
		}
	}
	cfg := filesystemConfig("relative/base", "csv", map[string]string{"path": "file.csv"})
	cfg.Schema = sampleSchema()
	if _, err := NewSource(cfg); err == nil {
		t.Fatal("expected relative basePath to be rejected")
	}
	cfg = filesystemConfig(base, "csv", map[string]string{"path": "file.csv", "delimiter": "||"})
	cfg.Schema = sampleSchema()
	if _, err := NewSource(cfg); err == nil {
		t.Fatal("expected multi-character delimiter to be rejected")
	}
}

func TestCSVRowErrorIsFailFast(t *testing.T) {
	base := t.TempDir()
	writeFixture(t, base, "bad.csv", "id,name\nnot-a-number,alice\n")
	cfg := filesystemConfig(base, "csv", map[string]string{"path": "bad.csv"})
	cfg.Schema = Schema{{Name: "id", Type: TypeInt64}, {Name: "name", Type: TypeString}}
	source, err := NewSource(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	_, err = source.ReadBatch(context.Background(), Split{ID: "0"})
	var rowErr *RowError
	if !errors.As(err, &rowErr) || rowErr.Row != 0 {
		t.Fatalf("expected RowError at row 0, got %v", err)
	}
	if _, err := source.ReadBatch(context.Background(), Split{ID: "0"}); err != io.EOF {
		t.Fatalf("expected sticky EOF after failure, got %v", err)
	}
}

func TestTXTHeaderAndDiscovery(t *testing.T) {
	base := t.TempDir()
	writeFixture(t, base, "people.txt", "id\tname\tscore\tactive\n1\talice\t9.5\ttrue\n2\tbob\t8\tfalse\n")

	cfg := filesystemConfig(base, "txt", map[string]string{"path": "people.txt", "header": "true"})
	cfg.Schema = Schema{
		{Name: "id", Type: TypeInt64},
		{Name: "name", Type: TypeString},
		{Name: "score", Type: TypeFloat64},
		{Name: "active", Type: TypeBool},
	}
	source, err := NewSource(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	rows := readAll(t, source)
	if len(rows) != 2 || rows[0][1] != "alice" || rows[1][3] != false {
		t.Fatalf("unexpected rows: %#v", rows)
	}

	discovered, err := Discover(context.Background(), filesystemConfig(base, "txt", map[string]string{"path": "people.txt", "header": "true"}))
	if err != nil {
		t.Fatal(err)
	}
	expected := Schema{
		{Name: "id", Type: TypeInt64},
		{Name: "name", Type: TypeString},
		{Name: "score", Type: TypeFloat64},
		{Name: "active", Type: TypeBool},
	}
	if !schemaEqual(discovered, expected) {
		t.Fatalf("discovered %#v, want %#v", discovered, expected)
	}
}

func TestTXTWithoutHeaderUsesGeneratedNames(t *testing.T) {
	base := t.TempDir()
	writeFixture(t, base, "plain.txt", "1|alice|9.5\n2|bob|8\n")
	discovered, err := Discover(context.Background(), filesystemConfig(base, "txt", map[string]string{"path": "plain.txt", "delimiter": "|"}))
	if err != nil {
		t.Fatal(err)
	}
	expected := Schema{
		{Name: "column_1", Type: TypeInt64},
		{Name: "column_2", Type: TypeString},
		{Name: "column_3", Type: TypeFloat64},
	}
	if !schemaEqual(discovered, expected) {
		t.Fatalf("discovered %#v, want %#v", discovered, expected)
	}
}

func TestTXTSinkRoundTrip(t *testing.T) {
	base := t.TempDir()
	schema := Schema{{Name: "id", Type: TypeInt64}, {Name: "name", Type: TypeString}}
	cfg := filesystemConfig(base, "txt", map[string]string{"path": "out.txt", "writeMode": "truncate", "header": "true"})
	cfg.Schema = schema
	sink, err := NewSink(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.WriteBatch(context.Background(), Batch{Schema: schema, Rows: [][]any{{int64(1), "alice"}, {int64(2), "bob"}}}); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(base, "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "id\tname\n1\talice\n2\tbob\n" {
		t.Fatalf("unexpected output: %q", content)
	}
}

func TestJSONLSourceAndDiscovery(t *testing.T) {
	base := t.TempDir()
	writeFixture(t, base, "events.jsonl", `{"id":1,"name":"alice","score":9.5,"active":true,"created":"2024-01-02T03:04:05Z"}
{"id":2,"name":"bob","score":8,"active":false,"created":"2024-01-03T04:05:06Z","extra":"ignored"}
`)
	cfg := filesystemConfig(base, "jsonl", map[string]string{"path": "events.jsonl"})
	cfg.Schema = sampleSchema()
	source, err := NewSource(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	rows := readAll(t, source)
	if len(rows) != 2 || rows[0][0] != int64(1) || rows[1][3] != false {
		t.Fatalf("unexpected rows: %#v", rows)
	}
	if _, ok := rows[0][4].(time.Time); !ok {
		t.Fatalf("expected timestamp, got %#v", rows[0][4])
	}

	discovered, err := Discover(context.Background(), filesystemConfig(base, "jsonl", map[string]string{"path": "events.jsonl"}))
	if err != nil {
		t.Fatal(err)
	}
	expected := Schema{
		{Name: "id", Type: TypeInt64},
		{Name: "name", Type: TypeString},
		{Name: "score", Type: TypeFloat64},
		{Name: "active", Type: TypeBool},
		{Name: "created", Type: TypeString},
		{Name: "extra", Type: TypeString},
	}
	if !schemaEqual(discovered, expected) {
		t.Fatalf("discovered %#v, want %#v", discovered, expected)
	}
}

func TestJSONLSinkRoundTrip(t *testing.T) {
	base := t.TempDir()
	schema := sampleSchema()
	cfg := filesystemConfig(base, "jsonl", map[string]string{"path": "out.jsonl", "writeMode": "truncate"})
	cfg.Schema = schema
	sink, err := NewSink(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rows := [][]any{
		{int64(1), "alice", 9.5, true, time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)},
		{int64(2), "bob", nil, false, nil},
	}
	if err := sink.WriteBatch(context.Background(), Batch{Schema: schema, Rows: rows}); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(base, "out.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `{"id":2,"name":"bob","score":null,"active":false,"created":null}`) {
		t.Fatalf("unexpected output: %q", content)
	}

	sourceCfg := filesystemConfig(base, "jsonl", map[string]string{"path": "out.jsonl"})
	sourceCfg.Schema = schema
	source, err := NewSource(sourceCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	got := readAll(t, source)
	if len(got) != 2 || got[0][3] != true || got[1][2] != nil {
		t.Fatalf("round trip mismatch: %#v", got)
	}
}

func TestJSONLMissingKeyBecomesNull(t *testing.T) {
	base := t.TempDir()
	writeFixture(t, base, "missing.jsonl", `{"id":1}
`)
	cfg := filesystemConfig(base, "jsonl", map[string]string{"path": "missing.jsonl"})
	cfg.Schema = Schema{{Name: "id", Type: TypeInt64}, {Name: "name", Type: TypeString}}
	source, err := NewSource(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	rows := readAll(t, source)
	if rows[0][1] != nil {
		t.Fatalf("expected nil for missing key, got %#v", rows[0][1])
	}
}

func TestJSONLRejectsNestedValues(t *testing.T) {
	base := t.TempDir()
	writeFixture(t, base, "nested.jsonl", `{"id":1,"payload":{"a":1}}
`)
	_, err := Discover(context.Background(), filesystemConfig(base, "jsonl", map[string]string{"path": "nested.jsonl"}))
	if err == nil || !strings.Contains(err.Error(), "nested") {
		t.Fatalf("expected nested value error, got %v", err)
	}
}

func TestFileSourceRejectsUnknownSplit(t *testing.T) {
	base := t.TempDir()
	writeFixture(t, base, "one.csv", "id\n1\n")
	cfg := filesystemConfig(base, "csv", map[string]string{"path": "one.csv"})
	cfg.Schema = Schema{{Name: "id", Type: TypeInt64}}
	source, err := NewSource(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if _, err := source.ReadBatch(context.Background(), Split{ID: "1"}); err == nil {
		t.Fatal("expected unknown split error")
	}
}
