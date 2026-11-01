package validate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/assetroots"
)

const (
	// IssueCodeInvalidModuleName marks a module folder whose name breaks the
	// module name pattern; every loader ignores it.
	IssueCodeInvalidModuleName = "invalid_module_name"
	// IssueCodeReservedModuleName marks a module named like a built-in rzm
	// command, so `rzm <module>` never reaches it.
	IssueCodeReservedModuleName = "reserved_module_name"
)

// BuiltinCommandNames lists the top-level rzm command names and aliases a
// module may not take ([[rhizome-modules|SPEC-0130]] US4). A cmd test keeps
// it equal to the Cobra root's commands, since pkg must not import cmd.
var BuiltinCommandNames = []string{
	"agent", "auth", "changelog", "ci", "claude", "code", "codex", "completion", "config", "credentials",
	"desktop", "desktop-bridge", "diagnostics", "graph", "harness", "help", "hook", "index", "init", "l",
	"list", "mcp", "module", "new-worktree", "note", "ontology", "prompt", "query-recipe", "search", "serve",
	"start", "stop", "tasks", "trust", "untrust", "update", "validate", "vault", "view",
}

// RunModules reports module folders under `.rhizome/modules` whose names are
// invalid or equal a built-in command.
func RunModules(runCtx RunContext) CheckResult {
	result := CheckResult{Name: CheckModules, OK: true}
	if _, err := os.Stat(filepath.Join(runCtx.VaultPath, filepath.FromSlash(assetroots.ModulesDir))); errors.Is(err, fs.ErrNotExist) {
		result.NotApplicable = "no modules found"
		result.Summary = result.NotApplicable
		return result
	}
	valid, invalid, err := assetroots.Modules(runCtx.VaultPath)
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	}
	for _, name := range invalid {
		result.Issues = append(result.Issues, Issue{
			Code: IssueCodeInvalidModuleName, Path: assetroots.ModuleRoot(name),
			Message: fmt.Sprintf("module name %q must match ^[a-z0-9][a-z0-9-]{0,62}$; Rhizome ignores this folder", name),
		})
	}
	for _, name := range valid {
		if slices.Contains(BuiltinCommandNames, name) {
			result.Issues = append(result.Issues, Issue{
				Code: IssueCodeReservedModuleName, Path: assetroots.ModuleRoot(name),
				Message: fmt.Sprintf("module name %q is a built-in rzm command; run its commands with `rzm module %s`, or rename the folder", name, name),
			})
		}
	}
	slices.SortFunc(result.Issues, func(a, b Issue) int { return strings.Compare(a.Path, b.Path) })
	result.Summary = fmt.Sprintf("%d modules checked", len(valid)+len(invalid))
	result.IssueCount = len(result.Issues)
	result.OK = result.IssueCount == 0
	return result
}
