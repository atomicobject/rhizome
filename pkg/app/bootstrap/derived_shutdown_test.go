package bootstrap

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/embeddings"
)

// heldShutdownProvider separates observing cancellation from physical exit.
// This makes the shutdown join and resource ordering observable without sleeps.
type heldShutdownProvider struct {
	embeddings.Provider
	canceled    chan struct{}
	allowReturn <-chan struct{}
	once        sync.Once
	returned    atomic.Bool
}

func (p *heldShutdownProvider) EmbedTexts(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
	vectors, err := p.Provider.EmbedTexts(ctx, texts)
	if ctx.Err() != nil {
		p.once.Do(func() { close(p.canceled) })
		<-p.allowReturn
		p.returned.Store(true)
	}
	return vectors, err
}

func shutdownRelease(t *testing.T, ch chan struct{}) func() {
	t.Helper()
	var once sync.Once
	release := func() { once.Do(func() { close(ch) }) }
	t.Cleanup(release)
	return release
}

func shutdownDeadline(t *testing.T) <-chan time.Time {
	t.Helper()
	deadline, ok := t.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}
	timer := time.NewTimer(time.Until(deadline))
	t.Cleanup(func() { timer.Stop() })
	return timer.C
}

func waitForShutdown(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-signal:
	case <-t.Context().Done():
		t.Fatal(message)
	case <-shutdownDeadline(t):
		t.Fatal(message)
	}
}
