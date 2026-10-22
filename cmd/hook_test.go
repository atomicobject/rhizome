package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/cli/hook"
	"github.com/atomicobject/rhizome/pkg/repositorytrust"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestHookSelectionNeverPromptsProbesOrInstalls(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script probe fixture is Unix-only")
	}
	repo := t.TempDir()
	require.NoError(t, repoconfig.SaveLocalConfig(repo, repoconfig.LocalConfig{
		Rhizome: repoconfig.LocalRhizomeConfig{Version: "v1.2.3"},
	}))
	exe := filepath.Join(t.TempDir(), "global-rzm")
	store := repositorytrust.Store{Dir: filepath.Join(t.TempDir(), "trust")}

	// A missing pinned binary is not installed.
	require.Empty(t, hookDelegateTarget(repo, exe, store))

	// A pinned binary without a version marker is not probed.
	sentinel := filepath.Join(t.TempDir(), "probed")
	target := filepath.Join(repo, ".rhizome", "bin", repoPlatformDir(), repoExecutableName())
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, []byte("#!/bin/sh\ntouch \""+sentinel+"\"\necho 'rzm version v1.2.3'\n"), 0o755))
	_, err := store.Trust(repo)
	require.NoError(t, err)
	require.Empty(t, hookDelegateTarget(repo, exe, store))
	require.NoFileExists(t, sentinel)

	// A matching marker in a trusted checkout hands off; an untrusted one does not.
	writeVersionMarkerFile(t, target, "v1.2.3")
	requireSameExecutablePath(t, target, hookDelegateTarget(repo, exe, store))
	_, _, err = store.Untrust(repo)
	require.NoError(t, err)
	require.Empty(t, hookDelegateTarget(repo, exe, store))
	require.NoFileExists(t, sentinel)
}

func TestHookDeadlineCountsFromTheFirstLauncher(t *testing.T) {
	now := time.Now()
	inherited := now.Add(time.Second)
	require.Equal(t, inherited.UnixNano(), hookDeadline(now, strconv.FormatInt(inherited.UnixNano(), 10)).UnixNano())
	for _, bad := range []string{"", "x", strconv.FormatInt(now.Add(-time.Second).UnixNano(), 10), strconv.FormatInt(now.Add(time.Hour).UnixNano(), 10)} {
		require.Equal(t, now.Add(hook.Deadline), hookDeadline(now, bad), bad)
	}
}
