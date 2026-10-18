package operations

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/codefile"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/frontmatter"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// buildModuleFromDir constructs a CodeOverviewModule from a directory's anchors
func buildModuleFromDir(ctx context.Context, opts CodeOverviewOptions, dirPath string, depth int, byDir map[string][]codeanchor.IntelAnchor, fileSetByDir map[string]map[string]struct{}, docStatsByFQN map[string]codeanchor.DocStats, anchorScores map[string]float64, vaultPath string, score float64, vaultPaths paths.VaultPaths) CodeOverviewModule {
	dirAnchors := byDir[dirPath]
	stats := ModuleStats{}
	var anchorSummaries []ModuleAnchor

	for _, a := range dirAnchors {
		ds := docStatsByFQN[a.FQN]
		stats.DocMentions += ds.Mentions
		stats.DocLinks += ds.Links
		stats.Anchors++

		anchorSummaries = append(anchorSummaries, ModuleAnchor{
			Name:      anchorName(a),
			Kind:      a.Kind,
			FQN:       a.FQN,
			Lang:      string(a.Lang),
			Signature: a.Signature,
			Mentions:  ds.Mentions,
			Links:     ds.Links,
			Path:      a.Path,
			Doc:       strings.TrimSpace(a.DocComment),
		})
	}

	// Sort anchors by PageRank
	sort.Slice(anchorSummaries, func(i, j int) bool {
		prI := anchorScores[findAnchorIDFromAnchors(dirAnchors, anchorSummaries[i])]
		prJ := anchorScores[findAnchorIDFromAnchors(dirAnchors, anchorSummaries[j])]
		if prI != prJ {
			return prI > prJ
		}
		return anchorSummaries[i].Name < anchorSummaries[j].Name
	})
	if len(anchorSummaries) > 5 {
		anchorSummaries = anchorSummaries[:5]
	}

	// Collect doc links
	var allDocLinks []codeanchor.DocLink
	for filePath := range fileSetByDir[dirPath] {
		if opts.Intel != nil {
			links, _ := opts.Intel.DocLinksFromCodePath(ctx, filePath, 10)
			allDocLinks = append(allDocLinks, links...)
		}
	}
	docMap := dedupeDocs(allDocLinks)

	docPatterns := opts.DocPatterns
	primaryPattern := primaryDocPattern(docPatterns)
	primaryDocPath := ""
	if vaultPath != "" && primaryPattern != "" {
		absDir := filepath.Join(vaultPath, filepath.FromSlash(dirPath))
		if match, ok := findDocMatchInDir(absDir, []string{primaryPattern}, vaultPaths); ok {
			primaryDocPath = match.Path
		}
	}

	moduleDocs := buildModuleDocs(opts.Notes, dirPath, docMap, vaultPaths, docPatterns, primaryPattern, opts.MaxEmptyLevels)
	if primaryDocPath != "" {
		found := false
		for _, d := range moduleDocs {
			if d.Path == primaryDocPath {
				found = true
				break
			}
		}
		if !found {
			title := filepath.Base(primaryDocPath)
			summary := ""
			body := ""
			if fact, ok := NoteFactsFromReader(opts.Notes).LookupFact(primaryDocPath); ok {
				blessed := frontmatter.FilterBlessed(fact.Frontmatter)
				summary = firstSummary(blessed)
				if primaryPattern != "" && obsidian.MatchDocPatternNormalized(primaryDocPath, []string{primaryPattern}) {
					body = moduleDocContextBody(fact, 300)
				}
			}
			moduleDocs = append(moduleDocs, ModuleDoc{
				Path:    primaryDocPath,
				Title:   strings.TrimSuffix(title, filepath.Ext(title)),
				Summary: summary,
				Body:    body,
			})
		}
	}

	if len(moduleDocs) > 5 {
		moduleDocs = moduleDocs[:5]
	}

	contextSummary := ""
	moduleDirPrimary := ""
	if primaryPattern != "" {
		moduleDirPrimary = filepath.ToSlash(filepath.Join(dirPath, primaryPattern))
	}
	for _, d := range moduleDocs {
		if primaryPattern != "" && obsidian.MatchDocPatternNormalized(d.Path, []string{primaryPattern}) && d.Summary != "" {
			// Only use summary if the doc is directly in this module's directory
			// (not an ancestor doc). This avoids repeating root CONTEXT.md summary in every module.
			if d.Path == moduleDirPrimary || filepath.Dir(d.Path) == dirPath {
				contextSummary = d.Summary
				break
			}
		}
	}

	tests := collectTests(dirAnchors)
	submodules := findSubmodules(dirPath, byDir)

	return CodeOverviewModule{
		Path:              dirPath,
		Depth:             depth,
		Score:             score,
		Stats:             stats,
		Docs:              moduleDocs,
		Anchors:           anchorSummaries,
		Tests:             tests,
		DocLinkCount:      len(allDocLinks),
		FileCount:         len(fileSetByDir[dirPath]),
		Submodules:        submodules,
		ContextSummary:    contextSummary,
		PrimaryDocPattern: primaryPattern,
	}
}

func findAnchorIDFromAnchors(anchors []codeanchor.IntelAnchor, target ModuleAnchor) string {
	for _, a := range anchors {
		if a.FQN == target.FQN && a.Path == target.Path {
			return a.AnchorID
		}
	}
	return ""
}

func anchorName(a codeanchor.IntelAnchor) string {
	if a.Symbol != "" {
		return a.Symbol
	}
	if a.FQN != "" {
		return a.FQN
	}
	return filepath.Base(a.Path)
}

func findSubmodules(dirPath string, byDir map[string][]codeanchor.IntelAnchor) []string {
	dirPath = filepath.ToSlash(filepath.Clean(dirPath))
	if dirPath == "." {
		dirPath = ""
	}
	prefix := dirPath
	if prefix != "" {
		prefix += "/"
	}

	seen := make(map[string]struct{})
	var submodules []string
	for otherDir := range byDir {
		otherDir = filepath.ToSlash(filepath.Clean(otherDir))
		if strings.HasPrefix(otherDir, prefix) && otherDir != dirPath {
			// Extract the immediate subdirectory
			remainder := strings.TrimPrefix(otherDir, prefix)
			parts := strings.Split(remainder, "/")
			if len(parts) > 0 {
				subDir := prefix + parts[0]
				if _, exists := seen[subDir]; !exists {
					seen[subDir] = struct{}{}
					submodules = append(submodules, subDir)
				}
			}
		}
	}
	sort.Strings(submodules)
	if len(submodules) > 5 {
		submodules = submodules[:5]
	}
	return submodules
}

// isTestPath returns true if the path represents a test file or test directory.
// Detects:
// - Directories: tests/, test/, __tests__/, spec/, _test/, fixtures/, testdata/
// - Go: *_test.go, *_bench.go
// - Python: test_*.py, *_test.py, conftest.py
// - TS/JS: *.test.ts, *.test.js, *.spec.ts, *.spec.js
// - General: paths containing /tests/ or /test/ segments
func isTestPath(path string) bool {
	path = filepath.ToSlash(path)
	base := filepath.Base(path)
	dir := filepath.Dir(path)

	// Check for test directories
	testDirs := []string{"tests", "test", "__tests__", "spec", "_test", "fixtures", "testdata", ".test", "testing"}
	for _, testDir := range testDirs {
		if base == testDir || strings.Contains(dir, "/"+testDir+"/") || strings.HasPrefix(dir, testDir+"/") || strings.HasSuffix(dir, "/"+testDir) {
			return true
		}
	}

	// Check for test file patterns
	ext := strings.ToLower(filepath.Ext(path))
	baseLower := strings.ToLower(base)

	// Go: *_test.go, *_bench.go
	if ext == ".go" && (strings.HasSuffix(baseLower, "_test.go") || strings.HasSuffix(baseLower, "_bench.go")) {
		return true
	}

	// Python: test_*.py, *_test.py, conftest.py
	if ext == ".py" {
		if strings.HasPrefix(baseLower, "test_") || strings.HasSuffix(baseLower, "_test.py") || baseLower == "conftest.py" {
			return true
		}
	}

	// TS/JS: *.test.ts, *.test.js, *.spec.ts, *.spec.js
	if codefile.IsTypeScriptJavaScriptExtension(ext) {
		if strings.Contains(baseLower, ".test.") || strings.Contains(baseLower, ".spec.") {
			return true
		}
	}

	return false
}

func collectTests(anchors []codeanchor.IntelAnchor) []string {
	seen := map[string]struct{}{}
	var tests []string
	for _, a := range anchors {
		if isTestPath(a.Path) {
			if _, ok := seen[a.Path]; ok {
				continue
			}
			seen[a.Path] = struct{}{}
			tests = append(tests, a.Path)
		}
	}
	sort.Strings(tests)
	return tests
}

// isTestOnlyDirectory returns true if all anchors in a directory are test files.
func isTestOnlyDirectory(anchors []codeanchor.IntelAnchor) bool {
	if len(anchors) == 0 {
		return false
	}
	for _, a := range anchors {
		if !isTestPath(a.Path) {
			return false
		}
	}
	return true
}

// countEntryPoints counts the number of anchors with PageRank above the threshold.
func countEntryPoints(anchors []codeanchor.IntelAnchor, anchorScores map[string]float64) int {
	const threshold = 0.1
	count := 0
	for _, a := range anchors {
		if score, ok := anchorScores[a.AnchorID]; ok && score > threshold {
			count++
		}
	}
	return count
}
