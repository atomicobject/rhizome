package indexing

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/stretchr/testify/require"
)

func TestUnifiedSemanticResourcesReportsIntentSyncFailure(t *testing.T) {
	t.Parallel()

	res := &unifiedSemanticResources{}
	res.startIntentSync(context.Background(), nil)
	require.NoError(t, res.waitIntent())

	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	res = &unifiedSemanticResources{}
	res.startIntentSync(context.Background(), store)
	require.ErrorContains(t, res.waitIntent(), "semantic runtime unavailable")
}
