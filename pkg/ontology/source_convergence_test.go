package ontology

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestProjectMarkdownNoteSourcePreservesMarkdownIdentityAndMtime(t *testing.T) {
	content := "---\nname: Roadmap\n---\n\n## Stories\n\n### First\n^story-one\n"
	doc, err := projectMarkdownNoteSource(notemeta.NoteSourceSnapshot{
		Path:        "projects/roadmap.md",
		Format:      noteformat.FormatID("markdown"),
		Content:     content,
		ContentHash: "raw-hash",
		Mtime:       1_700_000_000,
		Frontmatter: map[string]any{"name": "Roadmap"},
	})
	require.NoError(t, err)
	require.Equal(t, content, doc.Content)
	require.Equal(t, "Roadmap", doc.Frontmatter["name"])
	require.Equal(t, time.Unix(1_700_000_000, 0), doc.Snapshot.ModTime)
	require.Equal(t, hashText(content), doc.Snapshot.ContentFingerprint)
	require.NotEmpty(t, doc.Snapshot.Sections)
}

func TestProjectMarkdownNoteSourceRejectsFutureProviderSource(t *testing.T) {
	_, err := projectMarkdownNoteSource(notemeta.NoteSourceSnapshot{
		Path:    "docs/guide.future",
		Format:  noteformat.FormatID("future"),
		Content: "# This must not be parsed as Markdown\n",
	})
	require.ErrorContains(t, err, "requires Markdown source")
}

func TestFullAndIncrementalOntologyBuildsConvergeAcrossRenameDelete(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project @node(paths: ["projects/*.md"]) {
  name: String!
  decision: Decision @link(source: "decision")
}

type Decision @node(paths: ["decisions/*.md"]) {
  name: String!
}
`)
	writeOntologyNote(t, root, "projects/roadmap.md", `---
name: Roadmap
decision: "[[choice]]"
---
`)
	writeOntologyNote(t, root, "decisions/choice.md", `---
name: API Choice
---
`)
	writeOntologyNote(t, root, "notes/plain.md", "# Plain\n\nUntyped body.\n")

	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	ctx := context.Background()
	vaultDef := obsidian.VaultDefinition{Path: root}
	note := &obsidian.Note{}
	_, err = testNoteMetadataIndexer(t).EnsureIndexed(ctx, vaultDef, note, store)
	require.NoError(t, err)
	result, err := SyncPaths(ctx, testNoteMetadataIndexer(t), vaultDef, note, store, nil, nil, nil)
	require.NoError(t, err)
	require.True(t, result.Rebuilt)
	assertOntologyBuildMatchesStore(t, ctx, vaultDef, note, store)

	require.NoError(t, os.Rename(
		filepath.Join(root, "decisions", "choice.md"),
		filepath.Join(root, "decisions", "selected.md"),
	))
	writeOntologyNote(t, root, "projects/roadmap.md", `---
name: Roadmap
decision: "[[selected]]"
---
`)
	require.NoError(t, testNoteMetadataIndexer(t).SyncPaths(ctx, vaultDef, note, store,
		[]string{"projects/roadmap.md", "decisions/selected.md"},
		[]string{"decisions/choice.md"},
	))
	_, err = SyncPaths(ctx, testNoteMetadataIndexer(t), vaultDef, note, store, nil,
		[]string{"projects/roadmap.md", "decisions/selected.md"},
		[]string{"decisions/choice.md"},
	)
	require.NoError(t, err)
	assertOntologyBuildMatchesStore(t, ctx, vaultDef, note, store)

	nodes, err := store.OntologyNodesByPaths(ctx, []string{"notes/plain.md", "decisions/choice.md"})
	require.NoError(t, err)
	require.NotEmpty(t, nodes)
	for _, node := range nodes {
		require.Equal(t, "notes/plain.md", node.NotePath)
	}
	require.Condition(t, func() bool {
		for _, node := range nodes {
			if node.TypeName == FallbackNoteTypeName {
				return true
			}
		}
		return false
	})
}

func TestSyncPathsHonorsCanceledContextBeforeProjection(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `type Project @node(paths: ["projects/*.md"]) { name: String! }`)
	writeOntologyNote(t, root, "projects/roadmap.md", "---\nname: Roadmap\n---\n")
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	vaultDef := obsidian.VaultDefinition{Path: root}
	require.NoError(t, testNoteMetadataIndexer(t).SyncPaths(context.Background(), vaultDef, &obsidian.Note{}, store, []string{"projects/roadmap.md"}, nil))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = SyncPaths(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store, nil, []string{"projects/roadmap.md"}, nil)
	require.ErrorIs(t, err, context.Canceled)
}

func assertOntologyBuildMatchesStore(t *testing.T, ctx context.Context, vaultDef obsidian.VaultDefinition, note obsidian.NoteReader, store *semdb.Store) {
	t.Helper()
	schema, err := LoadSchema(vaultDef.BasePath())
	require.NoError(t, err)
	state, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	build, err := BuildIndexWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, note, store, schema, state.NotesHash)
	require.NoError(t, err)
	paths, err := store.CurrentNoteMetadataPaths(ctx)
	require.NoError(t, err)

	storedNodes, err := store.OntologyNodesByPaths(ctx, paths)
	require.NoError(t, err)
	storedNodeIDs := make([]string, 0, len(storedNodes))
	for _, node := range storedNodes {
		storedNodeIDs = append(storedNodeIDs, node.NodeID)
	}
	storedFields, err := store.OntologyNodeFieldValuesByNodeIDs(ctx, storedNodeIDs, nil)
	require.NoError(t, err)
	storedEdges, err := store.OntologyEdgesForPaths(ctx, paths, true, "", 0)
	require.NoError(t, err)
	storedTypes, err := store.OntologyTypesByPaths(ctx, paths)
	require.NoError(t, err)
	storedAssessments, err := store.OntologyAssessmentsByPaths(ctx, paths)
	require.NoError(t, err)
	storedStates, err := store.OntologyNoteStatesByPaths(ctx, paths)
	require.NoError(t, err)

	require.Equal(t, nodeSignatures(build.Nodes), nodeSignatures(storedNodes))
	require.Equal(t, fieldSignatures(build.NodeFieldValues), fieldSignatures(storedFields))
	require.Equal(t, edgeSignatures(build.Edges), edgeSignatures(storedEdges))
	require.Equal(t, typeSignatures(build.NoteTypes), typeMapSignatures(storedTypes))
	require.Equal(t, assessmentSignatures(build.AssessmentRows), assessmentMapSignatures(storedAssessments))
	require.Equal(t, stateSignatures(build.NoteStates), stateMapSignatures(storedStates))
}

func nodeSignatures(rows []codeanchor.IntelOntologyNode) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%d\x00%d\x00%s", row.NodeID, row.NotePath, row.NodeRefJSON, row.NodeKind, row.TypeName, row.ParentNodeID, row.SourceLocator, row.Fragment, row.BlockID, row.StartByte, row.EndByte, row.StructuralFingerprint))
	}
	sort.Strings(out)
	return out
}

func fieldSignatures(rows []codeanchor.IntelOntologyNodeFieldValue) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%d", row.NodeID, row.FieldName, row.FieldKind, row.ValueText, row.ValueNorm, row.TargetNodeID, row.TargetNotePath, row.ListOrdinal))
	}
	sort.Strings(out)
	return out
}

func edgeSignatures(rows []semdb.OntologyEdgeRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%t", row.SrcPath, row.SrcNodeID, row.RelationName, row.DstPath, row.DstNodeID, row.DstType, row.Provenance, row.Structural))
	}
	sort.Strings(out)
	return out
}

func typeSignatures(rows []semdb.OntologyNoteTypeRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.NotePath+"\x00"+row.TypeName)
	}
	sort.Strings(out)
	return out
}

func typeMapSignatures(rows map[string]semdb.OntologyNoteTypeRow) []string {
	out := make([]semdb.OntologyNoteTypeRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, row)
	}
	return typeSignatures(out)
}

func assessmentSignatures(rows []semdb.OntologyNoteAssessmentRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.NotePath+"\x00"+row.DeclaredType+"\x00"+row.ResolvedType+"\x00"+row.AssessmentJSON)
	}
	sort.Strings(out)
	return out
}

func assessmentMapSignatures(rows map[string]semdb.OntologyNoteAssessmentRow) []string {
	out := make([]semdb.OntologyNoteAssessmentRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, row)
	}
	return assessmentSignatures(out)
}

func stateSignatures(rows []semdb.OntologyNoteStateRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.NotePath+"\x00"+row.InputFingerprint+"\x00"+row.ResolvedType)
	}
	sort.Strings(out)
	return out
}

func stateMapSignatures(rows map[string]semdb.OntologyNoteStateRow) []string {
	out := make([]semdb.OntologyNoteStateRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, row)
	}
	return stateSignatures(out)
}
