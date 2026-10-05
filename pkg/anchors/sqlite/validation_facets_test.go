package sqlite

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestValidationSnapshotCodesIncludeLaterPages(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "facets.db"))
	require.NoError(t, err)
	defer store.Close()
	generation, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	fixture := validationStoreFixture(generation, 201)
	fixture.Diagnostics[200].Code = "later_page_code"
	_, err = store.PublishValidationSnapshot(ctx, fixture)
	require.NoError(t, err)
	snapshot, ok, err := store.GetPublishedValidationSnapshot(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []string{"later_page_code", "missing_type"}, snapshot.IssueCodes)
}
