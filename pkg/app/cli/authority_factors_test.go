package actions

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestGraphAnalysisAppliesAuthorityFactorsAndRefreshesCommunities(t *testing.T) {
	dir := t.TempDir()

	// Minimal local config with graph authorityFactors.
	cfgDir := filepath.Join(dir, ".rhizome")
	require.NoError(t, os.MkdirAll(cfgDir, 0o755))
	cfg := `notes: {}
graph:
  authorityFactors:
    - inputs: ["tag:foo"]
      factor: 2.0
`
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.yml"), []byte(cfg), 0o644))

	// Graph:
	// a links to b and c; b/c have equal baseline authority and tie-break by path (b before c).
	// Tag c with #foo and boost it so it becomes the top authority in the community.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.md"), []byte("See [[b]] and [[c]]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.md"), []byte("b\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "c.md"), []byte("#foo\nc\n"), 0o644))

	vault := &obsidian.Vault{Name: dir}
	// Authority-factor predicates consume injected provider facts. A plain
	// NoteReader provides source bytes for graph construction but is not allowed
	// to parse Markdown metadata for matching.
	note := newProjectedFilesystemFactReader(t, obsidian.VaultDefinition{Name: dir, Path: dir}, &obsidian.Note{})

	analysis, err := GraphAnalysis(vault, note, GraphAnalysisParams{
		UseConfig: true,
		Options: obsidian.GraphAnalysisOptions{
			WikilinkOptions:   obsidian.DefaultWikilinkOptions,
			IncludeTags:       true,
			RecencyCascade:    true,
			RecencyCascadeSet: true,
		},
	})
	require.NoError(t, err)

	require.Contains(t, analysis.Nodes, "b.md")
	require.Contains(t, analysis.Nodes, "c.md")
	require.Greater(t, analysis.Nodes["c.md"].Authority, analysis.Nodes["b.md"].Authority)

	// Find the community containing all three notes and confirm TopAuthority ordering reflects the boost.
	var comm *obsidian.CommunitySummary
	for i := range analysis.Communities {
		c := &analysis.Communities[i]
		if containsAll(c.Nodes, []string{"a.md", "b.md", "c.md"}) {
			comm = c
			break
		}
	}
	require.NotNil(t, comm)
	require.NotEmpty(t, comm.TopAuthority)
	require.Equal(t, "c.md", comm.TopAuthority[0].Path)
	require.Equal(t, "c.md", comm.Anchor)
}

func containsAll(haystack []string, needles []string) bool {
	set := make(map[string]struct{}, len(haystack))
	for _, h := range haystack {
		set[h] = struct{}{}
	}
	for _, n := range needles {
		if _, ok := set[n]; !ok {
			return false
		}
	}
	return true
}
