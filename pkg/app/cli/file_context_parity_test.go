package actions

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type fileContextCancellationProbe struct {
	seenErr error
}

func (p *fileContextCancellationProbe) LoadOntologyRuntime(ctx context.Context) (*ontology.Runtime, error) {
	p.seenErr = ctx.Err()
	return nil, ctx.Err()
}

type fileContextParityFixture struct {
	root       string
	vault      stubContextVault
	note       obsidian.NoteReader
	codeFile   string
	codeDir    string
	noteFile   string
	noteRel    string
	codeRel    string
	codeDirRel string
	store      *semdb.Store
}

func newFileContextParityFixture(t *testing.T) fileContextParityFixture {
	t.Helper()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(root, ".rhizome", "config.yml"),
		[]byte("notes:\n  includes: [\"**/*.md\"]\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "CONTEXT.md"),
		[]byte("# Root guidance\n\nroot file-context contract\n"),
		0o644,
	))

	codeDirRel := filepath.Join("pkg", "service")
	codeDir := filepath.Join(root, codeDirRel)
	require.NoError(t, os.MkdirAll(codeDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(codeDir, "CONTEXT.md"),
		[]byte("# Service guidance\n\nservice file-context contract\n"),
		0o644,
	))
	codeRel := filepath.Join(codeDirRel, "worker.go")
	codeFile := filepath.Join(root, codeRel)
	require.NoError(t, os.WriteFile(codeFile, []byte("package service\n"), 0o644))

	noteRel := filepath.Join("notes", "Decision.md")
	noteFile := filepath.Join(root, noteRel)
	require.NoError(t, os.MkdirAll(filepath.Dir(noteFile), 0o755))
	require.NoError(t, os.WriteFile(
		noteFile,
		[]byte("---\nsummary: \"decision file-context contract\"\n---\n\n# Decision\n"),
		0o644,
	))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})
	require.NoError(t, store.ReplaceGraphDocScores(context.Background(), []semdb.GraphDocScore{{
		DocPath: filepath.ToSlash(noteRel),
		DocType: "note",
	}}))

	vaultDef := obsidian.VaultDefinition{Name: "test", Root: root, Includes: []string{"**/*.md"}}
	fixture := fileContextParityFixture{
		root:       root,
		vault:      stubContextVault{def: vaultDef},
		note:       newProjectedFilesystemFactReader(t, vaultDef, &obsidian.Note{}),
		codeFile:   codeFile,
		codeDir:    codeDir,
		noteFile:   noteFile,
		noteRel:    filepath.ToSlash(noteRel),
		codeRel:    filepath.ToSlash(codeRel),
		codeDirRel: filepath.ToSlash(codeDirRel),
		store:      store,
	}
	markFileContextIndexCurrent(t, context.Background(), fixture.vault, store, fixture.codeFile, fixture.noteFile)
	return fixture
}

func renderFileContextParity(t *testing.T, fixture fileContextParityFixture, files []string) (string, string) {
	t.Helper()

	render := func(indexedReadOnly bool) string {
		text, err := BuildFileContextText(fixture.vault, fixture.note, FileContextTextParams{
			Context:         context.Background(),
			NoteMetadata:    testNoteMetadataIndexer(t),
			BudgetChars:     40000,
			Profile:         ContextProfileCode,
			Files:           files,
			SessionStore:    fixture.store,
			IndexedReadOnly: indexedReadOnly,
		})
		require.NoError(t, err)
		return text
	}

	return render(false), render(true)
}

func TestBuildFileContextText_ParityAcrossTargetShapes(t *testing.T) {
	fixture := newFileContextParityFixture(t)

	tests := []struct {
		name    string
		target  string
		want    []string
		notWant []string
	}{
		{
			name:   "code file",
			target: fixture.codeFile,
			want: []string{
				"### " + fixture.codeRel + " (code)",
				"root file-context contract",
				"service file-context contract",
			},
			notWant: []string{"## Decision (note)"},
		},
		{
			name:   "directory",
			target: fixture.codeDir,
			want: []string{
				"### " + fixture.codeDirRel + " (code)",
				"root file-context contract",
				"service file-context contract",
			},
			notWant: []string{"## Decision (note)"},
		},
		{
			name:   "markdown note",
			target: fixture.noteFile,
			want: []string{
				"## Decision (note)",
				"- path: " + fixture.noteRel,
				"- summary: decision file-context contract",
			},
			notWant: []string{"service file-context contract"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			legacy, indexed := renderFileContextParity(t, fixture, []string{tt.target})
			require.Equal(t, legacy, indexed)

			for _, want := range tt.want {
				require.Contains(t, indexed, want)
			}
			for _, notWant := range tt.notWant {
				require.NotContains(t, indexed, notWant)
			}
		})
	}
}

func TestBuildFileContextText_MixedTargetsPreserveRequestOrder(t *testing.T) {
	fixture := newFileContextParityFixture(t)

	legacy, indexed := renderFileContextParity(t, fixture, []string{
		fixture.noteFile,
		fixture.codeFile,
		fixture.codeDir,
	})
	require.Equal(t, legacy, indexed)

	headings := []string{
		"## Decision (note)",
		"### " + fixture.codeRel + " (code)",
		"### " + fixture.codeDirRel + " (code)",
	}
	previous := -1
	for _, heading := range headings {
		index := strings.Index(indexed, heading)
		require.Greater(t, index, previous, "target %q must retain request order", heading)
		previous = index
	}
}

func TestBuildFileContextText_ExactDuplicateCodeAndDirectoryTargetsReuseConstructionAndPreserveOutputOrder(t *testing.T) {
	fixture := newFileContextParityFixture(t)
	reader := &boundedNoteReader{}

	text, err := BuildFileContextText(fixture.vault, reader, FileContextTextParams{
		Context:      context.Background(),
		NoteMetadata: testNoteMetadataIndexer(t),
		BudgetChars:  40000,
		Profile:      ContextProfileCode,
		Files: []string{
			fixture.codeFile,
			fixture.codeFile,
			fixture.codeDir,
			fixture.codeDir,
		},
	})
	require.NoError(t, err)
	require.Equal(t, 2, reader.listCalls, "each distinct code/directory target should build context only once")

	codeHeading := "### " + fixture.codeRel + " (code)"
	dirHeading := "### " + fixture.codeDirRel + " (code)"
	require.Equal(t, 2, strings.Count(text, codeHeading))
	require.Equal(t, 2, strings.Count(text, dirHeading))
	positions := []int{
		strings.Index(text, codeHeading),
		strings.LastIndex(text, codeHeading),
		strings.Index(text, dirHeading),
		strings.LastIndex(text, dirHeading),
	}
	for i, position := range positions {
		require.GreaterOrEqual(t, position, 0)
		if i > 0 {
			require.Greater(t, position, positions[i-1], "duplicate targets must retain request order")
		}
	}
}

func TestBuildFileContext_PreservesDistinctCoderefOccurrences(t *testing.T) {
	fixture := newFileContextParityFixture(t)
	refs := []coderefs.CodeRef{
		{
			SourceFile: fixture.codeRel,
			Language:   "go",
			Target:     fixture.noteRel,
			Fragment:   "first-decision",
			RawTarget:  "Decision#first-decision",
			Kind:       coderefs.RefKindWikilink,
			Line:       4,
			Snippet:    "// See [[Decision#first-decision]].",
		},
		{
			SourceFile: fixture.codeRel,
			Language:   "go",
			Target:     fixture.noteRel,
			Fragment:   "second-decision",
			RawTarget:  "Decision#second-decision",
			Kind:       coderefs.RefKindWikilink,
			Line:       9,
			Snippet:    "// See [[Decision#second-decision]].",
		},
	}
	refs = append(refs, refs[1])

	result, err := BuildFileContext(fixture.codeFile, FileContextParams{
		VaultDef:       fixture.vault.def,
		ProjectRoot:    fixture.root,
		CodeRefsByFile: map[string][]coderefs.CodeRef{fixture.codeRel: refs},
		NoteReader:     fixture.note,
	})
	require.NoError(t, err)
	require.Len(t, result.LinkedNotes, 2, "exact duplicate occurrences should collapse without losing distinct occurrences")

	require.Equal(t, "first-decision", result.LinkedNotes[0].Fragment)
	require.Equal(t, "Decision#first-decision", result.LinkedNotes[0].RawTarget)
	require.Equal(t, 4, result.LinkedNotes[0].Line)
	require.Equal(t, "// See [[Decision#first-decision]].", result.LinkedNotes[0].Snippet)

	require.Equal(t, "second-decision", result.LinkedNotes[1].Fragment)
	require.Equal(t, "Decision#second-decision", result.LinkedNotes[1].RawTarget)
	require.Equal(t, 9, result.LinkedNotes[1].Line)
	require.Equal(t, "// See [[Decision#second-decision]].", result.LinkedNotes[1].Snippet)
}

func TestBuildFileContextText_ForwardsCanceledContextToOntologyProvider(t *testing.T) {
	fixture := newFileContextParityFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	probe := &fileContextCancellationProbe{}

	_, _ = BuildFileContextText(fixture.vault, fixture.note, FileContextTextParams{
		Context:                 ctx,
		NoteMetadata:            testNoteMetadataIndexer(t),
		BudgetChars:             40000,
		Profile:                 ContextProfileCode,
		Files:                   []string{fixture.noteFile},
		OntologyRuntimeProvider: probe,
	})

	require.ErrorIs(t, probe.seenErr, context.Canceled)
}

func TestBuildVaultContextText_TargetedFileUsesStandaloneFileContextBody(t *testing.T) {
	fixture := newFileContextParityFixture(t)
	codeRefs := map[string][]coderefs.CodeRef{
		fixture.codeRel: {
			{
				SourceFile: fixture.codeRel,
				Target:     fixture.noteRel,
				Kind:       coderefs.RefKindWikilink,
				Line:       3,
				Snippet:    "// See [[Decision]].",
			},
		},
	}

	standalone, err := BuildFileContextText(fixture.vault, fixture.note, FileContextTextParams{
		Context:        context.Background(),
		NoteMetadata:   testNoteMetadataIndexer(t),
		BudgetChars:    12000,
		Profile:        ContextProfileCode,
		Files:          []string{fixture.codeFile},
		CodeRefsByFile: codeRefs,
	})
	require.NoError(t, err)
	wantBody := stripRenderedContextHeader(standalone)
	require.NotEmpty(t, wantBody)

	bundled, err := BuildVaultContextText(fixture.vault, fixture.note, VaultContextTextParams{
		Context:        context.Background(),
		NoteMetadata:   testNoteMetadataIndexer(t),
		BudgetChars:    30000,
		Profile:        ContextProfileCode,
		Files:          []string{fixture.codeFile},
		CodeRefsByFile: codeRefs,
	})
	require.NoError(t, err)
	require.Contains(t, bundled, "## Target context")
	require.Contains(t, bundled, wantBody)
}
