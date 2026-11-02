package store_test

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/dynamotest"
	"github.com/atomicobject/rhizome/services/team-api/internal/store"
	"github.com/stretchr/testify/require"
)

func TestSQLiteProviderConfig(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	providerConfigContract(t, store.NewSQLite(db))
}

func TestDynamoDBProviderConfig(t *testing.T) {
	providerConfigContract(t, store.NewDynamoDB(dynamotest.Open(t)))
}

func providerConfigContract(t *testing.T, st store.Store) {
	ctx := t.Context()
	initial := map[string]int64{"voyage": 20, "typesafe": 30}
	require.NoError(t, st.InitRuntimeConfig(ctx, wire.AdminConfig{ProviderDailyNanodollars: initial}))
	initial["voyage"] = 999
	require.NoError(t, st.InitRuntimeConfig(ctx, wire.AdminConfig{ProviderDailyNanodollars: initial}))
	got, err := st.RuntimeConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(20), got.ProviderDailyNanodollars["voyage"])
	got.ProviderDailyNanodollars["voyage"] = 888
	update := map[string]int64{"voyage": 50}
	got, err = st.UpdateRuntimeConfig(ctx, wire.AdminConfigUpdate{ProviderDailyNanodollars: update}, wire.AuditEntry{Action: "config.update"})
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"voyage": 50, "typesafe": 30}, got.ProviderDailyNanodollars)
	update["voyage"], got.ProviderDailyNanodollars["typesafe"] = 777, 777
	// Two operators change different providers concurrently; neither overwrites
	// the other provider's budget when the backend retries its atomic mutation.
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for provider, value := range map[string]int64{"voyage": 0, "typesafe": 70} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := st.UpdateRuntimeConfig(ctx, wire.AdminConfigUpdate{ProviderDailyNanodollars: map[string]int64{provider: value}}, wire.AuditEntry{Action: "config.update"})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	got, err = st.RuntimeConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"voyage": 0, "typesafe": 70}, got.ProviderDailyNanodollars)
}
