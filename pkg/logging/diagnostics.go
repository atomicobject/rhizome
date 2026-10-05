package logging

import (
	"context"
	"errors"
	"io"
	"log"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
)

// ClassifyError preserves an actionable category without persisting arbitrary
// provider responses, query text, credentials, or source paths in errors.
func ClassifyError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, os.ErrNotExist):
		return "not_found"
	case errors.Is(err, os.ErrPermission):
		return "permission_denied"
	case errors.Is(err, os.ErrExist):
		return "already_exists"
	}
	text := err.Error()
	if len(text) > 8192 {
		text = text[:8192]
	}
	text = strings.ToLower(text)
	for _, item := range []struct{ match, code string }{
		{"diagnostic boundary panicked", "handler_panicked"}, {"permission denied", "permission_denied"}, {"no such file", "not_found"},
		{"embeddings.provider is required", "not_configured"}, {"provider is required", "not_configured"},
		{"database is locked", "database_locked"}, {"database is busy", "database_busy"},
		{"sqlite_busy", "database_busy"}, {"database disk image is malformed", "database_corrupt"},
		{"no space left", "disk_full"}, {"connection refused", "connection_refused"},
		{"unauthorized", "unauthorized"}, {"rate limit", "rate_limited"},
		{"not configured", "not_configured"}, {"unavailable", "unavailable"},
		{"read-only", "read_only"}, {"readonly", "read_only"},
	} {
		if strings.Contains(text, item.match) {
			return item.code
		}
	}
	return "operation_failed"
}

// Complete publishes one terminal report. Diagnostics never changes the caller's
// result, including when persistence itself fails.
func Complete(ctx context.Context, op diagnostics.Operation, status, reason string, attrs map[string]any) {
	complete(ctx, op, status, reason, attrs, false)
}

// CompleteQuiet preserves structured failures for protocols that already return
// their error in a JSON envelope. Unrelated warnings still reach stderr.
func CompleteQuiet(ctx context.Context, op diagnostics.Operation, status, reason string, attrs map[string]any) {
	complete(ctx, op, status, reason, attrs, true)
}

func complete(ctx context.Context, op diagnostics.Operation, status, reason string, attrs map[string]any, quiet bool) {
	r := diagnostics.FromContext(ctx)
	if r == nil {
		return
	}
	finished := time.Now()
	_ = r.PublishReport(ctx, diagnostics.Report{
		OperationID: op.ID, ParentOperationID: op.ParentID, TraceID: op.TraceID,
		Kind: op.Kind, Trigger: op.Trigger, StartedAt: op.StartedAt, FinishedAt: finished,
		DurationMS: finished.Sub(op.StartedAt).Milliseconds(), Status: status, ReasonCode: reason, Attributes: attrs,
	})
	completeEvent(ctx, op, status, reason, attrs, finished, "operation.finished", terminalLevel(status), quiet)
}

// CompleteEvent records frequent request boundaries without creating a report
// file or forcing an informational event segment to sync on every poll.
func CompleteEvent(ctx context.Context, op diagnostics.Operation, status, reason string, attrs map[string]any) {
	completeEvent(ctx, op, status, reason, attrs, time.Now(), op.Kind+".finished", terminalLevel(status), false)
}

// CompleteEventQuiet retains request outcomes without duplicating the error
// output owned by a protocol or the caller.
func CompleteEventQuiet(ctx context.Context, op diagnostics.Operation, status, reason string, attrs map[string]any) {
	CompleteEventQuietAtLevel(ctx, op, terminalLevel(status), status, reason, attrs)
}

// CompleteEventQuietAtLevel separates severity from outcome. HTTP client errors
// are ordinary request outcomes, while server failures warrant ERROR history.
func CompleteEventQuietAtLevel(ctx context.Context, op diagnostics.Operation, level slog.Level, status, reason string, attrs map[string]any) {
	completeEvent(ctx, op, status, reason, attrs, time.Now(), op.Kind+".finished", level, true)
}

func terminalLevel(status string) slog.Level {
	if status == "error" {
		return slog.LevelError
	}
	return slog.LevelInfo
}

func completeEvent(ctx context.Context, op diagnostics.Operation, status, reason string, attrs map[string]any, finished time.Time, name string, level slog.Level, quiet bool) {
	recorder := diagnostics.FromContext(ctx)
	if recorder == nil {
		return
	}
	message := ""
	if status == "error" {
		message = op.Trigger + " failed (" + reason + ")"
	}
	fields := []slog.Attr{slog.String("kind", op.Kind), slog.String("trigger", op.Trigger), slog.String("status", status), slog.String("reason_code", reason), slog.Int64("duration_ms", finished.Sub(op.StartedAt).Milliseconds())}
	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fields = append(fields, slog.Any(key, attrs[key]))
	}
	if quiet {
		recorder.EventQuiet(diagnostics.WithOperation(ctx, op), level, op.Kind, name, message, fields...)
	} else {
		recorder.Event(diagnostics.WithOperation(ctx, op), level, op.Kind, name, message, fields...)
	}
}

// InstallStandard leases the process logger. Overlapping runtime scopes may
// restore in either order; the original writer returns after the last lease.
// Legacy text is classified and reduced to safe operational facts. New code
// should emit explicit structured events instead.
func InstallStandard(ctx context.Context) func() {
	if diagnostics.FromContext(ctx) == nil {
		return func() {}
	}
	standard.install.Lock()
	defer standard.install.Unlock()
	standard.mu.Lock()
	token := &standardLease{ctx: ctx}
	first := len(standard.leases) == 0
	if first {
		standard.writer, standard.flags = log.Writer(), log.Flags()
	}
	standard.leases = append(standard.leases, token)
	standard.mu.Unlock()
	if first {
		log.SetOutput(standardBridge{})
		log.SetFlags(0)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			standard.install.Lock()
			defer standard.install.Unlock()
			standard.mu.Lock()
			for i, lease := range standard.leases {
				if lease == token {
					standard.leases = append(standard.leases[:i], standard.leases[i+1:]...)
					break
				}
			}
			last := len(standard.leases) == 0
			writer, flags := standard.writer, standard.flags
			standard.mu.Unlock()
			if last {
				log.SetOutput(writer)
				log.SetFlags(flags)
			}
		})
	}
}

type standardLease struct{ ctx context.Context }

var standard struct {
	install sync.Mutex
	mu      sync.Mutex
	leases  []*standardLease
	writer  io.Writer
	flags   int
}

func StandardInstalled() bool {
	standard.mu.Lock()
	defer standard.mu.Unlock()
	return len(standard.leases) > 0
}

type standardBridge struct{}

func (standardBridge) Write(data []byte) (int, error) {
	standard.mu.Lock()
	if len(standard.leases) == 0 {
		standard.mu.Unlock()
		return len(data), nil
	}
	ctx := standard.leases[len(standard.leases)-1].ctx
	standard.mu.Unlock()
	r := diagnostics.FromContext(ctx)
	if r == nil {
		return len(data), nil
	}
	bounded := data
	if len(bounded) > 8192 {
		bounded = bounded[:8192]
	}
	text := strings.ToLower(strings.TrimSpace(string(bounded)))
	level := slog.LevelInfo
	if strings.Contains(text, "warning") || strings.Contains(text, "failed") || strings.Contains(text, "error") || strings.Contains(text, "unavailable") || strings.HasPrefix(text, "warn") {
		level = slog.LevelWarn
	}
	if strings.HasPrefix(text, "error") || strings.HasPrefix(text, "fatal") || strings.HasPrefix(text, "panic") {
		level = slog.LevelError
	}
	component := "legacy"
	for _, prefix := range []string{"live:", "serve:", "codeanchor:", "index:", "watcher:", "semantic:", "validation refresh:"} {
		if strings.HasPrefix(text, prefix) {
			component = strings.TrimSuffix(prefix, ":")
			break
		}
	}
	reason := "legacy_payload_redacted"
	for _, candidate := range []string{"started", "stopped", "ready", "closed", "shutdown", "cleanup"} {
		if strings.Contains(text, candidate) {
			reason = candidate
			break
		}
	}
	if category := ClassifyError(errors.New(text)); category != "operation_failed" {
		reason = category
	}
	message := ""
	if level >= slog.LevelWarn {
		message = component + " reported " + reason + ". Run `rzm diagnostics logs --level warn` for diagnostic history."
	}
	r.Event(ctx, level, component, "legacy.log", message, slog.String("reason_code", reason), slog.Bool("payload_redacted", true))
	return len(data), nil
}
