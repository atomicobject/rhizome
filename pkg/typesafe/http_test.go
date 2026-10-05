package typesafe_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/typesafe"
	"github.com/stretchr/testify/require"
)

func TestRetryPolicy(t *testing.T) {
	for _, status := range []int{408, 429, 500, 529, 401, 422} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var attempts int
			var bodies []string
			client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				attempts++
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				bodies = append(bodies, string(body))
				if attempts == 1 {
					w.Header().Set("retry-after-ms", "1")
					w.Header().Set("x-typesafe-request-id", "failed-request")
					w.WriteHeader(status)
					fmt.Fprint(w, `{"message":"temporary"}`)
					return
				}
				fmt.Fprint(w, mixedResponse)
			})
			_, err := client.Evaluate(context.Background(), mixedRequest())
			if status == 401 || status == 422 {
				var apiErr *typesafe.APIError
				require.ErrorAs(t, err, &apiErr)
				require.Equal(t, status, apiErr.StatusCode)
				require.Equal(t, "failed-request", apiErr.RequestID)
				require.False(t, apiErr.Retryable())
				require.Equal(t, 1, attempts)
			} else {
				require.NoError(t, err)
				require.Equal(t, 2, attempts)
				require.Equal(t, bodies[0], bodies[1], "retries must replay the same snapshot")
			}
		})
	}
}

func TestRetriesExhaustedOrDisabled(t *testing.T) {
	for _, retries := range []int{0, 2} {
		t.Run(fmt.Sprint(retries), func(t *testing.T) {
			attempts := 0
			client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				attempts++
				w.Header().Set("retry-after-ms", "1")
				w.WriteHeader(529)
			}, typesafe.WithMaxRetries(retries))
			_, err := client.Models(context.Background())
			var apiErr *typesafe.APIError
			require.ErrorAs(t, err, &apiErr)
			require.Equal(t, retries+1, attempts)
		})
	}
}

func TestRetryWithoutServerDelayUsesBackoff(t *testing.T) {
	var calls []time.Time
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, time.Now())
		if len(calls) == 1 {
			w.WriteHeader(503)
			return
		}
		fmt.Fprint(w, `{"models":[]}`)
	})
	_, err := client.Models(context.Background())
	require.NoError(t, err)
	require.Len(t, calls, 2)
	require.GreaterOrEqual(t, calls[1].Sub(calls[0]), 350*time.Millisecond)
}

func TestCancellationDuringBackoff(t *testing.T) {
	seen := make(chan struct{})
	var attempts atomic.Int32
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			close(seen)
		}
		w.Header().Set("Retry-After", "10")
		w.WriteHeader(429)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := client.Models(ctx); done <- err }()
	<-seen
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("cancellation did not interrupt the call")
	}
	require.Equal(t, int32(1), attempts.Load())
}

func TestServerRetryDelayBeyondBudgetReturnsWithoutRetry(t *testing.T) {
	for _, delay := range []string{"120", time.Now().Add(2 * time.Minute).UTC().Format(http.TimeFormat)} {
		t.Run(delay, func(t *testing.T) {
			attempts := 0
			client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				attempts++
				w.Header().Set("Retry-After", delay)
				w.WriteHeader(429)
			}, typesafe.WithTimeout(time.Second))
			_, err := client.Models(context.Background())
			var apiErr *typesafe.APIError
			require.ErrorAs(t, err, &apiErr)
			require.Greater(t, apiErr.RetryAfter, time.Minute)
			require.Equal(t, 1, attempts)
		})
	}
}

func TestRequestDeadline(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}, typesafe.WithTimeout(30*time.Millisecond))
	_, err := client.Models(context.Background())
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestErrorsAndLimits(t *testing.T) {
	t.Run("error does not log key or response body", func(t *testing.T) {
		client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(422)
			fmt.Fprint(w, `{"message":"test-key private state"}`)
		})
		_, err := client.Models(context.Background())
		require.NotContains(t, err.Error(), "test-key")
		require.NotContains(t, err.Error(), "private state")
		var apiErr *typesafe.APIError
		require.ErrorAs(t, err, &apiErr)
		require.NotContains(t, apiErr.Body, "test-key")
		require.Contains(t, apiErr.Body, "[REDACTED]")
	})
	for _, body := range []string{`not json`, `null`, `{}`, `{"models":null}`, `{"models":[{}]}`} {
		t.Run(body, func(t *testing.T) {
			client := newClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
			_, err := client.Models(context.Background())
			var responseErr *typesafe.ResponseError
			require.ErrorAs(t, err, &responseErr)
		})
	}
	t.Run("bounded response", func(t *testing.T) {
		client := newClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, strings.Repeat("x", 100)) }, typesafe.WithMaxResponseBytes(20))
		_, err := client.Models(context.Background())
		var responseErr *typesafe.ResponseError
		require.ErrorAs(t, err, &responseErr)
	})
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTransportFailuresAreNotReplayed(t *testing.T) {
	attempts := 0
	failure := errors.New("connection lost")
	client, err := typesafe.NewClient("test-key", typesafe.WithHTTPClient(&http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		attempts++
		return nil, failure
	})}))
	require.NoError(t, err)
	_, err = client.Check(context.Background(), "text", typesafe.Noul{Instructions: "True?"})
	require.ErrorIs(t, err, failure)
	require.Equal(t, 1, attempts)
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	_, err := client.Models(context.Background())
	var apiErr *typesafe.APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusTemporaryRedirect, apiErr.StatusCode)
	require.Zero(t, targetCalls.Load())
}

func TestInvalidClientConfiguration(t *testing.T) {
	_, err := typesafe.NewClient("")
	require.Error(t, err)
	for _, option := range []typesafe.Option{
		typesafe.WithBaseURL("file:///tmp/foo"), typesafe.WithBaseURL("https://user:secret@example.com"),
		typesafe.WithBaseURL("https://example.com?key=secret"), typesafe.WithHTTPClient(nil),
		typesafe.WithTimeout(0), typesafe.WithMaxRetries(-1), typesafe.WithModel(" "), typesafe.WithMaxResponseBytes(0), nil,
	} {
		_, err := typesafe.NewClient("test-key", option)
		require.Error(t, err)
	}
}
