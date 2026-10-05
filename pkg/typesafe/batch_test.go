package typesafe_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/typesafe"
	"github.com/stretchr/testify/require"
)

func TestEvaluateBatchConcurrentOrderedAndRetriesOnlyFailedItem(t *testing.T) {
	var active, peak atomic.Int32
	var mu sync.Mutex
	attempts := map[string]int{}
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		var request struct{ State string }
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		attempts[request.State]++
		count := attempts[request.State]
		mu.Unlock()
		time.Sleep(25 * time.Millisecond)
		if request.State == "retry" && count == 1 {
			w.Header().Set("retry-after-ms", "10")
			w.WriteHeader(429)
			return
		}
		if request.State == "bad" {
			w.WriteHeader(400)
			return
		}
		fmt.Fprint(w, mixedResponse)
	})
	items := make([]typesafe.BatchItem, 4)
	for i, id := range []string{"first", "retry", "bad", "last"} {
		req := mixedRequest()
		req.State = id
		items[i] = typesafe.BatchItem{ID: id, Request: req}
	}
	results, err := client.EvaluateBatch(t.Context(), items, 2)
	require.NoError(t, err)
	require.Equal(t, int32(2), peak.Load())
	for i := range items {
		require.Equal(t, items[i].ID, results[i].ID)
	}
	require.Equal(t, "succeeded", results[0].Status)
	require.Equal(t, 2, results[1].Attempts)
	require.Equal(t, "failed", results[2].Status)
	require.Equal(t, 400, results[2].StatusCode)
	require.Equal(t, "succeeded", results[3].Status)
	require.Equal(t, map[string]int{"first": 1, "retry": 2, "bad": 1, "last": 1}, attempts)
}

func TestEvaluateBatchCancellationPreservesSuccessAndUncertainty(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct{ State string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.State == "first" {
			fmt.Fprint(w, mixedResponse)
			return
		}
		cancel()
		<-r.Context().Done()
	})
	items := make([]typesafe.BatchItem, 3)
	for i, id := range []string{"first", "interrupted", "queued"} {
		req := mixedRequest()
		req.State = id
		items[i] = typesafe.BatchItem{ID: id, Request: req}
	}
	results, err := client.EvaluateBatch(ctx, items, 1)
	require.NoError(t, err)
	require.Equal(t, "succeeded", results[0].Status)
	require.Equal(t, "uncertain", results[1].Status)
	require.Equal(t, 1, results[1].Attempts)
	require.Equal(t, "not_started", results[2].Status)
	require.Zero(t, results[2].Attempts)
}

func TestEvaluateBatchSharedThrottle(t *testing.T) {
	var times []time.Time
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		times = append(times, time.Now())
		if len(times) == 1 {
			w.Header().Set("retry-after-ms", "70")
			w.WriteHeader(429)
			return
		}
		fmt.Fprint(w, mixedResponse)
	}, typesafe.WithMaxRetries(0))
	results, err := client.EvaluateBatch(t.Context(), []typesafe.BatchItem{{ID: "a", Request: mixedRequest()}, {ID: "b", Request: mixedRequest()}}, 1)
	require.NoError(t, err)
	require.Equal(t, "failed", results[0].Status)
	require.Equal(t, "succeeded", results[1].Status)
	require.GreaterOrEqual(t, times[1].Sub(times[0]), 60*time.Millisecond)
}
