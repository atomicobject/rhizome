package validate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunTasksReportsEachInvalidTaskFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "tasks")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	files := map[string]string{
		"good.yml":          "name: Good\nevery: 7d\nprompt: tidy\n",
		"Bad_Id.yml":        "name: Bad\nevery: 7d\nprompt: x\n",
		"unknown-field.yml": "name: X\nevery: 7d\nprompt: x\ncolor: red\n",
		"bad-every.yml":     "name: X\nevery: soon\nprompt: x\n",
		"bad-timeout.yml":   "name: X\nevery: 1d\ntimeout: -5m\nprompt: x\n",
		"bad-scope.yml":     "name: X\nevery: 1d\nscope: world\nprompt: x\n",
		"both-check.yml":    "name: X\nevery: 1d\ncheck:\n  command: [true]\n  script: x\n",
		"no-work.yml":       "name: X\nevery: 1d\n",
		"good.yaml":         "name: Dup\nevery: 1d\nprompt: x\n",
		"notes.txt":         "not a task",
	}
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	}

	result := RunTasks(RunContext{VaultPath: root})

	require.False(t, result.OK)
	require.Equal(t, "9 task files checked", result.Summary)
	want := map[string]string{
		".rhizome/tasks/Bad_Id.yml":        "lowercase letters",
		".rhizome/tasks/bad-every.yml":     "every: invalid duration",
		".rhizome/tasks/bad-scope.yml":     "scope must be",
		".rhizome/tasks/bad-timeout.yml":   "timeout: duration",
		".rhizome/tasks/both-check.yml":    "exactly one of command or script",
		".rhizome/tasks/good.yml":          `task id "good" is also declared by .rhizome/tasks/good.yaml`,
		".rhizome/tasks/no-work.yml":       "a check, a prompt, or both",
		".rhizome/tasks/unknown-field.yml": "field color not found",
	}
	require.Equal(t, len(want), result.IssueCount)
	got := map[string]string{}
	for _, issue := range result.Issues {
		require.Equal(t, IssueCodeInvalidTaskFile, issue.Code)
		got[issue.Path] = issue.Message
	}
	for path, fragment := range want {
		require.Contains(t, got[path], fragment, path)
	}

	finalized := finalizeCheckResult(CheckTasks, result, 0, 3)
	require.Len(t, finalized.Issues, 3)
	require.Equal(t, len(want), finalized.IssueCount)
}

func TestRunTasksWithoutTaskFolderIsNotApplicable(t *testing.T) {
	t.Parallel()
	result := RunTasks(RunContext{VaultPath: t.TempDir()})
	require.True(t, result.OK)
	require.Equal(t, "no task files found", result.NotApplicable)
}
