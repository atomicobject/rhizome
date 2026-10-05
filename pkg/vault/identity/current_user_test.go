package identity

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCurrentUserReadWriteUsesIgnoredAgentState(t *testing.T) {
	root := t.TempDir()
	cfg, err := WriteCurrentUser(root, "people/colthorp.md")
	require.NoError(t, err)
	require.Equal(t, "people/colthorp.md", cfg.Ref)

	path := CurrentUserPath(root)
	require.Equal(t, filepath.Join(root, ".rhizome", "agent", "user.yml"), path)
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(body), "ref: people/colthorp.md")

	loaded, ok, err := ReadCurrentUser(root)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, CurrentUser{Version: 1, Ref: "people/colthorp.md"}, loaded)
}

func TestCurrentUserReadMissingIsNotAnError(t *testing.T) {
	cfg, ok, err := ReadCurrentUser(t.TempDir())
	require.NoError(t, err)
	require.False(t, ok)
	require.Empty(t, cfg.Ref)
}

func TestResolutionForConfigRequiresPerson(t *testing.T) {
	cfg := CurrentUser{Ref: "people/colthorp.md"}
	require.Empty(t, ResolutionForConfig(cfg, true, "people/colthorp.md", "people/colthorp.md", "Colthorp", "Person").ErrorCode)
	require.Equal(t, "unresolved_current_user", ResolutionForConfig(cfg, false, "", "", "", "").ErrorCode)
	require.Equal(t, "current_user_not_person", ResolutionForConfig(cfg, true, "projects/foo.md", "projects/foo.md", "Foo", "Project").ErrorCode)
}
