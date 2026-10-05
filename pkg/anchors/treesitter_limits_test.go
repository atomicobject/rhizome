package codeanchor

import (
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestResolveTreeSitterLimits_DefaultsTimeout(t *testing.T) {
	t.Setenv(treeSitterEnvTimeout, "")

	to := resolveTreeSitterLimits(nil, TreeSitterLimits{})

	expected := treeSitterBaseTimeout
	if runtime.GOOS == "windows" {
		expected += 5 * time.Second
	}
	require.Equal(t, expected, to)
}

func TestResolveTreeSitterLimits_EnvTimeoutOverrides(t *testing.T) {
	t.Setenv(treeSitterEnvTimeout, "2s")

	to := resolveTreeSitterLimits([]byte("x"), TreeSitterLimits{ParseTimeout: 10 * time.Second})
	require.Equal(t, 2*time.Second, to)
}

func TestResolveTreeSitterLimits_ConfigTimeoutUsed(t *testing.T) {
	t.Setenv(treeSitterEnvTimeout, "")

	to := resolveTreeSitterLimits([]byte("x"), TreeSitterLimits{ParseTimeout: 15 * time.Second})
	require.Equal(t, 15*time.Second, to)
}
