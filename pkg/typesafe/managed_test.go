package typesafe_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/pkg/typesafe"
	"github.com/stretchr/testify/require"
)

// sessionTransport behaves like the team session's transport: it adds the
// bearer token and turns gateway-originated errors into *wire.GatewayError.
type sessionTransport struct{}

func (sessionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer rzt_a_synthetic.session")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	code := resp.Header.Get(wire.HeaderGatewayError)
	if code == "" {
		return resp, nil
	}
	defer resp.Body.Close()
	delay, _ := time.ParseDuration(resp.Header.Get("X-Test-Retry-After"))
	return nil, &wire.GatewayError{Code: wire.ErrorCode(code), Status: resp.StatusCode, Message: "synthetic", RetryAfter: delay}
}

type managedCall struct{ auth, key string }

// managedClient serves the gateway's TypeSafe routes. reply answers attempt n
// and returns false to send the default successful response.
func managedClient(t *testing.T, reply func(n int, w http.ResponseWriter) bool, options ...typesafe.Option) (*typesafe.Client, func() []managedCall) {
	t.Helper()
	var mu sync.Mutex
	var calls []managedCall
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wire.RouteTypeSafeEval && r.URL.Path != wire.RouteTypeSafeModel {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		mu.Lock()
		calls = append(calls, managedCall{r.Header.Get("Authorization"), r.Header.Get(wire.HeaderIdempotencyKey)})
		n := len(calls)
		mu.Unlock()
		if reply != nil && reply(n, w) {
			return
		}
		if r.URL.Path == wire.RouteTypeSafeModel {
			fmt.Fprint(w, `{"models":[]}`)
			return
		}
		fmt.Fprint(w, mixedResponse)
	}))
	t.Cleanup(server.Close)
	options = append([]typesafe.Option{typesafe.WithManaged(typesafe.Managed{Origin: server.URL, Transport: sessionTransport{}})}, options...)
	client, err := typesafe.NewClient("", options...)
	require.NoError(t, err)
	return client, func() []managedCall { mu.Lock(); defer mu.Unlock(); return append([]managedCall(nil), calls...) }
}

func writeGatewayError(w http.ResponseWriter, code wire.ErrorCode, retryAfter string) {
	w.Header().Set(wire.HeaderGatewayError, string(code))
	w.Header().Set("X-Test-Retry-After", retryAfter)
	w.WriteHeader(code.Status())
	_ = json.NewEncoder(w).Encode(wire.ErrorBody{Error: wire.ErrorDetail{Code: code, Message: "synthetic"}})
}

func TestManagedUsesSessionTransportWithoutKey(t *testing.T) {
	client, calls := managedClient(t, nil)
	_, err := client.Evaluate(context.Background(), mixedRequest())
	require.NoError(t, err)
	_, err = client.Models(context.Background())
	require.NoError(t, err)
	got := calls()
	require.Equal(t, "Bearer rzt_a_synthetic.session", got[0].auth)
	require.Regexp(t, `^[A-Za-z0-9._-]{8,128}$`, got[0].key)
	require.Empty(t, got[1].key, "unpaid model listing carries no idempotency key")
}

func TestManagedRetryableGatewayErrors(t *testing.T) {
	for _, tc := range []struct {
		code    wire.ErrorCode
		sameKey bool
	}{{wire.CodeRateLimited, false}, {wire.CodeUpstreamDown, false}, {wire.CodeInProgress, true}} {
		t.Run(string(tc.code), func(t *testing.T) {
			client, calls := managedClient(t, func(n int, w http.ResponseWriter) bool {
				if n == 1 {
					writeGatewayError(w, tc.code, "60ms")
					return true
				}
				return false
			})
			started := time.Now()
			_, err := client.Evaluate(context.Background(), mixedRequest())
			require.NoError(t, err)
			require.GreaterOrEqual(t, time.Since(started), 60*time.Millisecond, "honors RetryAfter")
			got := calls()
			require.Len(t, got, 2)
			require.Equal(t, tc.sameKey, got[0].key == got[1].key)
		})
	}
}

func TestManagedTerminalGatewayErrorsStop(t *testing.T) {
	for _, code := range []wire.ErrorCode{wire.CodeBudgetExhausted, wire.CodeOutcomeUnknown, wire.CodeResultLost, wire.CodeModelNotAllowed} {
		t.Run(string(code), func(t *testing.T) {
			client, calls := managedClient(t, func(_ int, w http.ResponseWriter) bool {
				writeGatewayError(w, code, "")
				return true
			})
			_, err := client.Evaluate(context.Background(), mixedRequest())
			var gatewayErr *wire.GatewayError
			require.ErrorAs(t, err, &gatewayErr)
			require.Equal(t, code, gatewayErr.Code)
			require.Len(t, calls(), 1, "terminal gateway errors are not retried")
		})
	}
}

func TestManagedUpstreamStatusKeyLifetime(t *testing.T) {
	for _, tc := range []struct {
		status  int
		sameKey bool
	}{{429, false}, {502, true}} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			client, calls := managedClient(t, func(n int, w http.ResponseWriter) bool {
				if n == 1 {
					w.Header().Set("retry-after-ms", "1")
					w.WriteHeader(tc.status) // passed through: no gateway header
					return true
				}
				return false
			})
			_, err := client.Evaluate(context.Background(), mixedRequest())
			require.NoError(t, err)
			got := calls()
			require.Len(t, got, 2)
			require.Equal(t, tc.sameKey, got[0].key == got[1].key)
		})
	}
}

func TestManagedBatchClassifiesGatewayRejections(t *testing.T) {
	codes := map[string]wire.ErrorCode{"budget": wire.CodeBudgetExhausted, "unknown": wire.CodeOutcomeUnknown, "lost": wire.CodeResultLost}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct{ State string }
		_ = json.NewDecoder(r.Body).Decode(&request)
		writeGatewayError(w, codes[request.State], "")
	}))
	t.Cleanup(server.Close)
	client, err := typesafe.NewClient("", typesafe.WithManaged(typesafe.Managed{Origin: server.URL, Transport: sessionTransport{}}))
	require.NoError(t, err)
	question := map[string]typesafe.Question{"q": typesafe.Noul{Instructions: "?"}}
	results, err := client.EvaluateBatch(context.Background(), []typesafe.BatchItem{
		{ID: "budget", Request: typesafe.Request{State: "budget", Questions: question}},
		{ID: "unknown", Request: typesafe.Request{State: "unknown", Questions: question}},
		{ID: "lost", Request: typesafe.Request{State: "lost", Questions: question}},
	}, 1)
	require.NoError(t, err)
	require.Equal(t, "failed", results[0].Status, "rejected before admission")
	require.Equal(t, 429, results[0].StatusCode)
	require.Equal(t, "uncertain", results[1].Status)
	require.Equal(t, "uncertain", results[2].Status)
}

func TestManagedAndKeyModesAreExclusive(t *testing.T) {
	managed := typesafe.WithManaged(typesafe.Managed{Origin: "http://127.0.0.1:1", Transport: sessionTransport{}})
	for _, options := range [][]typesafe.Option{
		{managed, typesafe.WithBaseURL("https://other.example.test")},
		{managed, typesafe.WithHTTPClient(&http.Client{})},
	} {
		_, err := typesafe.NewClient("", options...)
		require.ErrorContains(t, err, "managed access excludes")
	}
	_, err := typesafe.NewClient("synthetic-key", managed)
	require.ErrorContains(t, err, "managed access excludes")
	_, err = typesafe.NewClient("")
	require.ErrorContains(t, err, "a valid API key is required")
}

func TestDirectRequestHeadersUnchanged(t *testing.T) {
	var header http.Header
	transport := roundTrip(func(r *http.Request) (*http.Response, error) {
		header = r.Header.Clone()
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(mixedResponse))}, nil
	})
	client, err := typesafe.NewClient("synthetic-key", typesafe.WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	_, err = client.Evaluate(context.Background(), mixedRequest())
	require.NoError(t, err)
	require.Equal(t, http.Header{"Authorization": {"Bearer synthetic-key"}, "Accept": {"application/json"}, "Content-Type": {"application/json"}}, header)
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
