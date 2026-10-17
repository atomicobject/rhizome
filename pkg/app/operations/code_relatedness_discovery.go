package operations

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/codefile"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
)

type discoveredPkg struct {
	ImportPath string
	Canonical  string
	DirAbs     string
	DirRel     string
	FilesAbs   []string
	Files      int

	ImportsTo map[string]struct{}
	CallsTo   map[string]struct{}

	ImportFanIn  int
	ImportFanOut int
	CallFanIn    int
	CallFanOut   int

	AnchorIDs       map[int64]struct{}
	AnchorLabelsSet map[string]struct{}
	AnchorLabels    int

	NotePathsSet map[string]struct{}
	LinkedNotes  int

	TopRelated []CodeRelatednessEdge
}

func discoverPackages(ctx context.Context, vaultPath string, roots []string, matcher *ignore.Matcher, includeTests bool, intel CodeRelatednessIntel) ([]discoveredPkg, int, error) {
	if intel == nil {
		return discoverCodePackages(vaultPath, roots, matcher, includeTests)
	}

	vaultPaths, _ := paths.NewVaultPaths(vaultPath)
	files, err := intel.IndexedFilePaths(ctx)
	if err != nil {
		return nil, 0, err
	}
	if len(files) == 0 {
		return discoverCodePackages(vaultPath, roots, matcher, includeTests)
	}

	type dirInfo struct {
		abs   string
		rel   string
		files []string
	}

	dirs := make(map[string]*dirInfo)
	filesTotal := 0

	rootAbs := make([]string, 0, len(roots))
	for _, r := range roots {
		if r == "" {
			continue
		}
		if ref, err := paths.ResolveCodeRefWithVaultPaths(vaultPaths, r); err == nil && ref.Abs != "" {
			rootAbs = append(rootAbs, ref.Abs.String())
		}
	}

	addFile := func(abs string) {
		dir := filepath.Dir(abs)
		relDir := filepath.ToSlash(dir)
		if rel, err := vaultPaths.RelStrict(dir); err == nil {
			relDir = rel.String()
		}
		if relDir == "" {
			relDir = "."
		}
		info := dirs[dir]
		if info == nil {
			info = &dirInfo{abs: dir, rel: relDir}
			dirs[dir] = info
		}
		info.files = append(info.files, abs)
		filesTotal++
	}

	for _, file := range files {
		if strings.TrimSpace(file) == "" {
			continue
		}
		ref, err := paths.ResolveCodeRefWithVaultPaths(vaultPaths, file)
		if err != nil || ref.Abs == "" || ref.Rel == "" {
			continue
		}
		abs := ref.Abs.String()
		rel := ref.Rel.String()
		if matcher != nil && matcher.IsIgnored(rel, false) {
			continue
		}
		if len(rootAbs) > 0 && !withinRoots(abs, rootAbs) {
			continue
		}
		if !includeTests && isTestFile(abs) {
			continue
		}
		addFile(abs)
	}

	modulePath := readGoModulePath(vaultPath)

	var pkgs []discoveredPkg
	for _, d := range dirs {
		sort.Strings(d.files)
		importPath := d.rel
		if modulePath != "" {
			if d.rel == "." {
				importPath = modulePath
			} else {
				importPath = modulePath + "/" + strings.TrimPrefix(d.rel, "./")
				importPath = strings.TrimSuffix(importPath, "/.")
			}
		}
		pkgs = append(pkgs, discoveredPkg{
			ImportPath: importPath,
			Canonical:  normalizePkgName(importPath),
			DirAbs:     d.abs,
			DirRel:     d.rel,
			FilesAbs:   d.files,
			Files:      len(d.files),
		})
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].ImportPath < pkgs[j].ImportPath })
	return pkgs, filesTotal, nil
}

func discoverCodePackages(vaultPath string, roots []string, matcher *ignore.Matcher, includeTests bool) ([]discoveredPkg, int, error) {
	type dirInfo struct {
		abs   string
		rel   string
		files []string
	}

	vaultPaths, _ := paths.NewVaultPaths(vaultPath)
	dirs := make(map[string]*dirInfo)
	filesTotal := 0

	addFile := func(abs string) {
		dir := filepath.Dir(abs)
		relDir := filepath.ToSlash(dir)
		if rel, err := vaultPaths.RelStrict(dir); err == nil {
			relDir = rel.String()
		}
		if relDir == "." {
			relDir = "."
		}
		info := dirs[dir]
		if info == nil {
			info = &dirInfo{abs: dir, rel: relDir}
			dirs[dir] = info
		}
		info.files = append(info.files, abs)
		filesTotal++
	}

	for _, root := range roots {
		if root == "" {
			continue
		}
		ref, err := paths.ResolveCodeRefWithVaultPaths(vaultPaths, root)
		if err != nil || ref.Abs == "" {
			continue
		}
		rootAbs := ref.Abs.String()

		st, err := os.Stat(rootAbs)
		if err != nil {
			return nil, 0, err
		}
		if !st.IsDir() {
			if isCodeFile(rootAbs, includeTests) && !isIgnored(vaultPath, rootAbs, false, matcher) {
				addFile(rootAbs)
			}
			continue
		}

		err = filepath.WalkDir(rootAbs, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if isIgnored(vaultPath, path, d.IsDir(), matcher) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if isCodeFile(path, includeTests) {
				addFile(path)
			}
			return nil
		})
		if err != nil {
			return nil, 0, err
		}
	}

	modulePath := readGoModulePath(vaultPath)

	var pkgs []discoveredPkg
	for _, d := range dirs {
		sort.Strings(d.files)
		importPath := d.rel
		if modulePath != "" {
			if d.rel == "." {
				importPath = modulePath
			} else {
				importPath = modulePath + "/" + strings.TrimPrefix(d.rel, "./")
				importPath = strings.TrimSuffix(importPath, "/.")
			}
		}
		pkgs = append(pkgs, discoveredPkg{
			ImportPath: importPath,
			Canonical:  normalizePkgName(importPath),
			DirAbs:     d.abs,
			DirRel:     d.rel,
			FilesAbs:   d.files,
			Files:      len(d.files),
		})
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].ImportPath < pkgs[j].ImportPath })
	return pkgs, filesTotal, nil
}

// isCodeFile returns true if the path has a recognized code extension.
func isCodeFile(path string, includeTests bool) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if _, ok := langByExt[ext]; !ok {
		return false
	}
	base := filepath.Base(path)
	if strings.HasPrefix(base, ".") {
		return false
	}
	if !includeTests && isTestFile(path) {
		return false
	}
	return true
}

// isTestFile returns true if the path looks like a test file.
func isTestFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	// Go: _test.go
	if strings.HasSuffix(base, "_test.go") {
		return true
	}
	// Python: test_*.py or *_test.py
	if strings.HasSuffix(base, ".py") {
		if strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") {
			return true
		}
	}
	// TypeScript/JavaScript: *.test.ts, *.spec.ts, etc.
	if strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") {
		return true
	}
	return false
}

func isIgnored(vaultPath, abs string, isDir bool, matcher *ignore.Matcher) bool {
	if matcher == nil || vaultPath == "" {
		return false
	}
	vaultPaths, err := paths.NewVaultPaths(vaultPath)
	if err != nil {
		return false
	}
	rel, err := vaultPaths.RelStrict(abs)
	if err != nil {
		return false
	}
	return matcher.IsIgnored(rel.String(), isDir)
}

func withinRoots(abs string, roots []string) bool {
	if len(roots) == 0 {
		return true
	}
	abs = normalizeAbsPath(abs)
	for _, r := range roots {
		if r == "" {
			continue
		}
		root := normalizeAbsPath(r)
		if abs == root || strings.HasPrefix(abs, root+"/") {
			return true
		}
	}
	return false
}

func normalizeAbsPath(path string) string {
	return paths.NormalizeAbsPathForCompare(path)
}

var langByExt = buildLangByExt()

func buildLangByExt() map[string]codeanchor.Lang {
	out := map[string]codeanchor.Lang{
		".py":  codeanchor.LangPy,
		".go":  codeanchor.LangGo,
		".cs":  codeanchor.LangCs,
		".php": codeanchor.LangPhp,
	}
	for _, ext := range codefile.TypeScriptJavaScriptExtensions() {
		out[ext] = codeanchor.LangTS
	}
	return out
}

func langForPath(path string) codeanchor.Lang {
	ext := strings.ToLower(filepath.Ext(path))
	return langByExt[ext]
}

// normalizePkgName canonicalizes package/module identifiers for comparison.
// It converts both dots (Python) and backslashes (Windows) to forward slashes
// so matching works across languages (callee FQNs use dots, discovered packages use slashes).
func normalizePkgName(pkg string) string {
	pkg = strings.TrimSpace(pkg)
	pkg = strings.TrimPrefix(pkg, "./")
	pkg = strings.TrimPrefix(pkg, "/")
	pkg = filepath.ToSlash(pkg)
	// Convert Python-style dot separators to slashes for consistent matching.
	pkg = strings.ReplaceAll(pkg, ".", "/")
	return pkg
}

// matchInternalPkg returns the closest ancestor in the package alias map.
func matchInternalPkg(pkg string, aliasToCanonical map[string]string) (string, bool) {
	if pkg == "" {
		return "", false
	}
	if canon, ok := aliasToCanonical[pkg]; ok {
		return canon, true
	}
	parts := strings.Split(pkg, "/")
	for len(parts) > 1 {
		parts = parts[:len(parts)-1]
		cand := strings.Join(parts, "/")
		if canon, ok := aliasToCanonical[cand]; ok {
			return canon, true
		}
	}
	return "", false
}

func stripCommonRoots(path string) string {
	path = strings.TrimPrefix(path, "/")
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if p == "src" || p == "lib" {
			trimmed := strings.Join(parts[i+1:], "/")
			return trimmed
		}
	}
	return ""
}
