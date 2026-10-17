package llm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMockProviderIsNotASelectableRuntimeProvider(t *testing.T) {
	provider, err := NewProvider(ResolvedProfile{Provider: "mock", Model: "offline"})
	require.Error(t, err)
	require.Nil(t, provider)
	_, err = ParseModelSpec("mock/offline")
	require.Error(t, err)
}
