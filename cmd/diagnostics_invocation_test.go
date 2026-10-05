package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/repositorytrust"
	"github.com/stretchr/testify/require"
)

func TestOfflineDiagnosticsBypassesMalformedRepositoryConfiguration(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, ".rhizome"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("rhizome: [invalid"), 0o600))
	for _, args := range [][]string{
		{"diagnostics", "index", "--last"},
		{"--no-pager", "diagnostics", "logs", "--json"},
		{"--vault", root, "diagnostics", "reports"},
	} {
		decision := repoDelegateDecisionForTrust(args, root, filepath.Join(root, "global-rzm"), repositorytrust.Store{}, nil)
		require.NoError(t, decision.Err)
		require.False(t, decision.Delegate)
	}
	decision := repoDelegateDecisionForTrust([]string{"index"}, root, filepath.Join(root, "global-rzm"), repositorytrust.Store{}, nil)
	require.Error(t, decision.Err)
}
