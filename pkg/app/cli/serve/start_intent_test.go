package serve

import (
	"context"
	"os"
	"testing"
	"time"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/stretchr/testify/require"
)

func TestStartIntentPollBacksOffToItsCap(t *testing.T) {
	d := startIntentFirstPoll
	for range 10 {
		d = nextStartIntentPoll(d)
	}
	require.Equal(t, startIntentMaxPoll, d)
	require.Less(t, startIntentMaxPoll, appruntime.StopGrace)
}

// After the watcher has backed off, cancelling the intent still stops the
// runtime within one capped poll.
func TestStartIntentWatcherStopsOnCancelAfterBackoff(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv
	env.HomeDir = t.TempDir()
	registry, err := appruntime.NewRegistry(env)
	require.NoError(t, err)
	vault := t.TempDir()
	ctx := context.Background()
	created, err := registry.CreateSpawnIntent(ctx, vault, "tok")
	require.NoError(t, err)
	require.True(t, created)

	stopped := make(chan struct{})
	done := make(chan struct{})
	go watchStartIntent(ctx, registry, vault, "tok", func() { close(stopped) }, done)

	time.Sleep(2 * time.Second) // let the poll reach its cap
	select {
	case <-stopped:
		t.Fatal("watcher stopped before cancellation")
	default:
	}
	_, cancelled, err := registry.CancelIntent(ctx, vault, "tok")
	require.NoError(t, err)
	require.True(t, cancelled)
	select {
	case <-stopped:
	case <-time.After(startIntentMaxPoll + time.Second):
		t.Fatal("watcher did not stop after cancellation")
	}
	<-done
}
