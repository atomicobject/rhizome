package traffic

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/core"
)

func shortWaits(person, global int) func(*Config) {
	return func(c *Config) { c.PersonSlots, c.GlobalSlots, c.SlotWait = person, global, 300*time.Millisecond }
}

// flood sends n blocked requests, expects the extra ones to be rate limited
// after the slot wait, then opens the upstream.
func flood(t *testing.T, g *gated, n, admitted int, send func(i int) reply) {
	t.Helper()
	done := make(chan reply, n)
	for i := range n {
		go func() { done <- send(i) }()
	}
	for range n - admitted {
		r := <-done
		require.Equal(t, wire.CodeRateLimited, r.code(), r.body)
		require.Equal(t, "1", r.header.Get("Retry-After"))
	}
	close(g.open)
	for range admitted {
		require.Equal(t, http.StatusOK, (<-done).status)
	}
	require.Equal(t, int32(admitted), g.peak.Load())
}

func TestPersonSlotsHoldUnderFlood(t *testing.T) {
	g := newGated()
	h := newHarness(t, opts{voyage: http.HandlerFunc(g.handler), cfg: shortWaits(2, 8)})
	flood(t, g, 6, 2, func(i int) reply { return h.voyage("alice", key(i), voyageBody("x")) })
}

func TestGlobalSlotsHoldAcrossPeople(t *testing.T) {
	g := newGated()
	h := newHarness(t, opts{voyage: http.HandlerFunc(g.handler), cfg: shortWaits(4, 3)})
	people := []string{"a", "b", "c", "d", "e", "f"}
	flood(t, g, 6, 3, func(i int) reply { return h.voyage(people[i], key(i), voyageBody("x")) })
}

func TestPersonSlotsCombineVoyageAndTypeSafe(t *testing.T) {
	g := newGated()
	h := newHarness(t, opts{voyage: http.HandlerFunc(g.handler), typesafe: http.HandlerFunc(g.handler), cfg: shortWaits(4, 8)})
	flood(t, g, 8, 4, func(i int) reply {
		if i%2 == 0 {
			return h.voyage("alice", key(i), voyageBody("x"))
		}
		return h.post(wire.RouteTypeSafeEval, "alice", key(i), typesafeBody)
	})
}

func TestIngressGateBoundsBodiesBeingRead(t *testing.T) {
	v, hits := upstream(t, okVoyage)
	h := newHarness(t, opts{voyage: v.Config.Handler, cfg: func(c *Config) { c.BodyReaders, c.SlotWait = 2, 300*time.Millisecond }})
	const uploads = 6
	writers := make([]*io.PipeWriter, uploads)
	done := make(chan reply, uploads)
	for i := range uploads {
		pr, pw := io.Pipe()
		writers[i] = pw
		go func() {
			done <- h.do(context.Background(), http.MethodPost, wire.RouteVoyageEmbed, "alice", key(i), pr)
		}()
		go func() { _, _ = pw.Write([]byte(`{"model":"voyage-4-lite",`)) }() // a large upload, still arriving
	}
	// Two bodies are being read; the other four waited out the gate. (The
	// HTTP server sends their replies once it has drained their bodies.)
	time.Sleep(600 * time.Millisecond)
	require.Equal(t, 2, len(h.tr.ingress.ch))
	require.Equal(t, 2, cap(h.tr.ingress.ch))
	for _, pw := range writers {
		go func() { _, _ = pw.Write([]byte(`"input":"x"}`)); _ = pw.Close() }()
	}
	replies := make([]reply, 0, uploads)
	for range uploads {
		replies = append(replies, <-done)
	}
	limited := 0
	for _, r := range replies {
		if r.code() == wire.CodeRateLimited {
			limited++
		} else {
			require.Equal(t, http.StatusOK, r.status, r.body)
		}
	}
	require.Equal(t, uploads-2, limited)
	require.Equal(t, int32(2), hits.Load())
}

func TestHundredConcurrentRequestsNeverOverAdmit(t *testing.T) {
	v, hits := upstream(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, `{"data":[]}`) // no usage: the full reservation stays
	})
	h := newHarness(t, opts{voyage: v.Config.Handler, cfg: func(c *Config) { c.PersonSlots, c.GlobalSlots = 100, 100 }})
	charge := int64(1024 + 8)
	h.rt.set(func(rt *core.Runtime) { rt.GlobalLimits.ChargeCredits = 10 * charge })
	people := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	replies := collect(100, func(i int) reply { return h.voyage(people[i%10], key(i), voyageBody("x")) })
	counts := countStatus(replies)
	require.Equal(t, 10, counts[http.StatusOK], counts)
	require.Equal(t, 90, counts[http.StatusTooManyRequests], counts)
	require.Equal(t, int32(10), hits.Load())
	g, ps := h.usage("2026-10-31")
	require.Equal(t, 10*charge, g.ReservedCredits)
	var sum int64
	for _, u := range ps {
		sum += u.Requests
	}
	require.Equal(t, int64(10), sum)
}

func TestPersonBudgetAndDayRollover(t *testing.T) {
	v, _ := upstream(t, okVoyage)
	h := newHarness(t, opts{voyage: v.Config.Handler})
	h.person.Store(&wire.DailyLimits{Requests: 1, InputBytes: 1 << 20, ChargeCredits: 1 << 20})
	h.setClock(time.Date(2026, 10, 31, 23, 59, 59, 500e6, time.UTC))
	require.Equal(t, http.StatusOK, h.voyage("alice", key(1), voyageBody("x")).status)
	r := h.voyage("alice", key(2), voyageBody("x"))
	require.Equal(t, wire.CodeBudgetExhausted, r.code())
	require.Equal(t, "1", r.header.Get("Retry-After"), "until the next UTC midnight")
	require.Contains(t, r.body, `"retryable":false`)

	h.setClock(time.Date(2026, 11, 1, 0, 0, 1, 0, time.UTC))
	require.Equal(t, http.StatusOK, h.voyage("alice", key(2), voyageBody("x")).status)
	d1, _ := h.usage("2026-10-31")
	d2, _ := h.usage("2026-11-01")
	require.Equal(t, int64(1), d1.Requests)
	require.Equal(t, int64(1), d2.Requests)
}

func TestKillSwitchAndProviderSwitches(t *testing.T) {
	v, vHits := upstream(t, okVoyage)
	ts, _ := upstream(t, okTypeSafe)
	h := newHarness(t, opts{voyage: v.Config.Handler, typesafe: ts.Config.Handler})
	h.rt.set(func(rt *core.Runtime) { rt.PaidCallsEnabled = false })
	require.Equal(t, wire.CodeServiceDisabled, h.voyage("alice", key(1), voyageBody("x")).code())
	require.Equal(t, wire.CodeServiceDisabled, h.post(wire.RouteTypeSafeEval, "alice", key(2), typesafeBody).code())

	h.rt.set(func(rt *core.Runtime) { rt.PaidCallsEnabled, rt.VoyageEnabled = true, false })
	r := h.voyage("alice", key(1), voyageBody("x"))
	require.Equal(t, wire.CodeServiceDisabled, r.code())
	require.Equal(t, http.StatusServiceUnavailable, r.status)
	require.Equal(t, http.StatusOK, h.post(wire.RouteTypeSafeEval, "alice", key(2), typesafeBody).status)
	require.Zero(t, vHits.Load())
}

func TestQueuedRequestDoesNotDispatchAfterKillSwitch(t *testing.T) {
	g := newGated()
	h := newHarness(t, opts{voyage: http.HandlerFunc(g.handler), cfg: func(c *Config) { c.PersonSlots, c.SlotWait = 1, 5*time.Second }})
	first := make(chan reply)
	go func() { first <- h.voyage("alice", key(1), voyageBody("x")) }()
	require.Eventually(t, func() bool { return g.inflight.Load() == 1 }, 5*time.Second, time.Millisecond)
	queued := make(chan reply)
	go func() { queued <- h.voyage("alice", key(2), voyageBody("x")) }()
	time.Sleep(100 * time.Millisecond) // let it reach the slot wait
	h.rt.set(func(rt *core.Runtime) { rt.PaidCallsEnabled = false })
	close(g.open)
	require.Equal(t, http.StatusOK, (<-first).status)
	require.Equal(t, wire.CodeServiceDisabled, (<-queued).code())
	require.Equal(t, int32(1), g.peak.Load())
}

func TestCancellationReachesUpstream(t *testing.T) {
	got, gone := make(chan struct{}), make(chan time.Time, 1)
	h := newHarness(t, opts{voyage: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(got)
		<-r.Context().Done()
		gone <- time.Now()
	})})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.do(ctx, http.MethodPost, wire.RouteVoyageEmbed, "alice", key(1), strings.NewReader(voyageBody("x")))
	}()
	<-got
	at := time.Now()
	cancel()
	select {
	case when := <-gone:
		require.Less(t, when.Sub(at), time.Second)
	case <-time.After(time.Second):
		t.Fatal("upstream request was not cancelled within 1s")
	}
	<-done
	require.Eventually(t, func() bool { g, _ := h.usage("2026-10-31"); return g.Unknown == 1 }, 5*time.Second, 10*time.Millisecond)
}

func TestEnvironmentProxyIsIgnored(t *testing.T) {
	proxy, proxyHits := upstream(t, okVoyage)
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("HTTP_PROXY", proxy.URL)
	v, hits := upstream(t, okVoyage)
	h := newHarness(t, opts{voyage: v.Config.Handler})
	require.Nil(t, h.tr.transport.(*http.Transport).Proxy)
	require.Equal(t, http.StatusOK, h.voyage("alice", key(1), voyageBody("x")).status)
	require.Equal(t, int32(1), hits.Load())
	require.Zero(t, proxyHits.Load())
}

func TestDiagnosticsCarryNoBodiesKeysOrEmails(t *testing.T) {
	const canary = "CANARY-BODY-TEXT-5512"
	root := t.TempDir()
	var stderr bytes.Buffer
	rec, err := diagnostics.Open(root, diagnostics.Options{Role: "team-api", Stderr: &stderr})
	require.NoError(t, err)
	v := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(append(data, []byte(" key="+voyageCanary)...)) // echoes the body and key
	})
	h := newHarness(t, opts{voyage: v, rec: rec})
	r := h.post(wire.RouteVoyageEmbed, "alice", key(1), voyageBody(canary), "User-Agent", "rzm/1.4.0 (darwin)")
	require.Equal(t, http.StatusBadRequest, r.status)
	require.NotContains(t, r.body, voyageCanary)
	require.Equal(t, wire.CodeModelNotAllowed, h.voyage("alice", key(2), `{"model":"`+canary+`","input":"x"}`).code())
	require.NoError(t, rec.Close())

	events, err := diagnostics.ReadEvents(root, diagnostics.Filter{})
	require.NoError(t, err)
	require.Len(t, events.Events, 2)
	ev := events.Events[0]
	require.Equal(t, "teamapi.provider_request", ev.Name)
	require.Equal(t, "alice", ev.Attributes["sub"])
	require.Equal(t, "sess-alice", ev.Attributes["session_id"])
	require.Equal(t, wire.RouteVoyageEmbed, ev.Attributes["route"])
	require.Equal(t, "voyage-4-lite", ev.Attributes["model"])
	require.Equal(t, "1.4.0", ev.Attributes["client_version"])
	require.EqualValues(t, 400, ev.Attributes["upstream_status"])
	require.Equal(t, r.header.Get(wire.HeaderRequestID), ev.Attributes["request_id"])
	for _, phase := range []string{"validation", "admission_wait", "reservation", "upstream", "settlement", "total"} {
		require.Contains(t, string(ev.Metrics), `"`+phase+`"`)
	}
	require.Equal(t, string(wire.CodeModelNotAllowed), events.Events[1].Attributes["gateway_code"])

	var all []byte
	require.NoError(t, filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			data, _ := os.ReadFile(path)
			all = append(all, data...)
		}
		return err
	}))
	all = append(all, stderr.Bytes()...)
	for _, secret := range []string{canary, voyageCanary, typesafeCanary, "@example.com"} {
		require.NotContains(t, string(all), secret)
	}
}

func TestUsageScanner(t *testing.T) {
	cases := map[string]string{
		`{"data":[{"usage":{"total_tokens":1}}],"usage":{"total_tokens":42}}`: `{"total_tokens":42}`,
		`{"a":"usage","b":{"usage":{}}, "usage" : {"x":"}"}}`:                 `{"x":"}"}`,
		`{"usage":{"total_tokens":1},"usage":{"total_tokens":2}}`:             "",
		`{"usage":null}`:                         "",
		`{"us\` + `u0061ge":{"total_tokens":1}}`: "", // an escaped key
		`{"usage":{"total_tokens":1}`:            "",
	}
	for in, want := range cases {
		var s usageScanner
		for i := range len(in) { // byte-at-a-time, as a stream might arrive
			_, _ = s.Write([]byte{in[i]})
		}
		require.Equal(t, want, string(s.result()), in)
	}
}
