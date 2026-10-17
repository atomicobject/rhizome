package runtimeproviders

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestCodeForNoteAuthoredAnchorFallback(t *testing.T) {
	const authored = "---\ncode-anchors:\n  go:\n    - label: API\n      glob: pkg/api/*.go\n    - label: Query\n      glob: pkg/query/*.go\n---\n"
	formats, err := testkit.NoteMetadataIndexer(t).FormatRuntime()
	require.NoError(t, err)
	for _, tc := range []struct {
		name, path, content, warning string
		withoutRuntime               bool
		first                        int
		code                         []ontologyquery.RuntimePath
	}{
		{name: "markdown targets", path: "notes/Spec.MD", content: authored, warning: "code_anchor_scope_unavailable", code: []ontologyquery.RuntimePath{{Path: "pkg/api/*.go", Kind: "glob", Reason: "API"}, {Path: "pkg/query/*.go", Kind: "glob", Reason: "Query"}}},
		{name: "bounded targets", path: "notes/Spec.md", content: authored, first: 1, warning: "code_anchor_scope_unavailable", code: []ontologyquery.RuntimePath{{Path: "pkg/api/*.go", Kind: "glob", Reason: "API"}}},
		{name: "html is not parsed as markdown", path: "notes/Spec.html", content: authored, warning: "code_anchor_fallback_unsupported_format"},
		{name: "unowned source", path: "notes/Spec.txt", content: authored, warning: "note_format_unavailable"},
		{name: "missing runtime", path: "notes/Spec.md", content: authored, withoutRuntime: true, warning: "note_format_unavailable"},
		{name: "invalid anchor syntax", path: "notes/Spec.md", content: "---\ncode-anchors:\n  go:\n    - symbol: Unqualified\n---\n", warning: "note_anchor_parse_failed"},
		{name: "no anchors", path: "notes/Spec.md", content: "# No code anchors\n", warning: "code_anchors_not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			abs := filepath.Join(root, filepath.FromSlash(tc.path))
			require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
			require.NoError(t, os.WriteFile(abs, []byte(tc.content), 0o600))
			runtime := formats
			if tc.withoutRuntime {
				runtime = noteformat.Runtime{}
			}
			provider := New(nil, nil, ontologyquery.Deps{VaultDef: obsidian.VaultDefinition{Root: root, Includes: []string{"**/*.md", "**/*.MD", "**/*.html", "**/*.txt"}}, NoteFormats: runtime}, nil)
			pack, err := provider.CodeForNote(context.Background(), ontologyquery.CodeRuntimeRequest{Path: tc.path, First: tc.first})
			require.NoError(t, err)
			require.True(t, pack.Available)
			require.Equal(t, tc.path, pack.NormalizedPath)
			require.Equal(t, tc.code, pack.Code)
			require.Len(t, pack.Warnings, 1)
			require.Equal(t, tc.warning, pack.Warnings[0].Code)
			require.Equal(t, tc.path, pack.Warnings[0].Path)
		})
	}
}

func TestCodeForNoteAuthoredFallbackRejectsEscapingSymlink(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "private.md"), []byte("---\ncode-anchors:\n  go:\n    - glob: private/*.go\n---\n"), 0o600))
	if err := os.Symlink(filepath.Join(outside, "private.md"), filepath.Join(root, "alias.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	formats, err := testkit.NoteMetadataIndexer(t).FormatRuntime()
	require.NoError(t, err)
	provider := New(nil, nil, ontologyquery.Deps{VaultDef: obsidian.VaultDefinition{Path: root}, NoteFormats: formats}, nil)
	pack, err := provider.CodeForNote(context.Background(), ontologyquery.CodeRuntimeRequest{Path: "alias.md"})
	require.NoError(t, err)
	require.Empty(t, pack.Code)
	require.Len(t, pack.Warnings, 1)
	require.Equal(t, "invalid_note_path", pack.Warnings[0].Code)
}

func TestCodeForNoteChecksAdmissionBeforeReadingSource(t *testing.T) {
	formats, err := testkit.NoteMetadataIndexer(t).FormatRuntime()
	require.NoError(t, err)
	for _, tc := range []struct {
		name, path, warning string
		excludes            []string
		withoutRuntime      bool
	}{
		{name: "foreign provider", path: "notes/missing.html", warning: "code_anchor_fallback_unsupported_format"},
		{name: "excluded note", path: "notes/missing.md", excludes: []string{"notes/**"}, warning: "note_format_unavailable"},
		{name: "code owned", path: "missing.go", warning: "note_format_unavailable"},
		{name: "missing runtime", path: "notes/missing.md", withoutRuntime: true, warning: "note_format_unavailable"},
		{name: "admitted missing source", path: "notes/missing.md", warning: "note_read_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := formats
			if tc.withoutRuntime {
				runtime = noteformat.Runtime{}
			}
			provider := New(nil, nil, ontologyquery.Deps{
				VaultDef: obsidian.VaultDefinition{Root: t.TempDir(), Includes: []string{"notes/*.md", "notes/*.html"}, Excludes: tc.excludes}, NoteFormats: runtime,
			}, nil)
			pack, err := provider.CodeForNote(context.Background(), ontologyquery.CodeRuntimeRequest{Path: tc.path})
			require.NoError(t, err)
			require.Empty(t, pack.Code)
			require.Len(t, pack.Warnings, 1)
			require.Equal(t, tc.warning, pack.Warnings[0].Code)
		})
	}
}
