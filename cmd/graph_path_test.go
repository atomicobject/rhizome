package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestRunGraphPathUsesOntologyOnlyFacts(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "effort.md"), []byte("# Effort\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "spec.md"), []byte("# Spec\n"), 0o644))

	store, err := semdb.Open(obsidian.UnifiedIndexPath(root, ""))
	require.NoError(t, err)
	storyRef := ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindEmbedded, TypeName: "UserStory", NodeID: "story-a", Fragment: "^story-a"}
	storyJSON, err := json.Marshal(storyRef)
	require.NoError(t, err)
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "docs/effort.md", RelationName: "frozenSpecs", DstPath: "docs/spec.md", DstType: "TechnicalSpec", Provenance: "field", Structural: true},
			{SrcPath: "docs/effort.md", RelationName: "frozenStories", DstPath: "docs/spec.md", DstNodeID: "story-a", DstType: "UserStory", Provenance: "field", Structural: true},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "notes", LoadedAt: 1, Ready: true},
	}))
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{"docs/spec.md"}, []codeanchor.IntelOntologyNode{{
		NodeID: ontology.OntologyNodeID(storyRef), NotePath: "docs/spec.md", NodeRefJSON: string(storyJSON),
		NodeKind: string(ontology.NodeKindEmbedded), TypeName: "UserStory", DisplayLabel: "Story A",
		SourceLocator: "docs/spec.md#^story-a", BlockID: "story-a", LocatorStatus: string(ontology.NodeLocatorLinkable), UpdatedAt: 1,
	}}))
	require.NoError(t, store.Close())

	origVaultName := vaultName
	origMaxHops := graphPathMaxHops
	t.Cleanup(func() {
		vaultName = origVaultName
		graphPathMaxHops = origMaxHops
	})
	vaultName = root
	graphPathMaxHops = 4

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	require.NoError(t, runGraphPath(cmd, []string{"effort", "docs/spec.md"}))
	require.Contains(t, out.String(), "Shortest path (1 hop)")
	require.Contains(t, out.String(), "frozenSpecs")
	out.Reset()
	require.NoError(t, runGraphPath(cmd, []string{"docs/effort.md", "story-a"}))
	require.Contains(t, out.String(), "Shortest path (1 hop)")
	require.Contains(t, out.String(), "frozenStories")
	require.Contains(t, out.String(), "embedded:")
}
