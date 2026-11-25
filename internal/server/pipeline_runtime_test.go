package server

import (
	"bytes"
	"strings"
	"testing"
)

func TestPipelineLogWriterTruncates(t *testing.T) {
	var buffer bytes.Buffer
	writer := newPipelineLogWriter(&buffer, 64)
	writer.writeLog("%s", strings.Repeat("a", 200))
	writer.writeLog("ignored after truncation")
	output := buffer.String()
	if len(output) > 64 {
		t.Fatalf("log length = %d, want <= 64", len(output))
	}
	if !strings.Contains(output, "[log truncated]") {
		t.Fatalf("missing truncation marker: %q", output)
	}
	if strings.Contains(output, "ignored") {
		t.Fatalf("wrote after truncation: %q", output)
	}
}
