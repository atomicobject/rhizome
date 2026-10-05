package actions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type indexedCoderefNoteReader struct {
	note          obsidian.Note
	listCalls     int
	snapshotCalls int
	facts         NoteFacts
}

func newIndexedCoderefNoteReader(t *testing.T, vaultDef obsidian.VaultDefinition) *indexedCoderefNoteReader {
	t.Helper()
	return &indexedCoderefNoteReader{facts: newProjectedFilesystemFactReader(t, vaultDef, &obsidian.Note{}).NoteFacts()}
}

func (r *indexedCoderefNoteReader) NoteFacts() NoteFacts { return r.facts }

func (r *indexedCoderefNoteReader) GetContents(vault obsidian.VaultDefinition, path string) (string, error) {
	return r.note.GetContents(vault, path)
}

func (r *indexedCoderefNoteReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	r.listCalls++
	return nil, errors.New("vault-wide note enumeration is forbidden")
}

func (r *indexedCoderefNoteReader) GetModTime(vault obsidian.VaultDefinition, path string) (time.Time, error) {
	return r.note.GetModTime(vault, path)
}

func (r *indexedCoderefNoteReader) Title(path string) (string, bool) {
	return r.note.Title(path)
}

func (r *indexedCoderefNoteReader) NoteEntriesSnapshot(context.Context) ([]obsidian.NoteEntry, error) {
	r.snapshotCalls++
	return nil, errors.New("cache-wide note snapshots are forbidden")
}

func TestBuildFileContext_IndexedCoderefWhitelistRescansOnlyRequestedFile(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	codeRel := filepath.ToSlash(filepath.Join("pkg", "worker.go"))
	codePath := filepath.Join(root, filepath.FromSlash(codeRel))
	noteRel := filepath.ToSlash(filepath.Join("docs", "Decision.md"))
	notePath := filepath.Join(root, filepath.FromSlash(noteRel))
	require.NoError(t, os.MkdirAll(filepath.Dir(codePath), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	require.NoError(t, os.WriteFile(codePath, []byte(`package pkg

// See [[DEC-001#^story-a]].
// See [decision](docs/Decision.md#Details).
// Follow @DEC-001 for context.
`), 0o644))
	require.NoError(t, os.WriteFile(notePath, []byte("---\nsummary: indexed decision\n---\n"), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "file-context.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	require.NoError(t, store.ReplaceDocLinksForPath(ctx, codeRel, []codeanchor.DocLink{{
		SrcType: "code",
		SrcPath: codeRel,
		DstKind: "note",
		DstPath: noteRel,
	}}))
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{NotesHash: "notes", LoadedAt: 1, Ready: true},
		Notes: []semdb.NoteMetadataRow{{
			Path: noteRel, Title: "Decision", ContentHash: "decision", Mtime: 1, Size: 1,
		}},
		PropertyValues: []semdb.NotePropertyValueRow{{
			NotePath: noteRel, PropertyName: "aliases", Source: semdb.NotePropertySourceFrontmatter,
			ValueText: "DEC-001", ValueNorm: "dec-001", ValueKind: semdb.NotePropertyValueString,
		}},
	}))

	reader := newIndexedCoderefNoteReader(t, obsidian.VaultDefinition{Path: root})
	result, err := BuildFileContext(codePath, FileContextParams{
		Context:         ctx,
		IndexedReadOnly: true,
		VaultDef:        obsidian.VaultDefinition{Path: root},
		ProjectRoot:     root,
		NoteReader:      reader,
		SessionStore:    store,
	})
	require.NoError(t, err)
	require.Zero(t, reader.listCalls)
	require.Zero(t, reader.snapshotCalls)
	require.Len(t, result.LinkedNotes, 3)

	byKind := make(map[string]LinkedNoteContext, len(result.LinkedNotes))
	for _, note := range result.LinkedNotes {
		require.Equal(t, noteRel, note.Path)
		byKind[note.Kind] = note
	}
	require.Equal(t, LinkedNoteContext{
		Path: noteRel, Fragment: "^story-a", RawTarget: "DEC-001#^story-a", Title: "Decision",
		Kind: string(coderefs.RefKindWikilink), Line: 3, Snippet: "// See [[DEC-001#^story-a]].",
		Frontmatter: map[string]interface{}{"summary": "indexed decision"},
	}, byKind[string(coderefs.RefKindWikilink)])
	require.Equal(t, LinkedNoteContext{
		Path: noteRel, Fragment: "Details", RawTarget: "docs/Decision.md#Details", Title: "Decision",
		Kind: string(coderefs.RefKindMdLink), Line: 4, Snippet: "// See [decision](docs/Decision.md#Details).",
		Frontmatter: map[string]interface{}{"summary": "indexed decision"},
	}, byKind[string(coderefs.RefKindMdLink)])
	require.Equal(t, LinkedNoteContext{
		Path: noteRel, RawTarget: "DEC-001", Title: "Decision",
		Kind: string(coderefs.RefKindMention), Line: 5, Snippet: "// Follow @DEC-001 for context.",
		Frontmatter: map[string]interface{}{"summary": "indexed decision"},
	}, byKind[string(coderefs.RefKindMention)])
}

func TestBuildFileContext_IndexedCoderefReadUsesRequestContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	root := t.TempDir()
	codePath := filepath.Join(root, "worker.go")
	require.NoError(t, os.WriteFile(codePath, []byte("// See [[Decision]].\npackage pkg\n"), 0o644))
	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "file-context.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	reader := newIndexedCoderefNoteReader(t, obsidian.VaultDefinition{Path: root})
	result, err := BuildFileContext(codePath, FileContextParams{
		Context:         ctx,
		IndexedReadOnly: true,
		VaultDef:        obsidian.VaultDefinition{Path: root},
		ProjectRoot:     root,
		NoteReader:      reader,
		SessionStore:    store,
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, result.LinkedNotes)
	require.Zero(t, reader.listCalls)
	require.Zero(t, reader.snapshotCalls)
}

func TestBuildFileContext_IndexedCoderefResolvesTargetedAncestorAndExpansionLinks(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	codeRel := filepath.ToSlash(filepath.Join("pkg", "worker.go"))
	codePath := filepath.Join(root, filepath.FromSlash(codeRel))
	hubRel := "docs/Hub.md"
	guidanceRel := "docs/Guidance.md"
	expansionRel := "docs/Expansion.md"
	require.NoError(t, os.MkdirAll(filepath.Dir(codePath), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.WriteFile(codePath, []byte("// See [[HUB]].\npackage pkg\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "CONTEXT.md"), []byte("# Context\n\nFollow [[GUIDE.md]].\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(hubRel)), []byte("---\ntags: [type/hub]\nsummary: hub\n---\n# Hub\n\n- [[EXPANSION]]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(guidanceRel)), []byte("---\nsummary: ancestor guidance\n---\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(expansionRel)), []byte("---\nsummary: expanded guidance\n---\n"), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "file-context.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	require.NoError(t, store.ReplaceDocLinksForPath(ctx, codeRel, []codeanchor.DocLink{{
		SrcType: "code",
		SrcPath: codeRel,
		DstKind: "note",
		DstPath: hubRel,
	}}))
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{NotesHash: "notes", LoadedAt: 1, Ready: true},
		Notes: []semdb.NoteMetadataRow{
			{Path: hubRel, Title: "Hub", ContentHash: "hub", Mtime: 1, Size: 1},
			{Path: guidanceRel, Title: "Guidance", ContentHash: "guidance", Mtime: 1, Size: 1},
			{Path: expansionRel, Title: "Expansion", ContentHash: "expansion", Mtime: 1, Size: 1},
		},
		PropertyValues: []semdb.NotePropertyValueRow{
			{NotePath: hubRel, PropertyName: "aliases", Source: semdb.NotePropertySourceFrontmatter, ValueText: "HUB", ValueNorm: "hub", ValueKind: semdb.NotePropertyValueString},
			{NotePath: guidanceRel, PropertyName: "aliases", Source: semdb.NotePropertySourceFrontmatter, ValueText: "GUIDE", ValueNorm: "guide", ValueKind: semdb.NotePropertyValueString},
			{NotePath: expansionRel, PropertyName: "aliases", Source: semdb.NotePropertySourceFrontmatter, ValueText: "EXPANSION", ValueNorm: "expansion", ValueKind: semdb.NotePropertyValueString},
		},
	}))

	reader := newIndexedCoderefNoteReader(t, obsidian.VaultDefinition{Path: root})
	result, err := BuildFileContext(codePath, FileContextParams{
		Context:              ctx,
		IndexedReadOnly:      true,
		VaultDef:             obsidian.VaultDefinition{Path: root},
		ProjectRoot:          root,
		DocPatterns:          []string{"CONTEXT.md"},
		NoteReader:           reader,
		SessionStore:         store,
		ExpandNoteLinks:      []string{"tag:type/hub"},
		ExpandNoteLinksLimit: 8,
	})
	require.NoError(t, err)
	require.Zero(t, reader.listCalls)
	require.Zero(t, reader.snapshotCalls)

	byPath := make(map[string]LinkedNoteContext, len(result.LinkedNotes))
	for _, note := range result.LinkedNotes {
		byPath[note.Path] = note
	}
	require.Equal(t, string(coderefs.RefKindWikilink), byPath[hubRel].Kind)
	require.Equal(t, "ancestorDoc", byPath[guidanceRel].Kind)
	require.Equal(t, "linkedFromHub", byPath[expansionRel].Kind)
}

func TestBuildFileContext_IndexedDirectoryResolvesTargetedAncestorAndExpansionLinks(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	targetDir := filepath.Join(root, "pkg")
	hubRel := "docs/Hub.md"
	expansionRel := "docs/Expansion.md"
	require.NoError(t, os.MkdirAll(targetDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "CONTEXT.md"), []byte("# Context\n\nFollow [[HUB]].\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(hubRel)), []byte("---\ntags: [type/hub]\nsummary: hub\n---\n# Hub\n\n- [[EXPANSION]]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(expansionRel)), []byte("---\nsummary: expanded guidance\n---\n"), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "file-context.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{NotesHash: "notes", LoadedAt: 1, Ready: true},
		Notes: []semdb.NoteMetadataRow{
			{Path: hubRel, Title: "Hub", ContentHash: "hub", Mtime: 1, Size: 1},
			{Path: expansionRel, Title: "Expansion", ContentHash: "expansion", Mtime: 1, Size: 1},
		},
		PropertyValues: []semdb.NotePropertyValueRow{
			{NotePath: hubRel, PropertyName: "aliases", Source: semdb.NotePropertySourceFrontmatter, ValueText: "HUB", ValueNorm: "hub", ValueKind: semdb.NotePropertyValueString},
			{NotePath: expansionRel, PropertyName: "aliases", Source: semdb.NotePropertySourceFrontmatter, ValueText: "EXPANSION", ValueNorm: "expansion", ValueKind: semdb.NotePropertyValueString},
		},
	}))

	reader := newIndexedCoderefNoteReader(t, obsidian.VaultDefinition{Path: root})
	result, err := BuildFileContext(targetDir, FileContextParams{
		Context:              ctx,
		IndexedReadOnly:      true,
		VaultDef:             obsidian.VaultDefinition{Path: root},
		ProjectRoot:          root,
		DocPatterns:          []string{"CONTEXT.md"},
		NoteReader:           reader,
		SessionStore:         store,
		ExpandNoteLinks:      []string{"tag:type/hub"},
		ExpandNoteLinksLimit: 8,
	})
	require.NoError(t, err)
	require.Zero(t, reader.listCalls)
	require.Zero(t, reader.snapshotCalls)

	byPath := make(map[string]LinkedNoteContext, len(result.LinkedNotes))
	for _, note := range result.LinkedNotes {
		byPath[note.Path] = note
	}
	require.Equal(t, "ancestorDoc", byPath[hubRel].Kind)
	require.Equal(t, "linkedFromHub", byPath[expansionRel].Kind)
}

func TestBuildFileContext_IndexedReadOnlyWithoutStoreDoesNotInventoryNotes(t *testing.T) {
	root := t.TempDir()
	codePath := filepath.Join(root, "worker.go")
	require.NoError(t, os.WriteFile(codePath, []byte("package pkg\n"), 0o644))

	reader := &indexedCoderefNoteReader{}
	result, err := BuildFileContext(codePath, FileContextParams{
		Context:         context.Background(),
		IndexedReadOnly: true,
		VaultDef:        obsidian.VaultDefinition{Path: root},
		ProjectRoot:     root,
		NoteReader:      reader,
	})
	require.NoError(t, err)
	require.Empty(t, result.LinkedNotes)
	require.Zero(t, reader.listCalls)
	require.Zero(t, reader.snapshotCalls)
}

func TestBuildFileContext_IndexedCoderefCapMarksContextTruncated(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	codeRel := "pkg/worker.go"
	codePath := filepath.Join(root, filepath.FromSlash(codeRel))
	require.NoError(t, os.MkdirAll(filepath.Dir(codePath), 0o755))

	links := make([]codeanchor.DocLink, 0, indexedFileContextCoderefLimit)
	var source strings.Builder
	source.WriteString("package pkg\n")
	notes := make([]semdb.NoteMetadataRow, 0, indexedFileContextCoderefLimit)
	for i := 0; i < indexedFileContextCoderefLimit; i++ {
		notePath := fmt.Sprintf("docs/Note-%03d.md", i)
		links = append(links, codeanchor.DocLink{SrcType: "code", SrcPath: codeRel, DstKind: "note", DstPath: notePath})
		notes = append(notes, semdb.NoteMetadataRow{Path: notePath, Title: fmt.Sprintf("Note %03d", i), ContentHash: "hash", Mtime: 1, Size: 1})
		fmt.Fprintf(&source, "// See [[%s]].\n", notePath)
	}
	require.NoError(t, os.WriteFile(codePath, []byte(source.String()), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "file-context.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	require.NoError(t, store.ReplaceDocLinksForPath(ctx, codeRel, links))
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{NotesHash: "notes", LoadedAt: 1, Ready: true},
		Notes: notes,
	}))

	result, err := BuildFileContext(codePath, FileContextParams{
		Context:         ctx,
		IndexedReadOnly: true,
		VaultDef:        obsidian.VaultDefinition{Path: root},
		ProjectRoot:     root,
		NoteReader:      &indexedCoderefNoteReader{},
		SessionStore:    store,
	})
	require.NoError(t, err)
	require.True(t, result.Truncated)

	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root}}
	markFileContextIndexCurrent(t, ctx, vault, store, codePath)
	text, err := BuildFileContextText(vault, &indexedCoderefNoteReader{}, FileContextTextParams{
		Context:         ctx,
		NoteMetadata:    testNoteMetadataIndexer(t),
		BudgetChars:     200000,
		Profile:         ContextProfileCode,
		Files:           []string{codePath},
		SessionStore:    store,
		IndexedReadOnly: true,
	})
	require.NoError(t, err)
	require.Contains(t, text, "- trimmed: true", "source caps must use the existing rendered truncation signal")
}

func TestBuildFileContext_IndexedLinkTargetCapMarksContextTruncated(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	targetDir := filepath.Join(root, "pkg")
	require.NoError(t, os.MkdirAll(targetDir, 0o755))

	var contextDoc strings.Builder
	contextDoc.WriteString("# Context\n")
	notes := make([]semdb.NoteMetadataRow, 0, indexedFileContextLinkTargetLimit+1)
	for i := 0; i <= indexedFileContextLinkTargetLimit; i++ {
		notePath := fmt.Sprintf("docs/Note-%03d.md", i)
		fmt.Fprintf(&contextDoc, "- [[%s]]\n", notePath)
		notes = append(notes, semdb.NoteMetadataRow{Path: notePath, Title: fmt.Sprintf("Note %03d", i), ContentHash: "hash", Mtime: 1, Size: 1})
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "CONTEXT.md"), []byte(contextDoc.String()), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "file-context.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{NotesHash: "notes", LoadedAt: 1, Ready: true},
		Notes: notes,
	}))

	result, err := BuildFileContext(targetDir, FileContextParams{
		Context:         ctx,
		IndexedReadOnly: true,
		VaultDef:        obsidian.VaultDefinition{Path: root},
		ProjectRoot:     root,
		DocPatterns:     []string{"CONTEXT.md"},
		NoteReader:      &indexedCoderefNoteReader{},
		SessionStore:    store,
	})
	require.NoError(t, err)
	require.True(t, result.Truncated)
}
