package init

import (
	"bytes"
	"testing"

	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestWarnCompressionReadinessRequiresExplicitEnablement(t *testing.T) {
	enabled, disabled := true, false
	for _, tc := range []struct {
		name        string
		compression *repoconfig.LocalCompressionConfig
		key         string
		want        string
	}{
		{name: "missing config"},
		{name: "unset enablement", compression: &repoconfig.LocalCompressionConfig{Provider: "openai"}},
		{name: "disabled", compression: &repoconfig.LocalCompressionConfig{Enabled: &disabled}},
		{name: "default provider", compression: &repoconfig.LocalCompressionConfig{Enabled: &enabled}, want: "Warning: compression enabled but CEREBRAS_API_KEY is not set.\n"},
		{name: "normalized provider", compression: &repoconfig.LocalCompressionConfig{Enabled: &enabled, Provider: "  GeMiNi "}, want: "Warning: compression enabled but GEMINI_API_KEY is not set.\n"},
		{name: "configured key", compression: &repoconfig.LocalCompressionConfig{Enabled: &enabled, Provider: "  GeMiNi "}, key: "GEMINI_API_KEY"},
		{name: "unknown provider", compression: &repoconfig.LocalCompressionConfig{Enabled: &enabled, Provider: "example"}, want: "Warning: compression enabled but EXAMPLE_API_KEY is not set.\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := vaultconfig.Env{HomeDir: t.TempDir(), LookupEnv: func(key string) (string, bool) {
				if tc.key != "" && key == tc.key {
					return "synthetic-test-key", true
				}
				return "", false
			}}
			var out bytes.Buffer
			warnCompressionReadiness(env, repoconfig.LocalConfig{Compression: tc.compression}, &out)
			require.Equal(t, tc.want, out.String())
			warnCompressionReadiness(env, repoconfig.LocalConfig{Compression: tc.compression}, nil)
		})
	}
}
