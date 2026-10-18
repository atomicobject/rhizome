package operations

import (
	"path/filepath"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/frontmatter"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type docNote struct {
	Path string
}

func dedupeDocs(links []codeanchor.DocLink) map[string]docNote {
	out := map[string]docNote{}
	for _, l := range links {
		if l.DstPath == "" {
			continue
		}
		if _, exists := out[l.DstPath]; exists {
			continue
		}
		out[l.DstPath] = docNote{Path: l.DstPath}
	}
	return out
}

// moduleDocContextBody renders projected note evidence without inferring
// syntax from content. Markdown retains its legacy formatting through the
// exact FormatID-gated adapter; every other provider supplies search regions.
func moduleDocContextBody(fact NoteFact, maxChars int) string {
	if body, handled := markdownContextBodyCompat(fact, maxChars); handled {
		return body
	}
	return searchRegionContextBody(fact.SearchRegions, maxChars)
}

// markdownContextBodyCompat is the named Markdown-only compatibility adapter
// for legacy module-doc rendering. Foreign formats are left unhandled.
func markdownContextBodyCompat(fact NoteFact, maxChars int) (string, bool) {
	if fact.Format != noteformat.FormatID("markdown") {
		return "", false
	}
	content := fact.Content
	if maxChars <= 0 {
		return "", true
	}
	if strings.TrimSpace(content) == "" {
		return "", true
	}

	if _, bodyStart, ok := markdownsyntax.Frontmatter(content); ok {
		content = strings.TrimSpace(content[bodyStart:])
	}

	// Strip leading markdown title
	if strings.HasPrefix(content, "#") {
		lines := strings.Split(content, "\n")
		if len(lines) > 1 {
			content = strings.TrimSpace(strings.Join(lines[1:], "\n"))
		}
	}

	// Trim to maxChars, preferring paragraph break
	if len(content) > maxChars {
		trimmed := content[:maxChars]
		// Try to break at paragraph boundary (double newline)
		if idx := strings.LastIndex(trimmed, "\n\n"); idx > 0 {
			trimmed = trimmed[:idx]
		} else if idx := strings.LastIndex(trimmed, "\n"); idx > maxChars/2 {
			// Fallback to last line break within reasonable distance
			trimmed = trimmed[:idx]
		}
		trimmed = strings.TrimSpace(trimmed)
		// Add ellipsis if we actually trimmed
		if len(trimmed) < len(content) && trimmed != "" {
			trimmed += "…"
		}
		return trimmed, true
	}
	return strings.TrimSpace(content), true
}

func searchRegionContextBody(regions []noteformat.SearchRegionFact, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	visible := make([]string, 0, len(regions))
	for _, region := range regions {
		if region.Kind != noteformat.SearchRegionVisible || strings.TrimSpace(region.Text) == "" {
			continue
		}
		visible = append(visible, strings.TrimSpace(region.Text))
	}
	return truncateContextBody(strings.Join(visible, "\n\n"), maxChars)
}

func truncateContextBody(content string, maxChars int) string {
	content = strings.TrimSpace(content)
	if len(content) <= maxChars {
		return content
	}
	trimmed := content[:maxChars]
	if idx := strings.LastIndex(trimmed, "\n\n"); idx > 0 {
		trimmed = trimmed[:idx]
	} else if idx := strings.LastIndex(trimmed, "\n"); idx > maxChars/2 {
		trimmed = trimmed[:idx]
	}
	trimmed = strings.TrimSpace(trimmed)
	if trimmed != "" {
		return trimmed + "…"
	}
	return ""
}

func primaryDocPattern(patterns []string) string {
	patterns = obsidian.NormalizeDocPatterns(patterns)
	if len(patterns) == 0 {
		return ""
	}
	return patterns[0]
}

func buildModuleDocs(notes obsidian.NoteReader, modulePath string, docs map[string]docNote, vaultPaths paths.VaultPaths, docPatterns []string, primaryPattern string, maxEmptyLevels int) []ModuleDoc {
	if len(docs) == 0 {
		docs = map[string]docNote{}
	}
	docPatterns = obsidian.NormalizeDocPatterns(docPatterns)
	if vaultPaths.Root() != "" {
		absDir := filepath.Join(vaultPaths.Root(), filepath.FromSlash(modulePath))
		for _, match := range collectAncestorDocMatches(absDir, vaultPaths, docPatterns, maxEmptyLevels) {
			if _, exists := docs[match.Path]; !exists {
				docs[match.Path] = docNote{Path: match.Path}
			}
		}
	}
	var out []ModuleDoc
	for path := range docs {
		// Normalize path separators for consistent handling across platforms
		path = filepath.ToSlash(path)
		title := filepath.Base(path)
		summary := ""
		body := ""
		if fact, ok := NoteFactsFromReader(notes).LookupFact(path); ok {
			blessed := frontmatter.FilterBlessed(fact.Frontmatter)
			summary = firstSummary(blessed)
			// Extract body content for primary module docs.
			if primaryPattern != "" && obsidian.MatchDocPatternNormalized(path, []string{primaryPattern}) {
				body = moduleDocContextBody(fact, 300)
			}
		}
		out = append(out, ModuleDoc{
			Path:    path,
			Title:   strings.TrimSuffix(title, filepath.Ext(title)),
			Summary: summary,
			Body:    body,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Summary != out[j].Summary {
			return out[i].Summary > out[j].Summary
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func firstSummary(fm map[string]interface{}) string {
	if len(fm) == 0 {
		return ""
	}
	for _, key := range frontmatter.BlessedKeys {
		v, ok := fm[strings.ToLower(key)]
		if !ok {
			continue
		}
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
