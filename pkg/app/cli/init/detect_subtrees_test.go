package init

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/stretchr/testify/require"
)

func writeFixtureFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
}

func candidateRels(cands []IgnoredRepoCandidate) []string {
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.Rel)
	}
	return out
}

func TestDetectIgnoredNestedRepos_FindsGitignoredSubmodule(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, root, ".gitignore", "app/\nscratch/\n")
	// app is a nested repo with a language marker.
	require.NoError(t, os.MkdirAll(filepath.Join(root, "app", ".git"), 0o755))
	writeFixtureFile(t, root, "app/go.mod", "module example.com/app\n")
	writeFixtureFile(t, root, "app/main.go", "package main\n")
	// scratch is gitignored but has no repo/language markers.
	writeFixtureFile(t, root, "scratch/notes.txt", "junk\n")
	// regular code at root is not a candidate.
	writeFixtureFile(t, root, "go.mod", "module example.com/root\n")

	cands := detectIgnoredNestedRepos(root, ignore.LoadUnifiedMatcher(root, nil))
	require.Equal(t, []string{"app"}, candidateRels(cands))
	require.True(t, cands[0].HasGit)
	require.Contains(t, cands[0].Markers, "go.mod")
	require.NotNil(t, cands[0].Rule)
	require.Equal(t, ignore.LayerGitignore, cands[0].Rule.Layer)
	require.Equal(t, "app/", cands[0].Rule.Pattern)
}

func TestDetectIgnoredNestedRepos_ProbesOneChildLevel(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, root, ".gitignore", "modules/\n")
	writeFixtureFile(t, root, "modules/app/package.json", "{}\n")
	writeFixtureFile(t, root, "modules/other/readme.txt", "no markers\n")
	// deeper-than-one-level markers are not probed (bounded walk)
	writeFixtureFile(t, root, "modules/deep/nested/repo/go.mod", "module deep\n")

	cands := detectIgnoredNestedRepos(root, ignore.LoadUnifiedMatcher(root, nil))
	require.Equal(t, []string{"modules/app"}, candidateRels(cands))
	require.Contains(t, cands[0].Markers, "package.json")
}

func TestDetectIgnoredNestedRepos_SkipsNonGitignoreLayers(t *testing.T) {
	root := t.TempDir()
	// node_modules is excluded by defaults (no .rhizome/ignore present), and
	// explicit rhizome rules are deliberate user choices: neither is a candidate.
	writeFixtureFile(t, root, "node_modules/dep/package.json", "{}\n")
	cands := detectIgnoredNestedRepos(root, ignore.LoadUnifiedMatcher(root, nil))
	require.Empty(t, candidateRels(cands))

	writeFixtureFile(t, root, ".gitignore", "app/\n")
	writeFixtureFile(t, root, ".rhizome/ignore", "app/\n")
	writeFixtureFile(t, root, "app/go.mod", "module app\n")
	cands = detectIgnoredNestedRepos(root, ignore.LoadUnifiedMatcher(root, nil))
	require.Empty(t, candidateRels(cands), "rhizome-layer exclusions are deliberate, not candidates")
}

func TestDetectIgnoredNestedRepos_AlreadyIncludedSubtreeNotCandidate(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, root, ".gitignore", "app/\n")
	writeFixtureFile(t, root, ".rhizome/ignore", "# rhizome: included subtrees\n!/app/\n")
	writeFixtureFile(t, root, "app/go.mod", "module app\n")

	cands := detectIgnoredNestedRepos(root, ignore.LoadUnifiedMatcher(root, nil))
	require.Empty(t, candidateRels(cands), "an already re-included subtree is not ignored, so not a candidate")
}

func TestDetectLayout_PopulatesIgnoredRepoCandidates(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	writeFixtureFile(t, root, ".gitignore", "app/\n")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "app", ".git"), 0o755))
	writeFixtureFile(t, root, "app/go.mod", "module app\n")
	writeFixtureFile(t, root, "README.md", "# readme\n")

	layout, err := DetectLayout(root)
	require.NoError(t, err)
	require.Equal(t, []string{"app"}, candidateRels(layout.IgnoredRepoCandidates))
}
