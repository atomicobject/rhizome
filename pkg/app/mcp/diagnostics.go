package mcp

import (
	"context"
	"errors"
	"log/slog"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/logging"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// ObserveTool measures actual handler execution and MCP's structured error bit.
// Tool names come from the authoritative catalog, never request payloads.
func ObserveTool(name string, cfg Config, handler func(context.Context, mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error)) func(context.Context, mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (result *mcpgo.CallToolResult, err error) {
		recorder := diagnostics.FromContext(ctx)
		if recorder == nil {
			return handler(ctx, req)
		}
		op := diagnostics.NewOperation("tool", name)
		parent := diagnostics.OperationFromContext(ctx)
		op.ParentID = parent.ID
		if parent.TraceID != "" {
			op.TraceID = parent.TraceID
		}
		ctx = diagnostics.WithOperation(ctx, op)
		attrs := map[string]any{"tool": name, "read_write": cfg.ReadWrite}
		if cfg.Runtime != nil {
			snap := cfg.Runtime.Snapshot()
			attrs["search_ready"] = snap.Search.Ready
			attrs["semantic_ready"] = snap.Semantic.Ready
			attrs["code_ready"] = snap.Code.Ready
		}
		recorder.Event(ctx, slog.LevelInfo, "mcp", "tool.started", "", slog.String("tool", name))
		defer func() {
			panicked := recover()
			status, reason := "success", ""
			if err != nil {
				status = "error"
				reason = logging.ClassifyError(err)
			} else if result != nil && result.IsError {
				status = "error"
				reason = "tool_error"
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				status, reason = "canceled", logging.ClassifyError(err)
			}
			if ctx.Err() != nil {
				status = "canceled"
				reason = logging.ClassifyError(ctx.Err())
			}
			if result != nil {
				attrs["content_blocks"] = len(result.Content)
				attrs["structured_error"] = result.IsError
			}
			if panicked != nil {
				status = "error"
				reason = "handler_panicked"
			}
			logging.CompleteEventQuiet(ctx, op, status, reason, attrs)
			if panicked != nil {
				panic(panicked)
			}
		}()
		return handler(ctx, req)
	}
}
