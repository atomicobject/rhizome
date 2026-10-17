package ontology

import (
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestSyncRepairsCorruptFieldValuesBeforePublishingReadiness(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `type Project @node(paths: ["projects/*.md"]) { name: String! }`)
	writeOntologyNote(t, root, "projects/alpha.md", "---\nname: Alpha\n---\n")
	writeOntologyNote(t, root, "projects/beta.md", "---\nname: Beta\n---\n")
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	indexer := testkit.NoteMetadataIndexer(t)
	vault := obsidian.VaultDefinition{Path: root}
	note := &obsidian.Note{}
	_, err = indexer.EnsureIndexed(ctx, vault, note, store)
	require.NoError(t, err)
	queue := &recordingDeltaQueue{store: store}
	_, err = syncPublishedPathsForTest(ctx, indexer, vault, note, store, queue, nil, nil)
	require.NoError(t, err)
	formats, err := indexer.FormatRuntime()
	require.NoError(t, err)
	assertReady := func(want bool) {
		proof, err := store.GetOntologyReadiness(ctx, OntologyProviderHash(formats))
		require.NoError(t, err)
		require.Equal(t, want, proof.Ready)
	}
	assertReady(true)
	_, err = store.DB().ExecContext(ctx, `UPDATE ontology_node_field_values SET value_text='Corrupt',value_norm='corrupt' WHERE note_path='projects/alpha.md' AND field_name='name'`)
	require.NoError(t, err)
	assertReady(false)
	result, err := syncPublishedPathsForTest(ctx, indexer, vault, note, store, queue, nil, nil)
	require.NoError(t, err)
	require.False(t, result.Rebuilt)
	require.Equal(t, 1, result.Assessed, "repair only the corrupted source, even without an explicit dirty path")
	assertReady(true)
	assertOntologyBuildMatchesStore(t, ctx, vault, note, store)
	// An orphan row must be removed before its publication can be certified.
	_, err = store.DB().ExecContext(ctx, `INSERT INTO ontology_node_field_values (node_id,note_path,type_name,field_name,field_kind,source_kind,value_kind,value_text,value_norm,list_ordinal,schema_hash,updated_at) SELECT node_id,'projects/gone.md',type_name,'name',field_kind,source_kind,value_kind,'Orphan','orphan',0,schema_hash,updated_at FROM ontology_node_field_values LIMIT 1`)
	require.NoError(t, err)
	assertReady(false)
	result, err = syncPublishedPathsForTest(ctx, indexer, vault, note, store, queue, nil, nil)
	require.NoError(t, err)
	require.False(t, result.Rebuilt)
	assertReady(true)
	var orphans int
	require.NoError(t, store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM ontology_node_field_values WHERE note_path='projects/gone.md'`).Scan(&orphans))
	require.Zero(t, orphans)
	assertOntologyBuildMatchesStore(t, ctx, vault, note, store)
}

func TestSyncRepairsEveryDerivedOutputBeforePublishingReadiness(t *testing.T) {
	clearPublicationTimes := func(snapshot *semdb.OntologyReadinessSnapshot) {
		for i := range snapshot.NoteTypes {
			snapshot.NoteTypes[i].UpdatedAt = 0
		}
		for i := range snapshot.Edges {
			snapshot.Edges[i].UpdatedAt = 0
		}
		for i := range snapshot.LinkDependencies {
			snapshot.LinkDependencies[i].UpdatedAt = 0
		}
		for i := range snapshot.TypePolicies {
			snapshot.TypePolicies[i].UpdatedAt = 0
		}
	}
	for _, tc := range []struct {
		name         string
		mutation     string
		wantAssessed int
	}{
		{"assessment flags", `UPDATE ontology_note_assessments SET has_issues=1,type_ambiguous=1 WHERE note_path='projects/alpha.md'`, 1},
		{"assessment types", `UPDATE ontology_note_assessments SET declared_type='Ghost',resolved_type='Ghost' WHERE note_path='projects/alpha.md'`, 1},
		{"state type", `UPDATE ontology_note_state SET resolved_type='Ghost' WHERE note_path='projects/alpha.md'`, 1},
		{"note type", `DELETE FROM ontology_note_types WHERE note_path='projects/alpha.md'`, 1},
		{"relation", `DELETE FROM ontology_edges WHERE src_path='projects/alpha.md'`, 1},
		{"link dependency", `DELETE FROM ontology_node_field_value_dependencies WHERE source_note_path='projects/alpha.md'`, 1},
		{"type policy", `DELETE FROM ontology_type_policies`, 0},
		{"extra type policy", `INSERT INTO ontology_type_policies(type_name,policy_json,schema_hash,updated_at) VALUES('Ghost','{}','wrong',0)`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			writeOntologyTestConfig(t, root)
			writeOntologySchema(t, root, `type Project @node(paths: ["projects/*.md"]) @traversal(intents: ["docs_for_code"], includeAmbient: true) { name: String! related: Project @link }`)
			writeOntologyNote(t, root, "projects/alpha.md", "---\nname: Alpha\nrelated: '[[projects/beta]]'\n---\n")
			writeOntologyNote(t, root, "projects/beta.md", "---\nname: Beta\n---\n")
			store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, store.Close()) })
			indexer := testkit.NoteMetadataIndexer(t)
			vault := obsidian.VaultDefinition{Path: root}
			note := &obsidian.Note{}
			_, err = indexer.EnsureIndexed(ctx, vault, note, store)
			require.NoError(t, err)
			queue := &recordingDeltaQueue{store: store}
			_, err = syncPublishedPathsForTest(ctx, indexer, vault, note, store, queue, nil, nil)
			require.NoError(t, err)
			baseline, err := store.OntologyReadinessSnapshot(ctx)
			require.NoError(t, err)
			require.Len(t, baseline.NoteTypes, 2)
			require.NotEmpty(t, baseline.Edges)
			require.NotEmpty(t, baseline.LinkDependencies)
			require.NotEmpty(t, baseline.TypePolicies)
			formats, err := indexer.FormatRuntime()
			require.NoError(t, err)
			proof, err := store.GetOntologyReadiness(ctx, OntologyProviderHash(formats))
			require.NoError(t, err)
			require.True(t, proof.Ready)
			changed, err := store.DB().ExecContext(ctx, tc.mutation)
			require.NoError(t, err)
			count, err := changed.RowsAffected()
			require.NoError(t, err)
			require.Positive(t, count, "the fixture must actually damage the derived output")
			proof, err = store.GetOntologyReadiness(ctx, OntologyProviderHash(formats))
			require.NoError(t, err)
			require.False(t, proof.Ready)
			result, err := syncPublishedPathsForTest(ctx, indexer, vault, note, store, queue, nil, nil)
			require.NoError(t, err)
			require.False(t, result.Rebuilt)
			require.Equal(t, tc.wantAssessed, result.Assessed)
			proof, err = store.GetOntologyReadiness(ctx, OntologyProviderHash(formats))
			require.NoError(t, err)
			require.True(t, proof.Ready)
			repaired, err := store.OntologyReadinessSnapshot(ctx)
			require.NoError(t, err)
			clearPublicationTimes(&baseline)
			clearPublicationTimes(&repaired)
			require.ElementsMatch(t, baseline.NoteTypes, repaired.NoteTypes)
			require.ElementsMatch(t, baseline.Edges, repaired.Edges)
			require.ElementsMatch(t, baseline.LinkDependencies, repaired.LinkDependencies)
			require.ElementsMatch(t, baseline.TypePolicies, repaired.TypePolicies)
			assertOntologyBuildMatchesStore(t, ctx, vault, note, store)
		})
	}
}
