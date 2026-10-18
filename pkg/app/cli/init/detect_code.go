package init

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/codefile"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/codepatterns"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// detectCode inspects the tree for language signals and module roots.
func detectCode(root string) CodeSuggestion {
	langSet := map[string]struct{}{}
	pythonRoots := map[string]struct{}{}
	goMarkerRoots := map[string]struct{}{}
	goFallbackRoots := map[string]struct{}{}
	tsMarkerRoots := map[string]struct{}{}
	tsFallbackRoots := map[string]struct{}{}
	csMarkerRoots := map[string]struct{}{}
	csFallbackRoots := map[string]struct{}{}
	phpMarkerRoots := map[string]struct{}{}
	phpFallbackRoots := map[string]struct{}{}
	rootPaths, _ := paths.NewVaultPaths(root)
	matcher := initIgnoreMatcher(root)
	fileCount := 0
	var files []string

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, relErr := rootPaths.RelStrict(path)
		if relErr != nil {
			return relErr
		}
		rel := relPath.String()

		if d.IsDir() {
			if shouldSkipDir(rel, d.Name(), matcher) {
				return filepath.SkipDir
			}
			return nil
		}

		switch strings.ToLower(filepath.Base(path)) {
		case "go.mod":
			langSet["go"] = struct{}{}
			if rel != "" {
				// Root is the directory containing go.mod (module root).
				moduleRoot := filepath.ToSlash(filepath.Dir(rel))
				if moduleRoot == "." {
					moduleRoot = "."
				}
				goMarkerRoots[moduleRoot] = struct{}{}
			}
		case "package.json", "tsconfig.json":
			langSet["typescript"] = struct{}{}
			langSet["javascript"] = struct{}{}
			if rel != "" {
				// Root is the directory containing package.json/tsconfig.json.
				moduleRoot := filepath.ToSlash(filepath.Dir(rel))
				if moduleRoot == "." {
					moduleRoot = "."
				}
				tsMarkerRoots[moduleRoot] = struct{}{}
			}
		case "pyproject.toml", "requirements.txt", "setup.py":
			langSet["python"] = struct{}{}
		case "cargo.toml":
			langSet["rust"] = struct{}{}
		case "pom.xml", "build.gradle", "build.gradle.kts":
			langSet["java"] = struct{}{}
		case "global.json", "Directory.Build.props", "Directory.Build.targets":
			langSet["csharp"] = struct{}{}
		case "composer.json":
			langSet["php"] = struct{}{}
			if rel != "" {
				moduleRoot := filepath.ToSlash(filepath.Dir(rel))
				if moduleRoot == "." {
					moduleRoot = "."
				}
				phpMarkerRoots[moduleRoot] = struct{}{}
			}
		}

		if strings.HasSuffix(strings.ToLower(path), ".csproj") || strings.HasSuffix(strings.ToLower(path), ".sln") {
			langSet["csharp"] = struct{}{}
			if rel != "" {
				rootDir := filepath.ToSlash(filepath.Dir(rel))
				if rootDir == "." {
					rootDir = "."
				}
				csMarkerRoots[rootDir] = struct{}{}
			}
		}

		ext := strings.ToLower(filepath.Ext(path))
		files = append(files, rel)
		if sourceFileExtensions[ext] {
			fileCount++
		}
		if recordTypeScriptJavaScriptFile(ext, rel, langSet, tsFallbackRoots) {
			return nil
		}
		switch ext {
		case ".go":
			langSet["go"] = struct{}{}
			if rel != "" {
				parts := strings.Split(rel, "/")
				if len(parts) > 1 {
					goFallbackRoots[parts[0]] = struct{}{}
				} else {
					goFallbackRoots["."] = struct{}{}
				}
			}
		case ".py":
			langSet["python"] = struct{}{}
			root := pythonRootFromRel(rel)
			if root != "" {
				pythonRoots[root] = struct{}{}
			}
		case ".java":
			langSet["java"] = struct{}{}
		case ".cs":
			langSet["csharp"] = struct{}{}
			if rel != "" {
				parts := strings.Split(rel, "/")
				if len(parts) > 1 {
					csFallbackRoots[parts[0]] = struct{}{}
				} else {
					csFallbackRoots["."] = struct{}{}
				}
			}
		case ".php":
			langSet["php"] = struct{}{}
			if rel != "" {
				parts := strings.Split(rel, "/")
				if len(parts) > 1 {
					phpFallbackRoots[parts[0]] = struct{}{}
				} else {
					phpFallbackRoots["."] = struct{}{}
				}
			}
		case ".c", ".cpp", ".h", ".hpp":
			langSet["cpp"] = struct{}{}
		case ".rs":
			langSet["rust"] = struct{}{}
		case ".rb":
			langSet["ruby"] = struct{}{}
		case ".sh":
			langSet["shell"] = struct{}{}
		}

		return nil
	})

	if walkErr != nil {
		return CodeSuggestion{}
	}

	langs := make([]string, 0, len(langSet))
	for lang := range langSet {
		langs = append(langs, lang)
	}
	sort.Strings(langs)

	scan := includesForLanguages(langs)
	if len(scan) == 0 && len(langs) > 0 {
		scan = append(scan, coderefs.DefaultIncludes...)
	}

	var pyRoots []string
	for r := range pythonRoots {
		pyRoots = append(pyRoots, r)
	}
	pyRoots = rollupRoots(pyRoots)

	goRoots := goMarkerRoots
	if len(goRoots) == 0 {
		goRoots = goFallbackRoots
	}
	var gRoots []string
	for r := range goRoots {
		gRoots = append(gRoots, r)
	}
	gRoots = rollupRoots(gRoots)

	tsRoots := tsMarkerRoots
	if len(tsRoots) == 0 {
		tsRoots = tsFallbackRoots
	}
	var tRoots []string
	for r := range tsRoots {
		tRoots = append(tRoots, r)
	}
	tRoots = rollupRoots(tRoots)

	csRoots := csMarkerRoots
	if len(csRoots) == 0 {
		csRoots = csFallbackRoots
	}
	var cRoots []string
	for r := range csRoots {
		cRoots = append(cRoots, r)
	}
	cRoots = rollupRoots(cRoots)

	phpRoots := phpMarkerRoots
	if len(phpRoots) == 0 {
		phpRoots = phpFallbackRoots
	}
	var pRoots []string
	for r := range phpRoots {
		pRoots = append(pRoots, r)
	}
	pRoots = rollupRoots(pRoots)

	return CodeSuggestion{
		EnableCodeRefs: len(langs) > 0,
		Languages:      langs,
		Scan:           scan,
		Ignore:         append([]string{}, repoconfig.DefaultCodeConfig.Ignore...),
		PythonRoots:    pyRoots,
		PythonScan:     codepatterns.DefaultPythonGlobs(),
		PythonIgnore:   []string{},
		GoRoots:        gRoots,
		GoScan:         codepatterns.DefaultGoGlobs(),
		GoIgnore:       []string{},
		TSRoots:        tRoots,
		TSScan:         codepatterns.DefaultTypeScriptGlobs(),
		TSIgnore:       []string{},
		CSharpRoots:    cRoots,
		CSharpScan:     codepatterns.DefaultCSharpGlobs(),
		CSharpIgnore:   []string{},
		PHPRoots:       pRoots,
		PHPScan:        codepatterns.DefaultPHPGlobs(),
		PHPIgnore:      []string{},
		FileCount:      fileCount,
		Files:          files,
	}
}

var sourceFileExtensions = map[string]bool{
	".go": true, ".py": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".cjs": true,
	".cs": true, ".php": true, ".java": true, ".rs": true, ".rb": true, ".c": true, ".cpp": true, ".h": true, ".hpp": true,
}

func recordTypeScriptJavaScriptFile(ext, rel string, langSet, fallbackRoots map[string]struct{}) bool {
	switch {
	case codefile.IsTypeScriptExtension(ext):
		langSet["typescript"] = struct{}{}
		if ext == ".tsx" {
			langSet["javascript"] = struct{}{}
		}
	case codefile.IsJavaScriptExtension(ext):
		langSet["javascript"] = struct{}{}
	default:
		return false
	}
	if rel == "" {
		return true
	}
	parts := strings.Split(rel, "/")
	if len(parts) > 1 {
		fallbackRoots[parts[0]] = struct{}{}
	} else {
		fallbackRoots["."] = struct{}{}
	}
	return true
}

func includesForLanguages(langs []string) []string {
	patterns := map[string][]string{
		"go":         codepatterns.DefaultGoGlobs(),
		"typescript": codepatterns.DefaultTypeScriptOnlyGlobs(),
		"javascript": codepatterns.DefaultJavaScriptOnlyGlobs(),
		"python":     codepatterns.DefaultPythonGlobs(),
		"java":       {"**/*.java"},
		"csharp":     codepatterns.DefaultCSharpGlobs(),
		"php":        codepatterns.DefaultPHPGlobs(),
		"cpp":        {"**/*.c", "**/*.cpp", "**/*.h", "**/*.hpp"},
		"rust":       {"**/*.rs"},
		"ruby":       {"**/*.rb"},
		"shell":      {"**/*.sh"},
	}

	var result []string
	seen := map[string]struct{}{}
	for _, lang := range langs {
		for _, p := range patterns[lang] {
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			result = append(result, p)
		}
	}
	return result
}

func rollupRoots(roots []string) []string {
	seen := map[string]struct{}{}
	var cleaned []string
	for _, r := range roots {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		r = filepath.ToSlash(filepath.Clean(r))
		if r == "" || r == "/" {
			continue
		}
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		cleaned = append(cleaned, r)
	}

	if _, ok := seen["."]; ok {
		return []string{"."}
	}

	sort.Slice(cleaned, func(i, j int) bool {
		if len(cleaned[i]) == len(cleaned[j]) {
			return cleaned[i] < cleaned[j]
		}
		return len(cleaned[i]) < len(cleaned[j])
	})

	var kept []string
	for _, r := range cleaned {
		skip := false
		for _, k := range kept {
			if isSubpath(r, k) {
				skip = true
				break
			}
		}
		if !skip {
			kept = append(kept, r)
		}
	}
	return kept
}

func isSubpath(child, parent string) bool {
	if parent == "" || child == "" {
		return false
	}
	if child == parent {
		return true
	}
	return strings.HasPrefix(child, parent+"/")
}

func pythonRootFromRel(rel string) string {
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		switch part {
		case "src", "packages", "scripts", "app", "apps", "services", "pkg":
			return strings.Join(parts[:i+1], "/")
		}
	}
	if len(parts) > 1 {
		return parts[0]
	}
	return "."
}

func shouldSkipDir(rel, name string, matcher *ignore.Matcher) bool {
	if strings.HasPrefix(name, ".") && name != "." {
		return true
	}
	if matcher == nil {
		return false
	}
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return false
	}
	return matcher.IsIgnoredShallow(rel, true)
}
