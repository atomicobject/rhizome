package validate

import (
	"context"
	"errors"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// indexedValidationSnapshot prepares the shared runtime the way the indexing
// pipeline does and returns the snapshot it would publish.
func indexedValidationSnapshot(t *testing.T, files map[string]string) (semdb.ValidationSnapshot, error) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, files))
	store := openTestStore(t, root)
	vaultDef := obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth}
	_, err := testNoteMetadata(t).EnsureIndexed(context.Background(), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	runtime, err := ontology.EnsureFreshRuntimeWithStore(context.Background(), testNoteMetadata(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	snapshot, durationMs, err := IndexValidationSnapshot(context.Background(), testNoteMetadata(t), vaultDef, runtime, nil, 500)
	require.GreaterOrEqual(t, durationMs, int64(0))
	return snapshot, err
}

func snapshotOutcomes(snapshot semdb.ValidationSnapshot) []string {
	outcomes := make([]string, 0, len(snapshot.Checks))
	for _, check := range snapshot.Checks {
		outcomes = append(outcomes, check.Check+":"+check.Outcome)
	}
	return outcomes
}

func TestIndexValidationSnapshotReportsBrokenLinksWithoutOntologySchema(t *testing.T) {
	snapshot, err := indexedValidationSnapshot(t, map[string]string{
		"hello.md": "# Hello\n\nSee [[Does Not Exist]].\n",
	})
	require.NoError(t, err)

	require.Equal(t, []string{CheckOntology, CheckIdentifiers, CheckBrokenLinks}, snapshot.SelectedChecks)
	require.Equal(t, semdb.ValidationCompletionComplete, snapshot.Completion)
	require.Equal(t, []string{
		CheckOntology + ":" + semdb.ValidationCheckOutcomeNotApplicable,
		CheckIdentifiers + ":" + semdb.ValidationCheckOutcomeNotApplicable,
		CheckBrokenLinks + ":" + semdb.ValidationCheckOutcomeCompleted,
	}, snapshotOutcomes(snapshot))
	require.Equal(t, 1, snapshot.AffectedNoteCount)
	require.Len(t, snapshot.Diagnostics, 1)
	assert.Contains(t, snapshot.Diagnostics[0].Target, "Does Not Exist")
	assert.Equal(t, []string{"hello.md"}, snapshot.Diagnostics[0].AffectedNotePaths)
}

func TestIndexValidationSnapshotRunsConfiguredDefaultSuite(t *testing.T) {
	snapshot, err := indexedValidationSnapshot(t, map[string]string{
		".rhizome/config.yml": "validation:\n  default:\n    add: [link-hygiene]\n    skip: [broken-links]\n",
		"hello.md":            "# Hello\n\n[[target.md]] and [[Does Not Exist]]\n",
		"target.md":           "# Target\n",
	})
	require.NoError(t, err)

	assert.Equal(t, []string{CheckOntology, CheckIdentifiers, CheckLinkHygiene}, snapshot.SelectedChecks)
	require.Len(t, snapshot.Diagnostics, 1, "the skipped broken-links check must not report")
	assert.Equal(t, CheckLinkHygiene, snapshot.Diagnostics[0].Check)
	assert.Equal(t, "wikilink_target_has_md_extension", snapshot.Diagnostics[0].Code)
}

func TestIndexValidationSnapshotUnknownLocalConfigFieldDoesNotBlockIndexing(t *testing.T) {
	snapshot, err := indexedValidationSnapshot(t, map[string]string{
		".rhizome/config.yml": "validation:\n  unknown: true\n",
		"hello.md":            "# Hello\n\nSee [[Does Not Exist]].\n",
	})
	require.NoError(t, err)

	assert.Equal(t, []string{CheckOntology, CheckIdentifiers, CheckBrokenLinks}, snapshot.SelectedChecks)
	assert.Equal(t, semdb.ValidationCompletionComplete, snapshot.Completion)
	require.Len(t, snapshot.Diagnostics, 1)
	assert.Contains(t, snapshot.Diagnostics[0].Target, "Does Not Exist")
}

// A bad configured suite is an error, never a silent fallback to the built-in
// default. Durable error retention belongs to the refresh coordinator.
func TestIndexValidationSnapshotRejectsInvalidConfigWithoutDefaultFallback(t *testing.T) {
	for _, tt := range []struct{ name, config, err string }{
		{"malformed local config", "validation: invalid\n", "cannot unmarshal"},
		{"invalid suite composition", "validation:\n  all:\n    add: [unknown-check]\n", "unknown check"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			snapshot, err := indexedValidationSnapshot(t, map[string]string{
				".rhizome/config.yml": tt.config,
				"hello.md":            "# Hello\n\nSee [[Does Not Exist]].\n",
			})
			require.ErrorContains(t, err, tt.err)
			require.Empty(t, snapshot.SelectedChecks)
			require.Empty(t, snapshot.Checks)
			require.Empty(t, snapshot.Diagnostics)
		})
	}
}

func TestIndexValidationSnapshotUsesPreparedRuntimeError(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		".rhizome/config.yml": "validation:\n  default:\n    skip: [broken-links]\n",
		"hello.md":            "# Hello\n",
	}))
	vaultDef := obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth}

	snapshot, durationMs, err := IndexValidationSnapshot(context.Background(), testNoteMetadata(t), vaultDef, nil, errors.New("prepared runtime failed"), 500)
	require.NoError(t, err)
	require.GreaterOrEqual(t, durationMs, int64(0))

	assert.Equal(t, []string{CheckOntology, CheckIdentifiers}, snapshot.SelectedChecks)
	require.Positive(t, snapshot.ErrorCount)
	require.Len(t, snapshot.Checks, 2)
	for _, check := range snapshot.Checks {
		assert.Equal(t, "shared ontology runtime: prepared runtime failed", check.Error, check.Check)
	}
}

func TestConfiguredDefaultValidationChecksLoadsOnlySelectedVaultRoot(t *testing.T) {
	vaultRoot := t.TempDir()
	require.NoError(t, writeFixtureFiles(vaultRoot, map[string]string{
		".rhizome/config.yml": "validation:\n  default:\n    skip: [broken-links]\n",
	}))
	unrelatedRoot := t.TempDir()
	require.NoError(t, writeFixtureFiles(unrelatedRoot, map[string]string{
		".rhizome/config.yml": "validation:\n  unknown: true\n",
	}))
	t.Chdir(unrelatedRoot)

	checks, err := configuredDefaultValidationChecks(obsidian.VaultDefinition{Path: vaultRoot})
	require.NoError(t, err)
	assert.Equal(t, []string{CheckOntology, CheckIdentifiers}, checks)
}

func TestValidationSuiteConfigFromLocalPreservesSelectionSemantics(t *testing.T) {
	local := obsidian.LocalValidationConfig{
		Default: obsidian.LocalValidationSuiteConfig{
			Add:  []string{"link-hygiene"},
			Skip: []string{"broken-links"},
		},
		All: obsidian.LocalValidationSuiteConfig{
			Skip: []string{"aliases"},
		},
	}

	selection, err := ResolveSelection(nil, validationSuiteConfigFromLocal(local))
	require.NoError(t, err)
	assert.Equal(t, []string{CheckOntology, CheckIdentifiers, CheckLinkHygiene}, selection.Checks)
}
