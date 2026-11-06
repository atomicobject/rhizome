package appbundle

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOwnedImageEntryRetainsStructuralOwnership(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux CLI entry is a symlink")
	}
	for _, scenario := range []string{"current", "relative", "dangling", "disabled", "deleted-app", "missing-registration"} {
		t.Run(scenario, func(t *testing.T) {
			root, record, image := portableFixture(t)
			entry := filepath.Join(t.TempDir(), "rzm")
			target := image
			switch scenario {
			case "relative":
				var err error
				target, err = filepath.Rel(filepath.Dir(entry), image)
				require.NoError(t, err)
			case "dangling":
				require.NoError(t, os.Remove(image))
			case "disabled":
				record.Enabled = false
				writePortableFixture(t, root, record)
			case "deleted-app":
				require.NoError(t, os.Remove(record.AppPath))
			case "missing-registration":
				require.NoError(t, os.Remove(filepath.Join(root, "installation.json")))
			}
			require.NoError(t, os.Symlink(target, entry))
			owned, err := ownedImageEntry(entry)
			require.NoError(t, err)
			require.True(t, owned)
		})
	}
}

func TestOwnedImageEntryPreservesForeignCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux CLI entry is a symlink")
	}
	root, _, _ := portableFixture(t)
	bundle, err := Read(root)
	require.NoError(t, err)
	for _, scenario := range []string{"absent", "standalone", "foreign-symlink"} {
		t.Run(scenario, func(t *testing.T) {
			entry := filepath.Join(t.TempDir(), "rzm")
			switch scenario {
			case "standalone":
				require.NoError(t, os.WriteFile(entry, []byte("standalone"), 0o700))
			case "foreign-symlink":
				foreign := filepath.Join(t.TempDir(), "rzm")
				require.NoError(t, os.WriteFile(foreign, []byte("standalone"), 0o700))
				require.NoError(t, os.Symlink(foreign, entry))
			}
			owned, err := ownedImageEntry(entry)
			require.NoError(t, err)
			require.False(t, owned, "a registered app must not claim an unrelated CLI")
			state, _, err := EntryState(entry, bundle)
			require.NoError(t, err)
			if scenario == "absent" {
				require.Equal(t, "absent", state)
			} else {
				require.Equal(t, "foreign", state)
			}
		})
	}
}
