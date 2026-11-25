package connectiontest

import (
	"errors"
	"io"
	"os"
	"time"
)

// TestFileSystem checks that a filesystem root exists, is a directory and is readable.
func TestFileSystem(basePath string) Result {
	start := time.Now()
	info, err := os.Stat(basePath)
	if err != nil {
		return Result{Message: err.Error()}
	}
	if !info.IsDir() {
		return Result{Message: "根路径不是目录"}
	}
	dir, err := os.Open(basePath)
	if err != nil {
		return Result{Message: err.Error()}
	}
	defer dir.Close()
	if _, err := dir.Readdirnames(1); err != nil && !errors.Is(err, io.EOF) {
		return Result{Message: err.Error()}
	}
	return Result{
		OK:        true,
		LatencyMs: time.Since(start).Milliseconds(),
		Message:   "目录可访问",
	}
}
