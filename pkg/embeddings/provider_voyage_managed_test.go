package embeddings

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
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
	var body wire.ErrorBody
	_ = json.NewDecoder(resp.Body).Decode(&body)
	seconds, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
	return nil, &wire.GatewayError{Code: wire.ErrorCode(code), Status: resp.StatusCode, Message: body.Error.Message, RetryAfter: time.Duration(seconds) * time.Second}
}

type gatewayCall struct {
	auth, key string
	inputs    []string
}

type gatewayReply func(n int, call gatewayCall, w http.ResponseWriter) bool

// fakeVoyageGateway records every request and answers with reply, or with
// one-dimensional embeddings when reply returns false.
func fakeVoyageGateway(t *testing.T, reply gatewayReply) (*voyageProvider, func() []gatewayCall, *[]time.Duration) {
	t.Helper()
	var mu sync.Mutex
	var calls []gatewayCall
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, wire.RouteVoyageEmbed, r.URL.Path)
		var payload struct {
			Input []string `json:"input"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		call := gatewayCall{auth: r.Header.Get("Authorization"), key: r.Header.Get(wire.HeaderIdempotencyKey), inputs: payload.Input}
		mu.Lock()
		calls = append(calls, call)
		n := len(calls)
		mu.Unlock()
		if reply != nil && reply(n, call, w) {
			return
		}
		data := make([]map[string]any, len(payload.Input))
		for i, input := range payload.Input {
			data[i] = map[string]any{"embedding": []float32{float32(input[0])}, "index": i}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(server.Close)
	provider, err := NewVoyageProvider(ProviderConfig{Managed: &ManagedAccess{Origin: server.URL, Transport: sessionTransport{}}, BatchSize: 8, MaxConcurrency: 1})
	require.NoError(t, err)
	p := provider.(*voyageProvider)
	var sleeps []time.Duration
	p.sleep = func(_ context.Context, d time.Duration) error { sleeps = append(sleeps, d); return nil }
	p.jitter = func(d time.Duration) time.Duration { return d }
	return p, func() []gatewayCall { mu.Lock(); defer mu.Unlock(); return append([]gatewayCall(nil), calls...) }, &sleeps
}

func gatewayError(w http.ResponseWriter, code wire.ErrorCode, retryAfter string) {
	w.Header().Set(wire.HeaderGatewayError, string(code))
	if retryAfter != "" {
		w.Header().Set("Retry-After", retryAfter)
	}
	w.WriteHeader(code.Status())
	_ = json.NewEncoder(w).Encode(wire.ErrorBody{Error: wire.ErrorDetail{Code: code, Message: "synthetic"}})
}

func keysOf(calls []gatewayCall) []string {
	keys := make([]string, len(calls))
	for i, call := range calls {
		keys[i] = call.key
	}
	return keys
}

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{8,128}$`)

func TestVoyageManagedUsesSessionTransportAndNoProviderKey(t *testing.T) {
	p, calls, _ := fakeVoyageGateway(t, nil)
	vecs, err := p.EmbedTexts(context.Background(), []string{"a", "b"})
	require.NoError(t, err)
	require.Equal(t, []Embedding{{97}, {98}}, vecs)
	got := calls()
	require.Len(t, got, 1)
	require.Equal(t, "Bearer rzt_a_synthetic.session", got[0].auth)
	require.Regexp(t, idempotencyKeyPattern, got[0].key)
	require.Equal(t, 120*time.Second, p.httpClient.Timeout)
}

func TestVoyageManagedFreshKeyAfterUpstream429(t *testing.T) {
	p, calls, _ := fakeVoyageGateway(t, func(n int, _ gatewayCall, w http.ResponseWriter) bool {
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests) // passed through: no gateway header
			return true
		}
		return false
	})
	_, err := p.EmbedTexts(context.Background(), []string{"a", "b"})
	require.NoError(t, err)
	keys := keysOf(calls())
	require.Len(t, keys, 2)
	require.NotEqual(t, keys[0], keys[1], "the gateway settled the first key")
}

func TestVoyageManagedAmbiguousFailuresReuseKeyAndNeverSplit(t *testing.T) {
	t.Run("upstream 502 then outcome unknown", func(t *testing.T) {
		p, calls, _ := fakeVoyageGateway(t, func(n int, _ gatewayCall, w http.ResponseWriter) bool {
			if n == 1 {
				w.WriteHeader(http.StatusBadGateway)
			} else {
				gatewayError(w, wire.CodeOutcomeUnknown, "")
			}
			return true
		})
		_, err := p.EmbedTexts(context.Background(), []string{"a", "b", "c", "d"})
		var gatewayErr *wire.GatewayError
		require.ErrorAs(t, err, &gatewayErr)
		require.Equal(t, wire.CodeOutcomeUnknown, gatewayErr.Code)
		got := calls()
		require.Len(t, got, 2)
		require.Equal(t, got[0].key, got[1].key)
		for _, call := range got {
			require.Len(t, call.inputs, 4, "no split after an ambiguous failure")
		}
	})
	t.Run("connection drop after sending", func(t *testing.T) {
		p, calls, _ := fakeVoyageGateway(t, func(n int, _ gatewayCall, w http.ResponseWriter) bool {
			if n > 1 {
				return false
			}
			conn, _, err := w.(http.Hijacker).Hijack()
			require.NoError(t, err)
			_ = conn.Close()
			return true
		})
		_, err := p.EmbedTexts(context.Background(), []string{"a", "b", "c", "d"})
		require.NoError(t, err)
		got := calls()
		require.Len(t, got, 2)
		require.Equal(t, got[0].key, got[1].key)
		require.Len(t, got[1].inputs, 4)
	})
}

func TestVoyageManagedTerminalGatewayErrorsStop(t *testing.T) {
	for _, code := range []wire.ErrorCode{wire.CodeBudgetExhausted, wire.CodeResultLost, wire.CodeServiceDisabled, wire.CodeSessionRevoked} {
		t.Run(string(code), func(t *testing.T) {
			p, calls, _ := fakeVoyageGateway(t, func(_ int, _ gatewayCall, w http.ResponseWriter) bool {
				gatewayError(w, code, "")
				return true
			})
			_, err := p.EmbedTexts(context.Background(), []string{"a", "b", "c", "d"})
			var gatewayErr *wire.GatewayError
			require.ErrorAs(t, err, &gatewayErr)
			require.Equal(t, code, gatewayErr.Code)
			require.Len(t, calls(), 1, "no retry and no split")
		})
	}
}

func TestVoyageManagedRetryableGatewayErrorHonorsRetryAfter(t *testing.T) {
	for _, tc := range []struct {
		code    wire.ErrorCode
		sameKey bool
	}{{wire.CodeRateLimited, false}, {wire.CodeUpstreamDown, false}, {wire.CodeInProgress, true}} {
		t.Run(string(tc.code), func(t *testing.T) {
			p, calls, sleeps := fakeVoyageGateway(t, func(n int, _ gatewayCall, w http.ResponseWriter) bool {
				if n == 1 {
					gatewayError(w, tc.code, "7")
					return true
				}
				return false
			})
			_, err := p.EmbedTexts(context.Background(), []string{"a"})
			require.NoError(t, err)
			require.Equal(t, []time.Duration{7 * time.Second}, *sleeps)
			keys := keysOf(calls())
			require.Len(t, keys, 2)
			require.Equal(t, tc.sameKey, keys[0] == keys[1])
		})
	}
}

func TestVoyageManagedSplitsOnlyOnSizeRejectionWithNewKeys(t *testing.T) {
	p, calls, _ := fakeVoyageGateway(t, func(_ int, call gatewayCall, w http.ResponseWriter) bool {
		if len(call.inputs) > 2 {
			gatewayError(w, wire.CodeTooLarge, "")
			return true
		}
		return false
	})
	vecs, err := p.EmbedTexts(context.Background(), []string{"a", "b", "c", "d"})
	require.NoError(t, err)
	require.Len(t, vecs, 4)
	got := calls()
	require.Len(t, got, 3)
	seen := map[string]bool{}
	for _, call := range got {
		require.False(t, seen[call.key], "each split child is a new logical request")
		seen[call.key] = true
	}
}

func TestVoyageManagedConstruction(t *testing.T) {
	managed := &ManagedAccess{Origin: "http://127.0.0.1:1", Transport: sessionTransport{}, MaxConcurrency: 4}
	_, err := NewVoyageProvider(ProviderConfig{Managed: managed, Endpoint: "https://proxy.example.test/v1/embeddings"})
	require.ErrorContains(t, err, "team_session_endpoint_conflict")
	_, err = NewVoyageProvider(ProviderConfig{Managed: managed, APIKey: "pa-synthetic"})
	require.ErrorContains(t, err, "mutually exclusive")
	_, err = NewVoyageProvider(ProviderConfig{})
	require.EqualError(t, err, "voyage provider requires api key")

	provider, err := NewVoyageProvider(ProviderConfig{Managed: managed, Endpoint: DefaultVoyageEndpoint})
	require.NoError(t, err)
	p := provider.(*voyageProvider)
	require.Equal(t, "http://127.0.0.1:1"+wire.RouteVoyageEmbed, p.endpoint)
	require.Equal(t, 4, p.maxConc)
	direct, err := NewVoyageProvider(ProviderConfig{APIKey: "pa-synthetic"})
	require.NoError(t, err)
	require.Equal(t, direct.(*voyageProvider).fingerprint, p.fingerprint, "metrics identity ignores access mode")
}

func TestVoyageDirectRequestBytesAndHeadersUnchanged(t *testing.T) {
	provider, err := NewVoyageProvider(ProviderConfig{APIKey: "pa-synthetic", Dimensions: 256})
	require.NoError(t, err)
	p := provider.(*voyageProvider)
	var header http.Header
	var body string
	var url string
	p.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		header, url = req.Header.Clone(), req.URL.String()
		data, _ := io.ReadAll(req.Body)
		body = string(data)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":[{"embedding":[1],"index":0}]}`))}, nil
	})}
	_, err = p.EmbedTexts(context.Background(), []string{"a"})
	require.NoError(t, err)
	require.Equal(t, DefaultVoyageEndpoint, url)
	require.Equal(t, http.Header{"Content-Type": {"application/json"}, "Authorization": {"Bearer pa-synthetic"}}, header)
	require.Equal(t, `{"input":["a"],"input_type":"document","model":"voyage-4-lite","output_dimension":256}`, body)
}
