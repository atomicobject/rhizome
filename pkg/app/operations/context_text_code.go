package operations

import (
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func renderCodeFileContextBlock(noteMgr obsidian.NoteReader, vaultDef obsidian.VaultDefinition, display string, ctx FileContext, perTargetBudget int, profile ContextProfile, tracker DedupeTracker) (string, []DedupeItem) {
	var b strings.Builder
	b.WriteString("### ")
	b.WriteString(display)
	b.WriteString(" (code)\n")

	vaultPaths, _ := paths.NewVaultPaths(vaultDef.BasePath())

	// Track rough budget while rendering (best-effort; final packing will trim).
	remaining := perTargetBudget
	nestedDedupe := renderedNoteDedupe{vaultPaths: vaultPaths, tracker: tracker, items: make([]DedupeItem, 0, len(ctx.LinkedNotes))}

	if len(ctx.AncestorDocs) > 0 {
		docFraction := 0.55
		if ctx.IsDir {
			docFraction = 0.45
		}
		remain := int(float64(perTargetBudget) * docFraction)
		if remain < 800 {
			remain = perTargetBudget
		}
		docBlocks := make([]string, 0, len(ctx.AncestorDocs))
		for _, d := range ctx.AncestorDocs {
			content, truncated := contextpack.TrimMarkdown(d.Content, min(8000, remain))
			content = strings.TrimSpace(content)
			if content == "" {
				continue
			}
			if !nestedDedupe.include(d.Path, content) {
				continue
			}
			remain -= len(content) + 80
			docBlocks = append(docBlocks, renderEmbeddedFileXML("ancestor-doc", d.Path, content, d.Truncated || truncated))
			if remain < 500 {
				break
			}
		}
		if len(docBlocks) > 0 {
			b.WriteString("\n\n#### Docs\n")
			for _, block := range docBlocks {
				b.WriteString("\n\n")
				b.WriteString(block)
			}
			remaining -= int(float64(perTargetBudget) * docFraction)
		}
	}

	// Render submodule docs (from subdirectories when SubmoduleDepth > 0)
	if len(ctx.SubmoduleDocs) > 0 {
		submoduleBudget := min(remaining, max(1000, int(float64(perTargetBudget)*0.22)))
		remain := submoduleBudget
		rendered := 0
		for _, d := range ctx.SubmoduleDocs {
			content, truncated := contextpack.TrimMarkdown(d.Content, min(2200, remain))
			content = strings.TrimSpace(content)
			if content == "" {
				continue
			}
			if !nestedDedupe.include(d.Path, content) {
				continue
			}
			if rendered == 0 {
				b.WriteString("\n\n#### Submodule docs\n")
			}
			b.WriteString("\n\n")
			b.WriteString(renderEmbeddedFileXMLWithDepth("submodule-doc", d.Path, content, d.Depth, d.Truncated || truncated))
			rendered++
			remain -= len(content) + 80
			if remain < 400 {
				break
			}
		}
		if rendered > 0 {
			remaining -= submoduleBudget - max(0, remain)
		}
	}

	if len(ctx.Rationale) > 0 {
		rationaleBudget := min(remaining, max(700, int(float64(perTargetBudget)*0.18)))
		remain := rationaleBudget
		rendered := 0
		for _, r := range ctx.Rationale {
			content := strings.TrimSpace(contextpack.TrimToBudget(r.Content, min(500, remain)))
			if content == "" {
				continue
			}
			if rendered == 0 {
				b.WriteString("\n\n#### Rationale\n")
			}
			b.WriteString("\n\n")
			b.WriteString(renderRationaleXML(ctx.Path, r, content, len(content) < len(strings.TrimSpace(r.Content))))
			rendered++
			remain -= len(content) + 80
			if remain < 180 {
				break
			}
		}
		if rendered > 0 {
			remaining -= rationaleBudget - max(0, remain)
		}
	}

	if len(ctx.OntologyNodes) > 0 || len(ctx.NodeDiagnostics) > 0 {
		b.WriteString("\n\n#### Ontology nodes\n")
		for _, node := range ctx.OntologyNodes {
			nodeBudget := min(2200, max(400, remaining/2))
			before := b.Len()
			b.WriteString("\n\n")
			b.WriteString(renderOntologyNodeContextXML(node, nodeBudget))
			remaining -= b.Len() - before
			if remaining < 400 {
				break
			}
		}
		if len(ctx.NodeDiagnostics) > 0 {
			b.WriteString("\n\n##### Node diagnostics\n")
			for _, diagnostic := range ctx.NodeDiagnostics {
				b.WriteString("\n- ")
				if diagnostic.Input != "" {
					b.WriteString(diagnostic.Input)
					b.WriteString(": ")
				}
				b.WriteString(diagnostic.Code)
				if diagnostic.Message != "" {
					b.WriteString(" — ")
					b.WriteString(diagnostic.Message)
				}
			}
		}
	}

	if len(ctx.LinkedNotes) > 0 {
		b.WriteString("\n\n#### Linked notes\n")
		notes := append([]LinkedNoteContext{}, ctx.LinkedNotes...)
		sort.SliceStable(notes, func(i, j int) bool {
			wi := linkedNoteKindWeight(notes[i].Kind)
			wj := linkedNoteKindWeight(notes[j].Kind)
			if wi != wj {
				return wi > wj
			}
			return notes[i].Path < notes[j].Path
		})
		limit := 25
		if profile == ContextProfileCode {
			limit = 18
		}
		if ctx.IsDir {
			limit = 10
		}
		if len(notes) > limit {
			notes = notes[:limit]
		}

		// Prefer embedding full note bodies. Only fall back to summaries when budget is tight.
		const minNoteBodyBudget = 800
		noteFraction := 0.40
		if ctx.IsDir {
			noteFraction = 0.25
		}
		noteBudget := min(int(float64(perTargetBudget)*noteFraction), remaining)

		type noteBlock struct {
			LinkedNoteContext
			content   string
			truncated bool
		}

		blocks := make([]noteBlock, len(notes))
		for i, note := range notes {
			blocks[i].LinkedNoteContext = note
		}

		embeddedCount := 0
		for i := range blocks {
			if noteMgr == nil || blocks[i].Path == "" {
				continue
			}
			if noteBudget < minNoteBodyBudget {
				break
			}
			raw, err := noteMgr.GetContents(vaultDef, blocks[i].Path)
			if err != nil || strings.TrimSpace(raw) == "" {
				continue
			}
			content, truncated := contextpack.TrimMarkdown(raw, min(16000, noteBudget))
			content = strings.TrimSpace(content)
			if content == "" {
				continue
			}
			if !nestedDedupe.include(blocks[i].Path, content) {
				continue
			}
			blocks[i].content = content
			blocks[i].truncated = truncated
			noteBudget -= len(content) + 120
			embeddedCount++
		}

		for _, nb := range blocks {
			if nb.content == "" {
				continue
			}
			b.WriteString("\n\n")
			b.WriteString(renderEmbeddedFileXMLWithMeta("note", nb.Path, nb.Title, nb.Kind, nb.content, nb.truncated))
		}

		// If we couldn't embed all notes, list remaining as stubs with summary.
		if embeddedCount < len(blocks) {
			b.WriteString("\n\n##### Note stubs\n")
			for _, nb := range blocks {
				if nb.content != "" {
					continue
				}
				b.WriteString("\n- ")
				b.WriteString(nb.Title)
				if nb.Path != "" {
					b.WriteString(" (")
					b.WriteString(nb.Path)
					b.WriteString(")")
				}
				if value, ok := nb.Frontmatter["summary"]; ok {
					if summary := strings.TrimSpace(fmt.Sprint(value)); summary != "" {
						b.WriteString(" — ")
						b.WriteString(summary)
					}
				}
				if nb.Kind != "" {
					b.WriteString(" [")
					b.WriteString(nb.Kind)
					b.WriteString("]")
				}
				if nb.Line > 0 {
					b.WriteString(fmt.Sprintf(":%d", nb.Line))
				}
			}
		}
	}

	if len(ctx.ReturnedNotePaths) > 0 || len(ctx.ReturnedDocPaths) > 0 {
		b.WriteString("\n\n#### Returned paths\n")
		if len(ctx.ReturnedDocPaths) > 0 {
			b.WriteString("\n- docs: ")
			b.WriteString(strings.Join(ctx.ReturnedDocPaths, ", "))
		}
		if len(ctx.ReturnedNotePaths) > 0 {
			b.WriteString("\n- notes: ")
			b.WriteString(strings.Join(ctx.ReturnedNotePaths, ", "))
		}
	}
	if isThinCodeContext(ctx) {
		b.WriteString(renderThinContextWarning(display))
	}

	return strings.TrimSpace(b.String()), nestedDedupe.items
}

func chooseVaultContextFileBudget(totalBudget int, fileCount int) int {
	if totalBudget <= 0 || fileCount <= 0 {
		return 0
	}
	available := totalBudget - 420
	if available < 2400 {
		return 0
	}
	minOverview := 2600
	if available <= minOverview {
		return 0
	}
	wanted := max(2200*fileCount, available/2)
	limit := available - minOverview
	if wanted > limit {
		wanted = limit
	}
	return max(0, wanted)
}

func stripRenderedContextHeader(text string) string {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "# Rhizome context\n") {
		lines := strings.Split(trimmed, "\n")
		if len(lines) >= 3 && lines[0] == "# Rhizome context" && lines[1] == "" {
			sawBullet := false
			for i := 2; i < len(lines); i++ {
				switch {
				case strings.HasPrefix(lines[i], "- "):
					sawBullet = true
				case lines[i] == "" && sawBullet:
					return strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
				default:
					i = len(lines)
				}
			}
		}
	}

	candidates := []string{"\n\n### ", "\n\n## "}
	idx := -1
	for _, marker := range candidates {
		markerIdx := strings.Index(trimmed, marker)
		if markerIdx >= 0 && (idx == -1 || markerIdx < idx) {
			idx = markerIdx
		}
	}
	if idx < 0 {
		return trimmed
	}
	return strings.TrimSpace(trimmed[idx+2:])
}

func linkedNoteKindWeight(kind string) int {
	switch {
	case kind == "wikilink":
		return 100
	case kind == "mention":
		return 95
	case strings.HasPrefix(kind, "anchor"):
		return 90
	case strings.HasPrefix(kind, "linkedFrom"):
		return 60
	default:
		return 10
	}
}
