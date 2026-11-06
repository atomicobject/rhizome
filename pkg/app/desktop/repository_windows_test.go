package desktop

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRepositoryIdentityMatchesRustVerbatimWindowsPaths(t *testing.T) {
	s := testService(t)
	main, feature := fixtureRepository(t)
	ordinary, err := s.Repository(main, "")
	require.NoError(t, err)
	for _, path := range []string{main, feature, filepath.Join(main, ".git")} {
		fromRust, err := s.Repository(`\\?\`+path, "")
		require.NoError(t, err)
		require.Equal(t, ordinary.ID, fromRust.ID)
		require.Equal(t, ordinary.Root, fromRust.Root)
		require.Equal(t, paths(ordinary), paths(fromRust))
	}
}
