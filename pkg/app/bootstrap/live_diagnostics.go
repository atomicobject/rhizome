package bootstrap

import (
	"errors"
	"log/slog"
	"os"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/logging"
)

// observeCapabilityPhase records readiness independently for each async phase.
// It never waits or turns a degraded optional capability into a startup failure.
func (rt *LiveRuntime) observeCapabilityPhase(phase string) func() {
	recorder := diagnostics.FromContext(rt.ctx)
	if recorder == nil {
		return func() {}
	}
	op := diagnostics.NewOperation("runtime.capability", phase)
	parent := diagnostics.OperationFromContext(rt.ctx)
	op.ParentID = parent.ID
	if parent.TraceID != "" {
		op.TraceID = parent.TraceID
	}
	ctx := diagnostics.WithOperation(rt.ctx, op)
	recorder.Event(ctx, slog.LevelInfo, "runtime", "capability.started", "", slog.String("capability", phase))
	return func() {
		snap := rt.Snapshot()
		ready := false
		var err error
		switch phase {
		case "search":
			ready, err = snap.Search.Ready, snap.Search.Err
		case "semantic":
			ready, err = snap.Semantic.Ready, snap.Semantic.Err
		case "code":
			ready, err = snap.Code.Ready, snap.Code.Err
		case "leader":
			ready = snap.Leader
		}
		status, reason := "success", ""
		if rt.ctx.Err() != nil {
			status = "canceled"
			reason = logging.ClassifyError(rt.ctx.Err())
		} else if errors.Is(err, ErrCapabilityNotRequested{}) {
			status = "skipped"
			reason = "capability_not_requested"
		} else if errors.Is(err, ErrEmbeddingsDisabled) {
			status = "skipped"
			reason = "embeddings_disabled"
		} else if phase == "code" && rt.readOnlyCodeIndex && errors.Is(err, os.ErrNotExist) {
			status = "skipped"
			reason = "index_missing"
		} else if logging.ClassifyError(err) == "not_configured" {
			status = "skipped"
			reason = "not_configured"
		} else if err != nil {
			status = "error"
			reason = logging.ClassifyError(err)
		} else if !ready {
			status = "skipped"
			reason = "capability_not_enabled"
		}
		// Readiness transitions are frequent lifecycle facts. Keep their duration
		// and outcome in events without an atomic report for every CLI phase.
		logging.CompleteEventQuiet(ctx, op, status, reason, map[string]any{"capability": phase, "ready": ready, "leader": snap.Leader, "watch_hub": snap.WatchHub != nil, "note_semantic": snap.NoteIndex != nil && snap.NoteProvider != nil, "code_semantic": snap.CodeIndex != nil && snap.CodeProvider != nil})
	}
}

func (rt *LiveRuntime) recordDiagnostic(level slog.Level, name string, err error, attrs ...slog.Attr) {
	recorder := diagnostics.FromContext(rt.ctx)
	if recorder == nil {
		return
	}
	message := ""
	if err != nil {
		attrs = append(attrs, slog.String("reason_code", logging.ClassifyError(err)))
		message = name + ": " + logging.ClassifyError(err)
	}
	recorder.Event(rt.ctx, level, "runtime", name, message, attrs...)
}
