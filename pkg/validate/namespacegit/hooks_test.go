package namespacegit_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/atomicobject/rhizome/pkg/validate/namespacegit"
	"github.com/stretchr/testify/require"
)

func TestScratchCommandsCannotExecuteIndexHooks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses an executable POSIX hook")
	}
	f := newFixture(t)
	marker := filepath.Join(canonicalTemp(t), "hook-ran")
	writeFile(t, f.root, ".git/hooks/post-index-change", "#!/bin/sh\nprintf called > '"+marker+"'\n", 0o755)
	f.git(t, "-c", "core.hooksPath="+filepath.Join(f.root, ".git", "hooks"), "update-index", "--assume-unchanged", "Other.md")
	_, err := os.Stat(marker)
	require.NoError(t, err, "positive control must execute the configured hook")
	f.git(t, "update-index", "--no-assume-unchanged", "Other.md")
	require.NoError(t, os.Rename(marker, marker+".control"))
	moves := []namespacegit.Move{{Source: "notes/Old.md", Destination: "notes/New.md"}}
	before := inventory(t, f.root)
	prepared, err := namespacegit.Prepare(context.Background(), f.root, canonicalTemp(t), moves, originalFiles(t, f.root, moves))
	require.NoError(t, err)
	require.NotNil(t, prepared)
	_, err = os.Stat(marker)
	require.True(t, os.IsNotExist(err), "scratch index writes must not run live hooks")
	assertLiveUnchanged(t, before, inventory(t, f.root))
}
