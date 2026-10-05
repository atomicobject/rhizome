package diagnostics

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInvalidDiagnosticSettingsPreserveExplicitOptOut(t *testing.T) {
	for _, setting := range []string{"retentionDays: 0", "maxBytes: 1", "level: invalid", "retentionDays: wrong-type", "maxBytes: [1, 2]"} {
		t.Run(setting, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0700))
			require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("diagnostics:\n  enabled: false\n  "+setting+"\n"), 0600))
			opts, err := LoadOptions(root)
			require.Error(t, err)
			require.True(t, opts.Disabled)
			recorder, err := Open(root, opts)
			require.NoError(t, err)
			require.NoError(t, recorder.Close())
			_, err = os.Stat(filepath.Join(root, ".rhizome", "diagnostics"))
			require.True(t, os.IsNotExist(err))
		})
	}
}
