package operations

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func mergeAncestorDocLinkedNotes(docs []AncestorDocContext, linked []LinkedNoteContext, noteMgr obsidian.NoteReader, cache *obsidian.NotePathCache, exclude map[string]struct{}) []LinkedNoteContext {
	if len(docs) == 0 || noteMgr == nil || cache == nil {
		return linked
	}
	linkedSet := make(map[string]struct{}, len(linked))
	for _, ln := range linked {
		if ln.Path != "" {
			linkedSet[ln.Path] = struct{}{}
		}
	}
	seen := make(map[string]struct{})
	for p := range linkedSet {
		seen[p] = struct{}{}
	}

	// Hard cap to avoid runaway expansion from very link-heavy CONTEXT docs.
	const stubLimit = 25

	for _, doc := range docs {
		if doc.Content == "" {
			continue
		}
		matches := wikilinkPattern.FindAllStringSubmatch(doc.Content, -1)
		for _, m := range matches {
			if len(m) < 2 {
				continue
			}
			target := strings.TrimSpace(m[1])
			if target == "" {
				continue
			}
			if pipe := strings.Index(target, "|"); pipe >= 0 {
				target = target[:pipe]
			}
			if hash := strings.Index(target, "#"); hash >= 0 {
				target = target[:hash]
			}
			target = strings.TrimSpace(target)
			if target == "" {
				continue
			}

			// Only wikilinks are supported here; CONTEXT.md is outside the vault,
			// so markdown link relative resolution isn't meaningful.
			resolved, ok := cache.ResolveNote(target)
			if !ok || resolved == "" {
				continue
			}
			resolved = string(paths.NormalizeNotePath(resolved))
			if _, skip := exclude[resolved]; skip {
				continue
			}
			if _, ok := seen[resolved]; ok {
				continue
			}
			seen[resolved] = struct{}{}

			fm := blessedFrontmatter(noteMgr, resolved)
			linked = append(linked, LinkedNoteContext{
				Path:        resolved,
				Title:       paths.NoteTitle(resolved),
				Kind:        "ancestorDoc",
				Frontmatter: fm,
			})
			if len(seen) >= stubLimit+len(linkedSet) {
				return linked
			}
		}
	}
	return linked
}

func collectAncestorDocs(absFilePath string, rootPaths paths.VaultPaths, patterns []string, maxEmpty, budget int) ([]AncestorDocContext, bool) {
	var docs []AncestorDocContext
	truncated := false

	startDir := filepath.Dir(absFilePath)
	root := rootPaths.Root()
	if root == "" {
		root = startDir
	}
	emptyStreak := 0

	for dir := startDir; ; {
		match, ok := findDocMatchInDir(dir, patterns, rootPaths)
		if ok {
			content, err := os.ReadFile(match.AbsPath)
			if err == nil {
				emptyStreak = 0
				docs = append(docs, AncestorDocContext{
					Dir:     match.Dir,
					Pattern: match.Pattern,
					Path:    match.Path,
					Content: string(content),
				})
			} else {
				ok = false
			}
		}
		if !ok {
			emptyStreak++
			if maxEmpty > 0 && emptyStreak >= maxEmpty {
				break
			}
		}
		if dir == root {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir || !isSubdir(parent, rootPaths) {
			break
		}
		dir = parent
	}

	// Apply proximity-first budgeting (docs collected from closest to farthest).
	// This favors nearby docs and may truncate or drop higher-level CONTEXT files
	// once the budget is exhausted.
	remaining := budget
	for i := 0; i < len(docs); i++ {
		if remaining <= 0 {
			docs = docs[:i]
			truncated = true
			break
		}
		if len(docs[i].Content) > remaining {
			content := docs[i].Content[:remaining]
			if idx := strings.LastIndex(content, "\n"); idx > 0 {
				content = content[:idx+1]
			}
			if !strings.HasSuffix(content, "\n") {
				content += "\n"
			}
			docs[i].Content = content + "[... truncated ...]"
			docs[i].Truncated = true
			truncated = true
			remaining = 0
			docs = docs[:i+1]
			break
		}
		remaining -= len(docs[i].Content)
	}
	if remaining == 0 && len(docs) > 0 {
		truncated = true
	}

	return docs, truncated
}

func isSubdir(dir string, rootPaths paths.VaultPaths) bool {
	if dir == "" || rootPaths.Root() == "" {
		return false
	}
	_, err := rootPaths.RelStrict(dir)
	return err == nil
}

// SubmoduleDocsBudget is the dedicated budget for submodule docs (25000 chars).
const SubmoduleDocsBudget = 25000

// collectSubmoduleDocs finds CONTEXT.md files in subdirectories up to maxDepth.
// Returns docs ordered by depth (shallower first) for proximity-first budgeting.
func collectSubmoduleDocs(dirPath string, rootPaths paths.VaultPaths, patterns []string, maxDepth, budget int) ([]SubmoduleDocContext, bool) {
	if maxDepth <= 0 || dirPath == "" {
		return nil, false
	}
	patterns = obsidian.NormalizeDocPatterns(patterns)
	if len(patterns) == 0 {
		return nil, false
	}

	type candidate struct {
		absPath string
		relPath string
		depth   int
	}
	var candidates []candidate

	// Walk subdirectories up to maxDepth
	_ = filepath.WalkDir(dirPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if path == dirPath {
			return nil // skip the root directory itself
		}
		if !d.IsDir() {
			return nil // only interested in directories
		}

		// Calculate depth from dirPath
		relFromStart, relErr := filepath.Rel(dirPath, path)
		if relErr != nil {
			return nil
		}
		depth := strings.Count(relFromStart, string(filepath.Separator)) + 1
		if depth > maxDepth {
			return filepath.SkipDir // don't descend further
		}

		// Check for doc match in this directory
		match, ok := findDocMatchInDir(path, patterns, rootPaths)
		if ok {
			candidates = append(candidates, candidate{
				absPath: match.AbsPath,
				relPath: match.Path,
				depth:   depth,
			})
		}
		return nil
	})

	if len(candidates) == 0 {
		return nil, false
	}

	// Sort by depth (shallower first) for proximity-first budgeting
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].depth != candidates[j].depth {
			return candidates[i].depth < candidates[j].depth
		}
		return candidates[i].relPath < candidates[j].relPath
	})

	// Read content and apply budget
	var docs []SubmoduleDocContext
	truncated := false
	remaining := budget

	for _, c := range candidates {
		if remaining <= 0 {
			truncated = true
			break
		}

		content, err := os.ReadFile(c.absPath)
		if err != nil {
			continue
		}

		doc := SubmoduleDocContext{
			Path:  c.relPath,
			Depth: c.depth,
		}

		if len(content) > remaining {
			// Truncate at last newline
			truncContent := string(content[:remaining])
			if idx := strings.LastIndex(truncContent, "\n"); idx > 0 {
				truncContent = truncContent[:idx+1]
			}
			if !strings.HasSuffix(truncContent, "\n") {
				truncContent += "\n"
			}
			doc.Content = truncContent + "[... truncated ...]"
			doc.Truncated = true
			truncated = true
			remaining = 0
		} else {
			doc.Content = string(content)
			remaining -= len(content)
		}

		docs = append(docs, doc)

		if remaining <= 0 {
			break
		}
	}

	return docs, truncated
}

func toRelSet(items []string, vaultPaths paths.VaultPaths, note bool) map[string]struct{} {
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(items))
	for _, s := range items {
		if strings.TrimSpace(s) == "" {
			continue
		}
		clean := filepath.ToSlash(filepath.Clean(s))
		if vaultPaths.Root() != "" && filepath.IsAbs(s) {
			if note {
				if rel, err := vaultPaths.RelNotePathStrict(s); err == nil && rel.String() != "" {
					out[rel.String()] = struct{}{}
					continue
				}
			} else {
				if rel, err := vaultPaths.RelStrict(s); err == nil && rel.String() != "" {
					out[rel.String()] = struct{}{}
					continue
				}
			}
		}
		if note {
			clean = string(paths.NormalizeNotePath(clean))
		} else {
			clean = string(paths.Normalize(clean))
		}
		if clean != "" {
			out[clean] = struct{}{}
		}
	}
	return out
}

func uniqueNotePaths(notes []LinkedNoteContext) []string {
	if len(notes) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(notes))
	out := make([]string, 0, len(notes))
	for _, n := range notes {
		if n.Path == "" {
			continue
		}
		if _, ok := set[n.Path]; ok {
			continue
		}
		set[n.Path] = struct{}{}
		out = append(out, n.Path)
	}
	return out
}

func uniqueDocPaths(docs []AncestorDocContext) []string {
	if len(docs) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(docs))
	out := make([]string, 0, len(docs))
	for _, d := range docs {
		if d.Path == "" {
			continue
		}
		if _, ok := set[d.Path]; ok {
			continue
		}
		set[d.Path] = struct{}{}
		out = append(out, d.Path)
	}
	return out
}
