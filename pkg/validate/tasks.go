package validate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/taskdef"
)

// IssueCodeInvalidTaskFile marks a scheduled task file the task runner will
// skip: unreadable, unparseable, or declaring an id another file declares.
const IssueCodeInvalidTaskFile = "invalid_task_file"

// RunTasks parses the working tree's `.rhizome/tasks` files with the task
// runner's parser ([[scheduled-tasks|SPEC-0127]] US1).
func RunTasks(runCtx RunContext) CheckResult {
	result := CheckResult{Name: CheckTasks, OK: true}
	dir := filepath.Join(runCtx.VaultPath, filepath.FromSlash(taskdef.TasksDir))
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		result.NotApplicable = "no task files found"
		result.Summary = result.NotApplicable
		return result
	}
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	}
	var files []string
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() && (strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")) {
			files = append(files, path.Join(taskdef.TasksDir, name))
		}
	}
	_, problems := taskdef.Load(files, func(file string) ([]byte, error) {
		return os.ReadFile(filepath.Join(runCtx.VaultPath, filepath.FromSlash(file)))
	})
	result.Summary = fmt.Sprintf("%d task files checked", len(files))
	result.IssueCount = len(problems)
	for _, problem := range problems {
		result.OK = false
		result.Issues = append(result.Issues, Issue{Code: IssueCodeInvalidTaskFile, Path: problem.Path, Message: problem.Message})
	}
	return result
}
