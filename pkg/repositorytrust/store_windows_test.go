package repositorytrust

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
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
	for _, tc := range []struct {
		name, suffix string
		ancestor     bool
	}{
		{"dot_leaf", ".", false},
		{"space_leaf", " ", false},
		{"dot_ancestor", ".", true},
		{"space_ancestor", " ", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			ordinary := filepath.Join(base, "checkout")
			verbatim := `\\?\` + filepath.Join(base, "checkout"+tc.suffix)
			require.NoError(t, os.Mkdir(ordinary, 0o755))
			require.NoError(t, os.Mkdir(verbatim, 0o755))
			if tc.ancestor {
				ordinary = filepath.Join(ordinary, "repo")
				verbatim = filepath.Join(verbatim, "repo")
				require.NoError(t, os.Mkdir(ordinary, 0o755))
				require.NoError(t, os.Mkdir(verbatim, 0o755))
			}

			// Use raw Win32 calls, independently of Go's automatic path prefixes.
			ordinaryID := win32DirectoryIdentity(t, ordinary)
			verbatimID := win32DirectoryIdentity(t, verbatim)
			require.NotEqual(t, ordinaryID, verbatimID, "the fixture needs physically distinct directories")
			canonical, err := CanonicalCheckout(verbatim)
			require.NoError(t, err)
			require.Equal(t, verbatim, canonical, "ambiguous components require verbatim Win32 spelling")
			require.Equal(t, verbatimID, win32DirectoryIdentity(t, canonical))

			store := Store{Dir: filepath.Join(t.TempDir(), "trust")}
			_, err = store.Trust(ordinary)
			require.NoError(t, err)
			trusted, err := store.Trusted(verbatim)
			require.NoError(t, err)
			require.False(t, trusted, "trust must not cross distinct Windows directory identities")
			recorded, err := store.Trust(verbatim)
			require.NoError(t, err)
			require.Equal(t, canonical, recorded)
			trusted, err = store.Trusted(verbatim)
			require.NoError(t, err)
			require.True(t, trusted, "a trust record must preserve the entire canonical name")
			records, err := os.ReadDir(store.Dir)
			require.NoError(t, err)
			require.Len(t, records, 2)
			_, removed, err := store.Untrust(ordinary)
			require.NoError(t, err)
			require.True(t, removed)
			trusted, err = store.Trusted(verbatim)
			require.NoError(t, err)
			require.True(t, trusted)
		})
	}
}

func TestCanonicalCheckoutResolvesWindowsDirectorySymlinks(t *testing.T) {
	for _, name := range []string{"checkout", "checkout."} {
		t.Run(name, func(t *testing.T) {
			base, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			target := filepath.Join(base, name)
			if strings.HasSuffix(name, ".") {
				target = `\\?\` + target
			}
			require.NoError(t, os.Mkdir(target, 0o755))
			alias := filepath.Join(base, "alias")
			if err := os.Symlink(target, alias); errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD) {
				t.Skip("directory symlinks require Windows developer mode or symlink privilege")
			} else {
				require.NoError(t, err)
			}
			for _, spelling := range []string{alias, `\\?\` + alias} {
				canonical, err := CanonicalCheckout(spelling)
				require.NoError(t, err)
				require.Equal(t, target, canonical)
				require.Equal(t, win32DirectoryIdentity(t, target), win32DirectoryIdentity(t, canonical))
			}
			store := Store{Dir: filepath.Join(t.TempDir(), "trust")}
			_, err = store.Trust(alias)
			require.NoError(t, err)
			trusted, err := store.Trusted(target)
			require.NoError(t, err)
			require.True(t, trusted)
		})
	}
}

func win32DirectoryIdentity(t *testing.T, path string) [3]uint32 {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	require.NoError(t, err)
	handle, err := windows.CreateFile(name, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	require.NoError(t, err)
	defer windows.CloseHandle(handle)
	var info windows.ByHandleFileInformation
	require.NoError(t, windows.GetFileInformationByHandle(handle, &info))
	return [3]uint32{info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow}
}
