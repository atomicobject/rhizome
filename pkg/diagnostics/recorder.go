package diagnostics

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

var ErrNotFound = errors.New("diagnostics not found")
var ErrClosed = errors.New("diagnostics recorder closed")
var fallbackID atomic.Uint64

func newID() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err == nil {
		return hex.EncodeToString(data[:])
	}
	return fmt.Sprintf("%08x%016x%08x", uint32(os.Getpid()), uint64(time.Now().UnixNano()), uint32(fallbackID.Add(1)))
}

func NewOperation(kind, trigger string) Operation {
	id := newID()
	return Operation{ID: id, TraceID: id, Kind: kind, Trigger: trigger, StartedAt: time.Now().UTC()}
}

type Recorder struct {
	mu             sync.Mutex
	dir, processID string
	opts           Options
	seq            uint64
	segment        int
	file           *os.File
	size           int64
	date           string
	closed         bool
	dropped        uint64
	lastWarning    time.Time
}

func Open(vaultRoot string, opts Options) (*Recorder, error) {
	opts = defaults(opts)
	r := &Recorder{dir: filepath.Join(vaultRoot, ".rhizome", "diagnostics"), processID: newID(), opts: opts}
	if opts.Disabled {
		return r, nil
	}
	if err := ensureDirectories(r.dir); err != nil {
		return r, err
	}
	return r, nil
}

func (r *Recorder) Event(ctx context.Context, level slog.Level, subsystem, name, message string, attrs ...slog.Attr) {
	r.event(ctx, true, level, subsystem, name, message, attrs...)
}

// EventQuiet retains an event without mirroring it to stderr when a protocol
// response already owns the caller's error output. Persistence failures still warn.
func (r *Recorder) EventQuiet(ctx context.Context, level slog.Level, subsystem, name, message string, attrs ...slog.Attr) {
	r.event(ctx, false, level, subsystem, name, message, attrs...)
}

// SetStderr changes the console destination during a runtime output handoff.
// Restore it after that runtime has drained all writers.
func (r *Recorder) SetStderr(writer io.Writer) func() {
	if r == nil {
		return func() {}
	}
	if writer == nil {
		writer = io.Discard
	}
	r.mu.Lock()
	previous := r.opts.Stderr
	r.opts.Stderr = writer
	r.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			r.opts.Stderr = previous
			r.mu.Unlock()
		})
	}
}

func (r *Recorder) event(ctx context.Context, mirror bool, level slog.Level, subsystem, name, message string, attrs ...slog.Attr) {
	if r == nil {
		return
	}
	if r.opts.Disabled || level < r.opts.Level {
		if mirror && level >= slog.LevelWarn {
			r.mu.Lock()
			if !r.closed {
				subsystem, _ = boundedString(subsystem, 128)
				name, _ = boundedString(name, 128)
				message, _ = boundedString(message, 4096)
				fmt.Fprintf(r.opts.Stderr, "%s [%s] %s: %s\n", level.String(), subsystem, name, redact(message))
			}
			r.mu.Unlock()
		}
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.seq++
	op := OperationFromContext(ctx)
	e := Event{SchemaVersion: SchemaVersion, Time: r.opts.Now().UTC(), ProcessID: r.processID,
		PID: os.Getpid(), Role: r.opts.Role, Version: r.opts.Version, Sequence: r.seq,
		OperationID: op.ID, ParentOperationID: op.ParentID, TraceID: op.TraceID,
		Level: level.String()}
	var cut bool
	e.Subsystem, cut = boundedString(subsystem, 128)
	e.Truncated = e.Truncated || cut
	e.Name, cut = boundedString(name, 128)
	e.Truncated = e.Truncated || cut
	e.Message, cut = boundedString(message, 4096)
	e.Truncated = e.Truncated || cut
	e.Message = redact(e.Message)
	e.OperationID, _ = boundedString(e.OperationID, 128)
	e.ParentOperationID, _ = boundedString(e.ParentOperationID, 128)
	e.TraceID, _ = boundedString(e.TraceID, 128)
	e.Attributes, cut = attrsFromSlog(attrs)
	e.Truncated = e.Truncated || cut
	if r.dropped > 0 {
		if e.Attributes == nil {
			e.Attributes = map[string]any{}
		}
		e.Attributes["diagnostics.dropped"] = r.dropped
	}
	if mirror && level >= slog.LevelWarn {
		fmt.Fprintf(r.opts.Stderr, "%s [%s] %s: %s\n", e.Level, e.Subsystem, e.Name, e.Message)
	}
	data, err := json.Marshal(e)
	if err == nil && len(data)+1 > r.opts.MaxEventBytes {
		e.Attributes = nil
		e.Message = ""
		e.Truncated = true
		data, err = json.Marshal(e)
	}
	if err == nil && len(data)+1 > r.opts.MaxEventBytes {
		err = fmt.Errorf("event exceeds byte limit")
	}
	if err == nil {
		err = r.appendEventLocked(append(data, '\n'), level >= slog.LevelWarn || terminalEvent(name))
	}
	if err != nil {
		r.warningLocked(err)
		return
	}
	r.dropped = 0
}

func terminalEvent(name string) bool {
	switch name {
	case "operation.finished", "index.finished", "job.finished", "runtime.stopped":
		return true
	default:
		return false
	}
}

func (r *Recorder) warningLocked(err error) {
	r.dropped++
	now := r.opts.Now()
	if !r.lastWarning.IsZero() && now.Sub(r.lastWarning) < time.Minute {
		return
	}
	r.lastWarning = now
	// Never call slog or the standard logger from inside this sink.
	fmt.Fprintf(r.opts.Stderr, "WARN [diagnostics] persistence unavailable: %v\n", err)
}

func (r *Recorder) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	return r.closeSegmentLocked()
}

func (r *Recorder) PublishReport(ctx context.Context, report Report) error {
	if r == nil || r.opts.Disabled {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrClosed
	}
	report, data, err := r.prepareReport(ctx, report)
	if err == nil {
		err = r.publishReportLocked(report, data)
	}
	if err != nil {
		r.warningLocked(err)
	}
	return err
}

func (r *Recorder) Logger(subsystem string) *slog.Logger {
	return slog.New(&handler{recorder: r, subsystem: subsystem})
}

func (r *Recorder) StdlogWriter(subsystem string) io.Writer {
	return &stdlogWriter{recorder: r, subsystem: subsystem}
}
