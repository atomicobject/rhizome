package traffic

import (
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

func TestForwardsBytesUnchangedAndPassesResponseThrough(t *testing.T) {
	body := "{ \"model\":\"voyage-4-lite\", \"input\":[\"a\\\"b\",\"c\"],\"input_type\":\"document\",\n\"output_dimension\":512,\"truncation\":true,\"output_dtype\":\"float\"}"
	var got atomic.Pointer[http.Request]
	var gotBody atomic.Pointer[string]
	v := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		s := string(data)
		gotBody.Store(&s)
		got.Store(r)
		h := w.Header()
		h.Set("Content-Type", "application/json; charset=utf-8")
		h.Set("Retry-After", "7")
		h.Set("retry-after-ms", "7000")
		h.Set("x-typesafe-request-id", "up-123")
		h.Set("X-Upstream-Secret-Header", "nope")
		h.Set("Set-Cookie", "a=b")
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"data":[],"usage":{"total_tokens":5}}`)
	})
	h := newHarness(t, opts{voyage: v})
	r := h.post(wire.RouteVoyageEmbed, "alice", key(1), body,
		"Authorization", "Bearer rzt_a_client-token", "Cookie", "session=1", "X-Custom", "1", "Connection", "keep-alive, X-Custom",
		"User-Agent", "rzm/1.2.3 (darwin)")

	require.Equal(t, http.StatusAccepted, r.status)
	require.Equal(t, `{"data":[],"usage":{"total_tokens":5}}`, r.body)
	require.Equal(t, "application/json; charset=utf-8", r.header.Get("Content-Type"))
	require.Equal(t, "7", r.header.Get("Retry-After"))
	require.Equal(t, "7000", r.header.Get("retry-after-ms"))
	require.Equal(t, "up-123", r.header.Get("x-typesafe-request-id"))
	require.Empty(t, r.header.Get("X-Upstream-Secret-Header"))
	require.Empty(t, r.header.Get("Set-Cookie"))
	require.Empty(t, r.header.Get(wire.HeaderGatewayError))
	require.NotEmpty(t, r.header.Get(wire.HeaderRequestID))

	up := got.Load()
	require.Equal(t, body, *gotBody.Load())
	require.Equal(t, "/v1/embeddings", up.URL.Path)
	require.Equal(t, "Bearer "+voyageCanary, up.Header.Get("Authorization"))
	require.Equal(t, "application/json", up.Header.Get("Content-Type"))
	require.Equal(t, userAgent, up.Header.Get("User-Agent"))
	for _, name := range []string{"Cookie", "X-Custom", "X-Test-Sub", wire.HeaderIdempotencyKey} {
		require.Empty(t, up.Header.Get(name), name)
	}

	// voyage-4-lite reserved 1,024 + 2*8 credits; 5 actual tokens settle at 5.
	g, people := h.usage("2026-10-31")
	want := wire.DailyUsage{Requests: 1, InputBytes: int64(len(body)), ReservedCredits: 5, SettledCredits: 5}
	require.Equal(t, want, g)
	require.Equal(t, want, people["alice"])
}

func TestTypeSafeRoutes(t *testing.T) {
	var paths []string
	ts := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"models":[]}`)
			return
		}
		okTypeSafe(w, r)
	})
	h := newHarness(t, opts{typesafe: ts})
	r := h.post(wire.RouteTypeSafeEval, "alice", key(1), typesafeBody)
	require.Equal(t, http.StatusOK, r.status, r.body)
	g, _ := h.usage("2026-10-31")
	require.Equal(t, int64(10), g.SettledCredits)

	r = h.do(t.Context(), http.MethodGet, wire.RouteTypeSafeModel+"?x=1", "alice", "", nil)
	require.Equal(t, http.StatusOK, r.status)
	require.Equal(t, `{"models":[]}`, r.body)
	r = h.do(t.Context(), http.MethodGet, wire.RouteTypeSafeModel, "", "", nil)
	require.Equal(t, wire.CodeAccessRefreshRequired, r.code(), "models still needs a principal")
	require.Equal(t, []string{"POST /v1/systemone", "GET /v1/models"}, paths)
}

func TestRedactsKeyInUpstreamErrorBodies(t *testing.T) {
	v := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"detail":"bad key `+voyageCanary+` / `+voyageCanary+`"}`)
	})
	h := newHarness(t, opts{voyage: v})
	r := h.voyage("alice", key(1), voyageBody("x"))
	require.Equal(t, http.StatusUnauthorized, r.status)
	require.Equal(t, `{"detail":"bad key [REDACTED] / [REDACTED]"}`, r.body)
	require.Empty(t, r.header.Get(wire.HeaderGatewayError))

	// A definitive rejection releases the credits and settles the key.
	g, _ := h.usage("2026-10-31")
	require.Equal(t, wire.DailyUsage{Requests: 1, InputBytes: int64(len(voyageBody("x")))}, g)
	require.Equal(t, wire.CodeResultLost, h.voyage("alice", key(1), voyageBody("x")).code())
}

func TestValidationMatrix(t *testing.T) {
	v, hits := upstream(t, okVoyage)
	ts, tsHits := upstream(t, okTypeSafe)
	h := newHarness(t, opts{voyage: v.Config.Handler, typesafe: ts.Config.Handler})
	many := make([]string, wire.MaxVoyageInputs+1)
	for i := range many {
		many[i] = "x"
	}
	cases := []struct {
		name, route, key, body string
		hdr                    []string
		code                   wire.ErrorCode
	}{
		{"1,001 inputs", wire.RouteVoyageEmbed, key(1), voyageBody(many...), nil, wire.CodeBadRequest},
		{"no inputs", wire.RouteVoyageEmbed, key(1), `{"model":"voyage-4-lite","input":[]}`, nil, wire.CodeBadRequest},
		{"non-string input", wire.RouteVoyageEmbed, key(1), `{"model":"voyage-4-lite","input":[1]}`, nil, wire.CodeBadRequest},
		{"oversized body", wire.RouteTypeSafeEval, key(1), strings.Repeat(" ", wire.MaxTypeSafeRequestBytes+1), nil, wire.CodeTooLarge},
		{"disallowed model", wire.RouteVoyageEmbed, key(1), `{"model":"voyage-4-large","input":"x"}`, nil, wire.CodeModelNotAllowed},
		{"disallowed jev", wire.RouteTypeSafeEval, key(1), `{"model":"jev-9","state":"s","questions":{"q":{}}}`, nil, wire.CodeModelNotAllowed},
		{"unknown field", wire.RouteVoyageEmbed, key(1), `{"model":"voyage-4-lite","input":"x","encoding_format":"base64"}`, nil, wire.CodeBadRequest},
		{"unknown typesafe field", wire.RouteTypeSafeEval, key(1), `{"state":"s","questions":{"q":{}},"temperature":1}`, nil, wire.CodeBadRequest},
		{"case-variant field", wire.RouteVoyageEmbed, key(1), `{"Model":"voyage-4-lite","input":"x"}`, nil, wire.CodeBadRequest},
		{"duplicate model", wire.RouteVoyageEmbed, key(1), `{"model":"voyage-4-large","model":"voyage-4-lite","input":"x"}`, nil, wire.CodeBadRequest},
		{"trailing JSON", wire.RouteVoyageEmbed, key(1), voyageBody("x") + `{}`, nil, wire.CodeBadRequest},
		{"not an object", wire.RouteVoyageEmbed, key(1), `["x"]`, nil, wire.CodeBadRequest},
		{"dimension too large", wire.RouteVoyageEmbed, key(1), `{"model":"voyage-4-lite","input":"x","output_dimension":2048}`, nil, wire.CodeBadRequest},
		{"int8 dtype", wire.RouteVoyageEmbed, key(1), `{"model":"voyage-4-lite","input":"x","output_dtype":"int8"}`, nil, wire.CodeBadRequest},
		{"gzip", wire.RouteVoyageEmbed, key(1), voyageBody("x"), []string{"Content-Encoding", "gzip"}, wire.CodeBadRequest},
		{"no idempotency key", wire.RouteVoyageEmbed, "", voyageBody("x"), nil, wire.CodeBadRequest},
		{"short idempotency key", wire.RouteVoyageEmbed, "short", voyageBody("x"), nil, wire.CodeBadRequest},
		{"bad key characters", wire.RouteVoyageEmbed, "key/../../x", voyageBody("x"), nil, wire.CodeBadRequest},
		{"unknown route", "/v1/providers/voyage/v1/rerank", key(1), voyageBody("x"), nil, wire.CodeNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := h.post(c.route, "alice", c.key, c.body, c.hdr...)
			require.Equal(t, c.code, r.code(), r.body)
			require.Equal(t, c.code.Status(), r.status)
			require.Contains(t, r.body, `"code":"`+string(c.code)+`"`)
		})
	}
	r := h.do(t.Context(), http.MethodGet, wire.RouteVoyageEmbed, "alice", key(1), nil)
	require.Equal(t, wire.CodeNotFound, r.code(), "wrong method")
	require.Zero(t, hits.Load()+tsHits.Load(), "nothing invalid reaches an upstream")
	g, _ := h.usage("2026-10-31")
	require.Zero(t, g)
}

func TestDuplicateAfterSuccessIsResultUnavailable(t *testing.T) {
	v, hits := upstream(t, okVoyage)
	h := newHarness(t, opts{voyage: v.Config.Handler})
	require.Equal(t, http.StatusOK, h.voyage("alice", key(1), voyageBody("x")).status)
	r := h.voyage("alice", key(1), voyageBody("x"))
	require.Equal(t, wire.CodeResultLost, r.code())
	require.Equal(t, int32(1), hits.Load())

	r = h.voyage("alice", key(1), voyageBody("y"))
	require.Equal(t, wire.CodeIdempotencyClash, r.code(), "same key, different body")
	r = h.post(wire.RouteTypeSafeEval, "alice", key(1), typesafeBody)
	require.NotEqual(t, http.StatusOK, r.status, "same key, different route")
	require.Equal(t, http.StatusOK, h.voyage("bob", key(1), voyageBody("x")).status, "keys are per person")
}

func TestConcurrentDuplicateIsInProgress(t *testing.T) {
	g := newGated()
	h := newHarness(t, opts{voyage: http.HandlerFunc(g.handler)})
	first := make(chan reply)
	go func() { first <- h.voyage("alice", key(1), voyageBody("x")) }()
	require.Eventually(t, func() bool { return g.inflight.Load() == 1 }, 5e9, 1e6)
	r := h.voyage("alice", key(1), voyageBody("x"))
	require.Equal(t, wire.CodeInProgress, r.code())
	require.Equal(t, "1", r.header.Get("Retry-After"))
	require.Contains(t, r.body, `"retryable":true`)
	close(g.open)
	require.Equal(t, http.StatusOK, (<-first).status)
}

func TestUpstream5xxAndLostResponseAreUnknownOutcomes(t *testing.T) {
	var calls atomic.Int32
	v, hits := upstream(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if calls.Add(1) == 2 {
			conn, _, _ := w.(http.Hijacker).Hijack()
			_ = conn.Close() // the response is lost after dispatch
			return
		}
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"detail":"upstream broke"}`)
	})
	h := newHarness(t, opts{voyage: v.Config.Handler})

	r := h.voyage("alice", key(1), voyageBody("x"))
	require.Equal(t, http.StatusBadGateway, r.status, "the 5xx passes through")
	require.Empty(t, r.code())
	require.Equal(t, wire.CodeOutcomeUnknown, h.voyage("alice", key(1), voyageBody("x")).code())

	r = h.voyage("alice", key(2), voyageBody("x"))
	require.Equal(t, wire.CodeOutcomeUnknown, r.code(), r.body)
	require.Equal(t, wire.CodeOutcomeUnknown, h.voyage("alice", key(2), voyageBody("x")).code())
	require.Equal(t, int32(2), hits.Load(), "no key dispatched twice")

	g, _ := h.usage("2026-10-31")
	require.Equal(t, int64(2), g.Unknown)
	require.Equal(t, 2*(1024+8), int(g.ReservedCredits), "unknown outcomes keep the full reservation")
}

func TestOversized2xxResponseIsUnknownOutcome(t *testing.T) {
	chunk := strings.Repeat("0", 1<<20)
	v, _ := upstream(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":"`)
		for range wire.MaxResponseBytes>>20 + 1 {
			_, _ = io.WriteString(w, chunk)
		}
	})
	h := newHarness(t, opts{voyage: v.Config.Handler})
	r := h.voyage("alice", key(1), voyageBody("x"))
	require.True(t, r.status == -1 || len(r.body) <= wire.MaxResponseBytes, "the caller never sees more than the cap")
	require.Equal(t, wire.CodeOutcomeUnknown, h.voyage("alice", key(1), voyageBody("x")).code())
}

// A body cut off exactly at the response cap ends with a read error, not a
// clean EOF, so it is an unknown outcome rather than a complete response.
func TestResponseCutOffAtTheCapIsUnknownOutcome(t *testing.T) {
	v, _ := upstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(wire.MaxResponseBytes+10))
		_, _ = io.WriteString(w, strings.Repeat("0", wire.MaxResponseBytes))
	})
	h := newHarness(t, opts{voyage: v.Config.Handler})
	h.voyage("alice", key(1), voyageBody("x"))
	require.Equal(t, wire.CodeOutcomeUnknown, h.voyage("alice", key(1), voyageBody("x")).code())
}

func TestConnectionRefusedReleasesReservation(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	h := newHarness(t, opts{cfg: func(c *Config) { c.VoyageBaseURL = "http://" + addr }})

	for range 2 { // the same key may be retried: nothing was dispatched
		r := h.voyage("alice", key(1), voyageBody("x"))
		require.Equal(t, wire.CodeUpstreamDown, r.code(), r.body)
		require.Equal(t, http.StatusServiceUnavailable, r.status)
		require.Contains(t, r.body, `"retryable":true`)
	}
	g, people := h.usage("2026-10-31")
	require.Zero(t, g)
	require.Zero(t, people["alice"])
}

func TestRedirectIsNotFollowed(t *testing.T) {
	target, targetHits := upstream(t, okVoyage)
	v := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/v1/embeddings", http.StatusTemporaryRedirect)
	})
	h := newHarness(t, opts{voyage: v})
	r := h.voyage("alice", key(1), voyageBody("x"))
	require.Equal(t, http.StatusTemporaryRedirect, r.status)
	require.Empty(t, r.header.Get("Location"))
	require.Zero(t, targetHits.Load())
}
