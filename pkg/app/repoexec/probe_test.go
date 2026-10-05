package repoexec

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestVersionProbeUsesSelectedCwdAndDelegationGuards(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell executable fixture requires Unix")
	}
	cwd := t.TempDir()
	target := filepath.Join(t.TempDir(), "rzm")
	require.NoError(t, os.WriteFile(target, []byte("#!/bin/sh\n[ -f cwd-marker ] && [ \"$RZM_REPO_DELEGATED\" = 1 ] && [ \"$RZM_SKIP_REPO_DELEGATE\" = 1 ] || exit 1\necho 'rzm version v0.50.5'\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "cwd-marker"), nil, 0o600))
	t.Setenv("RZM_REPO_DELEGATED", "old")
	t.Setenv("RZM_SKIP_REPO_DELEGATE", "old")
	version, err := Version(context.Background(), target, cwd)
	require.NoError(t, err)
	require.Equal(t, "v0.50.5", version)
}

func TestProbeHonorsCancellationAndOutputLimit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell executable fixture requires Unix")
	}
	target := filepath.Join(t.TempDir(), "rzm")
	require.NoError(t, os.WriteFile(target, []byte("#!/bin/sh\nwhile :; do printf 'unbounded synthetic output for a failing executable\\n'; done\n"), 0o755))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Probe(ctx, target, t.TempDir(), "--version")
	require.Error(t, err)
	out := &boundedOutput{limit: 4}
	n, err := out.Write([]byte("12345"))
	require.Error(t, err)
	require.Zero(t, n)
	require.Zero(t, out.Len())
}

func TestProbeBoundsInheritedOutputPipes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell executable fixture requires Unix")
	}
	target := filepath.Join(t.TempDir(), "rzm")
	require.NoError(t, os.WriteFile(target, []byte("#!/bin/sh\nsleep 3 &\nexit 0\n"), 0o755))
	started := time.Now()
	_, err := Probe(context.Background(), target, t.TempDir(), "--version")
	require.Error(t, err)
	require.Less(t, time.Since(started), 2500*time.Millisecond)
}
