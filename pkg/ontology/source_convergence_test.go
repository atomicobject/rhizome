package ontology

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
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
	_, err = testkit.NoteMetadataIndexer(t).EnsureIndexed(ctx, vaultDef, note, store)
	require.NoError(t, err)
	result, err := syncPathsForTest(ctx, testkit.NoteMetadataIndexer(t), vaultDef, note, store, nil, nil, nil)
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
	require.NoError(t, testkit.NoteMetadataIndexer(t).SyncPaths(ctx, vaultDef, note, store,
		[]string{"projects/roadmap.md", "decisions/selected.md"},
		[]string{"decisions/choice.md"},
	))
	_, err = syncPathsForTest(ctx, testkit.NoteMetadataIndexer(t), vaultDef, note, store, nil,
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
			if node.TypeName == ontoschema.FallbackNoteTypeName {
				return true
			}
		}
		return false
	})
}

func TestFullAndIncrementalOntologyValidationConverge(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
 type Story implements Section @node(locator: EMBEDDED) {
   id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
   status: String! @field
 }
 type Stories implements Section { stories: [Story!] @contains(level: H3, min: 2, max: 2) }
 type Spec @node(paths: ["specs/*.md"]) {
   name: String!
   peer: Spec @link(inverse: "peer")
   stories: Stories @contains(level: H2, heading: "Stories")
 }
 type Task implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["notes/*.md"]) {
  owner: String! @field
 }
 `)
	const content = "---\nname: Example\npeer: two\n---\n\n## Stories\n\n### First\nid:: SPEC-0001.US1\n^first\n"
	notes := map[string]string{
		"specs/one.md":   content,
		"specs/two.md":   "---\nname: Second\n---\n\n## Stories\n\n### Second\nid:: SPEC-0002.US1\nstatus:: ready\n^second\n\n### Third\nid:: SPEC-0002.US2\nstatus:: ready\n^third\n\n### Fourth\nid:: SPEC-0002.US3\nstatus:: ready\n^fourth\n",
		"notes/plain.md": "# Plain\n\n- [ ] Missing owner #action-item ^task\n\n[[one]]\n",
	}
	paths := []string{"specs/one.md", "specs/two.md", "notes/plain.md"}
	for _, path := range paths {
		writeOntologyNote(t, root, path, notes[path])
	}
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	ctx := context.Background()
	vault := obsidian.VaultDefinition{Path: root}
	note := &obsidian.Note{}
	_, err = testkit.NoteMetadataIndexer(t).EnsureIndexed(ctx, vault, note, store)
	require.NoError(t, err)
	queue := &recordingDeltaQueue{store: store}
	result, err := syncPublishedPathsForTest(ctx, testkit.NoteMetadataIndexer(t), vault, note, store, queue, nil, nil)
	require.NoError(t, err)
	require.True(t, result.Rebuilt)
	assertOntologyBuildMatchesStore(t, ctx, vault, note, store)
	for _, code := range []string{"contains_min_not_met", "contains_max_exceeded", "missing_required_field", "inverse_mismatch"} {
		require.Contains(t, issueCodes(result.Issues), code)
	}
	for i, path := range paths {
		writeOntologyNote(t, root, path, notes[path]+fmt.Sprintf("\nChanged %d.\n", i))
		require.NoError(t, testkit.NoteMetadataIndexer(t).SyncPaths(ctx, vault, note, store, []string{path}, nil))
		_, err = syncPublishedPathsForTest(ctx, testkit.NoteMetadataIndexer(t), vault, note, store, queue, []string{path}, nil)
		require.NoError(t, err)
		assertOntologyBuildMatchesStore(t, ctx, vault, note, store)
	}
}

func TestOntologyCatalogUsesProviderTypeResolution(t *testing.T) {
	for _, fixture := range []struct {
		name, matcher, title string
		tags                 []string
	}{
		{name: "provider tags", matcher: "tag:provider-only", tags: []string{"provider-only"}},
		{name: "provider title", matcher: "find:ProviderUnique", title: "ProviderUnique"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			writeOntologyTestConfig(t, root)
			writeOntologySchema(t, root, fmt.Sprintf(`type Subject @node(matches: [%q]) { name: String }`, fixture.matcher))
			schema, err := ontoschema.LoadSchema(root)
			require.NoError(t, err)
			source := mixedFormatSource(t, markdown.New(), "notes/source.md", []byte("# Different source title\n"))
			source.Title, source.Tags = fixture.title, fixture.tags
			result, err := BuildIndexFromNoteSources(context.Background(), obsidian.VaultDefinition{Path: root}, []notemeta.NoteSourceSnapshot{source}, schema, "notes")
			require.NoError(t, err)
			require.Len(t, result.NoteTypes, 1)
			require.Equal(t, "Subject", result.NoteTypes[0].TypeName)
			require.Equal(t, result.NoteTypes[0].TypeName, result.Nodes[0].TypeName)
		})
	}
}

func TestOntologyAssessmentBuildIsDeterministic(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
 type Item implements Section @node(locator: EMBEDDED) { status: String! @field }
 type Group implements Section { items: [Item!] @contains(level: H3) }
 type Subject @node(paths: ["notes/*.md"]) {
  first: Group @contains(level: H2, heading: "First")
  second: Group @contains(level: H2, heading: "Second")
 }
 `)
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	source := mixedFormatSource(t, markdown.New(), "notes/source.md", []byte("## First\n\n### One\n^one\n\n## Second\n\n### Two\n^two\n"))
	var expected string
	for i := 0; i < 20; i++ {
		result, err := BuildIndexFromNoteSources(context.Background(), obsidian.VaultDefinition{Path: root}, []notemeta.NoteSourceSnapshot{source}, schema, "notes")
		require.NoError(t, err)
		got := mustJSON(struct {
			Assessments []NoteAssessment
			Issues      []ValidationIssue
		}{result.Assessments, result.ValidationIssues})
		if i == 0 {
			expected = got
		} else {
			require.Equal(t, expected, got)
		}
	}
}

func issueCodes(issues []ValidationIssue) []string {
	codes := make([]string, 0, len(issues))
	for _, issue := range issues {
		codes = append(codes, issue.Code)
	}
	return codes
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
	require.NoError(t, testkit.NoteMetadataIndexer(t).SyncPaths(context.Background(), vaultDef, &obsidian.Note{}, store, []string{"projects/roadmap.md"}, nil))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = syncPathsForTest(ctx, testkit.NoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store, nil, []string{"projects/roadmap.md"}, nil)
	require.ErrorIs(t, err, context.Canceled)
}

func assertOntologyBuildMatchesStore(t *testing.T, ctx context.Context, vaultDef obsidian.VaultDefinition, note obsidian.NoteReader, store *semdb.Store) {
	t.Helper()
	schema, err := ontoschema.LoadSchema(vaultDef.BasePath())
	require.NoError(t, err)
	state, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	build, err := BuildIndexWithStore(ctx, testkit.NoteMetadataIndexer(t), vaultDef, note, store, schema, state.NotesHash)
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

	for i := range build.Nodes {
		build.Nodes[i].UpdatedAt = 0
	}
	for i := range storedNodes {
		storedNodes[i].UpdatedAt = 0
	}
	for i := range build.NodeFieldValues {
		build.NodeFieldValues[i].UpdatedAt = 0
	}
	for i := range storedFields {
		storedFields[i].UpdatedAt = 0
	}
	for i := range build.Edges {
		build.Edges[i].UpdatedAt = 0
	}
	for i := range storedEdges {
		storedEdges[i].UpdatedAt = 0
	}
	require.ElementsMatch(t, build.Nodes, storedNodes)
	require.ElementsMatch(t, build.NodeFieldValues, storedFields)
	require.ElementsMatch(t, build.Edges, storedEdges)
	require.Equal(t, typeSignatures(build.NoteTypes), typeMapSignatures(storedTypes))
	require.Equal(t, assessmentSignatures(build.AssessmentRows), assessmentMapSignatures(storedAssessments))
	require.Equal(t, stateSignatures(build.NoteStates), stateMapSignatures(storedStates))
	ontologyState, err := store.GetOntologySchemaState(ctx)
	require.NoError(t, err)
	require.Equal(t, build.ValidationIssues, decodeIssues(ontologyState.ErrorJSON))
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

func TestGlobalSourceLinkTargetDiscoveryScalesLinearly(t *testing.T) {
	schema := loadEmbeddedValidationSchema(t)
	measure := func(count int) float64 {
		snapshot, _ := buildEmbeddedValidationCase(t, schema, "notes/scratch.md", count)
		doc := &noteDoc{Path: snapshot.NotePath, Content: snapshot.Content, Frontmatter: snapshot.Frontmatter, Snapshot: snapshot}
		cache := obsidian.BuildNotePathCache([]string{snapshot.NotePath})
		return testing.AllocsPerRun(1, func() {
			projected, err := projectResolvedNote(doc, schema, resolveNoteAssessment(doc, schema))
			require.NoError(t, err)
			require.Len(t, projected.resolver.globalSourceNodeRefs(), count)
			targets, err := globalSourceLinkTargets(projected, schema, cache)
			require.NoError(t, err)
			require.Empty(t, targets)
		})
	}
	small, large := measure(500), measure(2000)
	require.Less(t, large, small*6, "four times as many source nodes must not rebuild one whole-document resolver per node")
}
