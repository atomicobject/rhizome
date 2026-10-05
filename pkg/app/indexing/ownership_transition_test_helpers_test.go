package indexing

import (
	"sort"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func ownershipTransitionVaultPaths(t *testing.T, root string) paths.VaultPaths {
	t.Helper()
	vaultPaths, err := paths.NewVaultPaths(root)
	require.NoError(t, err)
	return vaultPaths
}

func ownershipTransitionRuntime(t *testing.T) noteformat.Runtime {
	t.Helper()
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	runtime, err := noteformat.NewRuntime(registry, markdown.New())
	require.NoError(t, err)
	return runtime
}

func ownershipTransitionShapes(transitions []semdb.OwnershipTransition) []string {
	sort.Slice(transitions, func(i, j int) bool { return transitions[i].Path < transitions[j].Path })
	result := make([]string, 0, len(transitions))
	for _, transition := range transitions {
		result = append(result, transition.Path+"|"+string(transition.Target))
	}
	return result
}
