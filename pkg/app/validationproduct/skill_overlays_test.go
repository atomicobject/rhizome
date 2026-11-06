package validationproduct

import (
	"errors"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/validationrun"
	"github.com/atomicobject/rhizome/pkg/skilloverlay"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestProjectionFallbackUsesInjectedSkillOverlayValidation(t *testing.T) {
	for _, test := range []struct {
		name      string
		manifest  skilloverlay.Manifest
		err       error
		absent    bool
		wantError string
	}{
		{name: "issues", manifest: skilloverlay.Manifest{Issues: []skilloverlay.Issue{{Code: "fixture_issue", Path: "overlay.yaml", Message: "invalid fixture overlay"}}}},
		{name: "failure", err: errors.New("fixture checker failed"), wantError: "fixture checker failed"},
		{name: "absent", absent: true, wantError: "skill overlay validator is not configured"},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			checker := func() (skilloverlay.Manifest, error) {
				calls++
				return test.manifest, test.err
			}
			if test.absent {
				checker = nil
			}
			result, err := RunWithProjection(t.Context(), validationrun.ValidationRunRequest{Selectors: []string{"skill-overlays"}}, ValidationProjectionRequest{
				VaultDef: obsidian.VaultDefinition{Path: writeProjectionVault(t)}, NoteMetadata: testProjectionNoteMetadata(t), SkillOverlays: checker,
			}, validate.ScanOptions{})
			require.NoError(t, err)
			require.False(t, result.OK)
			require.Len(t, result.Checks, 1)
			require.Equal(t, test.wantError, result.Checks[0].Error)
			if test.absent {
				require.Zero(t, calls)
			} else {
				require.Equal(t, 1, calls)
			}
			if test.name == "issues" {
				require.Equal(t, 1, result.IssueCount)
				require.Equal(t, "fixture_issue", result.Checks[0].Issues[0].Code)
			}
		})
	}
}

func TestRefreshCoordinatorUsesInjectedSkillOverlayValidation(t *testing.T) {
	root := writeProjectionVault(t)
	require.NoError(t, repoconfig.SaveLocalConfig(root, repoconfig.LocalConfig{Validation: repoconfig.LocalValidationConfig{
		Default: repoconfig.LocalValidationSuiteConfig{Add: []string{"skill-overlays"}, Skip: []string{"ontology", "identifiers", "broken-links"}},
	}}))
	calls := 0
	store := openCoordinatorStore(t)
	coordinator := NewRefreshCoordinator(RefreshCoordinatorOptions{
		Store: store, VaultDef: obsidian.VaultDefinition{Path: root}, NoteMetadata: testProjectionNoteMetadata(t),
		SkillOverlays: func() (skilloverlay.Manifest, error) {
			calls++
			return skilloverlay.Manifest{Issues: []skilloverlay.Issue{{Code: "fixture_issue", Path: "overlay.yaml", Message: "invalid fixture overlay"}}}, nil
		},
	})
	require.NoError(t, coordinator.Refresh(t.Context()))
	require.Equal(t, 1, calls)
	state, err := store.GetValidationStateSnapshot(t.Context())
	require.NoError(t, err)
	require.True(t, state.HasSnapshot)
	require.Equal(t, 1, state.Snapshot.IssueCount)
	require.Len(t, state.Snapshot.Checks, 1)
	require.Equal(t, validate.CheckSkillOverlays, state.Snapshot.Checks[0].Check)
}
