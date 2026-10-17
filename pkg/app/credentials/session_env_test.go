package credentials

import (
	"github.com/atomicobject/rhizome/internal/testkit"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestCredentialSessionsKeepProvidedValuesInTheirInvocation(t *testing.T) {
	t.Parallel()
	const key = "RZM_TEST_INVOCATION_CREDENTIAL_FIXTURE"
	before, beforeSet := os.LookupEnv(key)
	firstEnv, secondEnv := testkit.Env(t), testkit.Env(t)
	first, second := NewSession(firstEnv), NewSession(secondEnv)
	require.NoError(t, first.Provide(key, "synthetic-first"))
	require.Equal(t, "synthetic-first", first.Resolve(key))
	require.Equal(t, "synthetic-first", vaultconfig.ResolveValue(firstEnv, key))
	require.Empty(t, second.Resolve(key))
	require.Empty(t, vaultconfig.ResolveValue(secondEnv, key))
	require.NoError(t, second.Provide(key, "synthetic-second"))
	require.Equal(t, "synthetic-first", first.Resolve(key))
	require.Equal(t, "synthetic-second", second.Resolve(key))
	after, afterSet := os.LookupEnv(key)
	require.Equal(t, beforeSet, afterSet)
	require.Equal(t, before, after)
}
