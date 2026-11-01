package validate

import (
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/atomicobject/rhizome/pkg/vault/assetroots"
	"github.com/atomicobject/rhizome/pkg/vault/taskdef"
)

// IssueCodeInvalidTaskFile marks a scheduled task file the task runner will
// skip: unreadable, unparseable, or declaring an id another file declares.
const IssueCodeInvalidTaskFile = "invalid_task_file"

// RunTasks parses the working tree's task files, in `.rhizome/tasks` and in
// every module's `tasks` folder, with the task runner's parser
// ([[scheduled-tasks|SPEC-0127]] US1, [[rhizome-modules|SPEC-0130]]).
func RunTasks(runCtx RunContext) CheckResult {
	result := CheckResult{Name: CheckTasks, OK: true}
	fail := func(err error) CheckResult {
		result.OK = false
		result.Error = err.Error()
		return result
	}
	roots, err := assetroots.Roots(runCtx.VaultPath, assetroots.Tasks)
	if err != nil {
		return fail(err)
	}
	if len(roots) == 0 {
		result.NotApplicable = "no task files found"
		result.Summary = result.NotApplicable
		return result
	}
	var files []string
	for _, root := range roots {
		entries, err := os.ReadDir(root.Abs(runCtx.VaultPath))
		if err != nil {
			return fail(err)
		}
		for _, entry := range entries {
			if rel := path.Join(root.Rel, entry.Name()); !entry.IsDir() && taskdef.IsTaskFile(rel) {
				files = append(files, rel)
			}
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
