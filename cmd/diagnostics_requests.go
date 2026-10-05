package cmd

import (
	"context"
	"errors"
	"log/slog"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/logging"
)

// observeAgentRequest covers preparation, authorization, local execution and
// forwarding. Catalog handler reports remain child operations, so only the
// runtime that actually executed a handler reports tool execution.
func observeAgentRequest(ctx context.Context, name, transport string) (context.Context, func(bool, error)) {
	safeName := "unknown"
	for _, descriptor := range agentapi.ToolCatalog() {
		if descriptor.Name == name {
			safeName = name
			break
		}
	}
	if _, ok := agentapi.CodeOperationDescriptor(name); ok {
		safeName = name
	}
	op := diagnostics.NewOperation("agent.request", transport)
	parent := diagnostics.OperationFromContext(ctx)
	op.ParentID = parent.ID
	if parent.TraceID != "" {
		op.TraceID = parent.TraceID
	}
	ctx = diagnostics.WithOperation(ctx, op)
	if recorder := diagnostics.FromContext(ctx); recorder != nil {
		recorder.Event(ctx, slog.LevelInfo, "agent", "request.started", "", slog.String("tool", safeName), slog.String("transport", transport))
	}
	return ctx, func(ok bool, err error) {
		status, reason := "success", ""
		if err != nil {
			status = "error"
			reason = logging.ClassifyError(err)
		} else if !ok {
			status = "error"
			reason = "structured_error"
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			status, reason = "canceled", logging.ClassifyError(err)
		}
		if ctx.Err() != nil {
			status = "canceled"
			reason = logging.ClassifyError(ctx.Err())
		}
		logging.CompleteEventQuiet(ctx, op, status, reason, map[string]any{"tool": safeName, "transport": transport})
	}
}
