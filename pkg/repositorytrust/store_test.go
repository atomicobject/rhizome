package repositorytrust

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStoreTrustIsCanonicalAndPerCheckout(t *testing.T) {
	store := Store{Dir: filepath.Join(t.TempDir(), "trust")}
	checkout := t.TempDir()
	otherWorktree := t.TempDir()
	alias := checkout
	symlinkAlias := filepath.Join(t.TempDir(), "checkout")
	if err := os.Symlink(checkout, symlinkAlias); err == nil {
		alias = symlinkAlias
	}

	trusted, err := store.Trusted(checkout)
	require.NoError(t, err)
	require.False(t, trusted)

	canonical, err := store.Trust(alias)
	require.NoError(t, err)
	expectedCanonical, err := CanonicalCheckout(checkout)
	require.NoError(t, err)
	require.Equal(t, expectedCanonical, canonical)

	trusted, err = store.Trusted(checkout)
	require.NoError(t, err)
	require.True(t, trusted)
	trusted, err = store.Trusted(otherWorktree)
	require.NoError(t, err)
	require.False(t, trusted)

	canonical, removed, err := store.Untrust(alias)
	require.NoError(t, err)
	require.True(t, removed)
	require.Equal(t, expectedCanonical, canonical)
	trusted, err = store.Trusted(checkout)
	require.NoError(t, err)
	require.False(t, trusted)
}

func TestStoreRejectsMismatchedRecordContents(t *testing.T) {
	store := Store{Dir: filepath.Join(t.TempDir(), "trust")}
	checkout := t.TempDir()
	_, err := store.Trust(checkout)
	require.NoError(t, err)

	records, err := os.ReadDir(store.Dir)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.NoError(t, os.WriteFile(filepath.Join(store.Dir, records[0].Name()), []byte("another checkout\n"), 0o600))

	trusted, err := store.Trusted(checkout)
	require.NoError(t, err)
	require.False(t, trusted)
}

func TestDefaultStoreIgnoresHomeChangesAfterProcessStart(t *testing.T) {
	if processUserHomeErr != nil || processUserHome == "" {
		t.Skip("process user home unavailable")
	}
	t.Setenv("HOME", filepath.Join(t.TempDir(), "repository-controlled-home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "repository-controlled-config"))

	store, err := DefaultStore()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(processUserHome, ".config", "rhizome", storeDirectoryName), store.Dir)
}

func TestLinkedWorktreesInheritMainCheckoutTrust(t *testing.T) {
	store := Store{Dir: filepath.Join(t.TempDir(), "trust")}
	base := t.TempDir()
	main := filepath.Join(base, "main")
	worktree := filepath.Join(base, "worktree")
	for _, args := range [][]string{
		{"init", "-q", main},
		{"-C", main, "-c", "user.name=Rhizome Tests", "-c", "user.email=tests@rhizome.invalid", "commit", "-q", "--allow-empty", "-m", "start"},
		{"-C", main, "worktree", "add", "-q", worktree},
	} {
		output, err := exec.Command("git", args...).CombinedOutput()
		require.NoError(t, err, "%s", output)
	}
	require.NoError(t, os.MkdirAll(filepath.Join(main, "vault"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(worktree, "vault"), 0o755))
	// A copied folder whose .git file claims the worktree's Git directory, but
	// which the repository does not link back to.
	forged := filepath.Join(base, "forged")
	require.NoError(t, os.MkdirAll(forged, 0o755))
	dotGit, err := os.ReadFile(filepath.Join(worktree, ".git"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(forged, ".git"), dotGit, 0o644))
	// A folder that brings its own Git metadata, back-linked to itself and
	// claiming the trusted repository as its common directory.
	planted := filepath.Join(base, "planted")
	metadata := filepath.Join(planted, "metadata")
	require.NoError(t, os.MkdirAll(metadata, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(planted, ".git"), []byte("gitdir: "+metadata+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(metadata, "gitdir"), []byte(filepath.Join(planted, ".git")+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(metadata, "commondir"), []byte(filepath.Join(main, ".git")+"\n"), 0o644))

	trusted := func(checkout string) bool {
		ok, err := store.Trusted(checkout)
		require.NoError(t, err)
		return ok
	}
	require.False(t, trusted(worktree))

	_, err = store.Trust(main)
	require.NoError(t, err)
	require.True(t, trusted(worktree))
	require.False(t, trusted(filepath.Join(worktree, "vault")), "a subfolder maps to the same subfolder")
	require.False(t, trusted(forged))
	require.False(t, trusted(planted))

	_, err = store.Trust(filepath.Join(main, "vault"))
	require.NoError(t, err)
	require.True(t, trusted(filepath.Join(worktree, "vault")))

	_, _, err = store.Untrust(main)
	require.NoError(t, err)
	require.False(t, trusted(worktree))
}
