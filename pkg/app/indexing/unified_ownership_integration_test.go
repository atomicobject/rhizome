package indexing

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/noteownership"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// This tests the filesystem-to-transition boundary that RunUnifiedCore must
// consume before choosing its code or semantic execution paths. It deliberately
// uses no configured code roots: an explicit descriptor-only HTML include must
// still become a note ownership transition, but must not enter either legacy
// Markdown ingestion or code discovery.
func TestUnifiedOwnershipPrefix_NoCodeRootsPublishesHTMLSourceOnly(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeOwnershipFile(t, root, "docs/Release.HTML")
	runtime := ownershipTransitionRuntime(t)

	snapshot, err := noteownership.Discover(context.Background(), noteownership.DiscoveryInput{
		VaultDefinition: obsidian.VaultDefinition{
			Root:     root,
			Includes: []string{"docs/*.html"},
		},
		Registry:     runtime.Registry(),
		CodeLanguage: ownershipTestLanguage,
	})
	require.NoError(t, err)
	run := snapshot
	require.Empty(t, run.CodeCandidates())
	require.Empty(t, run.CodeCandidates())
	require.Empty(t, markdownCandidatePaths(run.PresentNoteCandidates()))
	require.Equal(t, []paths.NotePath{"docs/Release.HTML"}, run.NoteKeepPaths())

	plan, err := noteownership.BuildTransitionPlan(context.Background(), noteownership.TransitionInput{
		VaultPaths: ownershipTransitionVaultPaths(t, root),
		Snapshot:   snapshot,
		Runtime:    runtime,
		ObservedAt: 100,
	})
	require.NoError(t, err)
	transitions := plan.Transitions()
	require.Len(t, transitions, 1)
	require.Equal(t, "docs/Release.HTML", transitions[0].Path)
	require.Equal(t, semdb.OwnershipTargetNote, transitions[0].Target)
	require.NotNil(t, transitions[0].Note)
	require.Equal(t, "html", transitions[0].Note.FormatID)
	require.Equal(t, semdb.NoteProjectionStatusStale, transitions[0].Note.Status)
	require.NotEmpty(t, transitions[0].Note.ContentHash)
	_, cached := plan.Source("docs/Release.HTML")
	require.False(t, cached, "descriptor-only HTML must not enter a projector-bound source cache")
}

// This is the end-to-end guard for the removed no-code-root semantic fallback.
// A trusted, explicitly included HTML root must persist source identity even
// when no code or embedding lane is enabled; it remains absent from code state.
func TestRunUnifiedCore_NoCodeRootsAndDisabledEmbeddingsPersistsHTMLSourceOnly(t *testing.T) {
	root := t.TempDir()
	writeOwnershipFile(t, root, "docs/Release.HTML")
	ontologyDir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(ontologyDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ontologyDir, "schema.graphql"), []byte(`
type Record @node(paths: ["records/*.md"]) {
  title: String
}
`), 0o600))
	require.NoError(t, obsidian.SaveLocalConfig(root, obsidian.LocalConfig{
		Notes:          obsidian.LocalVaultConfig{Includes: []string{"docs/*.html"}},
		NoteEmbeddings: &embeddings.Config{Enabled: false},
		CodeEmbeddings: &embeddings.Config{Enabled: false},
	}))
	definition := obsidian.VaultDefinition{Root: root, Includes: []string{"docs/*.html"}}

	require.NoError(t, RunUnifiedCore(context.Background(), UnifiedOptions{
		VaultPath:    root,
		VaultDef:     definition,
		NoteMetadata: descriptorOnlyHTMLNoteMetadataIndexer(t),
	}))

	store, cleanup, err := obsidian.OpenIntelStore(root, false)
	require.NoError(t, err)
	require.NotNil(t, store)
	debt, err := store.PendingDerivedWork(context.Background(), time.Now(), 64)
	require.NoError(t, err)
	require.Empty(t, debt, "disabled semantic kinds and completed graph must be acknowledged")
	unready, err := store.HasUnreadyDerivedWork(context.Background())
	require.NoError(t, err)
	require.False(t, unready)

	require.NotNil(t, store)
	t.Cleanup(cleanup)
	notes, err := store.NotePaths(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"docs/Release.HTML"}, notes)
	codePaths, err := store.IndexedFilePaths(context.Background())
	require.NoError(t, err)
	require.Empty(t, codePaths)
	generation, pending, err := store.PendingOwnershipReconciliation(context.Background())
	require.NoError(t, err)
	require.Positive(t, generation)
	require.False(t, pending)

	var formatID, status, providerVersion, projectionVersion string
	require.NoError(t, store.DB().QueryRowContext(context.Background(), `
		SELECT n.format_id, p.status, p.provider_version, p.projection_version
		FROM notes n
		JOIN note_projection_state p ON p.note_id = n.id
		WHERE n.path = ?
	`, "docs/Release.HTML").Scan(&formatID, &status, &providerVersion, &projectionVersion))
	require.Equal(t, "html", formatID)
	require.Equal(t, string(semdb.NoteProjectionStatusStale), status)
	require.NotEmpty(t, providerVersion)
	require.NotEmpty(t, projectionVersion)

	var symbols, docLinks int
	require.NoError(t, store.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM symbols`).Scan(&symbols))
	require.NoError(t, store.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM doc_links`).Scan(&docLinks))
	require.Zero(t, symbols)
	require.Zero(t, docLinks)

	var ontologyNodes, ontologyStates, ontologyTypes, ontologyAssessments, ontologyEdges, ontologyFields int
	require.NoError(t, store.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM ontology_nodes WHERE note_path = ?`, "docs/Release.HTML").Scan(&ontologyNodes))
	require.NoError(t, store.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM ontology_note_state WHERE note_path = ?`, "docs/Release.HTML").Scan(&ontologyStates))
	require.NoError(t, store.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM ontology_note_types WHERE note_path = ?`, "docs/Release.HTML").Scan(&ontologyTypes))
	require.NoError(t, store.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM ontology_note_assessments WHERE note_path = ?`, "docs/Release.HTML").Scan(&ontologyAssessments))
	require.NoError(t, store.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM ontology_edges WHERE src_path = ? OR dst_path = ?`, "docs/Release.HTML", "docs/Release.HTML").Scan(&ontologyEdges))
	require.NoError(t, store.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM ontology_node_field_values WHERE note_path = ?`, "docs/Release.HTML").Scan(&ontologyFields))
	require.Zero(t, ontologyNodes)
	require.Zero(t, ontologyStates)
	require.Zero(t, ontologyTypes)
	require.Zero(t, ontologyAssessments)
	require.Zero(t, ontologyEdges)
	require.Zero(t, ontologyFields)
}

// Markdown keeps its existing provider-derived metadata contract when it
// shares one ownership run with a descriptor-only HTML root. HTML contributes
// durable source identity only; it must not borrow Markdown-derived facts.
func TestRunUnifiedCore_MarkdownParityAlongsideDescriptorOnlyHTML(t *testing.T) {
	root := t.TempDir()
	writeOwnershipFile(t, root, "docs/Reference.html")
	writeOwnershipFile(t, root, "notes/Target.md")
	decisionPath := filepath.Join(root, "notes", "Decision.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(decisionPath), 0o755))
	require.NoError(t, os.WriteFile(decisionPath, []byte(`---
title: Frontmatter Decision
status: accepted
tags: [team/alpha]
---
# Ignored Heading

See [[Target]].
`), 0o600))
	require.NoError(t, obsidian.SaveLocalConfig(root, obsidian.LocalConfig{
		Notes:          obsidian.LocalVaultConfig{Includes: []string{"notes/**/*.md", "docs/*.html"}},
		NoteEmbeddings: &embeddings.Config{Enabled: false},
		CodeEmbeddings: &embeddings.Config{Enabled: false},
	}))
	definition := obsidian.VaultDefinition{Root: root, Includes: []string{"notes/**/*.md", "docs/*.html"}}
	require.NoError(t, RunUnifiedCore(context.Background(), UnifiedOptions{
		VaultPath:    root,
		VaultDef:     definition,
		NoteMetadata: descriptorOnlyHTMLNoteMetadataIndexer(t),
	}))

	store, cleanup, err := obsidian.OpenIntelStore(root, false)
	require.NoError(t, err)
	require.NotNil(t, store)
	t.Cleanup(cleanup)
	rows, err := store.CurrentNoteMetadataRows(context.Background())
	require.NoError(t, err)
	rowsByPath := make(map[string]semdb.NoteMetadataRow, len(rows))
	for _, row := range rows {
		rowsByPath[row.Path] = row
	}
	decision, found := rowsByPath["notes/Decision.md"]
	require.True(t, found)
	require.Equal(t, "Frontmatter Decision", decision.Title)
	require.Equal(t, "markdown", decision.FormatID)
	require.Equal(t, semdb.NoteProjectionStatusCurrent, decision.Projection.Status)
	require.Equal(t, decision.ContentHash, decision.Projection.SourceContentHash)

	properties, err := store.CurrentNotePropertyValues(context.Background(), []string{"notes/Decision.md"}, []string{"status"}, semdb.NotePropertySourceFrontmatter)
	require.NoError(t, err)
	require.Len(t, properties, 1)
	require.Equal(t, "accepted", properties[0].ValueText)
	tags, err := store.CurrentNoteTags(context.Background(), []string{"notes/Decision.md"})
	require.NoError(t, err)
	require.Len(t, tags, 1)
	require.Equal(t, "notes/Decision.md", tags[0].NotePath)
	require.Equal(t, "team/alpha", tags[0].TagNorm)
	edges, err := store.GraphDocNoteEdgesForPaths(context.Background(), []string{"notes/Decision.md"})
	require.NoError(t, err)
	require.Equal(t, []semdb.GraphDocEdge{{SrcPath: "notes/Decision.md", DstPath: "notes/Target.md", Kind: "wikilink", Weight: 1}}, edges)

	html, found := rowsByPath["docs/Reference.html"]
	require.True(t, found)
	require.Equal(t, "html", html.FormatID)
	require.Equal(t, semdb.NoteProjectionStatusStale, html.Projection.Status)
	htmlProperties, err := store.CurrentNotePropertyValues(context.Background(), []string{"docs/Reference.html"}, nil, 0)
	require.NoError(t, err)
	require.Empty(t, htmlProperties)
	htmlTargets, err := store.CurrentNoteFragmentTargets(context.Background(), []string{"docs/Reference.html"}, "", "")
	require.NoError(t, err)
	require.Empty(t, htmlTargets)
	htmlEdges, err := store.GraphDocNoteEdgesForPaths(context.Background(), []string{"docs/Reference.html"})
	require.NoError(t, err)
	require.Empty(t, htmlEdges)

	generation, pending, err := store.PendingOwnershipReconciliation(context.Background())
	require.NoError(t, err)
	require.Positive(t, generation)
	require.False(t, pending)
}

// An unchanged full run must leave the published metadata and ontology at one
// generation. The metadata no-op preserves this barrier instead of advancing
// LoadedAt without re-publishing ontology.
func TestRunUnifiedCore_UnchangedPublishedStateStaysReady(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type First @node(matches: ["tag:overlap"]) { title: String }
type Second @node(matches: ["tag:overlap"]) { title: String }
type Decision @node(paths: ["notes/Decision.md"]) {
  title: String
}
`), 0o600))
	writeOwnershipFile(t, root, "docs/Reference.html")
	decisionPath := filepath.Join(root, "notes", "Decision.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(decisionPath), 0o755))
	require.NoError(t, os.WriteFile(decisionPath, []byte("---\ntitle: Durable Decision\n---\n"), 0o600))
	for name, content := range map[string]string{"Blank.md": "", "Space.md": " \n", "Ambiguous.md": "---\ntags: [overlap]\n---\n"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, "notes", name), []byte(content), 0o600))
	}
	require.NoError(t, obsidian.SaveLocalConfig(root, obsidian.LocalConfig{
		Notes:          obsidian.LocalVaultConfig{Includes: []string{"notes/**/*.md", "docs/*.html"}},
		NoteEmbeddings: &embeddings.Config{Enabled: false},
		CodeEmbeddings: &embeddings.Config{Enabled: false},
	}))
	definition := obsidian.VaultDefinition{Root: root, Includes: []string{"notes/**/*.md", "docs/*.html"}}
	indexer := descriptorOnlyHTMLNoteMetadataIndexer(t)
	require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: indexer}))

	store, cleanup, err := obsidian.OpenIntelStore(root, false)
	require.NoError(t, err)
	firstNoteState, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	firstOntologyState, err := store.GetOntologySchemaState(ctx)
	require.NoError(t, err)
	require.Equal(t, firstNoteState.LoadedAt, firstOntologyState.LoadedAt)
	paths := []string{"notes/Decision.md", "notes/Blank.md", "notes/Space.md", "notes/Ambiguous.md"}
	firstAssessments, err := store.OntologyAssessmentsByPaths(ctx, paths)
	require.NoError(t, err)
	firstNodes, err := store.OntologyNodesByPaths(ctx, paths)
	require.NoError(t, err)
	cleanup()

	require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: indexer}))
	store, cleanup, err = obsidian.OpenIntelStore(root, false)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	runtime, err := ontology.PublishedRuntimeWithStore(ctx, indexer, definition, store)
	require.NoError(t, err)
	require.True(t, runtime.Ready)
	secondNoteState, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	secondOntologyState, err := store.GetOntologySchemaState(ctx)
	require.NoError(t, err)
	require.Equal(t, firstNoteState.LoadedAt, secondNoteState.LoadedAt)
	require.Equal(t, secondNoteState.LoadedAt, secondOntologyState.LoadedAt)
	secondAssessments, err := store.OntologyAssessmentsByPaths(ctx, paths)
	require.NoError(t, err)
	secondNodes, err := store.OntologyNodesByPaths(ctx, paths)
	require.NoError(t, err)
	require.Equal(t, firstAssessments, secondAssessments, "warm run must preserve assessments without rewriting timestamps")
	require.Equal(t, firstNodes, secondNodes, "warm run must preserve catalog rows without rewriting timestamps")
}

// A published metadata delta carries the complete current projection state,
// but only the ownership transition for its changed source may bypass the
// Markdown mtime/content fast path.
func TestRunUnifiedCore_PublishedMetadataChangeForcesOnlyChangedMarkdownSource(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	changedPath := filepath.Join(root, "notes", "Changed.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(changedPath), 0o755))
	require.NoError(t, os.WriteFile(changedPath, []byte("# First\n"), 0o600))
	writeOwnershipFile(t, root, "notes/Unchanged.md")
	require.NoError(t, obsidian.SaveLocalConfig(root, obsidian.LocalConfig{
		Notes:          obsidian.LocalVaultConfig{Includes: []string{"notes/**/*.md"}},
		NoteEmbeddings: &embeddings.Config{Enabled: false},
		CodeEmbeddings: &embeddings.Config{Enabled: false},
	}))
	definition := obsidian.VaultDefinition{Root: root, Includes: []string{"notes/**/*.md"}}
	indexer := testNoteMetadataIndexer(t)
	require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: indexer}))

	require.NoError(t, os.WriteFile(changedPath, []byte("# Second\n"), 0o600))
	progress := &progressRecorder{}
	require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{
		VaultPath: root, VaultDef: definition, NoteMetadata: indexer, ProgressBar: progress, Verbose: true,
	}))
	require.Contains(t, progress.lines, "[index] Ingested 1 notes (1 unchanged)")
}

// A durable pending generation is recovered by an ordinary later full run.
// The test creates the marker through the store contract, then proves the
// production coordinator forces and republishes Markdown before acknowledging
// that exact outstanding generation.
func TestRunUnifiedCore_RecoversPendingOwnershipGeneration(t *testing.T) {
	root := t.TempDir()
	decisionPath := filepath.Join(root, "notes", "Decision.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(decisionPath), 0o755))
	require.NoError(t, os.WriteFile(decisionPath, []byte("---\nstatus: accepted\n---\n# Decision\n"), 0o600))
	require.NoError(t, obsidian.SaveLocalConfig(root, obsidian.LocalConfig{
		Notes:          obsidian.LocalVaultConfig{Includes: []string{"notes/**/*.md"}},
		NoteEmbeddings: &embeddings.Config{Enabled: false},
		CodeEmbeddings: &embeddings.Config{Enabled: false},
	}))
	definition := obsidian.VaultDefinition{Root: root, Includes: []string{"notes/**/*.md"}}
	indexer := testNoteMetadataIndexer(t)
	require.NoError(t, RunUnifiedCore(context.Background(), UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: indexer}))

	store, cleanup, err := obsidian.OpenIntelStore(root, false)
	require.NoError(t, err)
	rows, err := store.CurrentNoteMetadataRows(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	row := rows[0]
	pendingResult, err := store.ApplyOwnershipTransitions(context.Background(), []semdb.OwnershipTransition{{
		Path: "notes/Decision.md", Target: semdb.OwnershipTargetNote,
		Note: &semdb.NoteSourceState{
			Title: row.Title, FormatID: row.FormatID, ContentHash: "pending-recovery-source", Mtime: row.Mtime, Size: row.Size,
			ProviderVersion: row.Projection.ProviderVersion, ProjectionVersion: row.Projection.ProjectionVersion,
			Status: semdb.NoteProjectionStatusStale, ObservedAt: row.IndexedAt,
		},
	}})
	require.NoError(t, err)
	require.Positive(t, pendingResult.ReconciliationGeneration)
	_, pending, err := store.PendingOwnershipReconciliation(context.Background())
	require.NoError(t, err)
	require.True(t, pending)
	cleanup()

	require.NoError(t, RunUnifiedCore(context.Background(), UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: indexer}))
	store, cleanup, err = obsidian.OpenIntelStore(root, false)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	generation, pending, err := store.PendingOwnershipReconciliation(context.Background())
	require.NoError(t, err)
	require.Greater(t, generation, pendingResult.ReconciliationGeneration, "recovery published the actual changed source before acknowledgement")
	require.False(t, pending)
	rows, err = store.CurrentNoteMetadataRows(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, semdb.NoteProjectionStatusCurrent, rows[0].Projection.Status)
	properties, err := store.CurrentNotePropertyValues(context.Background(), []string{"notes/Decision.md"}, []string{"status"}, semdb.NotePropertySourceFrontmatter)
	require.NoError(t, err)
	require.Len(t, properties, 1)
	require.Equal(t, "accepted", properties[0].ValueText)
}

// A transition must consume the current ownership decision, not the previous
// durable owner. This composes the real discovery run with transition planning
// and catches a code path leaking into Markdown ingestion after note-to-code
// ownership changes.
func TestUnifiedOwnershipPrefix_NoteToCodeUsesOnlyCodeLane(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeOwnershipFile(t, root, "src/reclaimed.go")
	runtime := ownershipTransitionRuntime(t)

	snapshot, err := noteownership.Discover(context.Background(), noteownership.DiscoveryInput{
		VaultDefinition:    obsidian.VaultDefinition{Path: root},
		Registry:           runtime.Registry(),
		CodeRoots:          []paths.AbsPath{paths.AbsPath(filepath.Join(root, "src"))},
		CodeLanguage:       ownershipTestLanguage,
		PersistedNotePaths: []paths.NotePath{"src/reclaimed.go"},
	})
	require.NoError(t, err)
	run := snapshot
	require.Empty(t, markdownCandidatePaths(run.PresentNoteCandidates()))
	code := run.CodeCandidates()
	require.Len(t, code, 1)

	plan, err := noteownership.BuildTransitionPlan(context.Background(), noteownership.TransitionInput{
		VaultPaths: ownershipTransitionVaultPaths(t, root),
		Snapshot:   snapshot,
		Runtime:    runtime,
		ObservedAt: 101,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"src/reclaimed.go|code"}, ownershipTransitionShapes(plan.Transitions()))

	candidates := run.Candidates()
	require.Len(t, candidates, 1)
	require.Equal(t, notediscovery.Note, candidates[0].PreviousOwner)
	require.Equal(t, notediscovery.Code, candidates[0].Owner)
}

type unifiedOwnershipTestStore struct {
	notePaths  []string
	codePaths  []string
	generation int64
	pending    bool
}

func (s *unifiedOwnershipTestStore) NotePaths(context.Context) ([]string, error) {
	return append([]string(nil), s.notePaths...), nil
}

func (s *unifiedOwnershipTestStore) IndexedFilePaths(context.Context) ([]string, error) {
	return append([]string(nil), s.codePaths...), nil
}

func (s *unifiedOwnershipTestStore) PendingOwnershipReconciliation(context.Context) (int64, bool, error) {
	return s.generation, s.pending, nil
}

func ownershipTransitionBatches(batches [][]semdb.OwnershipTransition) [][]string {
	result := make([][]string, 0, len(batches))
	for _, batch := range batches {
		result = append(result, ownershipTransitionShapes(batch))
	}
	return result
}
