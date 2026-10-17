package operations

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// renderCodeOverviewMarkdownHierarchical renders modules with header levels based on depth.
// Depth 1 = ##, Depth 2 = ###, Depth 3+ = ####
func renderCodeOverviewMarkdownHierarchical(modules []CodeOverviewModule, budget int) (string, bool) {
	var b strings.Builder
	b.WriteString("# Code Overview\n")
	truncated := false

	for i, m := range modules {
		section := renderModuleHierarchical(m)
		if budget > 0 && b.Len()+len(section) > budget {
			truncated = true
			break
		}
		if i == 0 {
			b.WriteString(section)
		} else {
			b.WriteString("\n")
			b.WriteString(section)
		}
	}
	if truncated {
		b.WriteString("\n\n(truncated; increase --budget-chars or --limit-modules to see more)\n")
	}
	return b.String(), truncated
}

// renderModuleHierarchical renders a module with header level based on its depth.
func renderModuleHierarchical(m CodeOverviewModule) string {
	var b strings.Builder

	// Determine header level: depth 1 = ##, depth 2 = ###, depth 3+ = ####
	headerLevel := m.Depth + 1
	if headerLevel < 2 {
		headerLevel = 2
	}
	if headerLevel > 4 {
		headerLevel = 4
	}
	header := strings.Repeat("#", headerLevel)

	b.WriteString(header)
	b.WriteString(" ")
	if m.Path == "" || m.Path == "." {
		b.WriteString("(root)")
	} else {
		b.WriteString(m.Path)
		if !strings.HasSuffix(m.Path, "/") {
			b.WriteString("/")
		}
	}
	b.WriteString("\n")

	// Show primary module doc body if available - prefer the one in this module's directory
	contextBody := ""
	primaryPattern := m.PrimaryDocPattern
	if primaryPattern == "" {
		primaryPattern = primaryDocPattern(nil)
	}
	moduleDirPrimary := ""
	if primaryPattern != "" {
		moduleDirPrimary = filepath.ToSlash(filepath.Join(m.Path, primaryPattern))
	}
	for _, d := range m.Docs {
		if primaryPattern != "" && obsidian.MatchDocPatternNormalized(d.Path, []string{primaryPattern}) && d.Body != "" {
			// Only show body if the primary doc is directly in this module's directory
			// (not an ancestor doc). This avoids repeating root CONTEXT.md in every module.
			if d.Path == moduleDirPrimary || filepath.Dir(d.Path) == m.Path {
				contextBody = d.Body
				break
			}
			// No fallback to ancestor docs - that causes repetition of root intro text
		}
	}
	if contextBody != "" {
		b.WriteString(contextBody)
		b.WriteString("\n\n")
	} else if m.ContextSummary != "" {
		// Fall back to summary if no body available
		b.WriteString(m.ContextSummary)
		b.WriteString("\n\n")
	}

	// Stats line: files, docs, anchors
	stats := fmt.Sprintf("_files %d | docs %d | anchors %d_", m.FileCount, m.Stats.DocMentions+m.Stats.DocLinks, m.Stats.Anchors)
	if len(m.Tests) > 0 {
		stats = fmt.Sprintf("%s | tests %d", stats, len(m.Tests))
	}
	b.WriteString(stats)
	b.WriteString("\n")

	// Key exports (top anchors)
	if len(m.Anchors) > 0 {
		b.WriteString("- Key exports: ")
		for i, a := range m.Anchors {
			if i > 0 {
				b.WriteString("; ")
			}
			b.WriteString(a.Name)
			if a.Signature != "" {
				sig := a.Signature
				if len(sig) > 60 {
					sig = sig[:57] + "…"
				}
				b.WriteString(sig)
			}
			if a.Kind != "" && a.Kind != "module" {
				b.WriteString(" (")
				b.WriteString(a.Kind)
				b.WriteString(")")
			}
		}
		b.WriteString("\n")
	}

	// Submodules
	if len(m.Submodules) > 0 {
		b.WriteString("- Submodules: ")
		for i, sub := range m.Submodules {
			if i > 0 {
				b.WriteString("; ")
			}
			base := filepath.Base(sub)
			if base == "" {
				base = sub
			}
			b.WriteString(base)
		}
		b.WriteString("\n")
	}

	// Docs (excluding the primary module doc which is already shown)
	if len(m.Docs) > 0 {
		nonContextDocs := make([]ModuleDoc, 0)
		for _, d := range m.Docs {
			if primaryPattern == "" || !obsidian.MatchDocPatternNormalized(d.Path, []string{primaryPattern}) {
				nonContextDocs = append(nonContextDocs, d)
			}
		}
		if len(nonContextDocs) > 0 {
			b.WriteString("- Docs: ")
			for i, d := range nonContextDocs {
				if i > 0 {
					b.WriteString("; ")
				}
				b.WriteString(d.Title)
				if d.Summary != "" {
					b.WriteString(" — ")
					summary := d.Summary
					if len(summary) > 100 {
						summary = summary[:97] + "…"
					}
					b.WriteString(summary)
				}
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}
