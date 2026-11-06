package codeanchor

import "context"

type callEdgePlanProgressKey struct{}

type CallEdgePlanProgress interface {
	Start(total int)
	Advance(delta int)
}

func WithCallEdgePlanProgress(ctx context.Context, progress CallEdgePlanProgress) context.Context {
	if ctx == nil || progress == nil {
		return ctx
	}
	return context.WithValue(ctx, callEdgePlanProgressKey{}, progress)
}

func callEdgePlanProgressFromContext(ctx context.Context) CallEdgePlanProgress {
	if ctx == nil {
		return nil
	}
	progress, _ := ctx.Value(callEdgePlanProgressKey{}).(CallEdgePlanProgress)
	return progress
}
