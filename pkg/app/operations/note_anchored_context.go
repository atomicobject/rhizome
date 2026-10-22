package operations

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// notePathAnchorKinds are the only anchor kinds that can bind a note to another
// note. Symbol, call, and annotation anchors need indexed code facts, which a
// note never has.
var notePathAnchorKinds = []codeanchor.AnchorKind{codeanchor.AnchorPath, codeanchor.AnchorGlob}

// anchoredNotesForNote returns the notes whose path or glob code anchors match
// the requested note, through the same anchor merge that code-file context uses,
// so attribution stays "anchor:<label>". The requested note itself and any path
// in skip are omitted.
func anchoredNotesForNote(ctx context.Context, svc *codeanchor.Service, absPath, notePath string, vaultDef obsidian.VaultDefinition, vaultPaths paths.VaultPaths, requestedKinds []codeanchor.AnchorKind, skipRefresh bool, skip map[string]struct{}) []LinkedNoteContext {
	if svc == nil {
		return nil
	}
	allowed := make(map[codeanchor.AnchorKind]bool, len(notePathAnchorKinds))
	for _, kind := range notePathAnchorKinds {
		if len(requestedKinds) == 0 || slices.Contains(requestedKinds, kind) {
			allowed[kind] = true
		}
	}
	if len(allowed) == 0 {
		return nil
	}
	linked, _ := mergeCodeAnchor(ctx, nil, svc, absPath, vaultDef, vaultPaths, allowed, skipRefresh)
	out := linked[:0]
	for _, note := range linked {
		if note.Path == notePath {
			continue
		}
		if _, ok := skip[note.Path]; ok {
			continue
		}
		out = append(out, note)
	}
	return out
}

// noteContextShownPaths collects the note paths whose bodies a note's context
// already embeds as ontology context, plus the caller's ExcludeNotePaths.
// Anchored notes in this set are not repeated. Top inbound and outbound
// neighbors list only paths, so an anchored note among them still gets its body
// and anchor label.
func noteContextShownPaths(embedded []DedupeItem, exclude []string, vaultPaths paths.VaultPaths) map[string]struct{} {
	shown := toRelSet(exclude, vaultPaths, true)
	if shown == nil {
		shown = make(map[string]struct{})
	}
	for _, item := range embedded {
		if p, ok := strings.CutPrefix(item.Key, "note:"); ok {
			shown[p] = struct{}{}
		}
	}
	return shown
}

// renderLinkedNotesSection writes a heading plus linked note bodies, falling
// back to summary stubs when the budget cannot hold every body.
func renderLinkedNotesSection(b *strings.Builder, heading string, noteMgr obsidian.NoteReader, vaultDef obsidian.VaultDefinition, linked []LinkedNoteContext, limit, noteBudget int, dedupe *renderedNoteDedupe) {
	b.WriteString("\n\n#### ")
	b.WriteString(heading)
	b.WriteString("\n")
	notes := append([]LinkedNoteContext{}, linked...)
	sort.SliceStable(notes, func(i, j int) bool {
		wi := linkedNoteKindWeight(notes[i].Kind)
		wj := linkedNoteKindWeight(notes[j].Kind)
		if wi != wj {
			return wi > wj
		}
		return notes[i].Path < notes[j].Path
	})
	if len(notes) > limit {
		notes = notes[:limit]
	}

	// Prefer embedding full note bodies. Only fall back to summaries when budget is tight.
	const minNoteBodyBudget = 800

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
		if !dedupe.include(blocks[i].Path, content) {
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
