package cmd

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/stretchr/testify/require"
)

func TestAgentEvaluateBatchCheckCredentialsReportsSourceWithoutTheKey(t *testing.T) {
	const syntheticKey = "ts-synthetic-check-credentials-0000"
	for _, tc := range []struct {
		name   string
		vars   map[string]string
		want   bool
		source string
	}{
		{name: "none", vars: map[string]string{}, want: false, source: "none"},
		{name: "direct", vars: map[string]string{"TYPESAFE_API_KEY": syntheticKey}, want: true, source: "direct"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := testkit.Env(t)
			env.LookupEnv = func(key string) (string, bool) { v, ok := tc.vars[key]; return v, ok }
			stdout, _, err := runRootCLIWithInput(t, vaultconfig.WithEnv(context.Background(), env), []string{"agent", "evaluate-batch", "--check-credentials"}, "")
			require.NoError(t, err)
			require.NotContains(t, stdout, syntheticKey)
			var got struct {
				Configured bool   `json:"configured"`
				Source     string `json:"source"`
			}
			require.NoError(t, json.Unmarshal([]byte(stdout), &got))
			require.Equal(t, tc.want, got.Configured)
			require.Equal(t, tc.source, got.Source)
		})
	}
}
