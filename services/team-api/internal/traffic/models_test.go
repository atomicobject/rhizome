package traffic

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

func (h *harness) models(sub string) reply {
	return h.do(context.Background(), http.MethodGet, wire.RouteTypeSafeModel, sub, "", nil)
}

// Concurrent misses wait for one upstream request, and the result is reused
// until the TTL passes.
func TestConcurrentModelListMissesShareOneUpstreamRequest(t *testing.T) {
	var hits atomic.Int32
	release := make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(release) }) }
	h := newHarness(t, opts{typesafe: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			<-release
		}
		_, _ = io.WriteString(w, `{"models":[]}`)
	})})
	t.Cleanup(open) // registered after the server, so it runs before the server closes
	var wg sync.WaitGroup
	replies := make([]reply, 8)
	for i := range replies {
		wg.Add(1)
		go func() {
			defer wg.Done()
			replies[i] = h.models("person-" + strconv.Itoa(i))
		}()
	}
	require.Eventually(t, func() bool { return hits.Load() >= 1 }, 5*time.Second, time.Millisecond)
	time.Sleep(100 * time.Millisecond) // let the other callers arrive and wait
	open()
	wg.Wait()
	for _, r := range replies {
		require.Equal(t, http.StatusOK, r.status, r.body)
		require.Equal(t, `{"models":[]}`, r.body)
	}
	require.Equal(t, int32(1), hits.Load(), "concurrent misses made more than one upstream request")

	require.Equal(t, http.StatusOK, h.models("alice").status)
	require.Equal(t, int32(1), hits.Load(), "a list inside the TTL was fetched again")
	h.setClock(time.Unix(0, h.clock.Load()).Add(modelListTTL))
	require.Equal(t, http.StatusOK, h.models("alice").status)
	require.Equal(t, int32(2), hits.Load(), "an expired list was not fetched again")
}

// An upstream error reaches the caller, key redacted, and is not cached.
func TestModelListErrorsAreNotCached(t *testing.T) {
	var hits atomic.Int32
	h := newHarness(t, opts{typesafe: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, "bad key "+typesafeCanary)
			return
		}
		_, _ = io.WriteString(w, `{"models":[]}`)
	})})
	r := h.models("alice")
	require.Equal(t, http.StatusInternalServerError, r.status)
	require.Equal(t, "bad key [REDACTED]", r.body)
	r = h.models("alice")
	require.Equal(t, http.StatusOK, r.status)
	require.Equal(t, `{"models":[]}`, r.body)
	require.Equal(t, int32(2), hits.Load())
}

// Each person has a request rate even when the list is cached.
func TestModelListPerPersonRate(t *testing.T) {
	h := newHarness(t, opts{typesafe: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"models":[]}`)
	})})
	for range modelListBurst {
		require.Equal(t, http.StatusOK, h.models("alice").status)
	}
	r := h.models("alice")
	require.Equal(t, wire.CodeRateLimited, r.code())
	require.NotEmpty(t, r.header.Get("Retry-After"))
	require.Equal(t, http.StatusOK, h.models("bob").status, "one person's limit must not affect another")
}
