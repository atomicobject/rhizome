package ontology

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// AO declares one relation from both ends (service.owns governs a collection;
// collection.owner is governed-by its service). Both hosts emit the same edge
// key, so the index must keep one edge carrying every occurrence, whichever
// host writes first, and incremental changes must match a fresh build.
func TestRecordRelationDeclaredFromBothEndsMergesOccurrences(t *testing.T) {
	for _, tc := range []struct{ name, services, collections string }{
		{"services host first", "model/a-services.yaml", "model/b-collections.yaml"},
		{"collections host first", "model/b-services.yaml", "model/a-collections.yaml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\", \"**/*.yaml\"]\n"), 0o644))
			writeOntologySchema(t, root, fmt.Sprintf(`
type Service @node(locator: EMBEDDED) @source(shape: RECORD, paths: [%q], select: "services", key: {field: "id"}, title: ["$key"], space: "test", logicalKind: "service") {
  owns: [String!] @field(path: "owns") @references(to: ["Collection"], mode: STRICT, values: ELEMENTS, edge: {name: "governs", direction: OUTBOUND})
}
type Collection @node(locator: EMBEDDED) @source(shape: RECORD, paths: [%q], select: "collections", key: {field: "id"}, title: ["$key"], space: "test", logicalKind: "collection") {
  owner: String @field(path: "owner") @references(to: ["Service"], mode: STRICT, values: VALUE, edge: {name: "governs", direction: INBOUND})
}`, tc.services, tc.collections))
			// C1 is declared from both ends, C2 only by owns, C3 only by owner: AO's edge set is their union.
			writeOntologyNote(t, root, tc.services, "services:\n  - id: S1\n    owns: [C1, C2]\n")
			writeOntologyNote(t, root, tc.collections, "collections:\n  - id: C1\n    owner: S1\n  - id: C2\n  - id: C3\n    owner: S1\n")
			store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, store.Close()) })
			indexer := testkit.NoteMetadataIndexer(t)
			vault := obsidian.VaultDefinition{Root: root, Includes: []string{"**/*.md", "**/*.yaml"}}
			note := &obsidian.Note{}
			formats, err := indexer.FormatRuntime()
			require.NoError(t, err)
			converged := func(want []string) {
				t.Helper()
				proof, err := store.GetOntologyReadiness(ctx, OntologyProviderHash(formats))
				require.NoError(t, err)
				require.True(t, proof.Ready, "completeness must hold regardless of host write order")
				assertOntologyBuildMatchesStore(t, ctx, vault, note, store)
				require.Equal(t, want, governsEdges(t, ctx, store))
			}
			_, err = indexer.EnsureIndexed(ctx, vault, note, store)
			require.NoError(t, err)
			_, err = syncPathsForTest(ctx, indexer, vault, note, store, nil, nil, nil)
			require.NoError(t, err)
			converged([]string{
				"service:S1 governs collection:C1 [collection:C1.owner#0=S1 service:S1.owns#0=C1]",
				"service:S1 governs collection:C2 [service:S1.owns#1=C2]",
				"service:S1 governs collection:C3 [collection:C3.owner#0=S1]",
			})

			writeOntologyNote(t, root, tc.services, "services:\n  - id: S1\n    owns: [C2]\n")
			require.NoError(t, indexer.SyncPaths(ctx, vault, note, store, []string{tc.services}, nil))
			_, err = syncPublishedPathsForTest(ctx, indexer, vault, note, store, nil, []string{tc.services}, nil)
			require.NoError(t, err)
			converged([]string{
				"service:S1 governs collection:C1 [collection:C1.owner#0=S1]",
				"service:S1 governs collection:C2 [service:S1.owns#0=C2]",
				"service:S1 governs collection:C3 [collection:C3.owner#0=S1]",
			})

			require.NoError(t, os.Remove(filepath.Join(root, tc.collections)))
			require.NoError(t, indexer.SyncPaths(ctx, vault, note, store, nil, []string{tc.collections}))
			_, err = syncPublishedPathsForTest(ctx, indexer, vault, note, store, nil, nil, []string{tc.collections})
			require.NoError(t, err)
			converged(nil)
		})
	}
}

// governsEdges names endpoints and authoring fields by logical identity, so
// both write orders must produce the same list.
func governsEdges(t *testing.T, ctx context.Context, store *semdb.Store) []string {
	t.Helper()
	paths, err := store.CurrentNoteMetadataPaths(ctx)
	require.NoError(t, err)
	nodes, err := store.OntologyNodesByPaths(ctx, paths)
	require.NoError(t, err)
	names := map[string]string{}
	for _, node := range nodes {
		var ref NodeRef
		require.NoError(t, json.Unmarshal([]byte(node.NodeRefJSON), &ref))
		names[ref.NodeID] = map[string]string{"Service": "service", "Collection": "collection"}[node.TypeName] + ":" + node.Title
	}
	edges, err := store.OntologyEdgesForPaths(ctx, paths, true, "", 0)
	require.NoError(t, err)
	var out []string
	for _, edge := range edges {
		if edge.RelationName != "governs" {
			continue
		}
		var occurrences []readmodel.RelationOccurrence
		require.NoError(t, json.Unmarshal([]byte(edge.OccurrencesJSON), &occurrences))
		var authored []string
		for _, occurrence := range occurrences {
			authored = append(authored, fmt.Sprintf("%s.%s#%d=%s", names[occurrence.Node], occurrence.Field, occurrence.Ordinal, occurrence.Value))
		}
		sort.Strings(authored)
		out = append(out, fmt.Sprintf("%s governs %s %v", names[edge.SrcNodeID], names[edge.DstNodeID], authored))
	}
	sort.Strings(out)
	return out
}
