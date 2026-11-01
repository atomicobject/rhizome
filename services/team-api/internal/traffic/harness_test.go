package traffic

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/core"
)

// Synthetic keys; never real credentials.
const (
	voyageCanary   = "SYNTHETIC-VOYAGE-CANARY"
	typesafeCanary = "SYNTHETIC-TYPESAFE-CANARY"
)

var roomy = wire.DailyLimits{Requests: 1_000_000, InputBytes: 1 << 40, ChargeCredits: 1 << 50}

type stubRuntime struct {
	mu sync.Mutex
	rt core.Runtime
}

func (s *stubRuntime) Runtime(context.Context) (core.Runtime, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rt, nil
}

func (s *stubRuntime) set(fn func(*core.Runtime)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.rt)
}

type harness struct {
	t      *testing.T
	tr     *Traffic
	gw     *httptest.Server
	rt     *stubRuntime
	clock  atomic.Int64 // unix nanos
	person atomic.Pointer[wire.DailyLimits]
}

type opts struct {
	voyage, typesafe http.Handler
	cfg              func(*Config)
	rec              *diagnostics.Recorder
}

// upstream is a fake provider that counts requests.
func upstream(t *testing.T, fn http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	hits := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fn(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

func okVoyage(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"object":"list","data":[{"object":"embedding","embedding":[0.1],"index":0}],"model":"voyage-4-lite","usage":{"total_tokens":10}}`)
}

func okTypeSafe(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"model":"jev-latest","answers":{},"usage":{"input_tokens":7,"output_tokens":3}}`)
}

func newHarness(t *testing.T, o opts) *harness {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+filepath.Join(t.TempDir(), "gw.db")+"?_journal_mode=WAL&_busy_timeout=5000")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	h := &harness{t: t, rt: &stubRuntime{rt: core.Runtime{PaidCallsEnabled: true, VoyageEnabled: true, TypeSafeEnabled: true, GlobalLimits: roomy}}}
	h.clock.Store(time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC).UnixNano())
	h.person.Store(&roomy)
	cfg := Config{VoyageKey: voyageCanary, TypeSafeKey: typesafeCanary}
	if o.voyage != nil {
		srv := httptest.NewServer(o.voyage)
		t.Cleanup(srv.Close)
		cfg.VoyageBaseURL = srv.URL
	}
	if o.typesafe != nil {
		srv := httptest.NewServer(o.typesafe)
		t.Cleanup(srv.Close)
		cfg.TypeSafeBaseURL = srv.URL
	}
	if o.cfg != nil {
		o.cfg(&cfg)
	}
	h.tr, err = New(cfg, db, h.rt, o.rec, func() time.Time { return time.Unix(0, h.clock.Load()).UTC() })
	require.NoError(t, err)
	mux := http.NewServeMux()
	h.tr.Routes(mux)
	// Stand-in for the identity lane's middleware.
	h.gw = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sub := r.Header.Get("X-Test-Sub"); sub != "" {
			r = r.WithContext(core.WithPrincipal(r.Context(), core.Principal{
				Sub: sub, Email: sub + "@example.com", SessionID: "sess-" + sub, Limits: *h.person.Load(),
			}))
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(h.gw.Close)
	return h
}

func (h *harness) setClock(at time.Time) { h.clock.Store(at.UnixNano()) }

type reply struct {
	status int
	header http.Header
	body   string
}

func (r reply) code() wire.ErrorCode { return wire.ErrorCode(r.header.Get(wire.HeaderGatewayError)) }

func (h *harness) do(ctx context.Context, method, route, sub, key string, body io.Reader, hdr ...string) reply {
	h.t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, h.gw.URL+route, body)
	require.NoError(h.t, err)
	if sub != "" {
		req.Header.Set("X-Test-Sub", sub)
	}
	if key != "" {
		req.Header.Set(wire.HeaderIdempotencyKey, key)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		return reply{status: -1, body: err.Error()}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return reply{resp.StatusCode, resp.Header, string(data)}
}

func (h *harness) post(route, sub, key, body string, hdr ...string) reply {
	return h.do(context.Background(), http.MethodPost, route, sub, key, strings.NewReader(body), hdr...)
}

func (h *harness) voyage(sub, key, body string) reply {
	return h.post(wire.RouteVoyageEmbed, sub, key, body)
}

func (h *harness) usage(day string) (wire.DailyUsage, map[string]wire.DailyUsage) {
	g, p, err := h.tr.Usage(context.Background(), day)
	require.NoError(h.t, err)
	return g, p
}

func voyageBody(inputs ...string) string {
	b, _ := json.Marshal(map[string]any{"model": "voyage-4-lite", "input": inputs})
	return string(b)
}

const typesafeBody = `{"state":"s","model":"jev-latest","questions":{"q":{"type":"noul","instructions":"x"}}}`

func key(i int) string { return fmt.Sprintf("key-%08d", i) }

// gated is a fake upstream that blocks until release and tracks the most
// requests it held at once, across every server that shares it.
type gated struct {
	open           chan struct{}
	inflight, peak atomic.Int32
}

func newGated() *gated { return &gated{open: make(chan struct{})} }

func (g *gated) handler(w http.ResponseWriter, r *http.Request) {
	n := g.inflight.Add(1)
	for {
		p := g.peak.Load()
		if n <= p || g.peak.CompareAndSwap(p, n) {
			break
		}
	}
	<-g.open
	g.inflight.Add(-1)
	okVoyage(w, r)
}

func collect(n int, fn func(i int) reply) []reply {
	out := make([]reply, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() { defer wg.Done(); out[i] = fn(i) }()
	}
	wg.Wait()
	return out
}

func countStatus(replies []reply) map[int]int {
	m := map[int]int{}
	for _, r := range replies {
		m[r.status]++
	}
	return m
}
