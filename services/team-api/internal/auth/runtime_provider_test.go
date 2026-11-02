package auth

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/config"
	"github.com/atomicobject/rhizome/services/team-api/internal/store"
	"github.com/stretchr/testify/require"
)

func TestRuntimeProviderBudgetsAreDetachedAndStartupBounded(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	st := store.NewSQLite(db)
	cfg := config.Config{ProviderDailyNanodollars: map[string]int64{"voyage": 50, "typesafe": 60}, MaxProviderDailyNanodollars: map[string]int64{"voyage": 50, "typesafe": 60}}
	rt, err := NewRuntime(t.Context(), st, cfg, time.Now)
	require.NoError(t, err)
	cfg.ProviderDailyNanodollars["voyage"] = 999
	first, err := rt.Runtime(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(50), first.ProviderDailyNanodollars["voyage"])
	first.ProviderDailyNanodollars["voyage"] = 777
	delete(first.PricingAssumptions, "voyage")
	again, err := rt.Runtime(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(50), again.ProviderDailyNanodollars["voyage"])
	require.Contains(t, again.PricingAssumptions, "voyage")
	// A persisted configuration from another process cannot exceed the new
	// process's immutable ceilings, even before the next admin mutation.
	_, err = st.UpdateRuntimeConfig(t.Context(), wire.AdminConfigUpdate{ProviderDailyNanodollars: map[string]int64{"voyage": 5000}}, wire.AuditEntry{})
	require.NoError(t, err)
	rt.Invalidate()
	bounded, err := rt.Runtime(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(50), bounded.ProviderDailyNanodollars["voyage"])
	cfg.MaxProviderDailyNanodollars = nil
	rt, err = NewRuntime(t.Context(), st, cfg, time.Now)
	require.NoError(t, err)
	disabled, err := rt.Runtime(t.Context())
	require.NoError(t, err)
	require.Zero(t, disabled.ProviderDailyNanodollars["voyage"])
	require.Zero(t, disabled.ProviderDailyNanodollars["typesafe"])
}
