package operations

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/vault/assetroots"
)

// ModuleRecipes is what a module offers as commands
// ([[rhizome-modules#^SPEC-0130-US4|SPEC-0130 US4]]).
type ModuleRecipes struct {
	// Commands are the module's recipes, each run by its id.
	Commands []queryrecipe.Recipe
	// Unnamed are module recipe ids that cannot be a command name because
	// they contain whitespace; `rzm query-recipe run --id` still runs them.
	Unnamed []string
	// All and Issues are every recipe the repository loads, which a run
	// needs to resolve shared ids and report problems.
	All    []queryrecipe.Recipe
	Issues []queryrecipe.Issue
}

// ModuleExists reports whether vaultPath has a module folder named name.
func ModuleExists(vaultPath, name string) bool {
	if !assetroots.ValidModuleName(name) {
		return false
	}
	info, err := os.Stat(filepath.Join(vaultPath, filepath.FromSlash(assetroots.ModuleRoot(name))))
	return err == nil && info.IsDir()
}

// LoadModuleRecipes loads the repository's recipes and selects the ones in
// the module's recipes folder.
func LoadModuleRecipes(vaultPath, name string) (ModuleRecipes, error) {
	if !ModuleExists(vaultPath, name) {
		return ModuleRecipes{}, fmt.Errorf("no module named %q in %s", name, filepath.Join(vaultPath, filepath.FromSlash(assetroots.ModulesDir)))
	}
	// WHY: the shared roots plus the module's own keep ids unique
	// repository-wide; LoadRoots skips files it already read.
	recipesRoot := filepath.Join(vaultPath, filepath.FromSlash(assetroots.ModuleDir(name, assetroots.Recipes)))
	all, issues := queryrecipe.LoadRoots(append(queryrecipe.DefaultSourceRoots(vaultPath), recipesRoot))
	out := ModuleRecipes{All: all, Issues: issues}
	for _, recipe := range all {
		if !strings.HasPrefix(recipe.Source.Path, recipesRoot+string(filepath.Separator)) {
			continue
		}
		if strings.IndexFunc(recipe.ID, unicode.IsSpace) >= 0 {
			out.Unnamed = append(out.Unnamed, recipe.ID)
			continue
		}
		out.Commands = append(out.Commands, recipe)
	}
	return out, nil
}
