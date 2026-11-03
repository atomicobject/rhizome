package traffic

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/core"
)

const voyageHold int64 = 20_001_300

func (h *harness) providerUsage(provider string) wire.ProviderUsage {
	h.t.Helper()
	global, people, err := h.tr.ProviderUsage(context.Background(), "2026-10-31")
	require.NoError(h.t, err)
	if len(people) == 1 {
		for _, p := range people {
			require.Equal(h.t, global[provider], p[provider])
		}
	}
	return global[provider]
}

func TestProviderBudgetsAreIndependentAcrossConcurrentPeople(t *testing.T) {
	v, hits := upstream(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, `{"data":[]}`)
	})
	h := newHarness(t, opts{voyage: v.Config.Handler, typesafe: http.HandlerFunc(okTypeSafe), cfg: shortWaits(100, 100)})
	h.rt.set(func(rt *core.Runtime) { rt.ProviderDailyNanodollars["voyage"] = 3 * voyageHold })
	responses := collect(30, func(i int) reply { return h.voyage(key(i), key(i), voyageBody("x")) })
	counts := countStatus(responses)
	require.Equal(t, 3, counts[http.StatusOK], counts)
	require.Equal(t, 27, counts[http.StatusTooManyRequests], counts)
	require.Equal(t, int32(3), hits.Load())
	require.Equal(t, 3*voyageHold, h.providerUsage("voyage").ReservedNanodollars)
	require.Equal(t, http.StatusOK, h.post(wire.RouteTypeSafeEval, "alice", key(31), typesafeBody).status)
	require.Equal(t, int64(294), h.providerUsage("typesafe").SettledNanodollars)
}

func TestProviderMissingBudgetIsDisabledWithoutUpstreamCall(t *testing.T) {
	v, hits := upstream(t, okVoyage)
	h := newHarness(t, opts{voyage: v.Config.Handler})
	h.rt.set(func(rt *core.Runtime) { rt.ProviderDailyNanodollars = nil })
	require.Equal(t, wire.CodeServiceDisabled, h.voyage("alice", key(1), voyageBody("x")).code())
	require.Zero(t, hits.Load())
	require.Zero(t, h.providerUsage("voyage").Requests)
}

func TestJevLatestBodyAndDefaultContextArePreserved(t *testing.T) {
	const original = "{ \"state\":\"s\", \"questions\":{\"q\":{\"type\":\"noul\"}}, \"model\":\"jev-latest\" }"
	v := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		require.Equal(t, original, string(b))
		require.Empty(t, r.Header.Get("TypeSafe-Danger-Zone-Long-Context"))
		_, _ = io.WriteString(w, `{"usage":{"input_tokens":7,"output_tokens":30}}`)
	})
	h := newHarness(t, opts{typesafe: v})
	r := h.post(wire.RouteTypeSafeEval, "alice", key(1), original, "TypeSafe-Danger-Zone-Long-Context", "true")
	require.Equal(t, http.StatusOK, r.status, r.body)
	u := h.providerUsage("typesafe")
	require.Equal(t, int64(294), u.ReservedNanodollars)
	require.Equal(t, int64(294), u.SettledNanodollars)
	shared, _ := h.usage("2026-10-31")
	require.Equal(t, int64(37), shared.SettledCredits, "shared credits retain the existing input+output policy")
}

func TestBillingUsageUnknownKeepsFullReservation(t *testing.T) {
	for _, body := range []string{
		`{"data":[]}`, `{"usage":{"total_tokens":null}}`,
		`{"usage":{"total_tokens":-1}}`, `{"usage":{"total_tokens":1.5}}`,
		`{"usage":{"total_tokens":7,"total_tokens":8}}`, `{"usage":{"total_tokens":"7"}}`,
		`{"usage":{"total_tokens":0},"usage":null}`, `{"usage":null,"usage":{"total_tokens":0}}`,
		`{"usage":{"total_tokens":0},"usage":7}`, `{"usage":7,"usage":{"total_tokens":0}}`,
		`{"usage":{"total_tokens":0},"usage":[]}`, `{"usage":[],"usage":{"total_tokens":0}}`,
		`{"usage":{"total_tokens":0}}{}`, `{}{"usage":{"total_tokens":0}}`,
		`{"usage":{"total_tokens":0}} garbage`, `{"usage":{"total_tokens":0},"data":invalid}`,
		`{"usage":{"total_tokens":0},"data":[1,]}`, `{"usage":{"total_tokens":0},"data":"\x"}`,
	} {
		t.Run(body, func(t *testing.T) {
			h := newHarness(t, opts{voyage: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, body)
			})})
			require.Equal(t, http.StatusOK, h.voyage("alice", key(1), voyageBody("x")).status)
			u := h.providerUsage("voyage")
			require.Equal(t, voyageHold, u.ReservedNanodollars)
			require.Zero(t, u.SettledNanodollars)
		})
	}
}

func TestReportedUsageAboveBoundFencesFurtherProviderCalls(t *testing.T) {
	root := t.TempDir()
	rec, err := diagnostics.Open(root, diagnostics.Options{Role: "team-api", Stderr: io.Discard})
	require.NoError(t, err)
	v, hits := upstream(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"usage":{"total_tokens":2000000}}`)
	})
	h := newHarness(t, opts{voyage: v.Config.Handler, rec: rec})
	require.Equal(t, http.StatusOK, h.voyage("alice", key(1), voyageBody("x")).status)
	u := h.providerUsage("voyage")
	require.Equal(t, voyageHold, u.ReservedNanodollars)
	require.Zero(t, u.SettledNanodollars)
	require.Equal(t, wire.CodeBudgetExhausted, h.voyage("bob", key(2), voyageBody("x")).code())
	require.Equal(t, int32(1), hits.Load())
	require.NoError(t, rec.Close())
	events, err := diagnostics.ReadEvents(root, diagnostics.Filter{})
	require.NoError(t, err)
	var found bool
	for _, event := range events.Events {
		if event.Name == "teamapi.price_bound_exceeded" {
			found = true
			require.Equal(t, "voyage", event.Attributes["provider"])
			require.Equal(t, "2026-11-03-v1", event.Attributes["price_version"])
		}
	}
	require.True(t, found, "the pricing assumption failure needs a durable error event")
}

func TestProviderBudgetRecheckedAfterSlotWait(t *testing.T) {
	g := newGated()
	h := newHarness(t, opts{voyage: http.HandlerFunc(g.handler), cfg: shortWaits(1, 1)})
	done := make(chan reply, 2)
	go func() { done <- h.voyage("alice", key(1), voyageBody("x")) }()
	require.Eventually(t, func() bool { return g.inflight.Load() == 1 }, 5*time.Second, time.Millisecond)
	go func() { done <- h.voyage("alice", key(2), voyageBody("x")) }()
	h.rt.set(func(rt *core.Runtime) { rt.ProviderDailyNanodollars["voyage"] = 0 })
	close(g.open)
	results := []reply{<-done, <-done}
	counts := countStatus(results)
	require.Equal(t, 1, counts[http.StatusOK])
	require.Equal(t, 1, counts[http.StatusServiceUnavailable])
	require.Equal(t, int32(1), g.peak.Load())
}

func TestBillingUsageParsesOnlyExactNonnegativeIntegers(t *testing.T) {
	for _, usage := range []string{`{"input_tokens":1} garbage`, `{"input_tokens":1e0}`, `{"input_tokens":9223372036854775808}`, `[]`} {
		_, ok := billingTokens([]byte(usage), "input_tokens")
		require.False(t, ok, usage)
	}
	n, ok := billingTokens([]byte(`{"input_tokens":0,"output_tokens":3}`), "input_tokens")
	require.True(t, ok)
	require.Zero(t, n)
	_, ok = billingTokens([]byte(strings.Repeat(" ", 3)), "input_tokens")
	require.False(t, ok)
}
