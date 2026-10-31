package serve

import (
	"context"
	"time"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
)

// Start-intent polls begin fast so stop can cancel a startup promptly, then
// back off: each poll takes a file lock, and at a fixed 50 ms an idle runtime
// spent most of its CPU here. The cap stays well inside appruntime.StopGrace.
const (
	startIntentFirstPoll = 50 * time.Millisecond
	startIntentMaxPoll   = time.Second
)

// nextStartIntentPoll doubles the poll interval up to startIntentMaxPoll.
func nextStartIntentPoll(d time.Duration) time.Duration {
	return min(2*d, startIntentMaxPoll)
}

// watchStartIntent lets stop cancel a startup still waiting on recovery, and
// gives an unresponsive attached owner the same graceful cancellation path.
func watchStartIntent(ctx context.Context, registry *appruntime.Registry, vaultPath, token string, stop func(), done chan<- struct{}) {
	defer close(done)
	poll := startIntentFirstPoll
	timer := time.NewTimer(poll)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			intent, found, err := registry.StartIntentFor(ctx, vaultPath)
			if err == nil && (!found || intent.Token != token || intent.Cancelled) {
				stop()
				return
			}
			poll = nextStartIntentPoll(poll)
			timer.Reset(poll)
		}
	}
}
