package validationrun

import (
	"errors"
	"testing"

	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/skilloverlay"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestPrepareUsesInjectedSkillOverlayValidation(t *testing.T) {
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
			root := t.TempDir()
			prepared, err := Prepare(obsidian.VaultDefinition{Path: root}, testNoteMetadata(t), []string{"skill-overlays"}, 20, checker)
			require.NoError(t, err)
			result, err := RunPrepared(t.Context(), prepared, validate.ScanOptions{}, skillOverlayRuntime(t, root), nil)
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
				require.Equal(t, "overlay.yaml", result.Checks[0].Issues[0].Path)
			}
		})
	}
}

func TestIndexValidationSnapshotUsesInjectedSkillOverlayValidation(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		".rhizome/config.yml": "validation:\n  default:\n    add: [skill-overlays]\n    skip: [ontology, identifiers, broken-links]\n",
	}))
	runtime := skillOverlayRuntime(t, root)
	calls := 0
	snapshot, _, err := IndexValidationSnapshot(t.Context(), testNoteMetadata(t), obsidian.VaultDefinition{Path: root}, runtime, nil, 20, func() (skilloverlay.Manifest, error) {
		calls++
		return skilloverlay.Manifest{Issues: []skilloverlay.Issue{{Code: "fixture_issue", Path: "overlay.yaml", Message: "invalid fixture overlay"}}}, nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Len(t, snapshot.Checks, 1)
	require.Equal(t, validate.CheckSkillOverlays, snapshot.Checks[0].Check)
	require.Equal(t, 1, snapshot.IssueCount)
	require.Len(t, snapshot.Diagnostics, 1)
	require.Equal(t, "fixture_issue", snapshot.Diagnostics[0].Code)
}

func skillOverlayRuntime(t *testing.T, root string) *ontology.Runtime {
	t.Helper()
	require.NoError(t, writeFixtureFiles(root, map[string]string{"note.md": "# Note\n"}))
	definition := obsidian.VaultDefinition{Path: root}
	metadata := testNoteMetadata(t)
	store := openTestStore(t, root)
	_, err := metadata.EnsureIndexed(t.Context(), definition, &obsidian.Note{}, store)
	require.NoError(t, err)
	runtime, err := ontologytest.Publish(t.Context(), metadata, definition, &obsidian.Note{}, store)
	require.NoError(t, err)
	return runtime
}
