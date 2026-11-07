package coderefs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// A cancelled validation must stop scanning code files instead of reading
// every configured file first.
func TestScanConfiguredStopsWhenCancelled(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.go"), []byte("// Coderefs: [[note]]\n"), 0o644))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ScanConfigured(ctx, root, NewConfig(true, []string{"*.go"}, nil), nil)
	require.ErrorIs(t, err, context.Canceled)
}
