package indexing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/noteownership"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRunUnifiedCore_UnreadableMarkdownClearsDerivedEvidenceAndRecovers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not enforce POSIX unreadable file permissions")
	}
	ctx := context.Background()
	root := t.TempDir()
	writeUnifiedFatalFixture(t, root)
	definition := unifiedFatalDefinition(root)
	indexer := testNoteMetadataIndexer(t)

	require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: indexer}))
	assertUnifiedFatalFixtureDerivedEvidence(t, ctx, root, true)
	sourcePath := filepath.Join(root, "notes", "Source.md")
	require.NoError(t, os.Chmod(sourcePath, 0o000))
	t.Cleanup(func() { _ = os.Chmod(sourcePath, 0o600) })
	require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: indexer}))
	assertUnifiedFatalFixtureDerivedEvidence(t, ctx, root, false)
	assertUnifiedFatalProjection(t, ctx, root, noteownership.UnreadableSourceDiagnosticCode)
	require.NoError(t, os.Chmod(sourcePath, 0o600))
	require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: indexer}))
	assertUnifiedFatalFixtureDerivedEvidence(t, ctx, root, true)
	require.Equal(t, semdb.NoteProjectionStatusCurrent, unifiedFatalMetadataRows(t, ctx, root)["notes/Source.md"].Projection.Status)
}

func TestRunUnifiedCore_ProjectorFatalSkipsMarkdownIngestAndRecovers(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeUnifiedFatalFixture(t, root)
	definition := unifiedFatalDefinition(root)
	goodIndexer := testNoteMetadataIndexer(t)
	projector := &unifiedFatalProjector{}
	require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: goodIndexer}))
	assertUnifiedFatalFixtureDerivedEvidence(t, ctx, root, true)
	projector.fatal.Store(true)
	require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: unifiedFatalIndexer(t, projector)}))
	assertUnifiedFatalFixtureDerivedEvidence(t, ctx, root, false)
	assertUnifiedFatalProjection(t, ctx, root, "projector_fatal")
	require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: goodIndexer}))
	assertUnifiedFatalFixtureDerivedEvidence(t, ctx, root, true)
}

func TestRunUnifiedCore_PreparedFatalCommitSurvivesFailureAndRecovers(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeUnifiedFatalFixture(t, root)
	definition := unifiedFatalDefinition(root)
	goodIndexer := testNoteMetadataIndexer(t)
	projector := &unifiedFatalProjector{}
	require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: goodIndexer}))

	projector.fatal.Store(true)
	commitFailure := errors.New("stop after prepared fatal ownership commit")
	err := RunUnifiedCore(ctx, UnifiedOptions{
		VaultPath: root, VaultDef: definition, NoteMetadata: unifiedFatalIndexer(t, projector),
		afterPreparedOwnershipCommit: func(context.Context) error {
			return commitFailure
		},
	})
	require.ErrorIs(t, err, commitFailure)
	assertUnifiedFatalProjection(t, ctx, root, "projector_fatal")
	assertUnifiedFatalFixtureDerivedEvidence(t, ctx, root, false)
	assertUnifiedOwnershipPending(t, ctx, root, true)

	// A quiet repeat is a source-envelope no-op, but pending recovery still
	// forces destination work and acknowledges the committed generation.
	require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: unifiedFatalIndexer(t, projector)}))
	assertUnifiedFatalProjection(t, ctx, root, "projector_fatal")
	assertUnifiedOwnershipPending(t, ctx, root, false)

	projector.fatal.Store(false)
	currentFailure := errors.New("stop after prepared current ownership commit")
	err = RunUnifiedCore(ctx, UnifiedOptions{
		VaultPath: root, VaultDef: definition, NoteMetadata: goodIndexer,
		afterPreparedOwnershipCommit: func(context.Context) error {
			return currentFailure
		},
	})
	require.ErrorIs(t, err, currentFailure)
	assertUnifiedProjectionStatus(t, ctx, root, semdb.NoteProjectionStatusCurrent)
	assertUnifiedFatalFixtureDerivedEvidence(t, ctx, root, false)
	assertUnifiedOwnershipPending(t, ctx, root, true)

	require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: goodIndexer}))
	assertUnifiedFatalFixtureDerivedEvidence(t, ctx, root, true)
	require.Equal(t, semdb.NoteProjectionStatusCurrent, unifiedFatalMetadataRows(t, ctx, root)["notes/Source.md"].Projection.Status)
}

func writeUnifiedFatalFixture(t *testing.T, root string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte("type Decision @node(paths: [\"notes/*.md\"]) { status: String }\n"), 0o600))
	writeOwnershipFile(t, root, "notes/Target.md")
	source := "---\nstatus: active\ntags: [team/alpha]\ncode-anchors:\n  go:\n    - label: fatal-source-anchor\n      glob: pkg/**/*.go\n---\n# Source\n\nSee [[Target]].\n"
	sourcePath := filepath.Join(root, "notes", "Source.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(sourcePath), 0o755))
	require.NoError(t, os.WriteFile(sourcePath, []byte(source), 0o600))
	require.NoError(t, obsidian.SaveLocalConfig(root, obsidian.LocalConfig{Notes: obsidian.LocalVaultConfig{Includes: []string{"notes/**/*.md"}}, NoteEmbeddings: &embeddings.Config{Enabled: false}, CodeEmbeddings: &embeddings.Config{Enabled: false}}))
}

func unifiedFatalDefinition(root string) obsidian.VaultDefinition {
	return obsidian.VaultDefinition{Root: root, Includes: []string{"notes/**/*.md"}}
}

func assertUnifiedFatalFixtureDerivedEvidence(t *testing.T, ctx context.Context, root string, present bool) {
	t.Helper()
	store, cleanup, err := obsidian.OpenIntelStore(root, false)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	properties, err := store.CurrentNotePropertyValues(ctx, []string{"notes/Source.md"}, []string{"status"}, semdb.NotePropertySourceFrontmatter)
	require.NoError(t, err)
	tags, err := store.CurrentNoteTags(ctx, []string{"notes/Source.md"})
	require.NoError(t, err)
	targets, err := store.CurrentNoteFragmentTargets(ctx, []string{"notes/Source.md"}, "", "")
	require.NoError(t, err)
	edges, err := store.GraphDocNoteEdgesForPaths(ctx, []string{"notes/Source.md"})
	require.NoError(t, err)
	var anchors, sections, ontologyNodes, searchTerms int
	require.NoError(t, store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM note_anchors na JOIN notes n ON n.id = na.note_id WHERE n.path = ?`, "notes/Source.md").Scan(&anchors))
	require.NoError(t, store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_doc_sections WHERE path = ?`, "notes/Source.md").Scan(&sections))
	require.NoError(t, store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM ontology_nodes WHERE note_path = ?`, "notes/Source.md").Scan(&ontologyNodes))
	require.NoError(t, store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM note_search_terms terms JOIN notes n ON n.id = terms.note_id WHERE n.path = ?`, "notes/Source.md").Scan(&searchTerms))
	if present {
		require.NotEmpty(t, properties)
		require.NotEmpty(t, tags)
		require.NotEmpty(t, targets)
		require.NotEmpty(t, edges)
		require.Positive(t, anchors)
		require.Positive(t, sections)
		require.Positive(t, ontologyNodes)
		require.Positive(t, searchTerms)
		return
	}
	require.Empty(t, properties)
	require.Empty(t, tags)
	require.Empty(t, targets)
	require.Empty(t, edges)
	require.Zero(t, anchors)
	require.Zero(t, sections)
	require.Zero(t, ontologyNodes)
	require.Zero(t, searchTerms)
}

func assertUnifiedFatalProjection(t *testing.T, ctx context.Context, root, diagnostic string) {
	t.Helper()
	row := unifiedFatalDurableRow(t, ctx, root)
	require.Equal(t, semdb.NoteProjectionStatusFatal, row.Projection.Status)
	require.Equal(t, diagnostic, row.Projection.DiagnosticCode)
	require.NotEmpty(t, row.Projection.DiagnosticDetail)
}

func assertUnifiedProjectionStatus(t *testing.T, ctx context.Context, root string, want semdb.NoteProjectionStatus) {
	t.Helper()
	row := unifiedFatalDurableRow(t, ctx, root)
	require.Equal(t, want, row.Projection.Status)
}

func unifiedFatalDurableRow(t *testing.T, ctx context.Context, root string) semdb.NoteMetadataRow {
	t.Helper()
	store, cleanup, err := obsidian.OpenIntelStore(root, false)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	rows, err := store.DurableNoteMetadataRowsByPaths(ctx, []string{"notes/Source.md"})
	require.NoError(t, err)
	row, found := rows["notes/Source.md"]
	require.True(t, found)
	return row
}

func assertUnifiedOwnershipPending(t *testing.T, ctx context.Context, root string, want bool) {
	t.Helper()
	store, cleanup, err := obsidian.OpenIntelStore(root, false)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	_, pending, err := store.PendingOwnershipReconciliation(ctx)
	require.NoError(t, err)
	require.Equal(t, want, pending)
}

func unifiedFatalMetadataRows(t *testing.T, ctx context.Context, root string) map[string]semdb.NoteMetadataRow {
	t.Helper()
	store, cleanup, err := obsidian.OpenIntelStore(root, false)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	rows, err := store.CurrentNoteMetadataRows(ctx)
	require.NoError(t, err)
	result := make(map[string]semdb.NoteMetadataRow, len(rows))
	for _, row := range rows {
		result[row.Path] = row
	}
	return result
}

type unifiedFatalProjector struct{ fatal atomic.Bool }

func (p *unifiedFatalProjector) Descriptor() noteformat.Descriptor {
	return markdown.New().Descriptor()
}

func (p *unifiedFatalProjector) Project(noteformat.AuthoredSource) (noteformat.Projection, error) {
	descriptor := p.Descriptor()
	if p.fatal.Load() {
		return noteformat.NewProjection(descriptor.ProviderVersion, descriptor.ProjectionVersion, noteformat.ProjectionStatusFatal, []noteformat.Diagnostic{{Code: "projector_fatal", Message: "projector rejected source", Blocking: true}}, descriptor.Capabilities)
	}
	return noteformat.NewProjection(descriptor.ProviderVersion, descriptor.ProjectionVersion, noteformat.ProjectionStatusCurrent, nil, descriptor.Capabilities)
}

func unifiedFatalIndexer(t *testing.T, projector noteformat.Projector) notemeta.Indexer {
	t.Helper()
	registry, err := noteformat.NewRegistry(projector)
	require.NoError(t, err)
	runtime, err := noteformat.NewRuntime(registry, projector)
	require.NoError(t, err)
	indexer, err := notemeta.NewIndexer(runtime)
	require.NoError(t, err)
	return indexer
}
