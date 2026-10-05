package ignore

import (
	"path"
	"strings"

	gitignore "github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// Layer names reported by Explain.
const (
	LayerDefault   = "default"
	LayerGitignore = "gitignore"
	LayerRhizome   = "rhizome"
	LayerConfig    = "config"
)

// RuleRef identifies a single ignore rule for diagnostics.
type RuleRef struct {
	Layer   string // default | gitignore | rhizome | config
	Source  string // root-relative source file ("" for builtin defaults / config excludes)
	Line    int    // 1-based line in Source (0 when synthetic)
	Pattern string // original pattern text, including any leading '!'
}

// Decision explains whether a path would be indexed and which rule decided.
type Decision struct {
	Path    string
	Ignored bool
	// Rule is the deciding rule; nil when no rule matched (included by default).
	Rule *RuleRef
	// IgnoredAncestor names the closest-to-root ancestor directory whose
	// exclusion decided the verdict, when the path itself did not match.
	IgnoredAncestor string
	// Boundary names the include boundary that re-included the path when an
	// earlier rule would otherwise have excluded it.
	Boundary *RuleRef
}

// Explain evaluates relPath against the full layer model and reports the
// deciding rule. It is pure pattern evaluation: the path does not need to
// exist on disk.
func (m *Matcher) Explain(relPath string, isDir bool) Decision {
	relPath = normalizeRel(relPath)
	d := Decision{Path: relPath}
	if m == nil || relPath == "" || relPath == "." {
		return d
	}

	// Walk ancestors root-down so attribution points at the first directory
	// where exclusion begins.
	for _, ancestor := range pathAncestors(relPath) {
		if rule, res := m.decideDirect(ancestor, true); res == gitignore.Exclude {
			d.Ignored = true
			d.Rule = rule
			d.IgnoredAncestor = ancestor
			return d
		}
	}

	rule, res := m.decideDirect(relPath, isDir)
	if res == gitignore.Exclude {
		d.Ignored = true
		d.Rule = rule
		return d
	}

	// Included. If suppression changed the outcome, attribute the rescue to
	// the governing boundary.
	if boundary := m.rescuedBy(relPath, isDir); boundary != nil {
		d.Boundary = boundary
	}
	return d
}

// decideDirect returns the deciding rule for the path itself (no ancestors).
func (m *Matcher) decideDirect(relPath string, isDir bool) (*RuleRef, gitignore.MatchResult) {
	asm := m.assembledFor(relPath, isDir)
	if asm == nil {
		return nil, gitignore.NoMatch
	}
	idx, res := decide(asm.patterns, strings.Split(relPath, "/"), isDir)
	if idx < 0 {
		return nil, gitignore.NoMatch
	}
	return ruleRefFor(asm.patterns[idx]), res
}

// rescuedBy reports the boundary responsible for including a path that would
// have been excluded without boundary suppression.
func (m *Matcher) rescuedBy(relPath string, isDir bool) *RuleRef {
	check := func(p string, dir bool) *RuleRef {
		asm := m.assembledFor(p, dir)
		if asm == nil || len(asm.boundaries) == 0 {
			return nil
		}
		_, res := decide(asm.unsuppressed, strings.Split(p, "/"), dir)
		if res != gitignore.Exclude {
			return nil
		}
		// The deepest governing boundary declared last wins attribution.
		return ruleRefFor(asm.boundaries[len(asm.boundaries)-1])
	}
	for _, ancestor := range pathAncestors(relPath) {
		if ref := check(ancestor, true); ref != nil {
			return ref
		}
	}
	return check(relPath, isDir)
}

func ruleRefFor(p patternWithMeta) *RuleRef {
	return &RuleRef{
		Layer:   p.source.layer,
		Source:  p.source.file,
		Line:    p.source.line,
		Pattern: p.source.raw,
	}
}

// pathAncestors returns the ancestor directories of relPath ordered from the
// root down, excluding relPath itself.
func pathAncestors(relPath string) []string {
	dir := path.Dir(relPath)
	if dir == "." || dir == "/" || dir == "" {
		return nil
	}
	parts := strings.Split(dir, "/")
	out := make([]string, 0, len(parts))
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
