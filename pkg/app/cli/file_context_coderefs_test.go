package actions

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestScanConfiguredCodeRefsForInputsHonorsIncludesExcludesAndAliases(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "src", "generated"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "other"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte(`notes:
  includes: ["**/*.md"]
code:
  scan: ["src/**/*.py"]
  ignore: ["src/generated/**"]
`), 0o644))
	for path, content := range map[string]string{
		"src/main.py":           "# WHY: implements [[SPEC-42#Story A]]\n",
		"src/generated/skip.py": "# WHY: implements [[SPEC-42#Story A]]\n",
		"other/skip.py":         "# WHY: implements [[SPEC-42#Story A]]\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte(content), 0o644))
	}

	reader := &configuredCodeRefTestReader{entries: []obsidian.NoteEntry{{
		Path:        "docs/spec.md",
		Frontmatter: map[string]interface{}{"aliases": []interface{}{"SPEC-42"}},
	}}}
	refs, err := ScanConfiguredCodeRefsForInputs(context.Background(), obsidian.VaultDefinition{Path: root}, reader, []string{
		"src/main.py",
		"src/generated/skip.py",
		"other/skip.py",
	})
	require.NoError(t, err)
	require.Equal(t, []string{"src/main.py"}, mapKeys(refs))
	require.Len(t, refs["src/main.py"], 1)
	require.Equal(t, "docs/spec.md", refs["src/main.py"][0].Target)
}

func TestScanConfiguredCodeRefsForInputsSkipsNonRegularTargets(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte(`notes:
  includes: ["**/*.md"]
code:
  scan: ["**"]
`), 0o644))

	refs, err := ScanConfiguredCodeRefsForInputs(
		context.Background(),
		obsidian.VaultDefinition{Path: root},
		&configuredCodeRefTestReader{},
		[]string{"src"},
	)
	require.NoError(t, err)
	require.Empty(t, refs)
}

type configuredCodeRefTestReader struct {
	entries []obsidian.NoteEntry
}

func (r *configuredCodeRefTestReader) GetContents(obsidian.VaultDefinition, string) (string, error) {
	return "", nil
}

func (r *configuredCodeRefTestReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	paths := make([]string, 0, len(r.entries))
	for _, entry := range r.entries {
		paths = append(paths, entry.Path)
	}
	return paths, nil
}

func (r *configuredCodeRefTestReader) GetModTime(obsidian.VaultDefinition, string) (time.Time, error) {
	return time.Time{}, nil
}

func (r *configuredCodeRefTestReader) Title(string) (string, bool) {
	return "", false
}

func (r *configuredCodeRefTestReader) NoteEntriesSnapshot(context.Context) ([]obsidian.NoteEntry, error) {
	return append([]obsidian.NoteEntry(nil), r.entries...), nil
}

func mapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
