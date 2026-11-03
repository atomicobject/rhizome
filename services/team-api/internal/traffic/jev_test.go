package traffic

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/core"
	"github.com/stretchr/testify/require"
)

const jevHold int64 = 2_752_512

func jevBody(model string) string {
	return fmt.Sprintf(`{ "state":"s", "questions":{"q":{"type":"noul"}}, "model":%q }`, model)
}

func TestJevModelsForwardAndSettle(t *testing.T) {
	for _, model := range []string{"jev-latest", "jev-1.13.0", "jev-1.14.0", "jev-999.0.0"} {
		t.Run(model, func(t *testing.T) {
			body := jevBody(model)
			var got string
			h := newHarness(t, opts{typesafe: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				got = string(b)
				_, _ = io.WriteString(w, `{"usage":{"input_tokens":7,"output_tokens":30}}`)
			})})
			r := h.post(wire.RouteTypeSafeEval, "alice", key(1), body)
			require.Equal(t, http.StatusOK, r.status, r.body)
			require.Equal(t, body, got, "forward the requested model without rewriting it")
			require.Equal(t, wire.ProviderUsage{Requests: 1, ReservedNanodollars: 294, SettledNanodollars: 294}, h.providerUsage("typesafe"))
			g, people := h.usage("2026-10-31")
			want := wire.DailyUsage{Requests: 1, InputBytes: int64(len(body)), ReservedCredits: 37, SettledCredits: 37}
			require.Equal(t, want, g, "versioned models must receive a nonzero shared-credit rate")
			require.Equal(t, want, people["alice"])
		})
	}
}

func TestJevModelsRejectMalformedIDsBeforeAdmission(t *testing.T) {
	v, hits := upstream(t, okTypeSafe)
	h := newHarness(t, opts{typesafe: v.Config.Handler})
	for _, model := range []string{"jev-preview", "jev-1.13", "jev-1.13.0-beta", "jev-01.13.0", "jev-1.13.0/other", "gpt-1.13.0", "jev-" + strings.Repeat("1", 60) + ".0.0"} {
		r := h.post(wire.RouteTypeSafeEval, "alice", key(1), jevBody(model))
		require.Equal(t, wire.CodeModelNotAllowed, r.code(), model)
	}
	require.Zero(t, hits.Load())
	g, _ := h.usage("2026-10-31")
	require.Zero(t, g)
	require.Zero(t, h.providerUsage("typesafe"))
}

func TestJevModelsKeepAdmissionLimits(t *testing.T) {
	for _, model := range []string{"jev-latest", "jev-1.13.0"} {
		for _, limit := range []string{"person credits", "global credits", "person bytes", "global requests", "provider currency"} {
			t.Run(model+"/"+limit, func(t *testing.T) {
				v, hits := upstream(t, okTypeSafe)
				h := newHarness(t, opts{typesafe: v.Config.Handler})
				body := jevBody(model)
				switch limit {
				case "person credits":
					limits := roomy
					limits.ChargeCredits = 5631
					h.person.Store(&limits)
				case "global credits":
					h.rt.set(func(rt *core.Runtime) { rt.GlobalLimits.ChargeCredits = 5631 })
				case "person bytes":
					limits := roomy
					limits.InputBytes = int64(len(body)) - 1
					h.person.Store(&limits)
				case "global requests":
					h.rt.set(func(rt *core.Runtime) { rt.GlobalLimits.Requests = 0 })
				case "provider currency":
					h.rt.set(func(rt *core.Runtime) { rt.ProviderDailyNanodollars["typesafe"] = jevHold - 1 })
				}
				r := h.post(wire.RouteTypeSafeEval, "alice", key(1), body)
				require.Equal(t, wire.CodeBudgetExhausted, r.code(), r.body)
				require.Zero(t, hits.Load())
				g, _ := h.usage("2026-10-31")
				require.Zero(t, g)
				require.Zero(t, h.providerUsage("typesafe"))
			})
		}
	}
}

func TestJevModelsKeepReservationsForUnverifiedBilling(t *testing.T) {
	for _, model := range []string{"jev-latest", "jev-1.13.0"} {
		for _, status := range []int{http.StatusOK, http.StatusBadRequest, http.StatusBadGateway} {
			t.Run(fmt.Sprintf("%s/%d", model, status), func(t *testing.T) {
				v, hits := upstream(t, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(status)
					_, _ = io.WriteString(w, `{"detail":"synthetic provider response"}`)
				})
				h := newHarness(t, opts{typesafe: v.Config.Handler})
				body := jevBody(model)
				r := h.post(wire.RouteTypeSafeEval, "alice", key(1), body)
				require.Equal(t, status, r.status, r.body)
				require.Empty(t, r.code(), "upstream rejections remain provider responses")
				require.Equal(t, wire.ProviderUsage{Requests: 1, ReservedNanodollars: jevHold, Unknown: 1}, h.providerUsage("typesafe"))
				g, _ := h.usage("2026-10-31")
				if status == http.StatusBadRequest {
					require.Zero(t, g.ReservedCredits, "definitive rejections keep the existing credit refund policy")
					require.Equal(t, wire.CodeResultLost, h.post(wire.RouteTypeSafeEval, "alice", key(1), body).code())
				} else {
					require.Equal(t, int64(5632), g.ReservedCredits)
					require.Equal(t, int64(1), g.Unknown)
					require.Equal(t, wire.CodeOutcomeUnknown, h.post(wire.RouteTypeSafeEval, "alice", key(1), body).code())
				}
				require.Zero(t, g.SettledCredits)
				require.Equal(t, int32(1), hits.Load(), "a retry never repeats the provider call")
			})
		}
	}
}

func TestJevVersionChargeUsesAliasAndExplicitOverrides(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprint(explicit), func(t *testing.T) {
			h := newHarness(t, opts{typesafe: http.HandlerFunc(okTypeSafe), cfg: func(c *Config) {
				c.TypeSafeModels = map[string]Charge{"jev-latest": {PerRequest: 5000, PerKToken: 2000}}
				if explicit {
					c.TypeSafeModels["jev-1.13.0"] = Charge{PerRequest: 5000, PerKToken: 3000}
				}
			}})
			r := h.post(wire.RouteTypeSafeEval, "alice", key(1), jevBody("jev-1.13.0"))
			require.Equal(t, http.StatusOK, r.status, r.body)
			g, _ := h.usage("2026-10-31")
			want := int64(20)
			if explicit {
				want = 30
			}
			require.Equal(t, want, g.SettledCredits)
		})
	}
}
