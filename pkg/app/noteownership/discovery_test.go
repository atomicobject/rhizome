package noteownership

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestNewSnapshot_SealsDeterministicCanonicalUniqueCandidates(t *testing.T) {
	t.Parallel()

	input := []Candidate{{Path: "z.md"}, {Path: "a.md", Owner: notediscovery.Note, Present: true}, {Path: "gone.md", Owner: notediscovery.Note}, {Path: "main.go", Owner: notediscovery.Code, Present: true}}
	snapshot, err := newSnapshot(input)
	require.NoError(t, err)
	input[1].Path = "mutated-input.md"
	candidates := snapshot.Candidates()
	require.Equal(t, []string{"a.md", "gone.md", "main.go", "z.md"}, candidatePathStrings(snapshot))
	candidates[0].Path = "changed.md"
	require.Equal(t, "a.md", snapshot.Candidates()[0].Path.String())
	require.Equal(t, []paths.NotePath{"a.md"}, snapshot.NoteKeepPaths(), "only present note owners are kept")

	_, err = newSnapshot([]Candidate{{Path: "a.md"}, {Path: "a.md"}})
	require.Error(t, err)
	_, err = newSnapshot([]Candidate{{Path: "./a.md"}})
	require.Error(t, err)
}

// scopedDiscoveryInput observes every classified path through the production
// CodeLanguage dependency, with the whole fixture root as a code root, so a walk
// beyond the requested scope is visible even if later output were filtered.
func scopedDiscoveryInput(t *testing.T, root string, observed *[]string) DiscoveryInput {
	t.Helper()
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	return DiscoveryInput{
		VaultDefinition: obsidian.VaultDefinition{Path: root},
		Registry:        runtime.Registry(),
		CodeRoots:       []paths.AbsPath{paths.AbsPath(root)},
		CodeLanguage: func(ref paths.CodePathRef) codeanchor.Lang {
			*observed = append(*observed, ref.Rel.String())
			return ""
		},
	}
}

func TestDiscoverScoped_ObservesOnlyRequestedPath(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "changed.md"), []byte("# changed"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "untouched.md"), []byte("# untouched"), 0o600))

	var observed []string
	snapshot, err := DiscoverScoped(context.Background(), scopedDiscoveryInput(t, root, &observed), []paths.RelPath{"notes/changed.md"})
	require.NoError(t, err)
	require.Equal(t, []string{"notes/changed.md"}, observed)
	candidates := snapshot.Candidates()
	require.Len(t, candidates, 1)
	require.Equal(t, "notes/changed.md", candidates[0].Path.String())
	require.Equal(t, notediscovery.Unowned, candidates[0].PreviousOwner)
	require.Equal(t, notediscovery.Note, candidates[0].Owner)
	require.Equal(t, MarkdownFormatID, candidates[0].Provider)
	require.True(t, candidates[0].Present)
}

func TestDiscoverScoped_WalksOnlyRequestedDirectorySubtree(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "changed", "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "changed", "first.md"), []byte("# first"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "changed", "nested", "second.md"), []byte("# second"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "untouched.md"), []byte("# untouched"), 0o600))

	var observed []string
	snapshot, err := DiscoverScoped(context.Background(), scopedDiscoveryInput(t, root, &observed), []paths.RelPath{"changed"})
	require.NoError(t, err)
	require.Equal(t, []string{"changed/first.md", "changed/nested/second.md"}, observed)
	require.Equal(t, []string{"changed/first.md", "changed/nested/second.md"}, candidatePathStrings(snapshot))
}

func TestDiscoverScoped_TracksPersistedDescendantsOfRemovedDirectory(t *testing.T) {
	root := t.TempDir()
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)

	snapshot, err := DiscoverScoped(context.Background(), DiscoveryInput{
		VaultDefinition:    obsidian.VaultDefinition{Path: root},
		Registry:           runtime.Registry(),
		PersistedNotePaths: []paths.NotePath{"removed/child.md"},
		PersistedCodePaths: []paths.CodePath{"removed/main.go"},
	}, []paths.RelPath{"removed"})
	require.NoError(t, err)
	require.Equal(t, []Candidate{
		{Path: "removed/child.md", PreviousOwner: notediscovery.Note, Owner: notediscovery.Unowned},
		{Path: "removed/main.go", PreviousOwner: notediscovery.Code, Owner: notediscovery.Unowned},
	}, snapshot.Candidates())
}

func candidatePathStrings(snapshot Snapshot) []string {
	candidates := snapshot.Candidates()
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, candidate.Path.String())
	}
	return result
}
