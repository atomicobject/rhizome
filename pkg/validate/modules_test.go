package validate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunModulesReportsInvalidAndReservedNames(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, name := range []string{"good", "Bad_Name", "validate", "help", ".hidden"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "modules", name), 0o755))
	}

	result := RunModules(RunContext{VaultPath: root})

	require.False(t, result.OK)
	require.Equal(t, "4 modules checked", result.Summary)
	require.Equal(t, 3, result.IssueCount)
	require.Equal(t, []string{".rhizome/modules/Bad_Name", ".rhizome/modules/help", ".rhizome/modules/validate"},
		[]string{result.Issues[0].Path, result.Issues[1].Path, result.Issues[2].Path})
	require.Equal(t, IssueCodeInvalidModuleName, result.Issues[0].Code)
	require.Equal(t, IssueCodeReservedModuleName, result.Issues[1].Code)
	require.Contains(t, result.Issues[2].Message, "rzm module validate")

	finalized := finalizeCheckResult(CheckModules, result, 0, 1)
	require.Len(t, finalized.Issues, 1)
	require.Equal(t, 3, finalized.IssueCount)
}

func TestRunModulesValidNamesPassAndMissingFolderIsNotApplicable(t *testing.T) {
	t.Parallel()
	require.Equal(t, "no modules found", RunModules(RunContext{VaultPath: t.TempDir()}).NotApplicable)

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "modules", "compounding"), 0o755))
	result := RunModules(RunContext{VaultPath: root})
	require.True(t, result.OK)
	require.Zero(t, result.IssueCount)
}
