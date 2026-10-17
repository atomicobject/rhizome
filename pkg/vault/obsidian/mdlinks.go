package obsidian

import (
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/paths"
	"path/filepath"
	"strings"
)

// ResolveMdLink resolves a markdown link target to an actual note path.
// It handles relative paths and anchors.
func (c *NotePathCache) ResolveMdLink(link string, fromNote string) (string, bool) {
	resolved, ok := c.ResolveMdLinkTarget(link, fromNote)
	if !ok {
		return "", false
	}
	return resolved.Path, true
}

// ResolveMdLinkTarget returns the resolved note path plus any trailing fragment.
func (c *NotePathCache) ResolveMdLinkTarget(link string, fromNote string) (ResolvedNoteTarget, bool) {
	candidates := c.ResolveMdLinkCandidates(link, fromNote)
	if len(candidates) != 1 {
		return ResolvedNoteTarget{}, false
	}
	return candidates[0], true
}

// ResolveMdLinkCandidates returns canonical Markdown targets, preserving ambiguity
// and rejecting invalid authored paths before any basename or alias fallback.
func (c *NotePathCache) ResolveMdLinkCandidates(link string, fromNote string) []ResolvedNoteTarget {
	target, fragment := markdownsyntax.SplitMarkdownTarget(link)

	// If empty after removing anchor, it's a same-file anchor link
	if target == "" {
		return []ResolvedNoteTarget{{Path: fromNote, Fragment: fragment}}
	}
	target = strings.TrimSpace(target)
	// Classify the decoded authored spelling before cleaning can erase a URI
	// authority or drive segment. Colon paths have only cached exact authority.
	if len(target)-len(strings.TrimLeft(target, "/\\")) > 1 {
		return nil
	}
	authoredPath := strings.TrimPrefix(target, "/")
	firstSeparator := strings.IndexAny(authoredPath, ":/\\")
	exactOnly := firstSeparator >= 0 && authoredPath[firstSeparator] == ':'
	authoredTarget := paths.Normalize(authoredPath).String()
	if exactOnly && authoredTarget != authoredPath {
		return nil
	}

	// Handle relative paths
	if !strings.HasPrefix(target, "/") && fromNote != "" {
		dir := filepath.Dir(fromNote)
		if dir != "." {
			target = filepath.Join(dir, target)
		}
	}

	// Normalize the path
	target = paths.Normalize(strings.TrimSpace(strings.TrimPrefix(target, "/"))).String()

	if _, err := paths.CleanRelPath(target); err != nil {
		firstSeparator := strings.IndexAny(target, ":/\\")
		if firstSeparator < 0 || target[firstSeparator] != ':' {
			return nil
		}
		exactOnly = true
	}

	// An authored Markdown path is stronger evidence than a basename. Keep
	// root-relative targets such as README.md resolvable even when many nested
	// notes share that basename.
	if canonical, ok := c.ResolveExplicitPath(target); ok {
		return []ResolvedNoteTarget{{Path: canonical, Fragment: fragment}}
	}
	if exactOnly {
		if c == nil {
			return nil
		}
		if candidates := c.explicitPathCandidates[paths.LinkKey(target)]; len(candidates) > 0 {
			return c.targets(exactPathCandidates(candidates, target, true), fragment)
		}
		// Cached authored colon paths permit exact extensionless lookup, never basename or alias fallback.
		var exact []string
		for _, candidate := range c.pathCandidates[paths.LinkKey(target)] {
			if paths.LinkKey(strings.TrimSuffix(candidate, filepath.Ext(candidate))) == paths.LinkKey(target) {
				exact = append(exact, candidate)
			}
		}
		return c.targets(exactPathCandidates(exact, target, false), fragment)
	}
	candidates := c.resolveNotePathCandidates(target, fragment)
	if len(candidates) == 0 && target != authoredTarget {
		candidates = c.resolveNotePathCandidates(authoredTarget, fragment)
	}
	return candidates
}
