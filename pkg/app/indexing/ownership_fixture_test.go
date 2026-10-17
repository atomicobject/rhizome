package indexing

import (
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/noteownership"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func ownershipTestLanguage(ref paths.CodePathRef) codeanchor.Lang {
	switch filepath.Ext(ref.Rel.String()) {
	case ".go":
		return codeanchor.LangGo
	case ".html":
		return "html"
	default:
		return ""
	}
}

func markdownCandidatePaths(candidates []noteownership.Candidate) []paths.NotePath {
	result := make([]paths.NotePath, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Provider == noteownership.MarkdownFormatID {
			result = append(result, paths.NotePath(candidate.Path))
		}
	}
	return result
}

func writeOwnershipFile(t *testing.T, root, rel string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte("fixture"), 0o600))
}
