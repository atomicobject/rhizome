package repositorytrust

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUntrustedResolvesRepositoryIdentity(t *testing.T) {
	base := t.TempDir()
	main := filepath.Join(base, "main")
	linked := filepath.Join(base, "linked")
	bare := filepath.Join(base, "bare")
	plain := filepath.Join(base, "plain")
	for _, args := range [][]string{
		{"init", "-q", main},
		{"-C", main, "-c", "user.name=Rhizome Tests", "-c", "user.email=tests@rhizome.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "start"},
		{"-C", main, "worktree", "add", "-q", linked},
		{"init", "-q", "--bare", bare},
	} {
		output, err := exec.Command("git", args...).CombinedOutput()
		require.NoError(t, err, "%s", output)
	}
	for _, path := range []string{filepath.Join(main, "nested"), filepath.Join(linked, "nested"), plain} {
		require.NoError(t, os.MkdirAll(path, 0755))
	}
	main, err := CanonicalCheckout(main)
	require.NoError(t, err)
	bare, err = CanonicalCheckout(bare)
	require.NoError(t, err)
	for _, test := range []struct{ name, dir, checkout string }{
		{"main", main, main},
		{"subdirectory", filepath.Join(main, "nested"), main},
		{"linked", linked, main},
		{"linked subdirectory", filepath.Join(linked, "nested"), main},
		{"bare", bare, bare},
		{"outside git", plain, plain},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, trusted := range []bool{false, true} {
				reason := Untrusted(context.Background(), test.dir, "actions", func(checkout string) (bool, error) {
					require.Equal(t, test.checkout, checkout)
					return trusted, nil
				})
				if trusted {
					require.Empty(t, reason)
				} else {
					require.Equal(t, "this repository is not trusted; run `rzm trust` in "+test.checkout+" to let its actions run", reason)
				}
			}
		})
	}
}

func TestUntrustedPreservesLookupFailureAndCancellation(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Empty(t, Untrusted(ctx, dir, "tasks", func(checkout string) (bool, error) {
		require.Equal(t, dir, checkout)
		return true, nil
	}), "a cancelled Git lookup retains the existing direct-checkout fallback")
	require.Equal(t, "repository trust could not be read: unavailable", DenialReason(dir, "actions", func(string) (bool, error) {
		return false, errors.New("unavailable")
	}))
	store := Store{Dir: t.TempDir()}
	canonical, err := CanonicalCheckout(dir)
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(store.recordPath(canonical), 0700))
	require.Contains(t, Untrusted(context.Background(), dir, "actions", store.Trusted), "repository trust could not be read:")
}
