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
	// An inherited deadline is kept, even one that has already passed.
	for _, inherited := range []time.Time{now.Add(time.Second), now.Add(-time.Second)} {
		require.Equal(t, inherited.UnixNano(), hookDeadline(now, strconv.FormatInt(inherited.UnixNano(), 10)).UnixNano())
	}
	for _, bad := range []string{"", "x", strconv.FormatInt(now.Add(time.Hour).UnixNano(), 10)} {
		require.Equal(t, now.Add(hook.Deadline), hookDeadline(now, bad), bad)
	}
}

// hookScratch runs the test in a scratch worktree with a hook payload on
// standard input, and returns the worktree and what rzm hook prints.
func hookScratch(t *testing.T) (string, func() string) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, ".git"), 0o755))
	t.Chdir(root)
	stdin := filepath.Join(t.TempDir(), "payload.json")
	require.NoError(t, os.WriteFile(stdin, []byte(`{"session_id":"s1","tool_name":"Read","tool_input":{"file_path":"a.go"}}`), 0o644))
	in, err := os.Open(stdin)
	require.NoError(t, err)
	out, err := os.Create(filepath.Join(t.TempDir(), "stdout"))
	require.NoError(t, err)
	savedIn, savedOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = in, out
	t.Cleanup(func() {
		os.Stdin, os.Stdout = savedIn, savedOut
		_ = in.Close()
		_ = out.Close()
	})
	return root, func() string {
		data, err := os.ReadFile(out.Name())
		require.NoError(t, err)
		return string(data)
	}
}

func hookLog(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".rhizome", "hooks", "diagnostics.log"))
	require.NoError(t, err)
	return string(data)
}

func TestAnExpiredInheritedDeadlineEndsAtOnce(t *testing.T) {
	root, stdout := hookScratch(t)
	t.Setenv(hookDeadlineEnv, strconv.FormatInt(time.Now().Add(-time.Second).UnixNano(), 10))
	saved := hookDelegate
	hookDelegate = func(string) string { t.Error("selection ran past the deadline"); return "" }
	t.Cleanup(func() { hookDelegate = saved })

	started := time.Now()
	require.Equal(t, 0, runHook([]string{"claude", "post-tool-use"}))
	require.Less(t, time.Since(started), 500*time.Millisecond)
	require.Empty(t, stdout())
	require.Contains(t, hookLog(t, root), "passed before rzm hook started")
}

func TestAFailingHandoffEmitsNothingAndExitsZero(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script binary fixture is Unix-only")
	}
	root, stdout := hookScratch(t)
	t.Setenv(hookDeadlineEnv, strconv.FormatInt(time.Now().Add(2*time.Second).UnixNano(), 10))
	pin := filepath.Join(t.TempDir(), "rzm")
	require.NoError(t, os.WriteFile(pin, []byte("#!/bin/sh\ncat >/dev/null\necho '{\"hookSpecificOutput\":{}}'\nexit 37\n"), 0o755))
	saved := hookDelegate
	hookDelegate = func(string) string { return pin }
	t.Cleanup(func() { hookDelegate = saved })

	require.Equal(t, 0, runHook([]string{"claude", "post-tool-use"}))
	require.Empty(t, stdout())
	require.Contains(t, hookLog(t, root), "exit status 37")
}
