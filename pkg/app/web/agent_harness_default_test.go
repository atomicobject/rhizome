//go:build !e2efake

package web

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultBuildHasNoAgentHarnessOverride(t *testing.T) {
	require.Nil(t, agentHarnessOverride)
}
