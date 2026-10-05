package actions

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRenderIndexedBootstrapAppliesStrictByteBudget(t *testing.T) {
	links := make([]codeanchor.IndexedCodeNoteLink, 20)
	for i := range links {
		links[i] = codeanchor.IndexedCodeNoteLink{
			CodePath: "pkg/file.go", NotePath: "docs/note.md", Snippet: strings.Repeat("context 🔎 ", 100),
		}
	}
	rendered := renderIndexedBootstrap(links, nil, nil, nil, nil, nil, 500)
	require.LessOrEqual(t, len(rendered), 500)
	require.True(t, strings.ToValidUTF8(rendered, "") == rendered)
}

func TestRenderIndexedBootstrapPreservesPersistedCoderefRationaleAndResolvedEdgeEvidence(t *testing.T) {
	rendered := renderIndexedBootstrap(
		[]codeanchor.IndexedCodeNoteLink{{
			CodePath: "pkg/start.go", NotePath: "docs/startup.md", Label: "wikilink", Snippet: "startup contract",
		}},
		[]codeanchor.IndexedRationale{{
			ID: "rat-1", CodePath: "pkg/start.go", SymbolFQN: "startAgent", Kind: codeanchor.RationaleWhy,
			Content: "startup is a read-only index consumer", StartLine: 42,
		}},
		[]codeanchor.IndexedCodeEdge{{
			SourcePath: "pkg/start.go", TargetPath: "pkg/store.go", Kind: "calls", Weight: 2,
		}},
		[]codeanchor.IndexedNoteMetadata{{Path: "docs/startup.md", Title: "Startup", Size: 200}},
		&codeanchor.IndexedGraphSummary{
			DocumentCount: 3, NoteCount: 1, OrphanCount: 1,
			Communities: []codeanchor.IndexedGraphCommunity{{ID: "startup", DocumentCount: 3, NoteCount: 1, TopPath: "docs/startup.md"}},
		},
		nil,
		4000,
	)
	require.Contains(t, rendered, "`docs/startup.md`: Startup (200 bytes)")
	require.Contains(t, rendered, "Documents: 3; notes: 1; orphans: 1")
	require.Contains(t, rendered, "`pkg/start.go` -> `docs/startup.md` [wikilink]: startup contract")
	require.Contains(t, rendered, "`pkg/start.go:42` `startAgent` [why]: startup is a read-only index consumer")
	require.Contains(t, rendered, "`pkg/start.go` -[calls x2]-> `pkg/store.go`")
}

func TestBuildIndexedBootstrapMissingStoreReturnsGuidanceAndStructuredWarning(t *testing.T) {
	root := t.TempDir()
	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root}}
	result, err := BuildVaultContextTextResult(vault, nil, VaultContextTextParams{
		RequestScope: VaultContextRequestScopeIndexedBootstrap,
		Files:        []string{"."},
		GraphSummary: true,
	})
	require.NoError(t, err)
	require.Equal(t, IndexedContextMissing, result.IndexedStatus)
	require.Contains(t, result.Text, "Repository guidance")
	require.Contains(t, result.Text, "indexed-context-missing")
	require.Equal(t, "indexed-context-missing", result.Warnings[0].Code)
	require.Equal(t, "rzm index", result.Warnings[0].Remediation)
	require.Zero(t, result.IndexedReads)
}

func TestBuildIndexedBootstrapPreservesStoreOpenIncompatibleAuthority(t *testing.T) {
	root := t.TempDir()
	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root}}
	unavailable := IndexedContextFreshness{
		State: IndexedContextIncompatible, WarningCode: "indexed-context-incompatible", Remediation: "rzm index --rebuild",
	}
	result, err := BuildVaultContextTextResult(vault, nil, VaultContextTextParams{
		RequestScope: VaultContextRequestScopeIndexedBootstrap,
		Files:        []string{"."}, IndexedUnavailable: &unavailable,
	})
	require.NoError(t, err)
	require.Equal(t, IndexedContextIncompatible, result.IndexedStatus)
	require.Equal(t, "indexed-context-incompatible", result.Warnings[0].Code)
	require.Equal(t, "rzm index --rebuild", result.Warnings[0].Remediation)
}

func TestBuildIndexedBootstrapRendersSelectedContextFileMetadataAndBoundedGraphSummary(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, "index.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	require.NoError(t, store.SetIndexerVersion(ctx, codeanchor.IndexerVersion))
	require.NoError(t, store.SetScopeConfigHash(ctx, ""))
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{NotesHash: "notes", RawNotesHash: "raw", LoadedAt: 10, Ready: true},
		Notes: []semdb.NoteMetadataRow{{
			Path: "docs/spec.md", Title: "Selected startup spec", Mtime: 9, Size: 321,
		}},
	}))
	require.NoError(t, store.ReplaceGraphDocScores(ctx, []semdb.GraphDocScore{
		{DocPath: "docs/spec.md", DocType: "note", Authority: 2, Community: "startup", Inbound: 0, Outbound: 0, UpdatedAt: 10},
	}))

	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root}}
	result, err := BuildVaultContextTextResult(vault, nil, VaultContextTextParams{
		Context: ctx, RequestScope: VaultContextRequestScopeIndexedBootstrap,
		ContextFiles: []string{"docs/spec.md"}, GraphSummary: true, SessionStore: store,
		NoteMetadata: testNoteMetadataIndexer(t),
	})
	require.NoError(t, err)
	require.Equal(t, IndexedContextAvailable, result.IndexedStatus)
	require.Contains(t, result.Text, "`docs/spec.md`: Selected startup spec (321 bytes)")
	require.Contains(t, result.Text, "## Indexed graph summary")
	require.Contains(t, result.Text, "startup: 1 documents, 1 notes")
	require.NotContains(t, result.Text, "graph-summary-unavailable")
}

func TestBuildIndexedBootstrapWholeVaultRootUsesBoundedSubtreeReaders(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg"), 0o755))

	store, err := sqlitefixture.Open(filepath.Join(root, "index.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	require.NoError(t, store.SetIndexerVersion(ctx, codeanchor.IndexerVersion))
	require.NoError(t, store.SetScopeConfigHash(ctx, ""))
	require.NoError(t, store.ReplaceDocLinksForPath(ctx, "pkg/start.go", []codeanchor.DocLink{
		{SrcPath: "pkg/start.go", SrcType: "code", DstPath: "docs/z.md", DstKind: "note", Snippet: "unrelated"},
		{SrcPath: "pkg/start.go", SrcType: "code", DstPath: "docs/start.md", DstKind: "note", Snippet: "agent startup path"},
	}))

	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root}}
	result, err := BuildVaultContextTextResult(vault, nil, VaultContextTextParams{
		Context: ctx, RequestScope: VaultContextRequestScopeIndexedBootstrap,
		Files: []string{".", "."}, Intent: "agent startup", SessionStore: store,
	})
	require.NoError(t, err)
	require.Equal(t, IndexedContextAvailable, result.IndexedStatus)
	require.Contains(t, result.Text, "`pkg/start.go` -> `docs/start.md`")
	require.Less(t, strings.Index(result.Text, "docs/start.md"), strings.Index(result.Text, "docs/z.md"))
	require.Equal(t, 2, result.IndexedResults, "repeated targets must not duplicate links")
	require.NotContains(t, result.Text, "indexed-context-read-failed")
}

func TestBuildIndexedBootstrapGraphSummaryWithoutGraphScoresFailsSoft(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, "index.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	require.NoError(t, store.SetIndexerVersion(ctx, codeanchor.IndexerVersion))
	require.NoError(t, store.SetScopeConfigHash(ctx, ""))

	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root}}
	result, err := BuildVaultContextTextResult(vault, nil, VaultContextTextParams{
		Context: ctx, RequestScope: VaultContextRequestScopeIndexedBootstrap,
		GraphSummary: true, SessionStore: store,
	})
	require.NoError(t, err)
	require.Equal(t, IndexedContextStale, result.IndexedStatus)
	require.NotContains(t, result.Text, "Documents: 0")
	require.Contains(t, result.Text, "indexed-context-graph-summary-missing")
	require.Contains(t, result.Text, "rzm index")
}

func TestIndexedBootstrapNestedProjectUsesVaultRelativeStoreKeyAndRendersLookup(t *testing.T) {
	ctx := context.Background()
	vaultRoot := t.TempDir()
	projectRoot := filepath.Join(vaultRoot, "repo")
	targetAbs := filepath.Join(projectRoot, "docs", "spec.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(targetAbs), 0o755))
	require.NoError(t, os.WriteFile(targetAbs, []byte("# Nested spec\n"), 0o644))
	projectPaths, err := paths.NewVaultPaths(projectRoot)
	require.NoError(t, err)
	key, kind, err := indexedBootstrapTarget(resolvedContextInputs{
		VaultPath: vaultRoot, ProjectRoot: projectRoot, ProjectRootPaths: projectPaths,
	}, "docs/spec.md")
	require.NoError(t, err)
	require.Equal(t, "repo/docs/spec.md", key)
	require.Equal(t, IndexedContextTargetFile, kind)
	require.NoError(t, os.MkdirAll(filepath.Join(vaultRoot, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultRoot, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(vaultRoot, "nested-index.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: vaultRoot, Includes: []string{"**/*.md"}}}
	markFileContextIndexCurrent(t, ctx, vault, store, key)
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{NotesHash: "notes", RawNotesHash: "raw", LoadedAt: 10, Ready: true},
		Notes: []semdb.NoteMetadataRow{{Path: key, Title: "Nested selected spec", Size: 14}},
	}))
	result, err := BuildVaultContextTextResult(vault, nil, VaultContextTextParams{
		Context: ctx, RequestScope: VaultContextRequestScopeIndexedBootstrap,
		ContextFiles: []string{targetAbs}, SessionStore: store,
		NoteMetadata: testNoteMetadataIndexer(t),
	})
	require.NoError(t, err)
	require.Equal(t, IndexedContextAvailable, result.IndexedStatus)
	require.Contains(t, result.Text, "`repo/docs/spec.md`: Nested selected spec (14 bytes)")
}
