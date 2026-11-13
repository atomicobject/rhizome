package embedindex

import (
	"context"
	"time"
)

const providerCancellationGrace = 5 * time.Second

// In-flight calls get a short chance to return paid-for vectors before shutdown.
func providerCallContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	stop := context.AfterFunc(parent, func() {
		timer := time.NewTimer(providerCancellationGrace)
		defer timer.Stop()
		select {
		case <-timer.C:
			cancel()
		case <-ctx.Done():
		}
	})
	return ctx, func() { stop(); cancel() }
}
