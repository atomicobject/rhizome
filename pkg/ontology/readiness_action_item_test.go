package ontology

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestSyncNoteGainingUnassignedActionItem(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `type Person @node(paths: ["people/*.md"]) { name: String! }
type ActionItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["notes/**/*.md"]) {
 done: Boolean! @field(sourceKind: CHECKBOX)
 assignee: Person! @link
}`)
	const path = "notes/effort.md"
	const initial = "# Effort\n\n## Compounding Follow-ups\n"
	writeOntologyNote(t, root, path, initial)
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	indexer := testkit.NoteMetadataIndexer(t)
	vault := obsidian.VaultDefinition{Path: root}
	reader := &obsidian.Note{}
	_, err = ensureIndexedForTest(ctx, indexer, vault, reader, store)
	require.NoError(t, err)
	writeOntologyNote(t, root, path, initial+"\n- [ ] Probe action item #action-item\n")
	_, err = syncPathsForTest(ctx, indexer, vault, reader, store, nil, []string{path}, nil)
	require.NoError(t, err)
	formats, err := indexer.FormatRuntime()
	require.NoError(t, err)
	proof, err := store.GetOntologyReadiness(ctx, OntologyProviderHash(formats))
	require.NoError(t, err)
	require.True(t, proof.Ready)
	snapshot, err := store.OntologyReadinessSnapshot(ctx)
	require.NoError(t, err)
	assessment, err := AssessmentFromJSON(snapshot.Assessments[path].AssessmentJSON)
	require.NoError(t, err)
	var missing int
	for _, issue := range assessment.Issues {
		if issue.Code == "missing_required_field" && issue.TypeName == "ActionItem" && issue.FieldName == "assignee" {
			missing++
		}
	}
	require.Equal(t, 1, missing, "the incomplete action item remains a validation finding")
}
