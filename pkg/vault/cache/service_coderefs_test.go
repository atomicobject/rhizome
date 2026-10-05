package cache

// Docs: [[Code reference scanning in cache]]

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helpers
func writeFile(t *testing.T, root, rel, content string) string {
	t.Helper()
	abs := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
	return abs
}

func newCodeRefService(t *testing.T, root string, opts Options) *Service {
	t.Helper()
	if opts.CodeRefConfig == nil {
		opts.CodeRefConfig = &coderefs.Config{
			Enabled:  true,
			Includes: []string{"**/*.go"},
			Excludes: []string{},
		}
	}
	svc, err := NewService(root, opts)
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))
	return svc
}

func TestCodeRefs_InitialCrawl(t *testing.T) {
	tmp := t.TempDir()

	writeFile(t, tmp, "Note.md", "# Note")
	writeFile(t, tmp, "main.go", "// [[Note]]")

	svc := newCodeRefService(t, tmp, Options{})

	refs := svc.CodeRefsByNote()
	require.Len(t, refs["Note.md"], 1)
	assert.Equal(t, "main.go", refs["Note.md"][0].SourceFile)
	assert.Equal(t, coderefs.RefKindWikilink, refs["Note.md"][0].Kind)

	// A content-only note change keeps the resolved reference.
	writeFile(t, tmp, "Note.md", "# Note updated content")
	svc.MarkDirty("Note.md", DirtyModified)
	require.NoError(t, svc.Refresh(context.Background()))

	refs = svc.CodeRefsByNote()
	require.Len(t, refs["Note.md"], 1)
	assert.Equal(t, "main.go", refs["Note.md"][0].SourceFile)
	assert.Equal(t, coderefs.RefKindWikilink, refs["Note.md"][0].Kind)
}

func TestCodeRefs_IncrementalCodeChange(t *testing.T) {
	tmp := t.TempDir()

	writeFile(t, tmp, "Note.md", "# Note")
	codePath := writeFile(t, tmp, "main.go", "// [[Note]]")

	svc := newCodeRefService(t, tmp, Options{})

	// Modify code file to add an @mention
	time.Sleep(10 * time.Millisecond) // ensure mtime changes
	writeFile(t, tmp, "main.go", "// [[Note]] and @Note")
	svc.markDirty(codePath, DirtyModified)
	require.NoError(t, svc.Refresh(context.Background()))

	refs := svc.CodeRefsByNote()
	require.Len(t, refs["Note.md"], 2)
	kinds := []coderefs.RefKind{refs["Note.md"][0].Kind, refs["Note.md"][1].Kind}
	assert.Contains(t, kinds, coderefs.RefKindWikilink)
	assert.Contains(t, kinds, coderefs.RefKindMention)
}

func TestCodeRefs_NoteDeleted(t *testing.T) {
	tmp := t.TempDir()

	writeFile(t, tmp, "Note.md", "# Note")
	writeFile(t, tmp, "main.go", "// [[Note]]")

	svc := newCodeRefService(t, tmp, Options{})

	refs := svc.CodeRefsByNote()
	require.Len(t, refs["Note.md"], 1)

	// Delete the note
	require.NoError(t, os.Remove(filepath.Join(tmp, "Note.md")))
	svc.MarkDirty("Note.md", DirtyRemoved)
	require.NoError(t, svc.Refresh(context.Background()))

	refs = svc.CodeRefsByNote()
	assert.Empty(t, refs["Note.md"])
}

func TestCodeRefs_SelectedMixedCaseMarkdownNoteDeleted(t *testing.T) {
	tmp := t.TempDir()

	writeFile(t, tmp, "Doc.MD", "# Doc")
	writeFile(t, tmp, "main.go", "// [[Doc]]")
	svc := newCodeRefService(t, tmp, Options{AdmitNote: func(path paths.NotePath) bool {
		return path.String() == "Doc.MD"
	}})

	refs := svc.CodeRefsByNote()
	require.Len(t, refs["Doc.MD"], 1)

	require.NoError(t, os.Remove(filepath.Join(tmp, "Doc.MD")))
	svc.markDirty(filepath.Join(tmp, "Doc.MD"), DirtyRemoved)
	require.NoError(t, svc.Refresh(context.Background()))

	assert.Empty(t, svc.CodeRefsByNote()["Doc.MD"])
}

func TestCodeRefs_NewNoteTriggersFullScan(t *testing.T) {
	tmp := t.TempDir()

	// Code references a note that doesn't exist yet
	writeFile(t, tmp, "main.go", "// [[NewNote]]")

	svc := newCodeRefService(t, tmp, Options{})

	refs := svc.CodeRefsByNote()
	assert.Empty(t, refs["NewNote.md"])

	// Create the note and mark dirty as created
	writeFile(t, tmp, "NewNote.md", "# New Note")
	svc.MarkDirty("NewNote.md", DirtyCreated)
	require.NoError(t, svc.Refresh(context.Background()))

	refs = svc.CodeRefsByNote()
	require.Len(t, refs["NewNote.md"], 1, "new note should be discovered and code refs populated")
}

func TestCodeRefs_DirectoryRefresh(t *testing.T) {
	tmp := t.TempDir()

	writeFile(t, tmp, "ExistingNote.md", "# Note")
	svc := newCodeRefService(t, tmp, Options{})
	assert.Empty(t, svc.CodeRefsByNote()["ExistingNote.md"])

	// A directory appears with shallow and nested code files (checkout, move).
	writeFile(t, tmp, "newdir/app.go", "// [[ExistingNote]] reference")
	writeFile(t, tmp, "newdir/helper.go", "// @ExistingNote mention")
	writeFile(t, tmp, "newdir/pkg/utils/util.go", "// See [[ExistingNote]]")
	svc.MarkDirty("newdir", DirtyCreated)
	require.NoError(t, svc.Refresh(context.Background()))

	kinds := map[string]coderefs.RefKind{}
	for _, ref := range svc.CodeRefsByNote()["ExistingNote.md"] {
		kinds[ref.SourceFile] = ref.Kind
	}
	require.Equal(t, map[string]coderefs.RefKind{
		"newdir/app.go":            coderefs.RefKindWikilink,
		"newdir/helper.go":         coderefs.RefKindMention,
		"newdir/pkg/utils/util.go": coderefs.RefKindWikilink,
	}, kinds)
	require.Len(t, svc.CodeRefsByNote()["ExistingNote.md"], 3)
}

func TestCodeRefs_GlobVaultRespectsExcludes(t *testing.T) {
	tmp := t.TempDir()

	// Markdown files discovered via glob (simulate collection vault)
	writeFile(t, tmp, "docs/Note.md", "# Note")
	writeFile(t, tmp, "docs/Skip.md", "# Skip")

	// Code files: one should be excluded by user excludes
	writeFile(t, tmp, "src/keep.go", "// [[docs/Note]]")
	writeFile(t, tmp, "skip/skip.go", "// [[docs/Note]]")

	opts := Options{
		// Simulate glob-based discovery returning only markdown files
		DiscoverFiles: func() ([]string, error) {
			return []string{"docs/Note.md", "docs/Skip.md"}, nil
		},
		UserExcludes: []string{"**/skip/**"},
		CodeRefConfig: &coderefs.Config{
			Enabled:  true,
			Includes: []string{"**/*.go"},
			Excludes: []string{},
		},
	}

	svc := newCodeRefService(t, tmp, opts)

	refs := svc.CodeRefsByNote()
	require.Len(t, refs["docs/Note.md"], 1, "only non-excluded code file should be indexed")
	assert.Equal(t, "src/keep.go", refs["docs/Note.md"][0].SourceFile)
}
