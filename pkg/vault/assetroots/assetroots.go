// Package assetroots locates Rhizome's repository assets in both layouts
// ([[rhizome-modules|SPEC-0130]]): the flat folders under `.rhizome/` and the
// module folders `.rhizome/modules/<name>/<kind>/`. Every loader, validator,
// watcher, and `rzm init` resolves asset folders here instead of hard-coding
// flat paths. Paths are repository-relative with forward slashes.
package assetroots

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Kind is one kind of repository asset.
type Kind string

const (
	Ontology Kind = "ontology"
	Recipes  Kind = "recipes"
	Views    Kind = "views"
	Tasks    Kind = "tasks"
	Scripts  Kind = "scripts"
	Actions  Kind = "actions"
)

// ModulesDir holds module folders, relative to the repository root.
const ModulesDir = ".rhizome/modules"

// Kinds lists every asset kind in a stable order.
var Kinds = []Kind{Ontology, Recipes, Views, Tasks, Scripts, Actions}

// flatDirs maps a kind to its flat folder. Scripts and actions have no flat
// folder.
var flatDirs = map[Kind]string{
	Ontology: ".rhizome/ontology",
	Recipes:  ".rhizome/query-recipes",
	Views:    ".rhizome/views",
	Tasks:    ".rhizome/tasks",
}

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// Root is one folder holding assets of one kind. Module is empty for the
// flat folder.
type Root struct {
	Module string
	Rel    string
}

// Abs returns the root's absolute path under repoRoot.
func (r Root) Abs(repoRoot string) string {
	return filepath.Join(repoRoot, filepath.FromSlash(r.Rel))
}

// FlatDir returns the kind's flat folder, or "" when the kind has none.
func FlatDir(k Kind) string { return flatDirs[k] }

// ModuleDir returns the kind's folder inside a module.
func ModuleDir(module string, k Kind) string {
	return ModulesDir + "/" + module + "/" + string(k)
}

// ModuleRoot returns a module's folder.
func ModuleRoot(module string) string { return ModulesDir + "/" + module }

// ValidModuleName reports whether name is a valid module folder name.
func ValidModuleName(name string) bool { return namePattern.MatchString(name) }

// Qualify joins a module and a name; an empty module leaves name unchanged.
func Qualify(module, name string) string {
	if module == "" {
		return name
	}
	return module + "/" + name
}

// Modules lists the module folders under repoRoot, sorted. Folders whose
// names are not valid module names are returned separately so validation
// can report them; loaders ignore them.
func Modules(repoRoot string) (valid, invalid []string, err error) {
	entries, err := os.ReadDir(filepath.Join(repoRoot, filepath.FromSlash(ModulesDir)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if ValidModuleName(e.Name()) {
			valid = append(valid, e.Name())
		} else {
			invalid = append(invalid, e.Name())
		}
	}
	sort.Strings(valid)
	sort.Strings(invalid)
	return valid, invalid, nil
}

// Roots returns the existing folders for a kind: the flat folder first, then
// each valid module's folder by module name. A root that exists but cannot be
// used is left out and reported in the error, which joins every such problem;
// the usable roots are still returned.
func Roots(repoRoot string, k Kind) ([]Root, error) {
	var roots []Root
	var problems []error
	if flat := flatDirs[k]; flat != "" {
		found, err := dirExists(repoRoot, flat)
		if err != nil {
			problems = append(problems, err)
		} else if found {
			roots = append(roots, Root{Rel: flat})
		}
	}
	modules, _, err := Modules(repoRoot)
	if err != nil {
		return roots, errors.Join(append(problems, err)...)
	}
	for _, m := range modules {
		rel := ModuleDir(m, k)
		found, err := dirExists(repoRoot, rel)
		if err != nil {
			problems = append(problems, err)
		} else if found {
			roots = append(roots, Root{Module: m, Rel: rel})
		}
	}
	return roots, errors.Join(problems...)
}

// AllRoots returns every root Roots would return if each folder existed:
// the flat folder (when the kind has one) and the kind's folder in each
// listed module. Callers that read Git trees instead of the file system
// pass the module names they found there.
func AllRoots(k Kind, modules []string) []Root {
	var roots []Root
	if flat := flatDirs[k]; flat != "" {
		roots = append(roots, Root{Rel: flat})
	}
	for _, m := range modules {
		if ValidModuleName(m) {
			roots = append(roots, Root{Module: m, Rel: ModuleDir(m, k)})
		}
	}
	return roots
}

// Classify maps a repository-relative path to the root and kind that hold
// it and the path inside that root. ok is false for paths outside every
// asset root, including a root folder itself.
func Classify(rel string) (root Root, kind Kind, sub string, ok bool) {
	rel = path.Clean(strings.TrimPrefix(filepath.ToSlash(rel), "./"))
	for _, k := range Kinds {
		if flat := flatDirs[k]; flat != "" {
			if rest, found := strings.CutPrefix(rel, flat+"/"); found && rest != "" {
				return Root{Rel: flat}, k, rest, true
			}
		}
	}
	rest, found := strings.CutPrefix(rel, ModulesDir+"/")
	if !found {
		return Root{}, "", "", false
	}
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 3 || parts[2] == "" || !ValidModuleName(parts[0]) {
		return Root{}, "", "", false
	}
	k := Kind(parts[1])
	if !knownKind(k) {
		return Root{}, "", "", false
	}
	return Root{Module: parts[0], Rel: ModuleDir(parts[0], k)}, k, parts[2], true
}

// IsAssetPath reports whether rel lies inside any asset root of kind k.
func IsAssetPath(rel string, k Kind) bool {
	_, kind, _, ok := Classify(rel)
	return ok && kind == k
}

func knownKind(k Kind) bool {
	for _, known := range Kinds {
		if k == known {
			return true
		}
	}
	return false
}

// dirExists reports whether rel is a folder. A missing path is absent; any
// other failure, or a path that exists but is not a folder, is an error, so
// a broken asset root is never mistaken for an empty one.
func dirExists(repoRoot, rel string) (bool, error) {
	abs := filepath.Join(repoRoot, filepath.FromSlash(rel))
	info, err := os.Stat(abs)
	if errors.Is(err, fs.ErrNotExist) {
		// A link whose target is missing is broken, not absent.
		if _, lerr := os.Lstat(abs); lerr == nil {
			return false, fmt.Errorf("%s is a broken link", rel)
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("%s is not a folder", rel)
	}
	return true, nil
}
