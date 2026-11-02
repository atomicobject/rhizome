package validate

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/vault/actiondef"
	"github.com/atomicobject/rhizome/pkg/vault/assetroots"
)

// IssueCodeInvalidActionFile marks a module action the runner will not run as
// declared: unreadable or invalid, naming a module file that does not exist,
// or named like a query recipe in its module.
const IssueCodeInvalidActionFile = "invalid_action_file"

// RunActions checks every module's action declarations with the actions
// runner's parser ([[module-actions|SPEC-0131]]), then the files they name and
// their names against the module's recipes.
func RunActions(runCtx RunContext) CheckResult {
	result := CheckResult{Name: CheckActions, OK: true}
	list, problems, err := actiondef.Load(runCtx.VaultPath)
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	}
	if len(list) == 0 && len(problems) == 0 {
		result.NotApplicable = "no module actions found"
		result.Summary = result.NotApplicable
		return result
	}
	var issues []Issue
	for _, p := range problems {
		issues = append(issues, Issue{Code: IssueCodeInvalidActionFile, Path: p.Path, Message: p.Message})
	}
	recipes := map[string][]string{}
	for _, a := range list {
		for _, missing := range actiondef.MissingPaths(runCtx.VaultPath, a) {
			issues = append(issues, Issue{Code: IssueCodeInvalidActionFile, Path: a.Path, Message: fmt.Sprintf("%s does not exist", missing)})
		}
		names, loaded := recipes[a.Module]
		if !loaded {
			names = moduleRecipeIDs(runCtx.VaultPath, a.Module)
			recipes[a.Module] = names
		}
		if slices.Contains(names, a.Name) {
			issues = append(issues, Issue{Code: IssueCodeInvalidActionFile, Path: a.Path, Message: fmt.Sprintf("action %q has the name of a query recipe in module %s; rename one so `rzm %s %s` is unambiguous", a.Name, a.Module, a.Module, a.Name)})
		}
	}
	slices.SortStableFunc(issues, func(a, b Issue) int { return strings.Compare(a.Path, b.Path) })
	result.Summary = fmt.Sprintf("%d actions checked", len(list)+countPaths(problems))
	result.IssueCount = len(issues)
	result.OK = len(issues) == 0
	result.Issues = issues
	return result
}

// moduleRecipeIDs lists the ids of the recipes in a module's recipes folder.
func moduleRecipeIDs(vaultPath, module string) []string {
	root := filepath.Join(vaultPath, filepath.FromSlash(assetroots.ModuleDir(module, assetroots.Recipes)))
	loaded, _ := queryrecipe.LoadRoots([]string{root})
	ids := make([]string, 0, len(loaded))
	for _, recipe := range loaded {
		ids = append(ids, recipe.ID)
	}
	return ids
}

// countPaths counts the distinct files with problems.
func countPaths(problems []actiondef.Problem) int {
	seen := map[string]bool{}
	for _, p := range problems {
		seen[p.Path] = true
	}
	return len(seen)
}
