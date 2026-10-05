package init

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/ignore"
)

// IgnoredRepoCandidate describes a directory excluded by gitignore-layer rules
// that looks like a real nested repo or code root, e.g. a wrapper repo's
// gitignored submodule (SPEC-0064). Candidates are surfaced for an explicit
// user inclusion decision; nothing is auto-included.
type IgnoredRepoCandidate struct {
	// Rel is the root-relative slash path of the candidate directory.
	Rel string
	// HasGit reports a .git entry at the candidate root (nested repo/submodule).
	HasGit bool
	// Markers lists recognized language/build markers found at the candidate root.
	Markers []string
	// Rule is the gitignore rule that excludes the candidate.
	Rule *ignore.RuleRef
}

// nestedRepoMarkers are filenames (or *.ext suffix probes) that mark a
// directory as a code root worth offering for inclusion.
var nestedRepoMarkerNames = []string{"go.mod", "package.json", "tsconfig.json", "pyproject.toml", "setup.py"}
var nestedRepoMarkerSuffixes = []string{".csproj", ".sln"}

// detectIgnoredNestedRepos walks the project looking for directories that the
// gitignore layer excludes but that contain a nested repo or language marker.
// The walk never descends into ignored trees: each ignored directory gets a
// bounded probe of its own top level plus one child level.
func detectIgnoredNestedRepos(root string, matcher *ignore.Matcher) []IgnoredRepoCandidate {
	if strings.TrimSpace(root) == "" || matcher == nil {
		return nil
	}

	var candidates []IgnoredRepoCandidate
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		name := d.Name()
		if path != root && strings.HasPrefix(name, ".") {
			return filepath.SkipDir
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !matcher.IsIgnoredShallow(rel, true) {
			return nil
		}

		// Ignored directory: bounded probe, then prune the walk.
		decision := matcher.Explain(rel, true)
		if decision.Rule == nil || decision.Rule.Layer != ignore.LayerGitignore {
			return filepath.SkipDir
		}
		if cand, ok := probeNestedRepo(root, rel, decision.Rule); ok {
			candidates = append(candidates, cand)
		} else {
			// One child level: a gitignored container like modules/ may hold
			// the real repos one level down.
			entries, readErr := os.ReadDir(path)
			if readErr == nil {
				for _, entry := range entries {
					if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
						continue
					}
					childRel := rel + "/" + entry.Name()
					if child, ok := probeNestedRepo(root, childRel, decision.Rule); ok {
						candidates = append(candidates, child)
					}
				}
			}
		}
		return filepath.SkipDir
	})

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Rel < candidates[j].Rel })
	return candidates
}

// probeNestedRepo inspects only the top level of rel for repo/language markers.
func probeNestedRepo(root, rel string, rule *ignore.RuleRef) (IgnoredRepoCandidate, bool) {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return IgnoredRepoCandidate{}, false
	}
	cand := IgnoredRepoCandidate{Rel: rel, Rule: rule}
	for _, entry := range entries {
		name := entry.Name()
		if name == ".git" {
			cand.HasGit = true
			continue
		}
		lower := strings.ToLower(name)
		for _, marker := range nestedRepoMarkerNames {
			if lower == marker {
				cand.Markers = append(cand.Markers, name)
			}
		}
		for _, suffix := range nestedRepoMarkerSuffixes {
			if strings.HasSuffix(lower, suffix) {
				cand.Markers = append(cand.Markers, name)
			}
		}
	}
	if !cand.HasGit && len(cand.Markers) == 0 {
		return IgnoredRepoCandidate{}, false
	}
	sort.Strings(cand.Markers)
	return cand, true
}
