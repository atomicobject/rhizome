package cmd

import (
	"context"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContextCompressorConfigLoading(t *testing.T) {
	commandOptions := newRuntimeOptionsForTest(t)

	t.Setenv("CEREBRAS_API_KEY", "test-key")
	for _, tc := range []struct {
		name          string
		config        string
		maxInputChars int
	}{
		{name: "missing config", maxInputChars: 0},
		{name: "no compression section", config: "notes: {}\n", maxInputChars: 0},
		{name: "invalid config", config: "compression: [\n", maxInputChars: 0},
		{name: "configured input limit", config: "compression:\n  enabled: true\n  maxInputTokens: 200\n", maxInputChars: 640},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vaultPath := t.TempDir()
			if tc.config != "" {
				require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, ".rhizome"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(vaultPath, ".rhizome", "config.yml"), []byte(tc.config), 0o644))
			}
			compressor := commandOptions.loadContextCompressor(vaultconfig.WithEnv(context.Background(), vaultconfig.Env{HomeDir: t.TempDir(), LookupEnv: os.LookupEnv}), vaultPath)
			if tc.maxInputChars == 0 {
				require.Nil(t, compressor)
				return
			}
			require.NotNil(t, compressor)
			require.Equal(t, tc.maxInputChars, compressor.MaxInputChars())
		})
	}
}
