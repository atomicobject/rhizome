package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// writeExplainFixture builds the wrapper-repo shape: root .gitignore excludes
// app/, app/.gitignore excludes dist/, .rhizome/ignore re-includes /app/.
func writeExplainFixture(t *testing.T, withRhizomeIgnore bool) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("app/\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "app", "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "app", ".gitignore"), []byte("dist/\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "app", "dist"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "app", "dist", "x.js"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "app", "src", "main.go"), []byte("package main\n"), 0o644))
	if withRhizomeIgnore {
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ignore"), []byte("!/app/\n"), 0o644))
	}
	return root
}

func explainOutput(t *testing.T, root, input string) string {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, runIndexExplain(&buf, root, nil, input))
	return buf.String()
}

func TestIndexExplainNestedGitignoreExcludes(t *testing.T) {
	root := writeExplainFixture(t, true)
	out := explainOutput(t, root, "app/dist/x.js")
	require.Contains(t, out, "app/dist/x.js: ignored")
	require.Contains(t, out, "app/.gitignore:1 'dist/' (gitignore layer)")
	require.Contains(t, out, "via ancestor 'app/dist'")
}

func TestIndexExplainReincludedByRhizomeIgnore(t *testing.T) {
	root := writeExplainFixture(t, true)
	out := explainOutput(t, root, "app/src/main.go")
	require.Contains(t, out, "app/src/main.go: indexed")
	require.Contains(t, out, "re-included by .rhizome/ignore:1 '!/app/'")
}

func TestIndexExplainBuiltinDefaults(t *testing.T) {
	root := writeExplainFixture(t, false)
	out := explainOutput(t, root, "node_modules/x")
	require.Contains(t, out, "node_modules/x: ignored")
	require.Contains(t, out, "built-in defaults")
	require.Contains(t, out, "node_modules")
}

func TestIndexExplainNonexistentPathStillEvaluates(t *testing.T) {
	root := writeExplainFixture(t, true)
	out := explainOutput(t, root, "app/dist/ghost/deep.ts")
	require.Contains(t, out, "app/dist/ghost/deep.ts: ignored")
	require.Contains(t, out, "'dist/'")

	out = explainOutput(t, root, "app/brand-new/note.md")
	require.Contains(t, out, "app/brand-new/note.md: indexed")
}

func TestIndexExplainIncludedNoRule(t *testing.T) {
	root := writeExplainFixture(t, true)
	out := explainOutput(t, root, "docs/readme.md")
	require.Contains(t, out, "docs/readme.md: indexed")
	require.Contains(t, out, "included (no ignore rule matches)")
}

func TestIndexExplainContextBypassesConfigExcludeButHonorsHardIgnore(t *testing.T) {
	root := t.TempDir()
	var buf bytes.Buffer
	require.NoError(t, runIndexExplain(&buf, root, []string{"src/**"}, "src/CONTEXT.md"))
	require.Contains(t, buf.String(), "src/CONTEXT.md: indexed")
	require.Contains(t, buf.String(), "system CONTEXT.md bypasses notes includes/excludes")

	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ignore"), []byte("src/\n"), 0o644))
	buf.Reset()
	require.NoError(t, runIndexExplain(&buf, root, []string{"!src/**"}, "src/CONTEXT.md"))
	require.Contains(t, buf.String(), "src/CONTEXT.md: ignored")
	require.Contains(t, buf.String(), ".rhizome/ignore:1 'src/'")
}

func TestIndexExplainPathOutsideVaultErrors(t *testing.T) {
	root := writeExplainFixture(t, true)
	var buf bytes.Buffer
	err := runIndexExplain(&buf, root, nil, filepath.Join(t.TempDir(), "outside.md"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "outside the vault root")

	err = runIndexExplain(&buf, root, nil, "../escape.md")
	require.Error(t, err)
}

func TestIndexExplainDirectoryHandling(t *testing.T) {
	root := writeExplainFixture(t, true)
	// Trailing slash forces directory evaluation even for nonexistent paths.
	out := explainOutput(t, root, "app/dist/")
	require.Contains(t, out, "app/dist: ignored")

	// Existing directory without trailing slash is evaluated as a directory.
	out = explainOutput(t, root, "app/dist")
	require.Contains(t, out, "app/dist: ignored")
	require.Contains(t, out, "'dist/'")
}

func TestRenderIgnoreExplainDirectMatcher(t *testing.T) {
	root := writeExplainFixture(t, true)
	matcher := obsidian.LoadVaultIgnoreMatcher(root, []string{"secrets/**"})
	var buf bytes.Buffer
	renderIgnoreExplain(&buf, matcher, "secrets/key.md", false)
	require.Contains(t, buf.String(), "secrets/key.md: ignored")
	require.Contains(t, buf.String(), "config excludes 'secrets/**'")
}

func TestIndexExplainReportsWhetherACodeFileIsIndexedAsCode(t *testing.T) {
	for _, tc := range []struct {
		name, config, want string
	}{
		{"code off", "notes:\n  includes: [\"docs/**/*.md\"]\n", "not indexed as Go code: code indexing is off; run rzm init to turn it on"},
		{"whole repository", "code:\n  enabled: true\n", "indexed as Go code (code indexing covers the whole repository)"},
		{"language turned off", "code:\n  enabled: true\n  disabledLanguages: [go]\n", "not indexed as Go code: code.disabledLanguages in .rhizome/config.yml turns Go off; remove it there to index this file"},
		{"inside folder limits", "code:\n  enabled: true\n  go:\n    roots: [tools]\n", "indexed as Go code (inside the code folders in .rhizome/config.yml)"},
		{"outside folder limits", "code:\n  enabled: true\n  go:\n    roots: [cmd]\n", "not indexed as Go code: outside the code folders in .rhizome/config.yml (cmd); run rzm init to remove the folder limits"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte(tc.config), 0o644))
			require.NoError(t, os.MkdirAll(filepath.Join(root, "tools"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(root, "tools", "main.go"), []byte("package main\n"), 0o644))

			out := explainOutput(t, root, "tools/main.go")

			require.Contains(t, out, "tools/main.go: indexed")
			require.Contains(t, out, tc.want)
		})
	}
}
