package session

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompiledDefaultAppliesToDesktopDevelopmentBuilds(t *testing.T) {
	previous := DefaultOrigin
	DefaultOrigin = "https://team.example.test/"
	t.Cleanup(func() { DefaultOrigin = previous })

	origin, err := ResolveOrigin(ResolveOptions{Development: true})
	require.NoError(t, err)
	require.Equal(t, "https://team.example.test", origin)

	selector := filepath.Join(t.TempDir(), "team-session.json")
	require.NoError(t, writeSelector(selector, DevelopmentOrigin))
	origin, err = ResolveOrigin(ResolveOptions{Development: true, SelectorPath: selector})
	require.NoError(t, err)
	require.Equal(t, DevelopmentOrigin, origin, "an explicit prior sign-in retains its origin")

	origin, err = ResolveOrigin(ResolveOptions{Development: true, Server: DevelopmentOrigin})
	require.NoError(t, err)
	require.Equal(t, DevelopmentOrigin, origin, "local testing remains available explicitly")
}
