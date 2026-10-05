package actions

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type stubContextVault struct {
	def obsidian.VaultDefinition
}

type countingOntologyRuntimeProvider struct {
	runtime *ontology.Runtime
	err     error
	calls   int
}

func (p *countingOntologyRuntimeProvider) LoadOntologyRuntime(context.Context) (*ontology.Runtime, error) {
	p.calls++
	return p.runtime, p.err
}

func (s stubContextVault) DefaultName() (string, error)                  { return s.def.Name, nil }
func (s stubContextVault) SetDefaultName(name string) error              { return nil }
func (s stubContextVault) Path() (string, error)                         { return s.def.BasePath(), nil }
func (s stubContextVault) Definition() (obsidian.VaultDefinition, error) { return s.def, nil }

type stubCompressor struct {
	text        string
	pieceStatus map[string]string
}

func (s stubCompressor) MaxInputChars() int {
	return 20000
}

func (s stubCompressor) Compress(_ context.Context, _ contextpack.CompressRequest) (contextpack.CompressResult, error) {
	return contextpack.CompressResult{
		Text:        s.text,
		Compressed:  true,
		PieceStatus: s.pieceStatus,
	}, nil
}

type fakeBatchDedupeTracker struct {
	committed map[string]string
}

type fakeBatchDedupeReservation struct {
	tracker *fakeBatchDedupeTracker
	allowed map[string]string
}

func (f *fakeBatchDedupeTracker) Allow(key, fingerprint string) bool {
	return f.committed[key] != fingerprint
}

func (f *fakeBatchDedupeTracker) MarkSent(key, fingerprint string) {
	if f.committed == nil {
		f.committed = make(map[string]string)
	}
	f.committed[key] = fingerprint
}

func (f *fakeBatchDedupeTracker) Seen(key, fingerprint string) bool {
	return f.committed[key] == fingerprint
}

func (f *fakeBatchDedupeTracker) Reserve(items []DedupeItem) BatchDedupeReservation {
	allowed := make(map[string]string, len(items))
	for _, item := range items {
		if f.committed[item.Key] != item.Fingerprint {
			allowed[item.Key] = item.Fingerprint
		}
	}
	return &fakeBatchDedupeReservation{tracker: f, allowed: allowed}
}

func (r *fakeBatchDedupeReservation) Allowed(key, fingerprint string) bool {
	return r.allowed[key] == fingerprint
}

func (r *fakeBatchDedupeReservation) Commit(items []DedupeItem) {
	for _, item := range items {
		if r.Allowed(item.Key, item.Fingerprint) {
			r.tracker.MarkSent(item.Key, item.Fingerprint)
		}
	}
}

func TestPackContextDedupeReleasesBudgetOmissions(t *testing.T) {
	for _, batch := range []bool{false, true} {
		for _, compressed := range []bool{false, true} {
			t.Run(fmt.Sprintf("batch=%t/compressed=%t", batch, compressed), func(t *testing.T) {
				state := &fakeBatchDedupeTracker{}
				var tracker DedupeTracker = state
				if !batch {
					tracker = struct{ DedupeTracker }{state}
				}
				pieces := []contextpack.Piece{
					{Key: "first", Priority: 2, Text: "first piece"},
					{Key: "second", Priority: 1, Text: "second piece"},
				}
				nested := map[string][]DedupeItem{
					"first":  {{Key: "note:first", Fingerprint: "first-fp"}},
					"second": {{Key: "note:second", Fingerprint: "second-fp"}},
				}
				opts := packContextOptions{Pieces: pieces, Budget: len("first piece"), Tracker: tracker, NestedDedupe: nested}
				if compressed {
					opts.Compressor = stubCompressor{text: "compressed first"}
				}
				first := packContext(opts)
				require.NoError(t, first.Delivery.finish(context.Background()))
				require.Equal(t, compressed, first.Compressed)
				require.Equal(t, []string{"first"}, first.Meta.IncludedKeys)
				if compressed {
					require.Empty(t, state.committed, "compression does not prove raw source delivery")
					return
				}
				require.Equal(t, "first piece", first.Text)
				require.Contains(t, state.committed, "first")
				require.NotContains(t, state.committed, "second")
				require.Equal(t, "first-fp", state.committed["note:first"])
				require.NotContains(t, state.committed, "note:second")

				second := packContext(packContextOptions{
					Pieces: pieces, Budget: len("second piece"), Tracker: tracker, NestedDedupe: nested,
				})
				require.NoError(t, second.Delivery.finish(context.Background()))
				require.Equal(t, "second piece", second.Text)
				require.Equal(t, []string{"second"}, second.Meta.IncludedKeys)
				require.Contains(t, state.committed, "second")
				require.Equal(t, "second-fp", state.committed["note:second"])
			})
		}
	}
}

func TestPackContextBatchDedupeKeepsCompressedRawPiecesEligible(t *testing.T) {
	for _, status := range []string{"full", "summarized"} {
		t.Run(status, func(t *testing.T) {
			tracker := &fakeBatchDedupeTracker{}
			pieces := []contextpack.Piece{
				{Key: "first", Priority: 2, Text: "first piece"},
				{Key: "second", Priority: 1, Text: "second piece"},
			}
			nested := map[string][]DedupeItem{
				"first":  {{Key: "note:first", Fingerprint: "first-fp"}},
				"second": {{Key: "note:second", Fingerprint: "second-fp"}},
			}
			result := packContext(packContextOptions{
				Pieces: pieces, Budget: len("first piece"), Tracker: tracker, NestedDedupe: nested,
				Compressor: stubCompressor{text: "second", pieceStatus: map[string]string{"first": "omitted", "second": status}},
			})
			require.True(t, result.Compressed)
			require.Equal(t, "second", result.Text)
			require.NotContains(t, tracker.committed, "first")
			require.NotContains(t, tracker.committed, "note:first")
			require.NoError(t, result.Delivery.finish(context.Background()))
			require.Empty(t, tracker.committed)
		})
	}
}

func TestBuildVaultContextText_ReturnsConfigParseError(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("vault: ["), 0o644))

	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root, Includes: []string{"**/*.md"}}}
	note := obsidian.Note{}

	_, err := BuildVaultContextText(vault, &note, VaultContextTextParams{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "yaml")
}

func TestBuildFileContextText_ReturnsConfigParseError(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("vault: ["), 0o644))

	target := filepath.Join(root, "src", "main.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, []byte("package main\n"), 0o644))

	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root, Includes: []string{"**/*.md"}}}
	note := obsidian.Note{}

	_, err := BuildFileContextText(vault, &note, FileContextTextParams{
		NoteMetadata: testNoteMetadataIndexer(t),
		Files:        []string{target},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "yaml")
}

func TestParseEnsureLinkTargetModeRejectsInvalidMode(t *testing.T) {
	mode, err := ParseEnsureLinkTargetMode("aply")
	require.Error(t, err)
	require.Equal(t, ontology.EnsureLinkTargetNever, mode)

	mode, err = ParseEnsureLinkTargetMode("plan")
	require.NoError(t, err)
	require.Equal(t, ontology.EnsureLinkTargetPlan, mode)
}

func TestBuildVaultContextText_IncludesBundledFileContext(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "CONTEXT.md"), []byte("# Root\n\nrepo overview\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "Notes.md"), []byte("# Notes\n\nseed note\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg", "app", "indexing"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "app", "indexing", "CONTEXT.md"), []byte("# Indexing\n\nindexing overview\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "app", "indexing", "worker.go"), []byte("package indexing\n"), 0o644))

	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root, Includes: []string{"**/*.md"}}}
	note := obsidian.Note{}

	text, err := BuildVaultContextText(vault, &note, VaultContextTextParams{
		BudgetChars:    9000,
		Profile:        ContextProfileCode,
		Files:          []string{"pkg/app/indexing"},
		SubmoduleDepth: 1,
	})
	require.NoError(t, err)
	require.Contains(t, text, "tool: vault_context")
	require.Contains(t, text, "## Target context")
	require.Contains(t, text, "### pkg/app/indexing (code)")
	require.Contains(t, text, "indexing overview")
}

func TestBuildVaultContextText_MinimalBootstrapReadsOnlyBoundedGuidance(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "CONTEXT.md"), []byte("# Root guidance\n\nroot operational rule\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg", "service", "child"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "service", "CONTEXT.md"), []byte("# Service guidance\n\nservice operational rule\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "service", "child", "CONTEXT.md"), []byte("# Child guidance\n\nchild operational rule\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "service", "worker.go"), []byte("package service // source must not be read\n"), 0o644))

	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root, Includes: []string{"**/*.md"}}}
	text, err := BuildVaultContextText(vault, nil, VaultContextTextParams{
		BudgetChars:    12000,
		Profile:        ContextProfileCode,
		RequestScope:   VaultContextRequestScopeMinimalBootstrap,
		Files:          []string{"pkg/service"},
		SubmoduleDepth: 1,
	})
	require.NoError(t, err)
	require.Contains(t, text, "tool: vault_context")
	require.Contains(t, text, "pkg/service")
	require.Contains(t, text, "root operational rule")
	require.Contains(t, text, "service operational rule")
	require.Contains(t, text, "child operational rule")
	require.NotContains(t, text, "source must not be read")

	fileText, err := BuildVaultContextText(vault, nil, VaultContextTextParams{
		BudgetChars:  12000,
		Profile:      ContextProfileCode,
		RequestScope: VaultContextRequestScopeMinimalBootstrap,
		Files:        []string{"pkg/service/worker.go"},
	})
	require.NoError(t, err)
	require.Contains(t, fileText, "pkg/service/worker.go (file)")
	require.Contains(t, fileText, "root operational rule")
	require.Contains(t, fileText, "service operational rule")
	require.NotContains(t, fileText, "source must not be read")
	bare, err := BuildVaultContextText(vault, nil, VaultContextTextParams{RequestScope: VaultContextRequestScopeMinimalBootstrap})
	require.NoError(t, err)
	require.Contains(t, bare, "root operational rule")
	require.NotContains(t, bare, "Vault graph")
}

func TestBuildVaultContextText_MinimalBootstrapRejectsTargetsOutsideProjectRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	external := filepath.Join(base, "external")
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.MkdirAll(external, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(external, "CONTEXT.md"), []byte("external secret guidance\n"), 0o644))
	require.NoError(t, os.Symlink(external, filepath.Join(root, "external-link")))

	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root, Includes: []string{"**/*.md"}}}
	for name, target := range map[string]string{
		"absolute external path": external,
		"relative parent escape": filepath.Join("..", "external"),
		"in-repo symlink escape": "external-link",
	} {
		t.Run(name, func(t *testing.T) {
			text, err := BuildVaultContextText(vault, nil, VaultContextTextParams{
				RequestScope:   VaultContextRequestScopeMinimalBootstrap,
				Files:          []string{target},
				SubmoduleDepth: 1,
			})
			require.ErrorIs(t, err, paths.ErrOutsideVault)
			require.Empty(t, text)
		})
	}
}

func TestBuildVaultContextText_CodeProfileScopesRepoDocsWhenTargetsAreExplicit(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "CONTEXT.md"), []byte("# Root guidance\n\nroot operational rule\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg", "service"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "service", "CONTEXT.md"), []byte("# Service guidance\n\nservice operational rule\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "service", "worker.go"), []byte("package service\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "Notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "Notes", "Decision.md"), []byte("# Decision\n\nretain explicit note context\n"), 0o644))

	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root, Includes: []string{"**/*.md"}}}
	note := obsidian.Note{}
	tests := []struct {
		name         string
		files        []string
		contextFiles []string
		wantWalks    int64
		contains     []string
	}{
		{
			name:      "untargeted keeps broad repo docs",
			wantWalks: 1,
			contains:  []string{"root operational rule", "service operational rule"},
		},
		{
			name:      "file target uses root and ancestor docs without broad walk",
			files:     []string{"pkg/service/worker.go"},
			wantWalks: 0,
			contains:  []string{"root operational rule", "service operational rule"},
		},
		{
			name:         "context file keeps note context without broad walk",
			contextFiles: []string{"Notes/Decision.md"},
			wantWalks:    0,
			contains:     []string{"Decision", "Notes/Decision.md"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector := indexingperf.New()
			ctx := indexingperf.WithCollector(context.Background(), collector)
			text, err := BuildVaultContextText(vault, &note, VaultContextTextParams{
				Context:      ctx,
				BudgetChars:  20000,
				Profile:      ContextProfileCode,
				Files:        tt.files,
				ContextFiles: tt.contextFiles,
				NoteMetadata: testNoteMetadataIndexer(t),
			})
			require.NoError(t, err)
			for _, want := range tt.contains {
				require.Contains(t, text, want)
			}
			require.Equal(t, tt.wantWalks, agentStartOperationCount(collector, indexingperf.AgentStartOpRepoWalks))
		})
	}
}

func agentStartOperationCount(collector *indexingperf.Collector, label string) int64 {
	for _, operation := range collector.AgentStartDiagnostics().Operations {
		if operation.Label == label {
			return operation.Count
		}
	}
	return 0
}

func TestBuildVaultContextText_BundledFileContextPreservesCodeRefs(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "CONTEXT.md"), []byte("# Root\n\nrepo overview\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "Notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "Notes", "MyNote.md"), []byte("---\nsummary: \"linked note summary\"\n---\n\n# MyNote\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg", "app", "indexing"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "app", "indexing", "worker.go"), []byte("package indexing\n"), 0o644))

	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root, Includes: []string{"**/*.md"}}}
	note := obsidian.Note{}

	text, err := BuildVaultContextText(vault, &note, VaultContextTextParams{
		BudgetChars:  9000,
		Profile:      ContextProfileCode,
		Files:        []string{"pkg/app/indexing/worker.go"},
		NoteMetadata: testNoteMetadataIndexer(t),
		CodeRefsByFile: map[string][]coderefs.CodeRef{
			"pkg/app/indexing/worker.go": {
				{
					SourceFile: "pkg/app/indexing/worker.go",
					Target:     "Notes/MyNote.md",
					Kind:       coderefs.RefKindWikilink,
					Line:       1,
					Snippet:    "// see [[MyNote]]",
				},
			},
		},
	})
	require.NoError(t, err)
	require.Contains(t, text, "## Target context")
	require.Contains(t, text, "linked note summary")
	require.Contains(t, text, "Notes/MyNote.md")
}

func TestBuildVaultContextText_BundledFileContextStripsNestedHeaderWhenCompressed(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "CONTEXT.md"), []byte("# Root\n\nrepo overview\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "Notes.md"), []byte("# Notes\n\nseed note\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg", "app", "indexing"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "app", "indexing", "worker.go"), []byte("package indexing\n"), 0o644))

	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root, Includes: []string{"**/*.md"}}}
	note := obsidian.Note{}

	text, err := BuildVaultContextText(vault, &note, VaultContextTextParams{
		BudgetChars:  9000,
		Profile:      ContextProfileCode,
		Files:        []string{"pkg/app/indexing/worker.go"},
		Compressor:   stubCompressor{text: "compressed body without headings"},
		NoteMetadata: testNoteMetadataIndexer(t),
	})
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(text, "# Rhizome context"))
	require.Contains(t, text, "## Target context\n\ncompressed body without headings")
	require.NotContains(t, text, "# Rhizome context\n\n# Rhizome context")
}

func TestBuildFileContextText_UsesProvidedSessionStoreForOntologyContext(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type Project @node(paths: ["notes/*.md"]) {
  name: String!
}
`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "Roadmap.md"), []byte(`---
type: Project
name: Roadmap
---
`), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, "supplied.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root, Includes: []string{"**/*.md"}}}
	note := obsidian.Note{}
	target := filepath.Join(root, "notes", "Roadmap.md")

	text, err := BuildFileContextText(vault, &note, FileContextTextParams{
		Files:        []string{target},
		SessionStore: store,
		NoteMetadata: testNoteMetadataIndexer(t),
	})
	require.NoError(t, err)
	require.Contains(t, text, "### Ontology")
	require.Contains(t, text, "type: Project")
	state, err := store.GetOntologySchemaState(context.Background())
	require.NoError(t, err)
	require.True(t, state.Ready, "the supplied store must receive the ontology projection")
	canonicalPath := obsidian.UnifiedIndexPath(root, "")
	if _, err := os.Stat(canonicalPath); err == nil {
		canonical, err := sqlitefixture.Open(canonicalPath)
		require.NoError(t, err)
		defer func() { _ = canonical.Close() }()
		canonicalState, err := canonical.GetOntologySchemaState(context.Background())
		require.NoError(t, err)
		require.False(t, canonicalState.Ready, "the request must not materialize ontology in a second store")
	} else {
		require.True(t, os.IsNotExist(err))
	}
}

func TestBuildVaultContextText_FilesUsesCommandOntologyProvider(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type Project @node(paths: ["notes/*.md"]) {
  name: String!
}
`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "Roadmap.md"), []byte("---\ntype: Project\nname: Roadmap\n---\n"), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	vaultDef := obsidian.VaultDefinition{Name: "test", Root: root, Includes: []string{"**/*.md"}}
	note := &obsidian.Note{}
	runtime, err := ontology.EnsureRuntimeWithStore(context.Background(), testNoteMetadataIndexer(t), vaultDef, note, store)
	require.NoError(t, err)
	provider := &countingOntologyRuntimeProvider{runtime: runtime}

	text, err := BuildVaultContextText(stubContextVault{def: vaultDef}, note, VaultContextTextParams{
		BudgetChars:             9000,
		Profile:                 ContextProfileCode,
		Files:                   []string{filepath.Join(root, "notes", "Roadmap.md")},
		SessionStore:            store,
		NoteMetadata:            testNoteMetadataIndexer(t),
		OntologyRuntimeProvider: provider,
	})
	require.NoError(t, err)
	require.Contains(t, text, "type: Project")
	require.Equal(t, 1, provider.calls, "the --file path must use the command-scoped ontology provider")
}

func TestBuildVaultContextText_OntologySummaryPreferredWhenSchemaPresent(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
}
`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes", "projects"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "projects", "A.md"), []byte(`---
type: Project
name: A
---
[[notes/projects/B]]
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "projects", "B.md"), []byte(`---
type: Project
name: B
---
[[notes/projects/C]]
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "projects", "C.md"), []byte(`---
type: Project
name: C
---
[[notes/projects/A]]
`), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root, Includes: []string{"**/*.md"}}}
	note := obsidian.Note{}

	text, err := BuildVaultContextText(vault, &note, VaultContextTextParams{
		BudgetChars:  12000,
		Profile:      ContextProfileVault,
		SessionStore: store,
		NoteMetadata: testNoteMetadataIndexer(t),
	})
	require.NoError(t, err)
	require.Contains(t, text, "## Ontology types")
	require.NotContains(t, text, "## Communities")
	withGraph, err := BuildVaultContextText(vault, &note, VaultContextTextParams{
		BudgetChars: 12000, Profile: ContextProfileVault, GraphSummary: true,
		SessionStore: store, NoteMetadata: testNoteMetadataIndexer(t),
	})
	require.NoError(t, err)
	require.NotContains(t, withGraph, "## Ontology types")
	require.Contains(t, withGraph, "## Communities")
}

func TestBuildVaultContextText_ContextFilesWorkWithoutGraphSummaryWhenOntologyPresent(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
}
`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes", "projects"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "projects", "Roadmap.md"), []byte(`---
type: Project
name: Roadmap
---
`), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root, Includes: []string{"**/*.md"}}}
	note := obsidian.Note{}

	text, err := BuildVaultContextText(vault, &note, VaultContextTextParams{
		BudgetChars:  12000,
		Profile:      ContextProfileVault,
		ContextFiles: []string{"notes/projects/Roadmap.md"},
		SessionStore: store,
		NoteMetadata: testNoteMetadataIndexer(t),
	})
	require.NoError(t, err)
	require.Contains(t, text, "## Roadmap (note)")
	require.Contains(t, text, "### Ontology")
}

func TestBuildVaultContextText_CodeProfileSkipsOntologyLoadWithoutContextFiles(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte("type Project @node(paths: [\"notes/**/*.md\"]) { name: String! }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "CONTEXT.md"), []byte("# Root\n\nrepo docs\n"), 0o644))

	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root, Includes: []string{"**/*.md"}}}
	note := obsidian.Note{}

	text, err := BuildVaultContextText(vault, &note, VaultContextTextParams{
		BudgetChars:  12000,
		Profile:      ContextProfileCode,
		NoteMetadata: testNoteMetadataIndexer(t),
	})
	require.NoError(t, err)
	require.Contains(t, text, "tool: vault_context")
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	state, err := store.GetOntologySchemaState(context.Background())
	require.NoError(t, err)
	require.False(t, state.Ready, "code profile must not materialize ontology state")
}
