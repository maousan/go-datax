package execution

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogWriterTruncatesAtLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.log")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create log: %v", err)
	}

	writer := newLogWriter(file, 20, nil)
	_, _ = writer.Write([]byte("line one\nline two\nline three\n"))
	lastLine := writer.LastLine()
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	if lastLine != "line three" {
		t.Errorf("last line = %q, want line three", lastLine)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "[日志已达到大小上限，后续输出被截断]") {
		t.Errorf("log has no truncation notice: %q", string(data))
	}
}

func TestLogWriterStripsANSIColors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.log")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create log: %v", err)
	}

	writer := newLogWriter(file, 1024, nil)
	_, _ = writer.Write([]byte("\x1b[34mINFO\x1b[0m job started\n"))
	_ = writer.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if strings.Contains(string(data), "\x1b[") {
		t.Errorf("log still contains ANSI escapes: %q", string(data))
	}
	if !strings.Contains(string(data), "INFO job started") {
		t.Errorf("log content was damaged: %q", string(data))
	}
}

func TestLogWriterReportsOffsets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.log")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create log: %v", err)
	}

	var seqs []int64
	writer := newLogWriter(file, 1024, func(_ string, seq int64) {
		seqs = append(seqs, seq)
	})
	_, _ = writer.Write([]byte("a\nbc\n"))
	_ = writer.Close()

	if len(seqs) != 2 {
		t.Fatalf("offsets = %v, want two entries", seqs)
	}
	if seqs[0] != 2 || seqs[1] != 5 {
		t.Errorf("offsets = %v, want [2 5]", seqs)
	}
}
