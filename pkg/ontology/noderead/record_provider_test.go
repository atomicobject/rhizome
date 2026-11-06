package noderead

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit/structuralfixture"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

const recordProviderSDL = `type Document @node(default: true) { status: String }
type Row @node(locator: EMBEDDED) @source(shape: RECORD, paths: ["*.native"], select: "rows", key: {field: "id"}, title: ["name"], space: "test", logicalKind: "row") {
 name: String @field(path: "name")
 empty: String @field(path: "empty")
 children: [Child!] @contains(shape: RECORD, select: "children")
}
type Child @node(locator: EMBEDDED) @source(shape: RECORD, paths: ["*.native"], select: "rows[].children", key: null, title: ["name"], container: "Row", space: "test", logicalKind: "child") { name: String @field(path: "name") }`

func TestRecordProviderSummaryAndRichReadsShareOneCapture(t *testing.T) {
	vault, store, schema := buildFixture(t, recordProviderSDL, "# Markdown remains separate\n")
	provider, err := structuralfixture.New(map[string]any{"rows": []any{
		map[string]any{"id": "A", "name": "First", "empty": "", "children": []any{map[string]any{"name": "Child"}}},
		map[string]any{"id": "B", "name": "Second"},
	}})
	require.NoError(t, err)
	registry, err := noteformat.NewRegistry(provider)
	require.NoError(t, err)
	formats, err := noteformat.NewRuntime(registry, provider)
	require.NoError(t, err)
	content := "# This is native source, never a Markdown heading\n"
	path := "model.native"
	require.NoError(t, os.WriteFile(filepath.Join(vault.Path, path), []byte(content), 0o644))
	source, err := noteformat.NewAuthoredSource(paths.NotePath(path), provider.Descriptor(), []byte(content), 1)
	require.NoError(t, err)
	projection, err := formats.Project(source)
	require.NoError(t, err)
	root := &ontology.RootDocumentSnapshot{NotePath: paths.NotePath(path), Format: "synthetic", RawSource: []byte(content), ContentHash: source.ContentHash(), Projection: projection, Root: ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote}}
	prepared, err := ontology.PrepareDocument(root, schema)
	require.NoError(t, err)
	rootNode, err := prepared.Project(root.Root)
	require.NoError(t, err)
	model, err := ontology.BuildIntelOntologyNodeReadModel(schema, rootNode, 1)
	require.NoError(t, err)
	require.NoError(t, store.ReplaceOntologyNodeReadModel(t.Context(), model))
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(t.Context(), semdb.NoteMetadataSnapshot{State: semdb.NoteMetadataState{Ready: true, LoadedAt: 1}, Notes: []semdb.NoteMetadataRow{{Path: path, FormatID: "synthetic", ContentHash: "old-index-hash", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent, ProviderVersion: "test-v1", ProjectionVersion: "test-v1", SourceContentHash: "old-index-hash"}}}}))
	reader := &structuralfixture.Reader{Note: &obsidian.Note{}, Paths: []string{path}}
	provider.Calls.Store(0)
	scope := NewService(vault, reader, store, schema).WithNoteFormats(formats).NewScope(ScopeOptions{})
	nodes, err := prepared.Nodes()
	require.NoError(t, err)
	var refs []ontology.NodeRef
	for _, node := range nodes {
		if node.Record != nil {
			refs = append(refs, node.Ref)
		}
	}
	require.Len(t, refs, 3)
	summary, err := scope.Hydrate(t.Context(), refs, HydrateOptions{Profile: HydrateSummary})
	require.NoError(t, err)
	require.Len(t, summary, 3)
	require.Zero(t, reader.Reads.Load())
	require.Zero(t, provider.Calls.Load())
	rich, err := scope.Hydrate(t.Context(), refs, HydrateOptions{Profile: HydrateContent})
	require.NoError(t, err)
	require.Len(t, rich, 3)
	require.EqualValues(t, 1, reader.Reads.Load())
	require.EqualValues(t, 1, provider.Calls.Load())
	var shared *ontology.PreparedDocument
	for _, ref := range refs {
		node, err := scope.Projection(t.Context(), ref)
		require.NoError(t, err)
		require.Nil(t, node.Snapshot)
		if shared == nil {
			shared = node.Prepared
		} else {
			require.Same(t, shared, node.Prepared)
		}
		require.Equal(t, source.ContentHash(), node.SourceContent().SourceHash)
		workspace := ontology.BuildNodeWorkspaceFromProjectionWithSchema(schema, node)
		require.False(t, workspace.Capabilities.CanEdit)
		require.NotNil(t, workspace.Content.Source)
		if node.Type.Name == "Row" && node.DisplayTitle() == "First" {
			require.True(t, workspace.Capabilities.CanNavigateChildren)
		}
	}
	require.EqualValues(t, 1, reader.Reads.Load())
	require.EqualValues(t, 1, provider.Calls.Load())
	require.NotNil(t, rich[0].SourceContent)
	copy := rich[0].SourceContent.Copy()
	copy.Value.Entries = nil
	again, err := scope.Hydrate(t.Context(), refs, HydrateOptions{Profile: HydrateContent})
	require.NoError(t, err)
	require.NotEmpty(t, again[0].SourceContent.Value.Entries)
	links, err := scope.Locators(t.Context(), refs)
	require.NoError(t, err)
	for _, ref := range refs {
		locator := links[ontology.NodeSourceLocator(ref)]
		require.Empty(t, locator.FixActions)
		require.NotNil(t, locator.LinkTarget)
		require.Empty(t, locator.LinkTarget.Wikilink)
	}
	// A positional address from this generation must fail after any host edit.
	var positional ontology.NodeRef
	for _, ref := range refs {
		if ref.TypeName == "Child" {
			positional = ref
		}
	}
	require.NoError(t, os.WriteFile(filepath.Join(vault.Path, path), []byte(content+"changed\n"), 0o644))
	fresh := NewService(vault, reader, store, schema).WithNoteFormats(formats).NewScope(ScopeOptions{})
	_, err = fresh.Projection(t.Context(), positional)
	require.ErrorContains(t, err, "record_locator_stale")
	missing := positional
	missing.Fragment = "record:v1:missing"
	missing.NodeID = ""
	_, err = fresh.Projection(t.Context(), missing)
	require.Error(t, err)
}
