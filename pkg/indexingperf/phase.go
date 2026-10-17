package indexingperf

import "context"

// RunPhase records the same phase identity for its measurements and total span.
func RunPhase(ctx context.Context, name string, run func(context.Context) error) error {
	phaseCtx := WithPhase(ctx, name)
	done := StartSpan(phaseCtx, name)
	err := run(phaseCtx)
	done(err)
	return err
}
