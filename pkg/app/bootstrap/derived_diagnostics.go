package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/logging"
)

func derivedAttrs(ticket codeanchor.DerivedWork) []slog.Attr {
	scope := "path"
	if ticket.Path == "" {
		scope = "full"
	}
	kind := "unknown"
	switch ticket.Kind {
	case codeanchor.DerivedNotes, codeanchor.DerivedCode, codeanchor.DerivedOntology, codeanchor.DerivedGraph:
		kind = string(ticket.Kind)
	}
	return []slog.Attr{slog.String("domain", kind), slog.String("scope", scope), slog.Int("attempt", ticket.Attempt)}
}

func observeDerivedWork(ctx context.Context, ticket codeanchor.DerivedWork) (context.Context, func(error)) {
	parent := diagnostics.OperationFromContext(ctx)
	attrs := derivedAttrs(ticket)
	op := diagnostics.NewOperation("derived-work", attrs[0].Value.String())
	op.ParentID = parent.ID
	if parent.TraceID != "" {
		op.TraceID = parent.TraceID
	}
	ctx = diagnostics.WithOperation(ctx, op)
	collector := indexingperf.NewBounded()
	ctx = indexingperf.WithCollector(ctx, collector)
	ctx, finish := watchPhase(ctx, "derived_work")
	watcherEvent(ctx, slog.LevelInfo, "derived.started", attrs...)
	return ctx, func(err error) {
		finish(err)
		status, reason := "success", ""
		switch {
		case errors.Is(err, errDerivedSuperseded):
			status, reason = "skipped", "source_changed"
		case errors.Is(err, lane.ErrPreempted):
			status, reason = "preempted", "explicit_index"
		case errors.Is(err, errDerivedExternalPriority):
			status, reason = "preempted", "external_priority"
		case errors.Is(err, lane.ErrClosed):
			status, reason = "canceled", "runtime_shutdown"
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			status, reason = "canceled", logging.ClassifyError(err)
		case err != nil:
			status, reason = "error", logging.ClassifyError(err)
		}
		snapshot := collector.Snapshot()
		metrics, _ := json.Marshal(snapshot)
		fields := map[string]any{"domain": attrs[0].Value.String(), "scope": attrs[1].Value.String(), "attempt": ticket.Attempt}
		logging.CompleteEventQuiet(ctx, op, status, reason, fields)
		if recorder := diagnostics.FromContext(ctx); recorder != nil {
			finished := time.Now()
			errorClass := logging.ClassifyError(err)
			if status == "skipped" {
				errorClass = ""
			}
			_ = recorder.PublishReport(ctx, diagnostics.Report{
				FinishedAt: finished, Status: status, ReasonCode: reason, Error: errorClass,
				Attributes: fields, Metrics: metrics,
				Truncated: snapshot.Coverage.RejectedObservations > 0 || snapshot.Coverage.DroppedSamples > 0 || snapshot.Coverage.DroppedIntervals > 0,
			})
		}
	}
}

func (s *derivedScheduler) compute(parent context.Context, phase string, run func(context.Context) error) (resultErr error) {
	ctx, stop := s.computeContext(parent)
	ctx, finish := watchPhase(ctx, phase)
	defer func() {
		if panicked := recover(); panicked != nil {
			stop()
			finish(errors.New("diagnostic boundary panicked"))
			panic(panicked)
		}
		if errors.Is(resultErr, context.Canceled) {
			if cause := context.Cause(ctx); errors.Is(cause, lane.ErrPreempted) || errors.Is(cause, errDerivedExternalPriority) {
				resultErr = errors.Join(resultErr, cause)
			}
		}
		stop()
		finish(resultErr)
	}()
	return run(ctx)
}
