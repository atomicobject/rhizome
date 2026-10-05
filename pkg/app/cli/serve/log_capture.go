package serve

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/atomicobject/rhizome/pkg/logging"
)

// LogMaxBytes caps .rhizome/diagnostics/runtime-output.log. One rotation is kept as
// runtime-output.log.1. Go output uses at most 10 MiB; inherited native crash
// descriptors can exceed it until the next open retains a bounded tail.
const LogMaxBytes int64 = 5 << 20

// rotatingFile writes to path, rotating to path+".1" before a write would
// exceed maxBytes. Oversized writes retain a marked tail within the cap.
type rotatingFile struct {
	path     string
	maxBytes int64

	mu   sync.Mutex
	file *os.File
	size int64
}

func newRotatingFile(path string, maxBytes int64) (*rotatingFile, error) {
	if maxBytes <= 0 {
		maxBytes = LogMaxBytes
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	for _, target := range []string{path, path + ".1"} {
		if err := logging.TrimOutputTail(target, maxBytes); err != nil {
			return nil, err
		}
	}
	w := &rotatingFile{path: path, maxBytes: maxBytes}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *rotatingFile) open() error {
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	size := int64(0)
	if info, statErr := file.Stat(); statErr == nil {
		size = info.Size()
	}
	w.file, w.size = file, size
	return nil
}

func (w *rotatingFile) Write(p []byte) (int, error) {
	originalBytes := len(p)
	if int64(len(p)) > w.maxBytes {
		p = boundedOutputTail(p, w.maxBytes)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return len(p), nil
	}
	if w.size > 0 && w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotateLocked(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	if err == nil && n == len(p) {
		return originalBytes, nil
	}
	return n, err
}

func (w *rotatingFile) rotateLocked() error {
	if err := w.file.Close(); err != nil {
		return err
	}
	// A failed rename must not lose the handle: reopen and keep appending.
	if err := os.Rename(w.path, w.path+".1"); err != nil && !os.IsNotExist(err) {
		return w.open()
	}
	return w.open()
}

func (w *rotatingFile) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

// LogCapture redirects the process's stdout and stderr into the runtime log.
// A pipe stands in for the real descriptors so the capped, rotating writer sees
// every byte, including output from code that writes to os.Stdout directly.
type LogCapture struct {
	writer  *rotatingFile
	read    *os.File
	write   *os.File
	prevOut *os.File
	prevErr *os.File
	drained chan struct{}
	once    sync.Once
}

// CaptureProcessOutput points os.Stdout, os.Stderr, and the returned writer at
// path. Callers set the `log` package output to Writer() themselves so they can
// wrap it first.
func CaptureProcessOutput(path string, maxBytes int64) (*LogCapture, error) {
	return captureProcessOutput(context.Background(), path, maxBytes)
}

func captureProcessOutput(ctx context.Context, path string, maxBytes int64) (*LogCapture, error) {
	writer, err := newRotatingFile(path, maxBytes)
	if err != nil {
		return nil, err
	}
	read, write, err := os.Pipe()
	if err != nil {
		_ = writer.Close()
		return nil, err
	}
	capture := &LogCapture{
		writer:  writer,
		read:    read,
		write:   write,
		prevOut: os.Stdout,
		prevErr: os.Stderr,
		drained: make(chan struct{}),
	}
	os.Stdout, os.Stderr = write, write
	go func() {
		defer close(capture.drained)
		if _, err := io.Copy(writer, read); err != nil {
			recordServeDiagnostic(ctx, slog.LevelWarn, "output_capture.write_failed", err)
			// Keep draining after a failed sink. Leaving the pipe's read end open
			// without a reader would block later stdout/stderr writes indefinitely.
			_, _ = io.Copy(io.Discard, read)
		}
	}()
	return capture, nil
}

// Writer is the destination for the command's own output while captured.
func (c *LogCapture) Writer() io.Writer {
	if c == nil {
		return io.Discard
	}
	return c.writer
}

// Close restores the previous descriptors and drains buffered output.
func (c *LogCapture) Close() error {
	if c == nil {
		return nil
	}
	c.once.Do(func() {
		os.Stdout, os.Stderr = c.prevOut, c.prevErr
		_ = c.write.Close()
		<-c.drained
		_ = c.read.Close()
		_ = c.writer.Close()
	})
	return nil
}

func boundedOutputTail(data []byte, max int64) []byte {
	if int64(len(data)) <= max {
		return data
	}
	marker := []byte("[earlier process output truncated]\n")
	if max < int64(len(marker)) {
		return append([]byte(nil), data[len(data)-int(max):]...)
	}
	tail := data[len(data)-int(max)+len(marker):]
	return append(marker, tail...)
}
