package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilesRequestPlanFactsUseContinuationCursorValues(t *testing.T) {
	token, err := encodeFilesCursor(filesCursor{
		Inputs:   []string{"pkg/example.go"},
		MaxDepth: 2,
	})
	require.NoError(t, err)

	facts, err := NormalizeFilesRequestPlan(map[string]any{
		"continuationToken": token,
	})
	require.NoError(t, err)
	require.Equal(t, 2, facts.MaxDepth)
	require.True(t, facts.HasPathInputs)
}

func TestFilesRequestPlanFactsKeepMarkdownPathsCodeEligibleUntilCommandClassification(t *testing.T) {
	facts, err := NormalizeFilesRequestPlan(map[string]any{
		"inputs": []any{"Alpha.md", "docs/*.md"},
	})
	require.NoError(t, err)
	require.True(t, facts.HasPathInputs)
}

func TestFilesRequestPlanFactsKeepAndOnlyTagPathQueryRuntimeFree(t *testing.T) {
	facts, err := NormalizeFilesRequestPlan(map[string]any{
		"inputs": []any{"pkg/example.go", "AND", "tag:documented"},
	})
	require.NoError(t, err)
	require.False(t, facts.HasPathInputs)
}
