package web

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

func TestRuntimeIndexStreamReportsInterruptionDuringShutdown(t *testing.T) {
	executor := lane.New(lane.Options{LockPath: filepath.Join(t.TempDir(), ".rhizome", "index.lock")})
	t.Cleanup(executor.Close)
	handle, _, err := executor.Submit(t.Context(), lane.Request{
		Kind: lane.KindExplicitIndex,
		Run: func(ctx context.Context, _ *indexlock.Lease, _ lane.Reporter) error {
			<-ctx.Done()
			return ctx.Err()
		},
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	streamStarted := make(chan struct{})
	server := &Server{cfg: Config{RuntimeControl: &RuntimeControl{}}}
	httpServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(streamStarted)
		server.streamLaneEvents(w, r, handle)
	}))
	httpServer.Config.BaseContext = func(net.Listener) context.Context { return ctx }
	httpServer.Start()
	t.Cleanup(httpServer.Close)
	client := &appruntime.Client{Manifest: appruntime.InstanceManifest{HTTPURL: httpServer.URL}, HTTP: httpServer.Client()}
	result := make(chan error, 1)
	go func() {
		result <- appruntime.StreamIndexJob(t.Context(), client, handle.ID(), func(lane.Event) error { return nil })
	}()
	<-streamStarted
	cancel()

	require.ErrorContains(t, <-result, "event stream ended without a result")
	select {
	case <-handle.Done():
		t.Fatal("closing the event stream must not cancel its shared index job")
	default:
	}
}
