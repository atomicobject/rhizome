// Package diagnostics persists bounded, offline-readable operational evidence.
package diagnostics

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"time"
)

const SchemaVersion = 1

type Options struct {
	Disabled                      bool
	Role, Version                 string
	RetentionDays                 int
	MaxBytes, MaxSegmentBytes     int64
	MaxEventBytes, MaxReportBytes int
	Level                         slog.Level
	Stderr                        io.Writer
	Now                           func() time.Time
}

type Operation struct {
	ID, Kind, Trigger string
	ParentID, TraceID string
	StartedAt         time.Time
}

type Event struct {
	SchemaVersion     int            `json:"schema_version"`
	Time              time.Time      `json:"time"`
	ProcessID         string         `json:"process_id"`
	PID               int            `json:"pid"`
	Role              string         `json:"role,omitempty"`
	Version           string         `json:"version,omitempty"`
	Sequence          uint64         `json:"sequence"`
	OperationID       string         `json:"operation_id,omitempty"`
	ParentOperationID string         `json:"parent_operation_id,omitempty"`
	TraceID           string         `json:"trace_id,omitempty"`
	Subsystem         string         `json:"subsystem"`
	Level             string         `json:"level"`
	Name              string         `json:"event"`
	Message           string         `json:"message,omitempty"`
	Attributes        map[string]any `json:"attributes,omitempty"`
	Truncated         bool           `json:"truncated,omitempty"`
}

type Report struct {
	SchemaVersion     int             `json:"schema_version"`
	OperationID       string          `json:"operation_id"`
	ParentOperationID string          `json:"parent_operation_id,omitempty"`
	TraceID           string          `json:"trace_id,omitempty"`
	ProcessID         string          `json:"process_id"`
	PID               int             `json:"pid"`
	Role              string          `json:"role,omitempty"`
	Version           string          `json:"version,omitempty"`
	Kind              string          `json:"kind"`
	Trigger           string          `json:"trigger,omitempty"`
	StartedAt         time.Time       `json:"started_at"`
	FinishedAt        time.Time       `json:"finished_at"`
	DurationMS        int64           `json:"duration_ms"`
	QueueWaitMS       int64           `json:"queue_wait_ms,omitempty"`
	LockWaitMS        int64           `json:"lock_wait_ms,omitempty"`
	ExecutionMS       int64           `json:"execution_ms,omitempty"`
	Status            string          `json:"status"`
	ReasonCode        string          `json:"reason_code,omitempty"`
	Error             string          `json:"error,omitempty"`
	Summary           string          `json:"summary,omitempty"`
	Attributes        map[string]any  `json:"attributes,omitempty"`
	Metrics           json.RawMessage `json:"metrics,omitempty"`
	Truncated         bool            `json:"truncated,omitempty"`
}

// Filter bounds offline reads. Zero limits use bounded defaults.
type Filter struct {
	Since, Until                                  time.Time
	Kind, Status, OperationID, TraceID, Subsystem string
	Level                                         slog.Level
	Limit                                         int
	MaxBytes                                      int64
}

type Coverage struct {
	Since     time.Time `json:"since,omitempty"`
	Until     time.Time `json:"until,omitempty"`
	Files     int       `json:"files"`
	Bytes     int64     `json:"bytes"`
	Truncated bool      `json:"truncated"`
	Warnings  []string  `json:"warnings,omitempty"`
}

type EventResult struct {
	Events   []Event  `json:"events"`
	Coverage Coverage `json:"coverage"`
}

type ReportResult struct {
	Reports  []Report `json:"reports"`
	Coverage Coverage `json:"coverage"`
}

type recorderKey struct{}
type operationKey struct{}

func WithRecorder(ctx context.Context, recorder *Recorder) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, recorderKey{}, recorder)
}

func FromContext(ctx context.Context) *Recorder {
	if ctx == nil {
		return nil
	}
	r, _ := ctx.Value(recorderKey{}).(*Recorder)
	return r
}

func WithOperation(ctx context.Context, operation Operation) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, operationKey{}, operation)
}

func OperationFromContext(ctx context.Context) Operation {
	if ctx == nil {
		return Operation{}
	}
	op, _ := ctx.Value(operationKey{}).(Operation)
	return op
}

// Propagate copies diagnostic identity without carrying cancellation or other values.
func Propagate(from, onto context.Context) context.Context {
	if onto == nil {
		onto = context.Background()
	}
	if r := FromContext(from); r != nil {
		onto = WithRecorder(onto, r)
	}
	if op := OperationFromContext(from); op.ID != "" {
		onto = WithOperation(onto, op)
	}
	return onto
}
