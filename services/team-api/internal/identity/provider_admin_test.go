package identity_test

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/stretchr/testify/require"
)

func TestAdminProviderBudgetsAndUsage(t *testing.T) {
	e := newEnv(t, options{usage: usageReader{}})
	tok := e.signIn(adminSub, adminEmail).AccessToken
	e.signIn(memberSub, member)
	for _, limits := range []map[string]int64{{"voyage": -1}, {"voyage": 5001}, {"typesafe": 6001}, {"jev": 1}} {
		e.wantCode(e.do("PATCH", wire.RouteAdminConfig, tok, wire.AdminConfigUpdate{ProviderDailyNanodollars: limits}), wire.CodeBadRequest)
	}
	var cfg wire.AdminConfig
	e.decode(e.do("PATCH", wire.RouteAdminConfig, tok, wire.AdminConfigUpdate{ProviderDailyNanodollars: map[string]int64{"voyage": 4000}}), &cfg)
	require.Equal(t, map[string]int64{"voyage": 4000, "typesafe": 2000}, cfg.ProviderDailyNanodollars)
	require.Equal(t, "jev-latest", cfg.PricingAssumptions["typesafe"].Model)
	e.decode(e.do("PATCH", wire.RouteAdminConfig, tok, wire.AdminConfigUpdate{ProviderDailyNanodollars: map[string]int64{"typesafe": 0}}), &cfg)
	require.Equal(t, map[string]int64{"voyage": 4000, "typesafe": 0}, cfg.ProviderDailyNanodollars)
	var usage wire.AdminUsage
	e.decode(e.do("GET", wire.RouteAdminUsage, tok, nil), &usage)
	require.Equal(t, int64(4000), usage.Providers["voyage"].DailyLimitNanodollars)
	require.Equal(t, int64(600), usage.Providers["voyage"].Usage.ReservedNanodollars)
	require.Equal(t, int64(400), usage.Providers["voyage"].Usage.SettledNanodollars)
	for _, person := range usage.People {
		if person.Sub == memberSub {
			require.Equal(t, int64(2), person.Providers["voyage"].Requests)
		}
	}
	var audit []wire.AuditEntry
	e.decode(e.do("GET", wire.RouteAdminAudit, tok, nil), &audit)
	require.Len(t, audit, 2)
	require.Equal(t, "provider_daily_nanodollars.typesafe=0", audit[0].Detail)
	require.Equal(t, "provider_daily_nanodollars.voyage=4000", audit[1].Detail)
	// Read-only assumptions are not accepted on PATCH.
	e.wantCode(e.do("PATCH", wire.RouteAdminConfig, tok, []byte(`{"pricing_assumptions":{}}`)), wire.CodeBadRequest)
}
