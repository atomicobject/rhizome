package web

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestReadFileViewResolvesHTMLURIsWithProviderSemantics(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	page := `<base href="/docs/"><a href="target.html?view=wide#section">query</a><a href="#only">fragment</a><a href="/docs/space%20name.html">percent</a><a href="target.html?q=a%26b%23c#frag%20ment">encoded components</a><a href="report">ambiguous</a><a href="SPEC-0042.US1">alias</a><a href="../../outside.html">traversal</a><a href="https://example.test/docs/remote.html">external</a>`
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "page.html"), []byte(page), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "external.html"), []byte(`<base href="https://example.test/docs/"><a href="target.html?view=wide#remote">external base</a>`), 0o644))

	vaultDef := obsidian.VaultDefinition{
		Root:     root,
		Includes: []string{"**/*.html", "**/*.md"},
	}
	indexer := testNoteMetadataIndexer(t)
	catalog, err := NewFileCatalog(vaultDef, nil, indexer)
	require.NoError(t, err)
	srv := &Server{
		cfg:     Config{VaultPath: root, VaultDef: vaultDef, NoteMetadata: indexer},
		catalog: catalog,
		noteCache: obsidian.BuildNotePathCacheWithAliases(
			[]string{
				"notes/page.html",
				"notes/external.html",
				"docs/target.html",
				"docs/space name.html",
				"docs/report.md",
				"docs/report.html",
				"docs/spec.md",
			},
			map[string][]string{"docs/spec.md": {"SPEC-0042.US1"}},
		),
	}

	got, err := srv.readFileView(t.Context(), "notes/page.html")
	require.NoError(t, err)
	require.Equal(t, "note", got.Kind)
	require.Equal(t, []ResolvedLink{
		{Target: "docs/target.html?view=wide#section", Text: "target.html?view=wide#section", Kind: "uri", Anchor: "section"},
		{Target: "#only", Text: "#only", Kind: "uri", Anchor: "only"},
		{Target: "docs/space name.html", Text: "/docs/space%20name.html", Kind: "uri"},
		{Target: "docs/target.html?q=a%26b%23c#frag ment", Text: "target.html?q=a%26b%23c#frag%20ment", Kind: "uri", Anchor: "frag ment"},
		{Target: "report", Text: "report", Kind: "uri"},
		{Target: "docs/spec.md", Text: "SPEC-0042.US1", Kind: "uri"},
		{Target: "../../outside.html", Text: "../../outside.html", Kind: "uri"},
		{Target: "https://example.test/docs/remote.html", Text: "https://example.test/docs/remote.html", Kind: "uri"},
	}, got.Links)

	got, err = srv.readFileView(t.Context(), "notes/external.html")
	require.NoError(t, err)
	require.Equal(t, []ResolvedLink{{
		Target: "target.html?view=wide#remote",
		Text:   "target.html?view=wide#remote",
		Kind:   "uri",
		Anchor: "remote",
	}}, got.Links, "an external base must keep relative hrefs external")
}
