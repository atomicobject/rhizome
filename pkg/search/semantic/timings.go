package semantic

import (
	"context"
	"errors"
	"time"
)

type timingKey struct{}

// TimingEvent captures a named timing span.
type TimingEvent struct {
	Name     string
	Kind     string
	Started  time.Time
	Duration time.Duration
	Status   string
	Err      string
}

// TimingSink accepts timing events for instrumentation.
type TimingSink interface {
	Add(TimingEvent)
}

// WithTimingSink attaches a timing sink to the context.
func WithTimingSink(ctx context.Context, sink TimingSink) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, timingKey{}, sink)
}

func timingSinkFromContext(ctx context.Context) TimingSink {
	if ctx == nil {
		return nil
	}
	if v := ctx.Value(timingKey{}); v != nil {
		if sink, ok := v.(TimingSink); ok {
			return sink
		}
	}
	return nil
}

func addTiming(ctx context.Context, ev TimingEvent) {
	if sink := timingSinkFromContext(ctx); sink != nil {
		sink.Add(ev)
	}
}

func timingStatus(err error) string {
	if err == nil {
		return "ok"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	return "error"
}

func timingErr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
