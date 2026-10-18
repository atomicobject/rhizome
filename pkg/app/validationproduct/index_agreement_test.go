package validationproduct

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/validationrun"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestIndexedAndLiveValidationPublishTheSameApplicability(t *testing.T) {
	root := writeProjectionVault(t)
	require.NoError(t, repoconfig.SaveLocalConfig(root, repoconfig.LocalConfig{Validation: repoconfig.LocalValidationConfig{
		Default: repoconfig.LocalValidationSuiteConfig{Add: []string{"link-hygiene", "code-anchors"}},
	}}))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "ref.md"), []byte("[[one.md]] [[missing]]\n"), 0o644))
	definition := obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth}
	metadata := testProjectionNoteMetadata(t)
	projection, err := RefreshValidationProjection(t.Context(), ValidationProjectionRequest{
		VaultPath: root, VaultDef: definition, NoteMetadata: metadata,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, projection.Close()) })
	indexed, _, err := validationrun.IndexValidationSnapshot(t.Context(), metadata, definition, projection.Runtime, nil, 100)
	require.NoError(t, err)
	require.NoError(t, projection.Close())
	live, err := RunLive(t.Context(), metadata, definition, 100)
	require.NoError(t, err)
	indexedOutcomes := make(map[string]string, len(indexed.Checks))
	for _, outcome := range indexed.Checks {
		indexedOutcomes[outcome.Check] = outcome.Outcome
	}
	require.Len(t, live.Result.Outcomes, len(indexedOutcomes))
	for _, outcome := range live.Result.Outcomes {
		canonical, ok := validate.CanonicalCheck(outcome.Check)
		require.True(t, ok)
		require.Equal(t, string(outcome.Outcome), indexedOutcomes[canonical], canonical)
	}
	require.Equal(t, live.Result.IssueCount, indexed.IssueCount)
	require.Equal(t, "not_applicable", indexedOutcomes[validate.CheckCodeAnchors])
}
