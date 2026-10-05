package codeintel

import (
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestOpenOptionalIntelStore_DisabledReturnsWarning(t *testing.T) {
	t.Parallel()

	cfg := codeanchor.Config{Enabled: false}
	store, cleanup, warning, err := OpenOptionalIntelStore(t.TempDir(), cfg)
	require.NoError(t, err)
	require.Nil(t, store)
	require.Nil(t, cleanup)
	require.Contains(t, warning, "code index disabled")
}
