package session

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/stretchr/testify/require"
)

func TestRetainedTransportFollowsSharedAccountChanges(t *testing.T) {
	for _, change := range []string{"sign-in", "sign-out"} {
		t.Run(change, func(t *testing.T) {
			var seen atomic.Value
			srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				seen.Store(r.Header.Get("Authorization"))
			}))
			t.Cleanup(srv.Close)
			store := &MemoryStore{}
			now := time.Now()
			old := newFakeGateway(t).record(now.Add(10 * time.Minute))
			old.Origin = srv.URL
			saveRecord(t, store, old)
			a, err := Open(Options{Origin: srv.URL, Store: store, LockPath: filepath.Join(t.TempDir(), "lock"), Now: func() time.Time { return now }})
			require.NoError(t, err)
			client := a.HTTPClient()
			resp, err := client.Get(srv.URL + wire.RouteSession)
			require.NoError(t, err)
			resp.Body.Close()
			require.Equal(t, "Bearer "+old.AccessToken, seen.Load())

			newer := old
			newer.Generation++
			newer.SessionID, newer.AccessToken = "s-new", wire.AccessTokenPrefix+"s-new.synthetic"
			if change == "sign-in" {
				saveRecord(t, store, newer)
			} else {
				require.NoError(t, store.Delete(srv.URL))
			}
			now = now.Add(31 * time.Second)
			seen.Store("")
			resp, err = client.Get(srv.URL + wire.RouteSession)
			if change == "sign-out" {
				require.ErrorIs(t, err, wire.ErrSignInRequired)
				require.Empty(t, seen.Load(), "a signed-out request must not reach the gateway")
				return
			}
			require.NoError(t, err)
			resp.Body.Close()
			require.Equal(t, "Bearer "+newer.AccessToken, seen.Load())
		})
	}
}
