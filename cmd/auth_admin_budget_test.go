package cmd

import (
	"encoding/json"
	"testing"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/stretchr/testify/require"
)

func TestParseDailyUSD(t *testing.T) {
	for raw, want := range map[string]int64{"0": 0, "50": 50_000_000_000, "0.000000001": 1, "50.123456789": 50_123_456_789, "00050.0": 50_000_000_000, "9223372036.854775807": 9223372036854775807} {
		got, err := parseDailyUSD(raw)
		require.NoError(t, err, raw)
		require.Equal(t, want, got, raw)
	}
	for _, raw := range []string{"", "-1", "+1", ".5", "1.", "1e2", "1.0000000001", "NaN", " 50", "9223372036.854775808"} {
		_, err := parseDailyUSD(raw)
		require.Error(t, err, raw)
	}
}

func TestAdminProviderBudgetRoundTrip(t *testing.T) {
	srv, patches := fakeAuthGateway(t)
	deps := testAuthDeps(t)
	_, err := runAuth(t, deps, "login", "--server", srv.URL, "--json")
	require.NoError(t, err)
	_, err = runAuth(t, deps, "admin", "config", "--voyage-daily-usd", "50.000000001", "--json")
	require.NoError(t, err)
	require.Len(t, *patches, 1)
	var got wire.AdminConfigUpdate
	require.NoError(t, json.Unmarshal([]byte((*patches)[0]), &got))
	require.Equal(t, map[string]int64{"voyage": 50_000_000_001}, got.ProviderDailyNanodollars)
	_, err = runAuth(t, deps, "admin", "config", "--typesafe-daily-usd", "0", "--json")
	require.NoError(t, err)
	// Decode into a new struct, as a real HTTP handler does.
	got = wire.AdminConfigUpdate{}
	require.NoError(t, json.Unmarshal([]byte((*patches)[1]), &got))
	require.Equal(t, map[string]int64{"typesafe": 0}, got.ProviderDailyNanodollars)
}
