package actions

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type boundedNoteReader struct {
	obsidian.Note
	listCalls int
	facts     NoteFacts
}

func (r *boundedNoteReader) NoteFacts() NoteFacts { return r.facts }

func markFileContextIndexCurrent(t *testing.T, ctx context.Context, vault stubContextVault, store *semdb.Store, files ...string) {
	t.Helper()
	inputs, err := resolveContextInputs(vault, ContextProfileCode)
	require.NoError(t, err)
	expectedScopeHash := ""
	if inputs.LocalCfg != nil && inputs.LocalCfg.cfg != nil {
		expectedScopeHash = inputs.LocalCfg.cfg.ScopeConfigHash()
	}
	require.NoError(t, store.SetIndexerVersion(ctx, codeanchor.IndexerVersion))
	require.NoError(t, store.SetScopeConfigHash(ctx, expectedScopeHash))
	for _, file := range files {
		abs := file
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(inputs.ProjectRoot, abs)
		}
		info, statErr := os.Stat(abs)
		require.NoError(t, statErr)
		if !info.Mode().IsRegular() {
			continue
		}
		content, readErr := os.ReadFile(abs)
		require.NoError(t, readErr)
		hash := fmt.Sprintf("%x", sha256.Sum256(content))
		rel, relErr := inputs.ProjectRootPaths.RelStrict(abs)
		require.NoError(t, relErr)
		classification, classifyErr := classifyConfiguredPath(inputs.VaultDef, abs, testNoteMetadataIndexer(t))
		require.NoError(t, classifyErr)
		if classification.Owner == notediscovery.Note {
			require.NoError(t, store.UpsertNoteMetadataBatch(ctx, map[string]codeanchor.NoteIndexMeta{
				string(paths.NormalizeNote(rel.String())): {
					ContentHash:    hash,
					IndexerVersion: codeanchor.NoteIndexerVersion,
				},
			}))
			continue
		}
		require.NoError(t, store.UpsertFileMeta(ctx, codeanchor.FileMeta{
			Path:        string(paths.NormalizeCode(rel.String())),
			Hash:        hash,
			ParseStatus: codeanchor.ParseOK,
		}))
	}
}

func (r *boundedNoteReader) GetNotesList(def obsidian.VaultDefinition) ([]string, error) {
	r.listCalls++
	return r.Note.GetNotesList(def)
}

func TestBuildFileContextTextIndexedReadOnlyBuildsBoundedNoteContext(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type Project @node(paths: ["notes/projects/*.md"]) {
  decision: Decision @link(source: "decision", contextInclude: true)
}

type Decision @node(paths: ["notes/decisions/*.md"]) {
  summary: String @field(source: "summary")
}
`), 0o644))

	writeNote := func(path, content string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(content), 0o644))
	}
	writeNote("notes/projects/Roadmap.md", "---\nsummary: indexed roadmap\n---\n\n# Roadmap\n")
	writeNote("notes/decisions/Architecture.md", "# Architecture\n\nUse the indexed read model.\n")
	writeNote("notes/Inbound.md", "# Inbound\n")
	writeNote("notes/Unrelated.md", "# Unrelated\n")

	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "index.db"))
	require.NoError(t, err)
	require.NoError(t, store.ReplaceGraphDocScores(ctx, []semdb.GraphDocScore{
		{DocPath: "notes/projects/Roadmap.md", DocType: "note", Hub: 0.25, Authority: 0.5, Inbound: 7, Outbound: 8},
		{DocPath: "notes/decisions/Architecture.md", DocType: "note", Authority: 2},
		{DocPath: "notes/Inbound.md", DocType: "note", Authority: 3},
		{DocPath: "notes/Unrelated.md", DocType: "note", Authority: 99},
	}))
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "notes/Inbound.md", semdb.GraphDocEdgeKindWikilink, []string{"notes/projects/Roadmap.md"}))
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "notes/projects/Roadmap.md", semdb.GraphDocEdgeKindWikilink, []string{"notes/decisions/Architecture.md"}))
	// Same pairs under a second kind: incident sets must dedupe by path across kinds.
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "notes/Inbound.md", semdb.GraphDocEdgeKindMarkdownLink, []string{"notes/projects/Roadmap.md"}))
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "notes/projects/Roadmap.md", semdb.GraphDocEdgeKindMarkdownLink, []string{"notes/decisions/Architecture.md"}))
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		NoteTypes: []semdb.OntologyNoteTypeRow{
			{NotePath: "notes/projects/Roadmap.md", TypeName: "Project", SchemaHash: schema.Hash},
			{NotePath: "notes/decisions/Architecture.md", TypeName: "Decision", SchemaHash: schema.Hash},
		},
		Edges: []semdb.OntologyEdgeRow{{
			SrcPath: "notes/projects/Roadmap.md", RelationName: "decision",
			DstPath: "notes/decisions/Architecture.md", DstType: "Decision",
			Provenance: "field", Structural: true, SchemaHash: schema.Hash,
		}},
		SchemaState: semdb.OntologySchemaState{
			SchemaHash:             schema.Hash,
			MaterializationVersion: ontology.OntologyMaterializationVersion,
			Ready:                  true,
		},
	}))

	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root, Includes: []string{"**/*.md"}}}
	reader := &boundedNoteReader{facts: newProjectedFilesystemFactReader(t, vault.def, &obsidian.Note{}).NoteFacts()}
	markFileContextIndexCurrent(t, ctx, vault, store, "notes/projects/Roadmap.md")
	require.NoError(t, store.Close())
	indexPath := filepath.Join(root, ".rhizome", "index.db")
	before, err := os.ReadFile(indexPath)
	require.NoError(t, err)
	readOnly, err := semdb.OpenReadOnlyExisting(indexPath, ctx, sqliteutil.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = readOnly.Close() })
	text, err := BuildFileContextText(vault, reader, FileContextTextParams{
		Context:         ctx,
		NoteMetadata:    testNoteMetadataIndexer(t),
		BudgetChars:     40000,
		Profile:         ContextProfileCode,
		Files:           []string{"notes/projects/Roadmap.md"},
		SessionStore:    readOnly,
		IndexedReadOnly: true,
	})
	require.NoError(t, err)
	require.Zero(t, reader.listCalls, "indexed read-only mode must not inventory the vault")
	after, err := os.ReadFile(indexPath)
	require.NoError(t, err)
	require.Equal(t, before, after, "indexed context must not change the supplied store")
	require.Contains(t, text, "## Roadmap (note)")
	require.Contains(t, text, "- summary: indexed roadmap")
	require.Contains(t, text, "- hub: 0.250")
	require.Contains(t, text, "- authority: 0.500")
	require.Contains(t, text, "- inbound: 7")
	require.Contains(t, text, "- outbound: 8")
	require.Contains(t, text, "### Top inbound")
	require.Contains(t, text, "- notes/Inbound.md")
	require.Equal(t, 1, strings.Count(text, "- notes/Inbound.md"))
	require.Contains(t, text, "### Top outbound")
	require.Contains(t, text, "- notes/decisions/Architecture.md")
	require.Equal(t, 1, strings.Count(text, "- notes/decisions/Architecture.md"))
	require.Contains(t, text, "- type: Project")
	require.Contains(t, text, "- decision (ctx) [field]: notes/decisions/Architecture.md")
	require.Contains(t, text, "#### Ontology context (ctx)")
	require.Contains(t, text, "Use the indexed read model.")
	require.NotContains(t, text, "notes/Unrelated.md")
}

func TestBuildFileContextTextIndexedReadOnlyFailsSoftWithoutStore(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "Decision.md"), []byte("---\nsummary: bounded fallback\n---\n"), 0o644))

	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root, Includes: []string{"**/*.md"}}}
	reader := newProjectedFilesystemFactReader(t, vault.def, &obsidian.Note{})
	text, err := BuildFileContextText(vault, reader, FileContextTextParams{
		Context:         context.Background(),
		NoteMetadata:    testNoteMetadataIndexer(t),
		BudgetChars:     12000,
		Profile:         ContextProfileCode,
		Files:           []string{"notes/Decision.md"},
		IndexedReadOnly: true,
	})
	require.NoError(t, err)
	require.Contains(t, text, "## Decision (note)")
	require.Contains(t, text, "- summary: bounded fallback")
	require.NotContains(t, text, "Error: not found in graph")
	_, statErr := os.Stat(obsidian.UnifiedIndexPath(root, ""))
	require.True(t, os.IsNotExist(statErr), "missing-store read-only request must not create an index")
}

func TestBuildVaultContextTextIndexedReadOnlyKeepsCurrentOntologySummary(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type Project @node(paths: ["notes/*.md"]) {
  name: String
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "Roadmap.md"), []byte("---\nname: Roadmap\n---\n# Roadmap\n"), 0o644))

	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		NoteTypes: []semdb.OntologyNoteTypeRow{{NotePath: "notes/Roadmap.md", TypeName: "Project", SchemaHash: schema.Hash}},
		SchemaState: semdb.OntologySchemaState{
			SchemaHash:             schema.Hash,
			MaterializationVersion: ontology.OntologyMaterializationVersion,
			Ready:                  true,
		},
	}))
	require.NoError(t, store.Close())
	indexPath := filepath.Join(root, ".rhizome", "db.sqlite")
	before, err := os.ReadFile(indexPath)
	require.NoError(t, err)
	readOnly, err := semdb.OpenReadOnlyExisting(indexPath, ctx, sqliteutil.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = readOnly.Close() })

	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root, Includes: []string{"**/*.md"}}}
	text, err := BuildVaultContextText(vault, &obsidian.Note{}, VaultContextTextParams{
		Context:         ctx,
		BudgetChars:     12000,
		Profile:         ContextProfileVault,
		SessionStore:    readOnly,
		NoteMetadata:    testNoteMetadataIndexer(t),
		IndexedReadOnly: true,
	})
	require.NoError(t, err)
	require.Contains(t, text, "## Ontology types")
	require.Contains(t, text, "Project (1): notes/Roadmap.md")
	require.NotContains(t, text, "## Communities")
	after, err := os.ReadFile(indexPath)
	require.NoError(t, err)
	require.Equal(t, before, after, "read-only context must leave ontology state unchanged")
	for _, tc := range []struct {
		name    string
		state   semdb.OntologySchemaState
		visible bool
	}{
		{"missing state", semdb.OntologySchemaState{}, false},
		{"not ready", semdb.OntologySchemaState{SchemaHash: schema.Hash}, false},
		{"stale schema", semdb.OntologySchemaState{SchemaHash: "stale", MaterializationVersion: ontology.OntologyMaterializationVersion, Ready: true}, false},
		{"stale materialization", semdb.OntologySchemaState{SchemaHash: schema.Hash, MaterializationVersion: ontology.OntologyMaterializationVersion - 1, Ready: true}, false},
		{"future materialization", semdb.OntologySchemaState{SchemaHash: schema.Hash, MaterializationVersion: ontology.OntologyMaterializationVersion + 1, Ready: true}, false},
		{"matching ready schema", semdb.OntologySchemaState{SchemaHash: schema.Hash, MaterializationVersion: ontology.OntologyMaterializationVersion, Ready: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "index.db"))
			require.NoError(t, err)
			defer func() { _ = candidate.Close() }()
			if tc.state != (semdb.OntologySchemaState{}) {
				require.NoError(t, candidate.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
					NoteTypes:   []semdb.OntologyNoteTypeRow{{NotePath: "notes/Roadmap.md", TypeName: "Project", SchemaHash: schema.Hash}},
					SchemaState: tc.state,
				}))
			}
			result, err := BuildVaultContextText(vault, &obsidian.Note{}, VaultContextTextParams{
				Context: ctx, BudgetChars: 12000, Profile: ContextProfileVault,
				SessionStore: candidate, NoteMetadata: testNoteMetadataIndexer(t), IndexedReadOnly: true,
			})
			require.NoError(t, err)
			if tc.visible {
				require.Contains(t, result, "Project (1): notes/Roadmap.md")
			} else {
				require.NotContains(t, result, "Project (1): notes/Roadmap.md")
			}
		})
	}
}

func TestBuildFileContextTextResultUsesUnavailableOpenAuthority(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "worker.go"), []byte("package worker\n"), 0o644))

	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root}}
	unavailable := IndexedContextFreshness{
		State:       IndexedContextIncompatible,
		WarningCode: "indexed-context-incompatible",
		Remediation: "rzm index --rebuild",
	}
	result, err := BuildFileContextTextResult(vault, &obsidian.Note{}, FileContextTextParams{
		Context:            context.Background(),
		NoteMetadata:       testNoteMetadataIndexer(t),
		BudgetChars:        12000,
		Profile:            ContextProfileCode,
		Files:              []string{"worker.go"},
		IndexedReadOnly:    true,
		IndexedUnavailable: &unavailable,
	})
	require.NoError(t, err)
	require.Equal(t, IndexedContextIncompatible, result.IndexedStatus)
	require.Zero(t, result.IndexedReads)
	require.Zero(t, result.IndexedResults)
	require.Len(t, result.Warnings, 1)
	require.Equal(t, "indexed-context-incompatible", result.Warnings[0].Code)
	require.Contains(t, result.Text, "indexed-context-incompatible")
	require.Contains(t, result.Text, "rzm index --rebuild")
}

func TestBuildFileContextTextResultRejectsStaleIndexMetadata(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "worker.go"), []byte("package worker\n"), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.SetIndexerVersion(ctx, codeanchor.IndexerVersion))
	require.NoError(t, store.SetScopeConfigHash(ctx, "stale-scope"))
	require.NoError(t, store.ReplaceRationaleForPath(ctx, "worker.go", []codeanchor.Rationale{{
		ID:        "stale-rationale",
		Path:      "worker.go",
		Kind:      codeanchor.RationaleWhy,
		Content:   "must not render stale indexed evidence",
		StartLine: 1,
		EndLine:   1,
	}}))

	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root}}
	result, err := BuildFileContextTextResult(vault, &obsidian.Note{}, FileContextTextParams{
		Context:         ctx,
		NoteMetadata:    testNoteMetadataIndexer(t),
		BudgetChars:     12000,
		Profile:         ContextProfileCode,
		Files:           []string{"worker.go"},
		SessionStore:    store,
		IndexedReadOnly: true,
	})
	require.NoError(t, err)
	require.Equal(t, IndexedContextStale, result.IndexedStatus)
	require.Equal(t, 1, result.IndexedReads)
	require.Zero(t, result.IndexedResults)
	require.Len(t, result.Warnings, 1)
	require.Equal(t, "indexed-context-stale", result.Warnings[0].Code)
	require.Contains(t, result.Text, "indexed-context-stale")
	require.NotContains(t, result.Text, "must not render stale indexed evidence")
}

func TestBuildFileContextTextResultUsesCurrentIndexMetadata(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "worker.go"), []byte("package worker\n"), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root}}
	markFileContextIndexCurrent(t, ctx, vault, store, "worker.go")
	require.NoError(t, store.ReplaceRationaleForPath(ctx, "worker.go", []codeanchor.Rationale{{
		ID:        "current-rationale",
		Path:      "worker.go",
		Kind:      codeanchor.RationaleWhy,
		Content:   "render current indexed evidence",
		StartLine: 1,
		EndLine:   1,
	}}))

	result, err := BuildFileContextTextResult(vault, &obsidian.Note{}, FileContextTextParams{
		Context:         ctx,
		NoteMetadata:    testNoteMetadataIndexer(t),
		BudgetChars:     12000,
		Profile:         ContextProfileCode,
		Files:           []string{"worker.go"},
		SessionStore:    store,
		IndexedReadOnly: true,
	})
	require.NoError(t, err)
	require.Equal(t, IndexedContextAvailable, result.IndexedStatus)
	require.Equal(t, 2, result.IndexedReads)
	require.Empty(t, result.Warnings)
	require.Contains(t, result.Text, "render current indexed evidence")
}

func TestBuildFileContextTextResultRejectsChangedExplicitCodeFile(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "worker.go"), []byte("package worker\n"), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root}}
	markFileContextIndexCurrent(t, ctx, vault, store, "worker.go")
	require.NoError(t, store.ReplaceRationaleForPath(ctx, "worker.go", []codeanchor.Rationale{{
		ID: "stale-rationale", Path: "worker.go", Kind: codeanchor.RationaleWhy,
		Content: "must not render after the source changes", StartLine: 1, EndLine: 1,
	}}))
	require.NoError(t, os.WriteFile(filepath.Join(root, "worker.go"), []byte("package worker\n\nfunc Changed() {}\n"), 0o644))

	result, err := BuildFileContextTextResult(vault, &obsidian.Note{}, FileContextTextParams{
		Context: ctx, NoteMetadata: testNoteMetadataIndexer(t), BudgetChars: 12000, Profile: ContextProfileCode,
		Files: []string{"worker.go"}, SessionStore: store, IndexedReadOnly: true,
	})
	require.NoError(t, err)
	require.Equal(t, IndexedContextStale, result.IndexedStatus)
	require.Equal(t, 2, result.IndexedReads)
	require.Zero(t, result.IndexedResults)
	require.Len(t, result.Warnings, 1)
	require.Contains(t, result.Warnings[0].Message, "changed since indexing")
	require.NotContains(t, result.Text, "must not render after the source changes")
}

func TestBuildFileContextTextResultRejectsMissingExplicitCodeMetadata(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "worker.go"), []byte("package worker\n"), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root}}
	markFileContextIndexCurrent(t, ctx, vault, store)

	result, err := BuildFileContextTextResult(vault, &obsidian.Note{}, FileContextTextParams{
		Context: ctx, NoteMetadata: testNoteMetadataIndexer(t), BudgetChars: 12000, Profile: ContextProfileCode,
		Files: []string{"worker.go"}, SessionStore: store, IndexedReadOnly: true,
	})
	require.NoError(t, err)
	require.Equal(t, IndexedContextStale, result.IndexedStatus)
	require.Equal(t, 2, result.IndexedReads)
	require.Zero(t, result.IndexedResults)
	require.Len(t, result.Warnings, 1)
	require.Contains(t, result.Warnings[0].Message, "metadata is missing or outdated")
}

func TestBuildFileContextTextResultRejectsChangedExplicitNote(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	notePath := filepath.Join(root, "notes", "Decision.md")
	require.NoError(t, os.WriteFile(notePath, []byte("---\nsummary: old\n---\n"), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceGraphDocScores(ctx, []semdb.GraphDocScore{{
		DocPath: "notes/Decision.md", DocType: "note", Authority: 9,
	}}))
	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root, Includes: []string{"**/*.md"}}}
	markFileContextIndexCurrent(t, ctx, vault, store, "notes/Decision.md")
	require.NoError(t, os.WriteFile(notePath, []byte("---\nsummary: changed\n---\n"), 0o644))
	reader := newProjectedFilesystemFactReader(t, vault.def, &obsidian.Note{})

	result, err := BuildFileContextTextResult(vault, reader, FileContextTextParams{
		Context: ctx, NoteMetadata: testNoteMetadataIndexer(t), BudgetChars: 12000, Profile: ContextProfileCode,
		Files: []string{"notes/Decision.md"}, SessionStore: store, IndexedReadOnly: true,
	})
	require.NoError(t, err)
	require.Equal(t, IndexedContextStale, result.IndexedStatus)
	require.Equal(t, 2, result.IndexedReads)
	require.Zero(t, result.IndexedResults)
	require.Len(t, result.Warnings, 1)
	require.Contains(t, result.Warnings[0].Message, "requested note changed since indexing")
	require.Contains(t, result.Text, "- summary: changed")
	require.NotContains(t, result.Text, "- authority: 9")
}

func TestIndexedOntologyPresentationFiltersRelationsBeforeLimit(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type ContextNode @node(paths: ["notes/*.md"]) {
  wanted: ContextNode @link(source: "wanted", contextInclude: true)
}
`), 0o644))
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)

	rows := make([]semdb.OntologyEdgeRow, 0, 71)
	for i := 0; i < 70; i++ {
		rows = append(rows, semdb.OntologyEdgeRow{
			SrcPath: "notes/Source.md", RelationName: "aaa",
			DstPath:    fmt.Sprintf("notes/Unrelated-%02d.md", i),
			Provenance: "field", Structural: false, SchemaHash: schema.Hash,
		})
	}
	rows = append(rows, semdb.OntologyEdgeRow{
		SrcPath: "notes/Source.md", RelationName: "wanted",
		DstPath:    "notes/Wanted.md",
		Provenance: "field", Structural: false, SchemaHash: schema.Hash,
	})

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "index.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		NoteTypes: []semdb.OntologyNoteTypeRow{{
			NotePath: "notes/Source.md", TypeName: "ContextNode", SchemaHash: schema.Hash,
		}},
		Edges: rows,
		SchemaState: semdb.OntologySchemaState{
			SchemaHash:             schema.Hash,
			MaterializationVersion: ontology.OntologyMaterializationVersion,
			Ready:                  true,
		},
	}))

	onto := loadIndexedOntologyTextContext(ctx, obsidian.VaultDefinition{Root: root}, &obsidian.Note{}, store)
	require.NotNil(t, onto)
	edges := noteOntologyPresentationEdges(ctx, onto, "notes/Source.md", []string{"wanted"}, 1)
	require.Len(t, edges, 1)
	require.Equal(t, "wanted", edges[0].RelationName)
	require.Equal(t, "notes/Wanted.md", edges[0].Target.NotePath)
}

func TestIndexedOntologyPresentationPreservesCanonicalOrderingBeforeLimit(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "index.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{
				SrcPath: "notes/Source.md", RelationName: "aaa", DstPath: "notes/Shared.md", DstNodeID: "^b",
				Provenance: "body_link", Structural: false,
			},
			{
				SrcPath: "notes/Source.md", RelationName: "aaa", DstPath: "notes/Shared.md", DstNodeID: "^a",
				Provenance: "body_link", Structural: false,
			},
			{
				SrcPath: "notes/Source.md", RelationName: "zzz", DstPath: "notes/Structural.md",
				Provenance: "field", Structural: true,
			},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", Ready: true},
	}))

	scope := noderead.NewService(obsidian.VaultDefinition{Root: root}, &obsidian.Note{}, store, nil).NewScope(ctx, noderead.ScopeOptions{})
	liveEdges := noteOntologyPresentationEdges(ctx, &ontologyTextContext{Scope: scope}, "notes/Source.md", nil, 2)
	indexedEdges := noteOntologyPresentationEdges(ctx, &ontologyTextContext{Scope: scope, IndexedReadOnly: true}, "notes/Source.md", nil, 2)

	edgeKeys := func(edges []noderead.NeighborhoodEdge) []string {
		keys := make([]string, 0, len(edges))
		for _, edge := range edges {
			keys = append(keys, fmt.Sprintf("%t|%s|%s|%s|%s", edge.Structural, edge.Provenance, edge.RelationName, edge.Target.NotePath, edge.Edge.DstNodeID))
		}
		return keys
	}
	require.Equal(t, edgeKeys(liveEdges), edgeKeys(indexedEdges))
	require.Equal(t, []string{
		"true|field|zzz|notes/Structural.md|",
		"false|body_link|aaa|notes/Shared.md|^a",
	}, edgeKeys(indexedEdges))
}

func TestIndexedOntologyPresentationExcludesEmbeddedSourcesFromNoteRoot(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "index.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{
				SrcPath: "notes/Source.md", RelationName: "owner", DstPath: "people/Owner.md",
				Provenance: "field", Structural: false,
			},
			{
				SrcPath: "notes/Source.md", SrcNodeID: "story-a", RelationName: "owner", DstPath: "people/Alice.md",
				Provenance: "field", Structural: false,
			},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", Ready: true},
	}))

	scope := noderead.NewService(obsidian.VaultDefinition{Root: root}, &obsidian.Note{}, store, nil).NewScope(ctx, noderead.ScopeOptions{})
	liveEdges := noteOntologyPresentationEdges(ctx, &ontologyTextContext{Scope: scope}, "notes/Source.md", nil, 5)
	indexedEdges := noteOntologyPresentationEdges(ctx, &ontologyTextContext{Scope: scope, IndexedReadOnly: true}, "notes/Source.md", nil, 5)

	require.Len(t, liveEdges, 1)
	require.Len(t, indexedEdges, 1)
	require.Equal(t, liveEdges[0].Target, indexedEdges[0].Target)
	require.Equal(t, "people/Owner.md", indexedEdges[0].Target.NotePath)
}

func TestBuildFileContextTextIndexedReadOnlyKeepsIncidentEdgesWithoutScores(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "Decision.md"), []byte("# Decision\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "Neighbor.md"), []byte("# Neighbor\n"), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "index.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "notes/Decision.md", semdb.GraphDocEdgeKindWikilink, []string{"notes/Neighbor.md"}))
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "notes/Decision.md", semdb.GraphDocEdgeKindMarkdownLink, []string{"notes/Neighbor.md"}))

	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root, Includes: []string{"**/*.md"}}}
	markFileContextIndexCurrent(t, ctx, vault, store, "notes/Decision.md", "notes/Neighbor.md")
	text, err := BuildFileContextText(vault, &obsidian.Note{}, FileContextTextParams{
		Context:         ctx,
		NoteMetadata:    testNoteMetadataIndexer(t),
		BudgetChars:     12000,
		Profile:         ContextProfileCode,
		Files:           []string{"notes/Decision.md"},
		SessionStore:    store,
		IndexedReadOnly: true,
	})
	require.NoError(t, err)
	require.Contains(t, text, "## Decision (note)")
	require.Contains(t, text, "- inbound: 0")
	require.Contains(t, text, "- outbound: 1")
	require.Contains(t, text, "### Top outbound")
	require.Contains(t, text, "- notes/Neighbor.md")
	require.NotContains(t, text, "Error: not found in graph")

	neighborText, err := BuildFileContextText(vault, &obsidian.Note{}, FileContextTextParams{
		Context:         ctx,
		NoteMetadata:    testNoteMetadataIndexer(t),
		BudgetChars:     12000,
		Profile:         ContextProfileCode,
		Files:           []string{"notes/Neighbor.md"},
		SessionStore:    store,
		IndexedReadOnly: true,
	})
	require.NoError(t, err)
	require.Contains(t, neighborText, "- inbound: 1", "wikilink+mdlink from one source count as one inbound neighbor")
	require.Contains(t, neighborText, "- outbound: 0")

	// Both endpoints requested: the neighborhood returns the edge as outbound
	// and inbound, so each neighbor line must still render once.
	bothText, err := BuildFileContextText(vault, &obsidian.Note{}, FileContextTextParams{
		Context:         ctx,
		NoteMetadata:    testNoteMetadataIndexer(t),
		BudgetChars:     12000,
		Profile:         ContextProfileCode,
		Files:           []string{"notes/Decision.md", "notes/Neighbor.md"},
		SessionStore:    store,
		IndexedReadOnly: true,
	})
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(bothText, "- notes/Neighbor.md"))
	require.Equal(t, 1, strings.Count(bothText, "- notes/Decision.md"))

	for _, tt := range []struct {
		name        string
		skipAnchors bool
		skipEmbeds  bool
	}{
		{name: "skip anchors", skipAnchors: true},
		{name: "skip embeds", skipEmbeds: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			filtered, err := BuildFileContextText(vault, &obsidian.Note{}, FileContextTextParams{
				Context:         ctx,
				NoteMetadata:    testNoteMetadataIndexer(t),
				BudgetChars:     12000,
				Profile:         ContextProfileCode,
				Files:           []string{"notes/Decision.md"},
				SessionStore:    store,
				IndexedReadOnly: true,
				SkipAnchors:     tt.skipAnchors,
				SkipEmbeds:      tt.skipEmbeds,
			})
			require.NoError(t, err)
			require.Contains(t, filtered, "## Decision (note)")
			require.NotContains(t, filtered, "### Top outbound")
			require.NotContains(t, filtered, "notes/Neighbor.md")
		})
	}
}

func TestBuildFileContextTextIndexedReadOnlyBoundsHighDegreeNeighborhoodWithoutChangingTopNeighbors(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "Target.md"), []byte("# Target\n"), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "index.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	const neighborCount = 40
	outbound := make([]string, 0, neighborCount)
	scores := make([]semdb.GraphDocScore, 0, neighborCount*2)
	for i := 0; i < neighborCount; i++ {
		inPath := fmt.Sprintf("notes/Inbound-%02d.md", i)
		outPath := fmt.Sprintf("notes/Outbound-%02d.md", i)
		require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, inPath, semdb.GraphDocEdgeKindWikilink, []string{"notes/Target.md"}))
		outbound = append(outbound, outPath)
		scores = append(scores,
			semdb.GraphDocScore{DocPath: inPath, DocType: "note", Authority: float64(i)},
			semdb.GraphDocScore{DocPath: outPath, DocType: "note", Authority: float64(i)},
		)
	}
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "notes/Target.md", semdb.GraphDocEdgeKindWikilink, outbound))
	require.NoError(t, store.ReplaceGraphDocScores(ctx, scores))

	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root, Includes: []string{"**/*.md"}}}
	markFileContextIndexCurrent(t, ctx, vault, store, "notes/Target.md")
	text, err := BuildFileContextText(vault, &obsidian.Note{}, FileContextTextParams{
		Context:         ctx,
		NoteMetadata:    testNoteMetadataIndexer(t),
		BudgetChars:     12000,
		Profile:         ContextProfileCode,
		Files:           []string{"notes/Target.md"},
		SessionStore:    store,
		IndexedReadOnly: true,
	})
	require.NoError(t, err)
	require.Contains(t, text, "- inbound: 40")
	require.Contains(t, text, "- outbound: 40")
	for i := 35; i < 40; i++ {
		require.Contains(t, text, fmt.Sprintf("- notes/Inbound-%02d.md", i))
		require.Contains(t, text, fmt.Sprintf("- notes/Outbound-%02d.md", i))
	}
	require.NotContains(t, text, "- notes/Inbound-34.md")
	require.NotContains(t, text, "- notes/Outbound-34.md")
}

func TestBuildFileContextTextIndexedReadOnlyHonorsCanceledContext(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "Decision.md"), []byte("# Decision\n"), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "index.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root, Includes: []string{"**/*.md"}}}
	start := time.Now()
	_, err = BuildFileContextText(vault, &obsidian.Note{}, FileContextTextParams{
		Context:         ctx,
		NoteMetadata:    testNoteMetadataIndexer(t),
		BudgetChars:     12000,
		Profile:         ContextProfileCode,
		Files:           []string{"notes/Decision.md"},
		SessionStore:    store,
		IndexedReadOnly: true,
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Less(t, time.Since(start), time.Second)
}
