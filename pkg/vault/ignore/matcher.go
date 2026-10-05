package ignore

import (
	"path"
	"path/filepath"
	"strings"
	"sync"
)

// Matcher implements gitignore-style matching (including negations) over
// vault-relative paths.
//
// Rules are evaluated in-order; the last matching rule wins. If any parent
// directory is ignored, children are treated as ignored too. Literal dir-only
// negations (e.g. `!/app/`) act as include boundaries: they suppress earlier
// rules that excluded the boundary directory so a gitignored subtree can be
// re-included while its own nested .gitignore files keep applying (SPEC-0064).
type Matcher struct {
	root            string
	defaultPatterns []patternWithMeta
	rhizomePatterns []patternWithMeta
	userPatterns    []patternWithMeta
	// allPatterns holds the full ordered list for root-less matchers built
	// from a single flat pattern list (NewMatcher).
	allPatterns []patternWithMeta
	mu          sync.RWMutex
	dirPatterns map[string][]patternWithMeta
	assembled   map[string]*assembled
}

// assembled is the per-directory evaluation state: nested .gitignore files
// change the active pattern domain and include boundaries change suppression,
// so compiled matchers are cached per containing directory.
type assembled struct {
	// patterns is the boundary-suppressed, boundary-stripped ordered rule list.
	patterns []patternWithMeta
	compiled compiledMatcher
	// unsuppressed is the same ordered list with boundary declarations
	// stripped but no suppression applied; Explain uses it to attribute
	// re-inclusions to their boundary.
	unsuppressed []patternWithMeta
	// boundaries lists the boundary declarations governing this directory.
	boundaries []patternWithMeta
}

// NewMatcher compiles patterns expressed in gitignore-like syntax.
// Lines may include comments (#...) and negations (!...).
func NewMatcher(lines []string) *Matcher {
	patterns := parsePatterns(lines, nil)
	return NewMatcherFromPatterns(patterns)
}

func NewMatcherFromPatterns(patterns []patternWithMeta) *Matcher {
	if len(patterns) == 0 {
		return &Matcher{}
	}
	return &Matcher{
		allPatterns: patterns,
		dirPatterns: make(map[string][]patternWithMeta),
		assembled:   make(map[string]*assembled),
	}
}

func NewMatcherWithRoot(root string, defaults, rhizome, user []patternWithMeta) *Matcher {
	m := &Matcher{
		root:            root,
		defaultPatterns: defaults,
		rhizomePatterns: rhizome,
		userPatterns:    user,
		dirPatterns:     make(map[string][]patternWithMeta),
		assembled:       make(map[string]*assembled),
	}
	if strings.TrimSpace(root) == "" {
		patterns := append([]patternWithMeta{}, defaults...)
		patterns = append(patterns, rhizome...)
		patterns = append(patterns, user...)
		m.allPatterns = patterns
	}
	return m
}

// IsIgnored reports whether relPath should be skipped.
// relPath must be relative to the matcher root and use either OS separators or '/'.
func (m *Matcher) IsIgnored(relPath string, isDir bool) bool {
	if m == nil {
		return false
	}
	relPath = normalizeRel(relPath)
	if relPath == "" || relPath == "." {
		return false
	}

	// If any ancestor directory is ignored, the path is effectively ignored.
	// This is required for glob-based discovery which doesn't naturally prune
	// ignored directories during traversal.
	dir := path.Dir(relPath)
	for dir != "." && dir != "/" && dir != "" {
		if m.match(dir, true) {
			return true
		}
		dir = path.Dir(dir)
	}
	return m.match(relPath, isDir)
}

// IsIgnoredShallow reports whether relPath matches an ignore rule without checking
// ancestors. This is safe (and faster) for top-down directory walks that already
// prune ignored directories (e.g., filepath.WalkDir with SkipDir).
func (m *Matcher) IsIgnoredShallow(relPath string, isDir bool) bool {
	if m == nil {
		return false
	}
	relPath = normalizeRel(relPath)
	if relPath == "" || relPath == "." {
		return false
	}
	return m.match(relPath, isDir)
}

func (m *Matcher) match(relPath string, isDir bool) bool {
	relPath = normalizeRel(relPath)
	if relPath == "" || relPath == "." {
		return false
	}
	asm := m.assembledFor(relPath, isDir)
	if asm == nil || asm.compiled == nil {
		return false
	}
	return asm.compiled.Match(strings.Split(relPath, "/"), isDir)
}

// isBoundaryAncestor reports whether relPath is a proper ancestor of any
// declared include boundary, meaning the walk must stay able to descend
// through it even when the directory itself matches an exclude rule.
func (m *Matcher) isBoundaryAncestor(relPath string) bool {
	for _, p := range m.staticBoundaries() {
		if strings.HasPrefix(p.boundaryPath, relPath+"/") {
			return true
		}
	}
	return false
}

// staticBoundaries lists boundary declarations from the statically known
// layers (flat list, rhizome, config). Boundaries inside lazily loaded
// .gitignore files are not consulted for ancestor traversal.
func (m *Matcher) staticBoundaries() []patternWithMeta {
	var out []patternWithMeta
	scan := func(patterns []patternWithMeta) {
		for _, p := range patterns {
			if p.boundaryPath != "" {
				out = append(out, p)
			}
		}
	}
	if m.allPatterns != nil {
		scan(m.allPatterns)
		return out
	}
	scan(m.rhizomePatterns)
	scan(m.userPatterns)
	return out
}

func normalizeRel(p string) string {
	p = filepath.ToSlash(p)
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	return p
}

// keyFor returns the containing-directory cache key for a path.
func keyFor(relPath string, isDir bool) string {
	relPath = normalizeRel(relPath)
	dir := relPath
	if !isDir {
		dir = path.Dir(relPath)
	}
	if dir == "." || dir == "/" {
		dir = ""
	}
	return dir
}

func (m *Matcher) assembledFor(relPath string, isDir bool) *assembled {
	if m == nil {
		return nil
	}
	hasLayers := m.allPatterns != nil || strings.TrimSpace(m.root) != ""
	if !hasLayers {
		return nil
	}
	key := keyFor(relPath, isDir)
	// Ancestors of a nested boundary stay traversable: when evaluating such a
	// directory itself, suppression extends to boundaries below it.
	ancestorMode := isDir && normalizeRel(relPath) == key && m.isBoundaryAncestor(key)
	cacheKey := key
	if ancestorMode {
		cacheKey = key + "\x00ancestor"
	}

	m.mu.RLock()
	if cached, ok := m.assembled[cacheKey]; ok {
		m.mu.RUnlock()
		return cached
	}
	m.mu.RUnlock()

	ordered := m.orderedFor(key)
	suppressedList := applyBoundaries(ordered, key, ancestorMode)
	asm := &assembled{
		patterns:     suppressedList,
		compiled:     newPatternMatcher(suppressedList),
		unsuppressed: withoutBoundaryPatterns(ordered),
	}
	for _, p := range ordered {
		if p.boundaryPath == "" {
			continue
		}
		if pathWithin(key, p.boundaryPath) || (ancestorMode && strings.HasPrefix(p.boundaryPath, key+"/")) {
			asm.boundaries = append(asm.boundaries, p)
		}
	}

	m.mu.Lock()
	m.assembled[cacheKey] = asm
	m.mu.Unlock()
	return asm
}

// orderedFor assembles the full ordered rule list that applies to paths inside
// dir: defaults, then root-to-leaf .gitignore files, then .rhizome/ignore,
// then config excludes.
func (m *Matcher) orderedFor(dir string) []patternWithMeta {
	if m.allPatterns != nil {
		return m.allPatterns
	}

	patterns := make([]patternWithMeta, 0, len(m.defaultPatterns)+len(m.rhizomePatterns)+len(m.userPatterns)+8)
	patterns = append(patterns, m.defaultPatterns...)

	var gitPatterns []patternWithMeta
	for _, ancestor := range dirAncestors(dir) {
		context := make([]patternWithMeta, 0, len(patterns)+len(gitPatterns)+len(m.rhizomePatterns)+len(m.userPatterns))
		context = append(context, m.defaultPatterns...)
		context = append(context, gitPatterns...)
		context = append(context, m.rhizomePatterns...)
		context = append(context, m.userPatterns...)
		if !shouldLoadGitignore(ancestor, context) {
			continue
		}
		// Load only ancestors relevant to this path. Directory walking calls the
		// shallow matcher after pruning, so this lazy read avoids scanning every
		// .gitignore in large repos up front.
		loaded := m.gitignorePatternsForDir(ancestor)
		patterns = append(patterns, loaded...)
		gitPatterns = append(gitPatterns, loaded...)
	}
	patterns = append(patterns, m.rhizomePatterns...)
	patterns = append(patterns, m.userPatterns...)
	return patterns
}

func (m *Matcher) gitignorePatternsForDir(relDir string) []patternWithMeta {
	relDir = strings.Trim(relDir, "/")
	if strings.TrimSpace(m.root) == "" || shouldSkipGitignoreDir(relDir) {
		return nil
	}

	m.mu.RLock()
	if cached, ok := m.dirPatterns[relDir]; ok {
		m.mu.RUnlock()
		return cached
	}
	m.mu.RUnlock()

	dirPath := m.root
	if relDir != "" {
		dirPath = filepath.Join(m.root, filepath.FromSlash(relDir))
	}
	sourceFile := ".gitignore"
	if relDir != "" {
		sourceFile = relDir + "/.gitignore"
	}
	lines := readIgnoreLines(filepath.Join(dirPath, ".gitignore"))
	var patterns []patternWithMeta
	if len(lines) > 0 {
		patterns = parsePatternsWithSource(lines, splitDomain(relDir), LayerGitignore, sourceFile)
	}

	m.mu.Lock()
	m.dirPatterns[relDir] = patterns
	m.mu.Unlock()
	return patterns
}

func splitDomain(rel string) []string {
	rel = strings.Trim(rel, "/")
	if rel == "" || rel == "." {
		return nil
	}
	return strings.Split(rel, "/")
}

func dirAncestors(dir string) []string {
	dir = strings.Trim(dir, "/")
	if dir == "" {
		return []string{""}
	}
	parts := strings.Split(dir, "/")
	out := make([]string, 0, len(parts)+1)
	out = append(out, "")
	current := ""
	for _, part := range parts {
		if current == "" {
			current = part
		} else {
			current = current + "/" + part
		}
		out = append(out, current)
	}
	return out
}

func shouldSkipGitignoreDir(relDir string) bool {
	if relDir == "" {
		return false
	}
	parts := strings.Split(relDir, "/")
	for _, part := range parts {
		if part == ".git" || part == ".rhizome" {
			return true
		}
	}
	return false
}

// shouldLoadGitignore reports whether relDir's own .gitignore should join the
// pattern domain. A directory's .gitignore applies only when the directory
// itself is included after evaluating the rules known so far — including
// rhizome/user layers and include-boundary suppression, so a re-included
// subtree contributes its own .gitignore (SPEC-0064).
func shouldLoadGitignore(relDir string, patterns []patternWithMeta) bool {
	if relDir == "" {
		return true
	}
	if len(patterns) == 0 {
		return true
	}
	matcher := newPatternMatcher(applyBoundaries(patterns, relDir, true))
	if matcher == nil {
		return true
	}
	return !matcher.Match(strings.Split(relDir, "/"), true)
}
