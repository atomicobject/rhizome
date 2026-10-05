package ignore

import (
	"path"
	"strings"

	gitignore "github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// boundaryPathFor reports the include-boundary path declared by a pattern, or
// "" when the pattern is not a boundary. A boundary is a literal dir-only
// negation such as `!/app/`, `!app/`, or `!modules/app/`: it declares "index
// this subtree even though earlier rules exclude it" rather than acting as an
// ordinary last-match-wins negation. Glob negations (e.g. `!app/**`) keep
// plain negation semantics.
func boundaryPathFor(normalized string, dirOnly bool, domain []string) string {
	if !dirOnly || !strings.HasPrefix(normalized, "!") {
		return ""
	}
	p := strings.TrimRight(strings.TrimPrefix(normalized, "!"), " ")
	p = strings.Trim(p, "/")
	if p == "" || strings.ContainsAny(p, "*?[]") {
		return ""
	}
	if len(domain) > 0 {
		p = path.Join(strings.Join(domain, "/"), p)
	}
	return p
}

// applyBoundaries resolves include boundaries for paths evaluated under key
// (a root-relative directory). Boundary patterns are always removed from
// plain evaluation. For each boundary containing key, every earlier-ordered
// rule that excluded the boundary directory itself is suppressed, so the
// subtree is re-included while rules that target paths inside the subtree
// (its own .gitignore, later rhizome rules, config excludes, and defaults
// that do not match the boundary) keep applying.
//
// includeAncestors additionally applies suppression when key is a proper
// ancestor of a boundary. That is used only when evaluating the ancestor
// directory itself, so the walk can descend toward a nested boundary like
// `!/modules/app/` without re-including the ancestor's sibling content.
func applyBoundaries(ordered []patternWithMeta, key string, includeAncestors bool) []patternWithMeta {
	hasBoundary := false
	for _, p := range ordered {
		if p.boundaryPath != "" {
			hasBoundary = true
			break
		}
	}
	if !hasBoundary {
		return ordered
	}

	key = normalizeRel(key)
	suppressed := make([]bool, len(ordered))
	for i, p := range ordered {
		if p.boundaryPath == "" {
			continue
		}
		suppressed[i] = true
		applicable := pathWithin(key, p.boundaryPath) ||
			(includeAncestors && strings.HasPrefix(p.boundaryPath, key+"/"))
		if !applicable {
			continue
		}
		segments := strings.Split(p.boundaryPath, "/")
		for j := 0; j < i; j++ {
			if suppressed[j] || ordered[j].boundaryPath != "" {
				continue
			}
			if ordered[j].pattern.Match(segments, true) == gitignore.Exclude {
				suppressed[j] = true
			}
		}
	}

	out := make([]patternWithMeta, 0, len(ordered))
	for i, p := range ordered {
		if !suppressed[i] {
			out = append(out, p)
		}
	}
	return out
}

// withoutBoundaryPatterns strips boundary declarations without applying their
// suppression; used to compute what the verdict would have been so Explain can
// attribute a re-inclusion to its boundary.
func withoutBoundaryPatterns(ordered []patternWithMeta) []patternWithMeta {
	out := make([]patternWithMeta, 0, len(ordered))
	for _, p := range ordered {
		if p.boundaryPath == "" {
			out = append(out, p)
		}
	}
	return out
}

func pathWithin(key, boundary string) bool {
	if key == boundary {
		return true
	}
	return strings.HasPrefix(key, boundary+"/")
}
