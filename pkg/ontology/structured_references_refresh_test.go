package ontology

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestStructuredReferencesRefreshWhenMarkdownTargetsChange(t *testing.T) {
	root := testkit.Vault(t, map[string]string{
		".rhizome/ontology/schema.graphql": `type Host @node(default:true){status:String}
type Evidence @node(paths:["evidence/*.md"]){name:String @field}
type Row @node(locator:EMBEDDED) @source(shape:RECORD,paths:["model.yaml"],select:"rows",key:{field:"id"},title:["id"],space:"test",logicalKind:"row"){
id:ID @field(path:"id")
evidence:[Evidence!] @field(path:"evidence") @references(to:["Evidence"],mode:STRICT,values:ELEMENTS,edge:{name:"evidence"})
}`,
		"model.yaml": "rows: [{id: A, evidence: [evidence/source.md]}]\n",
	})
	vault := obsidian.VaultDefinition{Root: root, Includes: []string{"model.yaml", "evidence/*.md"}}
	indexer := testkit.NoteMetadataIndexer(t)
	store, err := semdb.Open(filepath.Join(t.TempDir(), "incremental.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	_, err = syncPathsForTest(t.Context(), indexer, vault, &obsidian.Note{}, store, nil, nil, nil)
	require.NoError(t, err)
	for _, present := range []bool{true, false} {
		path := filepath.Join(root, "evidence/source.md")
		var changed, deleted []string
		if present {
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
			require.NoError(t, os.WriteFile(path, []byte("---\nname: Source\n---\n# Source\n"), 0o644))
			changed = []string{"evidence/source.md"}
		} else {
			require.NoError(t, os.Remove(path))
			deleted = []string{"evidence/source.md"}
		}
		_, err = syncPathsForTest(t.Context(), indexer, vault, &obsidian.Note{}, store, nil, changed, deleted)
		require.NoError(t, err)
		full, err := semdb.Open(filepath.Join(t.TempDir(), "full.sqlite"))
		require.NoError(t, err)
		_, err = syncPathsForTest(t.Context(), indexer, vault, &obsidian.Note{}, full, nil, nil, nil)
		require.NoError(t, err)
		require.Equal(t, structuredSnapshot(t, full), structuredSnapshot(t, store))
		require.NoError(t, full.Close())
		edges, err := store.OntologyEdgesForPaths(t.Context(), []string{"model.yaml"}, true, "", 0)
		require.NoError(t, err)
		var evidenceTargets []string
		for _, edge := range edges {
			if edge.RelationName == "evidence" {
				evidenceTargets = append(evidenceTargets, edge.DstPath)
			}
		}
		if present {
			require.Equal(t, []string{"evidence/source.md"}, evidenceTargets)
		} else {
			require.Empty(t, evidenceTargets)
		}
	}
}
