package namespacegit_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConsecutiveInventoriesWithoutPreparation(t *testing.T) {
	f := newFixture(t)
	// Observe enumeration versus fresh directory metadata without calling the
	// helper. This exposes stale Windows enumeration as a fixture observation,
	// while repeated fresh snapshots still require every live invariant.
	mismatches := 0
	require.NoError(t, filepath.WalkDir(f.root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.IsDir() {
			return err
		}
		cached, err := entry.Info()
		if err != nil {
			return err
		}
		fresh, err := os.Lstat(name)
		if err != nil {
			return err
		}
		if !cached.ModTime().Equal(fresh.ModTime()) {
			mismatches++
			t.Logf("directory enumeration differs from fresh Lstat: %s (%s / %s)", name, cached.ModTime(), fresh.ModTime())
		}
		return nil
	}))
	t.Logf("cached/fresh directory timestamp differences: %d", mismatches)
	before := inventory(t, f.root)
	for i := 0; i < 3; i++ {
		assertLiveUnchanged(t, before, inventory(t, f.root))
	}
}
