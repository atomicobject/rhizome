package mcp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/typesafe"
	"github.com/stretchr/testify/require"
)

func TestEvaluateBatchCancellationPreservesPartialItems(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if calls.Add(1) == 1 {
			fmt.Fprint(w, `{"model":"fixture","answers":{"ok":{"type":"noul","noul":0.9}},"usage":{"input_tokens":12,"output_tokens":2}}`)
			return
		}
		cancel()
		<-r.Context().Done()
	}))
	defer server.Close()

	handler := evaluateBatchTool(func() (*typesafe.Client, error) {
		return typesafe.NewClient("fixture", typesafe.WithBaseURL(server.URL))
	})
	result, err := handler(ctx, evaluationCall(t, `{"concurrency":1,"budgetMs":30000,"items":[{"id":"a","state":"first","questions":{"ok":{"type":"noul","instructions":"Supported?"}}},{"id":"b","state":"second","questions":{"ok":{"type":"noul","instructions":"Supported?"}}},{"id":"c","state":"third","questions":{"ok":{"type":"noul","instructions":"Supported?"}}}]}`))
	require.NoError(t, err)
	require.False(t, result.IsError)
	var wire struct {
		Items []typesafe.BatchResult `json:"items"`
		Usage typesafe.Usage         `json:"usage"`
	}
	decodeToolResult(t, result, &wire)
	require.Len(t, wire.Items, 3)
	for i, id := range []string{"a", "b", "c"} {
		require.Equal(t, id, wire.Items[i].ID)
	}
	require.Equal(t, "succeeded", wire.Items[0].Status)
	require.Equal(t, "uncertain", wire.Items[1].Status)
	require.Equal(t, 1, wire.Items[1].Attempts)
	require.Equal(t, "not_started", wire.Items[2].Status)
	require.Zero(t, wire.Items[2].Attempts)
	require.Equal(t, typesafe.Usage{InputTokens: 12, OutputTokens: 2}, wire.Usage)
	require.Equal(t, int32(2), calls.Load())
}

func TestEvaluateBatchDeadlineSelection(t *testing.T) {
	for _, tc := range []struct {
		name          string
		budgetMS      int
		effectiveMS   int
		outerDeadline bool
	}{
		{"explicit budget", 10000, 10000, false},
		{"default budget", 0, 20000, false},
		{"transport headroom", 300000, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var outer time.Time
			if tc.outerDeadline {
				outer = time.Now().Add(10 * time.Second)
				var cancelDeadline context.CancelFunc
				ctx, cancelDeadline = context.WithDeadline(ctx, outer)
				defer cancelDeadline()
			}
			var deadline time.Time
			var hasDeadline bool
			handler := evaluateBatchTool(func() (*typesafe.Client, error) {
				return typesafe.NewClient("fixture", typesafe.WithHTTPClient(&http.Client{Transport: evaluationTransport(func(r *http.Request) (*http.Response, error) {
					deadline, hasDeadline = r.Context().Deadline()
					cancel()
					return nil, r.Context().Err()
				})}))
			})
			before := time.Now()
			result, err := handler(ctx, evaluationCall(t, fmt.Sprintf(`{"budgetMs":%d,"items":[{"id":"a","state":"first","questions":{"ok":{"type":"noul"}}}]}`, tc.budgetMS)))
			after := time.Now()
			require.NoError(t, err)
			require.False(t, result.IsError)
			require.True(t, hasDeadline)
			if tc.outerDeadline {
				require.Equal(t, outer.Add(-250*time.Millisecond), deadline)
			} else {
				budget := time.Duration(tc.effectiveMS) * time.Millisecond
				require.False(t, deadline.Before(before.Add(budget)))
				require.False(t, deadline.After(after.Add(budget)))
			}
		})
	}
}

func TestEvaluateBatchRejectsInvalidBudgetBeforeCreatingClient(t *testing.T) {
	handler := evaluateBatchTool(func() (*typesafe.Client, error) {
		t.Fatal("invalid budget must not create a client")
		return nil, nil
	})
	for _, budget := range []int{-1, 300001} {
		t.Run(fmt.Sprint(budget), func(t *testing.T) {
			result, err := handler(t.Context(), evaluationCall(t, fmt.Sprintf(`{"budgetMs":%d}`, budget)))
			require.NoError(t, err)
			require.True(t, result.IsError)
			require.Contains(t, fmt.Sprint(result.Content), "budgetMs must be 1..300000")
		})
	}
}

func TestEvaluateBatchBudgetExpiresWithoutCompletedItems(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var deadline, requestedAt time.Time
	handler := evaluateBatchTool(func() (*typesafe.Client, error) {
		return typesafe.NewClient("fixture", typesafe.WithHTTPClient(&http.Client{Transport: evaluationTransport(func(r *http.Request) (*http.Response, error) {
			requestedAt = time.Now()
			deadline, _ = r.Context().Deadline()
			<-r.Context().Done()
			return nil, r.Context().Err()
		})}))
	})
	before := time.Now()
	result, err := handler(ctx, evaluationCall(t, `{"concurrency":1,"budgetMs":100,"items":[{"id":"a","state":"first","questions":{"ok":{"type":"noul"}}},{"id":"b","state":"second","questions":{"ok":{"type":"noul"}}}]}`))
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.NoError(t, ctx.Err())
	var wire struct {
		Items []typesafe.BatchResult `json:"items"`
		Usage typesafe.Usage         `json:"usage"`
	}
	decodeToolResult(t, result, &wire)
	require.Len(t, wire.Items, 2)
	require.Contains(t, []string{"uncertain", "not_started"}, wire.Items[0].Status)
	if wire.Items[0].Status == "uncertain" {
		require.Equal(t, 1, wire.Items[0].Attempts)
		require.Contains(t, wire.Items[0].Error, "context deadline exceeded")
	} else {
		require.Zero(t, wire.Items[0].Attempts)
	}
	if !requestedAt.IsZero() {
		require.False(t, deadline.Before(before.Add(100*time.Millisecond)))
		require.False(t, deadline.After(requestedAt.Add(100*time.Millisecond)))
	}
	require.Equal(t, "not_started", wire.Items[1].Status)
	require.Zero(t, wire.Items[1].Attempts)
	require.Zero(t, wire.Usage)
}

type evaluationTransport func(*http.Request) (*http.Response, error)

func (f evaluationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
