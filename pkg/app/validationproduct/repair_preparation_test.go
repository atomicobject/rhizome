package validationproduct

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/validationrun"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestPublishedReadSummaryPreparesRepairOnlyWhenReviewIsRequested(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.md"), []byte("See [[target.md]].\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "target.md"), []byte("# Target\n"), 0o644))
	definition := obsidian.VaultDefinition{Name: "test", Path: root, Links: obsidian.LinkTypeBoth}
	metadata := testProjectionNoteMetadata(t)
	prepared, err := validationrun.Prepare(definition, metadata, []string{"link-hygiene"}, 20)
	require.NoError(t, err)
	var published AuthoritativeRun
	store := openCoordinatorStore(t)
	coordinator := NewRefreshCoordinator(RefreshCoordinatorOptions{
		Store: store, VaultDef: definition,
		Run: func(ctx context.Context) (AuthoritativeRun, error) {
			result, err := RunWithProjection(ctx, prepared.Request, ValidationProjectionRequest{
				VaultPath: root, VaultDef: definition, NoteMetadata: metadata, NoteReader: prepared.RunContext.NoteReader,
			}, validate.ScanOptions{RunContext: &prepared.RunContext})
			published = AuthoritativeRun{Result: result, RunContext: prepared.RunContext, NotePaths: []string{"source.md", "target.md"}}
			return published, err
		},
	})
	require.NoError(t, coordinator.Refresh(t.Context()))
	plan := published.Result.FixPlan
	require.NotNil(t, plan)
	require.True(t, plan.PreparationRequired)
	require.Empty(t, plan.Operations)
	state, err := store.GetValidationState(t.Context())
	require.NoError(t, err)
	review, err := coordinator.CreateRepairReview(t.Context(), state.PublishedGeneration, plan.Fingerprint, []string{plan.Actions[0].ID})
	require.NoError(t, err)
	require.Equal(t, []string{plan.Actions[0].ID}, review.ActionIDs)
	require.Equal(t, []string{"source.md"}, review.AffectedPaths)
	require.Len(t, review.Preview, 1)
	require.NotEqual(t, plan.Fingerprint, review.PlanFingerprint, "review authority binds the prepared source bytes")
	require.True(t, published.Result.FixPlan.PreparationRequired, "review preparation must not mutate published read authority")
	unchanged, err := os.ReadFile(filepath.Join(root, "source.md"))
	require.NoError(t, err)
	require.Equal(t, "See [[target.md]].\n", string(unchanged))

	require.NoError(t, os.WriteFile(filepath.Join(root, "source.md"), []byte("User changed [[target.md]].\n"), 0o644))
	_, err = coordinator.CreateRepairReview(t.Context(), state.PublishedGeneration, plan.Fingerprint, []string{plan.Actions[0].ID})
	var reviewError *validate.RepairReviewError
	require.ErrorAs(t, err, &reviewError)
	require.Equal(t, validate.RepairReviewErrorRevalidationRequired, reviewError.Code)
}
