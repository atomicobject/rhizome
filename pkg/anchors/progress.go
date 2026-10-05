package codeanchor

import "context"

type callEdgeProgressKey struct{}
type callEdgePlanProgressKey struct{}
type scopeProgressKey struct{}
type scopePlanProgressKey struct{}

type CallEdgeProgress interface {
	Start(total int)
	Advance(delta int)
}

type CallEdgePlanProgress interface {
	Start(total int)
	Advance(delta int)
}

type ScopeProgress interface {
	Start(total int)
	Advance(delta int)
}

type ScopePlanProgress interface {
	Start(total int)
	Advance(delta int)
}

func WithCallEdgeProgress(ctx context.Context, progress CallEdgeProgress) context.Context {
	if ctx == nil || progress == nil {
		return ctx
	}
	return context.WithValue(ctx, callEdgeProgressKey{}, progress)
}

func callEdgeProgressFromContext(ctx context.Context) CallEdgeProgress {
	if ctx == nil {
		return nil
	}
	progress, _ := ctx.Value(callEdgeProgressKey{}).(CallEdgeProgress)
	return progress
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

func WithScopeProgress(ctx context.Context, progress ScopeProgress) context.Context {
	if ctx == nil || progress == nil {
		return ctx
	}
	return context.WithValue(ctx, scopeProgressKey{}, progress)
}

func scopeProgressFromContext(ctx context.Context) ScopeProgress {
	if ctx == nil {
		return nil
	}
	progress, _ := ctx.Value(scopeProgressKey{}).(ScopeProgress)
	return progress
}

func WithScopePlanProgress(ctx context.Context, progress ScopePlanProgress) context.Context {
	if ctx == nil || progress == nil {
		return ctx
	}
	return context.WithValue(ctx, scopePlanProgressKey{}, progress)
}
