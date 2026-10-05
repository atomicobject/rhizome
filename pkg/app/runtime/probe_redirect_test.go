package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProbeDoesNotFollowRedirects(t *testing.T) {
	var requests atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(http.StatusOK) }))
	defer destination.Close()
	listener := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer listener.Close()
	_, err := Probe(context.Background(), InstanceManifest{HTTPURL: listener.URL, PID: 123, RunID: "fixture"})
	require.ErrorIs(t, err, ErrRuntimeUnresponsive)
	require.Zero(t, requests.Load(), "a runtime probe must stay at the manifest listener")
}
