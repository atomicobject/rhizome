package serve

import (
	"context"
	"time"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
)

// watchStartIntent lets stop cancel a startup still waiting on recovery, and
// gives an unresponsive attached owner the same graceful cancellation path.
func watchStartIntent(ctx context.Context, registry *appruntime.Registry, vaultPath, token string, stop func(), done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			intent, found, err := registry.StartIntentFor(ctx, vaultPath)
			if err == nil && (!found || intent.Token != token || intent.Cancelled) {
				stop()
				return
			}
		}
	}
}
