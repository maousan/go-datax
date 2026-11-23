package execution

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
)

var ansiPattern = regexp.MustCompile("\x1b\\[[0-9;]*m")

type logWriter struct {
	mu        sync.Mutex
	file      *os.File
	maxBytes  int64
	offset    int64
	truncated bool
	buffer    []byte
	lastLine  string
	onLine    func(line string, seq int64)
}

func newLogWriter(file *os.File, maxBytes int64, onLine func(string, int64)) *logWriter {
	return &logWriter{file: file, maxBytes: maxBytes, onLine: onLine}
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buffer = append(w.buffer, p...)
	for {
		index := bytes.IndexByte(w.buffer, '\n')
		if index < 0 {
			break
		}
		line := strings.TrimRight(string(w.buffer[:index]), "\r")
		w.buffer = w.buffer[index+1:]
		w.writeLine(line)
	}
	return len(p), nil
}

func (w *logWriter) writeLine(line string) {
	line = ansiPattern.ReplaceAllString(line, "")
	if strings.TrimSpace(line) != "" {
		w.lastLine = line
	}
	if w.truncated {
		return
	}

	if w.offset+int64(len(line))+1 > w.maxBytes {
		notice := "[日志已达到大小上限，后续输出被截断]"
		_, _ = w.file.WriteString(notice + "\n")
		w.offset += int64(len(notice)) + 1
		w.truncated = true
		return
	}

	_, _ = w.file.WriteString(line + "\n")
	w.offset += int64(len(line)) + 1
	if w.onLine != nil {
		w.onLine(line, w.offset)
	}
}

func (w *logWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.buffer) > 0 {
		w.writeLine(strings.TrimRight(string(w.buffer), "\r\n"))
		w.buffer = nil
	}
	return w.file.Close()
}

func (w *logWriter) LastLine() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastLine
}

type lineReader struct {
	reader *bufio.Reader
}

func newLineReader(r io.Reader) *lineReader {
	return &lineReader{reader: bufio.NewReader(r)}
}

// Next 返回一行与读取的字节数（包含换行符）。
func (l *lineReader) Next() (string, int64, error) {
	raw, err := l.reader.ReadString('\n')
	if len(raw) == 0 {
		return "", 0, err
	}
	line := strings.TrimRight(raw, "\r\n")
	return line, int64(len(raw)), err
}
