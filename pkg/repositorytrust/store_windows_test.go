package repositorytrust

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalCheckoutSharesVerbatimWindowsTrustAndLongPathIO(t *testing.T) {
	for _, long := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary_path", true: "long_path"}[long], func(t *testing.T) {
			checkout := filepath.Join(t.TempDir(), "checkout")
			if long {
				for len(checkout) <= 320 {
					checkout = filepath.Join(checkout, strings.Repeat("segment", 8))
				}
			}
			require.NoError(t, os.MkdirAll(checkout, 0o755))
			want, err := filepath.EvalSymlinks(checkout)
			require.NoError(t, err)
			verbatim := `\\?\` + want
			canonical, err := CanonicalCheckout(verbatim)
			require.NoError(t, err)
			require.Equal(t, want, canonical)
			ordinary, err := CanonicalCheckout(want)
			require.NoError(t, err)
			require.Equal(t, ordinary, canonical)

			marker := filepath.Join(canonical, "identity.txt")
			require.NoError(t, os.WriteFile(marker, []byte("same physical checkout"), 0o600))
			data, err := os.ReadFile(filepath.Join(verbatim, "identity.txt"))
			require.NoError(t, err)
			require.Equal(t, "same physical checkout", string(data))

			store := Store{Dir: filepath.Join(t.TempDir(), "trust")}
			_, err = store.Trust(ordinary)
			require.NoError(t, err)
			trusted, err := store.Trusted(verbatim)
			require.NoError(t, err)
			require.True(t, trusted)
			recorded, err := store.Trust(verbatim)
			require.NoError(t, err)
			require.Equal(t, ordinary, recorded)
			records, err := os.ReadDir(store.Dir)
			require.NoError(t, err)
			require.Len(t, records, 1)
			_, removed, err := store.Untrust(verbatim)
			require.NoError(t, err)
			require.True(t, removed)
			trusted, err = store.Trusted(ordinary)
			require.NoError(t, err)
			require.False(t, trusted)
		})
	}
}

func TestCanonicalCheckoutDoesNotMergeDistinctVerbatimWindowsNames(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	ordinary := filepath.Join(base, "checkout")
	verbatim := `\\?\` + filepath.Join(base, "checkout.")
	require.NoError(t, os.Mkdir(ordinary, 0o755))
	require.NoError(t, os.Mkdir(verbatim, 0o755))
	canonical, err := CanonicalCheckout(verbatim)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(canonical, `\\?\`))
	store := Store{Dir: filepath.Join(t.TempDir(), "trust")}
	_, err = store.Trust(ordinary)
	require.NoError(t, err)
	trusted, err := store.Trusted(verbatim)
	require.NoError(t, err)
	require.False(t, trusted, "Win32's ordinary spelling names a different directory")
}
