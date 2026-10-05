package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSemanticQueryOptionsRejectsUnsafePathPrefixes(t *testing.T) {
	for _, pathPrefix := range []string{
		"/absolute/path",
		"../outside",
		"docs/../../outside",
		"C:/workspace/docs",
		`C:\workspace\docs`,
	} {
		t.Run(pathPrefix, func(t *testing.T) {
			_, err := SemanticQueryUnifiedWithOptions(context.Background(), Config{}, SemanticQueryOptions{
				Query:      "needle",
				PathPrefix: pathPrefix,
			})
			require.Error(t, err)
			require.Contains(t, err.Error(), "pathPrefix")
		})
	}
}
