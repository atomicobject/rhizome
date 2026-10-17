package operations

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/frontmatter"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func prioritizeRationaleForContext(rationales []codeanchor.Rationale) []codeanchor.Rationale {
	if len(rationales) <= 1 {
		return rationales
	}
	out := append([]codeanchor.Rationale(nil), rationales...)
	sort.SliceStable(out, func(i, j int) bool {
		pi := rationaleContextPriority(out[i].Kind)
		pj := rationaleContextPriority(out[j].Kind)
		if pi != pj {
			return pi < pj
		}
		return out[i].StartLine < out[j].StartLine
	})
	return out
}

func rationaleContextPriority(kind codeanchor.RationaleKind) int {
	switch kind {
	case codeanchor.RationaleWhy, codeanchor.RationaleRationale, codeanchor.RationaleImportant:
		return 0
	case codeanchor.RationaleNote:
		return 1
	default:
		return 2
	}
}

func buildLinkedNotes(refs []coderefs.CodeRef, noteMgr obsidian.NoteReader) []LinkedNoteContext {
	if len(refs) == 0 {
		return nil
	}
	if noteMgr == nil {
		noteMgr = &obsidian.Note{}
	}

	seen := make(map[string]bool)
	out := make([]LinkedNoteContext, 0, len(refs))
	for _, ref := range refs {
		key := ref.Target + "#" + ref.Fragment + "|" + string(ref.Kind) + "|" + ref.SourceFile + "|" + strconv.Itoa(ref.Line)
		if seen[key] {
			continue
		}
		seen[key] = true

		fm := blessedFrontmatter(noteMgr, ref.Target)
		out = append(out, LinkedNoteContext{
			Path:        ref.Target,
			Fragment:    strings.TrimSpace(ref.Fragment),
			RawTarget:   strings.TrimSpace(ref.RawTarget),
			Title:       paths.NoteTitle(ref.Target),
			Kind:        string(ref.Kind),
			Line:        ref.Line,
			Snippet:     ref.Snippet,
			Frontmatter: fm,
		})
	}
	return out
}

func resolveLinkedOntologyNodes(ctx context.Context, scope *noderead.Scope, notes []LinkedNoteContext, ensure ontology.EnsureLinkTargetMode) ([]OntologyNodeContext, []noderead.NodeResolveDiagnostic) {
	if scope == nil || len(notes) == 0 {
		return nil, nil
	}
	targets := make([]noderead.NodeTarget, 0)
	byInput := map[string]LinkedNoteContext{}
	for _, note := range notes {
		if strings.TrimSpace(note.Fragment) == "" {
			continue
		}
		input := strings.TrimSpace(note.RawTarget)
		if input == "" {
			input = note.Path + "#" + note.Fragment
		}
		if !strings.HasPrefix(input, "[[") && !strings.Contains(input, "](") {
			input = note.Path + "#" + note.Fragment
		}
		targets = append(targets, noderead.NodeTarget{Input: input})
		byInput[input] = note
	}
	if len(targets) == 0 {
		return nil, nil
	}
	result, err := scope.Resolve(ctx, noderead.ResolveRequest{
		Targets:          targets,
		EnsureLinkTarget: ensure,
		IncludeLocators:  true,
		Hydrate:          noderead.HydrateOptions{Profile: noderead.HydrateContent},
	})
	if err != nil {
		return nil, []noderead.NodeResolveDiagnostic{{Code: "resolve_failed", Message: err.Error()}}
	}
	out := make([]OntologyNodeContext, 0, len(result.Resolved))
	for _, resolved := range result.Resolved {
		source := byInput[resolved.Input]
		locator := resolved.Locator
		record := resolved.Record
		ctx := OntologyNodeContext{
			Input:         resolved.Input,
			SourcePath:    source.Path,
			Line:          source.Line,
			Snippet:       source.Snippet,
			Ref:           resolved.Ref,
			TypeName:      firstNonEmptyString(record.TypeName, resolved.Ref.TypeName),
			Title:         firstNonEmptyString(record.Title, locator.SourceLocator, resolved.Ref.String()),
			SourceLocator: locator.SourceLocator,
			Status:        locator.Status,
			Content:       record.Content,
			RequiresFix:   locator.Status == ontology.NodeLocatorRequiresFix,
		}
		if locator.LinkTarget != nil {
			ctx.Wikilink = locator.LinkTarget.Wikilink
			ctx.Markdown = locator.LinkTarget.Markdown
		}
		out = append(out, ctx)
	}
	return out, result.Diagnostics
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func blessedFrontmatter(noteMgr obsidian.NoteReader, notePath string) map[string]interface{} {
	if noteMgr == nil || notePath == "" {
		return nil
	}
	fact, ok := NoteFactsFromReader(noteMgr).LookupFact(notePath)
	if !ok {
		return nil
	}
	return frontmatter.FilterBlessed(fact.Frontmatter)
}

func mergeCodeAnchor(ctx context.Context, existing []LinkedNoteContext, svc *codeanchor.Service, absPath string, vaultDef obsidian.VaultDefinition, vaultPaths paths.VaultPaths, allowedKinds map[codeanchor.AnchorKind]bool, skipRefresh bool) ([]LinkedNoteContext, []codeanchor.AnchorMatchTrace) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !skipRefresh && svc.HasDirtyAnchors() {
		// Best-effort to refresh scopes when ad-hoc indexing just ran.
		_ = svc.RecomputeAnchorScopes(ctx)
	}
	fc, err := svc.NotesForFile(ctx, absPath)
	if err != nil {
		return existing, nil
	}
	seen := make(map[string]bool, len(existing))
	for _, ln := range existing {
		seen[ln.Path+"|"+ln.Kind] = true
	}
	// Prefer per-anchor attribution when available.
	if len(allowedKinds) > 0 && len(fc.AnchorKinds) > 0 {
		allowedLabels := make(map[string]struct{}, len(fc.AnchorKinds))
		for label, kind := range fc.AnchorKinds {
			if allowedKinds[kind] {
				allowedLabels[label] = struct{}{}
			}
		}
		if len(allowedLabels) == 0 {
			return existing, nil
		}
		if len(fc.AnchorNotes) > 0 {
			filtered := fc.AnchorNotes[:0]
			for _, an := range fc.AnchorNotes {
				if _, ok := allowedLabels[an.Label]; ok {
					filtered = append(filtered, an)
				}
			}
			fc.AnchorNotes = filtered
		}
		if len(fc.Anchors) > 0 {
			filtered := fc.Anchors[:0]
			for _, label := range fc.Anchors {
				if _, ok := allowedLabels[label]; ok {
					filtered = append(filtered, label)
				}
			}
			fc.Anchors = filtered
		}
		if len(fc.Trace) > 0 {
			filtered := fc.Trace[:0]
			for _, tr := range fc.Trace {
				if _, ok := allowedLabels[tr.AnchorLabel]; ok {
					filtered = append(filtered, tr)
				}
			}
			fc.Trace = filtered
		}
		if len(fc.AnchorNotes) == 0 && len(fc.Anchors) == 0 {
			return existing, nil
		}
	}
	if len(fc.AnchorNotes) > 0 {
		for _, an := range fc.AnchorNotes {
			label := strings.TrimSpace(an.Label)
			if label == "" {
				continue
			}
			kind := "anchor:" + label
			for _, note := range an.Notes {
				normPath := normalizeNotePath(note.Path, vaultDef, vaultPaths)
				if normPath == "" {
					normPath = note.Path
				}
				key := normPath + "|" + kind
				if seen[key] {
					continue
				}
				seen[key] = true
				existing = append(existing, LinkedNoteContext{
					Path:  normPath,
					Title: note.Title,
					Kind:  kind,
				})
			}
		}
		return existing, fc.Trace
	}

	// Fallback (older stores/services): emit anchors without per-note attribution.
	kind := "anchor"
	if len(fc.Anchors) > 0 {
		kind = "anchor:" + strings.Join(fc.Anchors, ",")
	}
	for _, note := range fc.Notes {
		normPath := normalizeNotePath(note.Path, vaultDef, vaultPaths)
		if normPath == "" {
			normPath = note.Path
		}
		key := normPath + "|" + kind
		if seen[key] {
			continue
		}
		seen[key] = true
		existing = append(existing, LinkedNoteContext{
			Path:  normPath,
			Title: note.Title,
			Kind:  kind,
		})
	}
	return existing, fc.Trace
}

func expandLinkedNoteFrontmatter(notes []LinkedNoteContext, noteMgr obsidian.NoteReader) []LinkedNoteContext {
	if noteMgr == nil || len(notes) == 0 {
		return notes
	}
	for i := range notes {
		if notes[i].Frontmatter != nil {
			continue
		}
		fm := blessedFrontmatter(noteMgr, notes[i].Path)
		notes[i].Frontmatter = fm
	}
	return notes
}
