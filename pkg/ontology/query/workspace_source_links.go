package query

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/vektah/gqlparser/v2/ast"
)

const maxWorkspaceSourceLinks = 100

// workspaceSourceLink is a source-preserving public projection. It keeps one
// entry per authored occurrence; only the scope resolution inputs are deduped.
type workspaceSourceLink struct {
	Target, AuthoredTarget, Text, Kind, Anchor, TargetKind, Title, Preview string
	Embed, Resolved                                                        bool
	ResolvedRef                                                            *ontology.NodeRef
}

func (e *executor) workspaceSourceLinks(ctx context.Context, projection *ontology.NodeProjection, first int, path []string) []workspaceSourceLink {
	if e == nil || e.loaders == nil || e.loaders.scope == nil || projection == nil {
		return []workspaceSourceLink{}
	}
	first = workspaceSourceLinkLimit(first)
	if first == 0 {
		return []workspaceSourceLink{}
	}

	links := ontology.MarkdownProjectionStructuredLinks(projection)
	if len(links) == 0 {
		return []workspaceSourceLink{}
	}
	if len(links) > first {
		links = links[:first]
	}
	cache, err := e.notePathCacheForSections(ctx)
	if err != nil {
		e.addError(path, err.Error())
		return []workspaceSourceLink{}
	}

	resolverInputs := make([]noderead.NodeTarget, 0, len(links))
	resolverInputByLink := make([]string, len(links))
	seenInputs := make(map[string]struct{}, len(links))
	for i, link := range links {
		input, ok := workspaceSourceLinkResolverInput(cache, projection.Ref.NotePath, link, e.deps.VaultDef.SupportsMarkdownLinks())
		if !ok {
			continue
		}
		resolverInputByLink[i] = input
		if _, seen := seenInputs[input]; seen {
			continue
		}
		seenInputs[input] = struct{}{}
		resolverInputs = append(resolverInputs, noderead.NodeTarget{Input: input})
	}

	resolvedByInput := map[string]noderead.ResolvedNode{}
	if len(resolverInputs) > 0 {
		result, err := e.loaders.scope.Resolve(ctx, noderead.ResolveRequest{
			Targets:  resolverInputs,
			FromPath: projection.Ref.NotePath,
			Hydrate:  noderead.HydrateOptions{Profile: noderead.HydrateSummary},
		})
		if err != nil {
			e.addError(path, err.Error())
		} else {
			// Resolve canonicalizes target refs before hydrating them. For note
			// refs that gain a structural fingerprint during canonicalization, its
			// result record may not share the original identity key. Rehydrate the
			// resolved set through the same request scope so this is a single
			// cached summary batch, never a content read or per-link fetch.
			resolvedRecords, hydrateErr := workspaceSourceLinkResolvedRecords(ctx, e.loaders.scope, result.Resolved)
			if hydrateErr != nil {
				e.addError(path, hydrateErr.Error())
			}
			for _, item := range result.Resolved {
				if record, ok := resolvedRecords[item.Ref.NotePath]; ok {
					item.Record = record
				}
				resolvedByInput[item.Input] = item
			}
		}
	}

	out := make([]workspaceSourceLink, 0, len(links))
	for i, link := range links {
		item := workspaceSourceLink{
			Target:         link.Target,
			AuthoredTarget: link.Target,
			Text:           link.Display,
			Kind:           workspaceSourceLinkKind(link.Kind),
			Anchor:         workspaceSourceLinkAnchor(link.Fragment),
			Embed:          link.Embed,
			TargetKind:     workspaceSourceLinkTargetKind(link.Path),
		}
		if resolved, ok := resolvedByInput[resolverInputByLink[i]]; ok && !resolved.Ref.IsZero() {
			ref := resolved.Ref
			item.Resolved = true
			item.ResolvedRef = &ref
			item.Target = workspaceSourceLinkResolvedTarget(ref, link.Fragment)
			item.TargetKind = "note"
			item.Title = resolved.Record.Title
			item.Preview = workspaceSourceLinkPreview(resolved.Record)
		}
		out = append(out, item)
	}
	return out
}

func workspaceSourceLinkResolvedRecords(ctx context.Context, scope *noderead.Scope, resolved []noderead.ResolvedNode) (map[string]noderead.NodeRecord, error) {
	if scope == nil || len(resolved) == 0 {
		return map[string]noderead.NodeRecord{}, nil
	}
	refs := make([]ontology.NodeRef, 0, len(resolved))
	for _, item := range resolved {
		if !item.Ref.IsZero() {
			refs = append(refs, item.Ref)
		}
	}
	records, err := scope.Hydrate(ctx, refs, noderead.HydrateOptions{Profile: noderead.HydrateSummary})
	if err != nil {
		return nil, err
	}
	out := make(map[string]noderead.NodeRecord, len(records))
	for _, record := range records {
		if notePath := strings.TrimSpace(record.Path); notePath != "" {
			out[notePath] = record
		}
	}
	return out, nil
}

// workspaceSourceLinkPreview uses only the summary fields already present in
// HydrateSummary records. It must not request note content for link previews.
func workspaceSourceLinkPreview(record noderead.NodeRecord) string {
	if value := firstWorkspaceSourceLinkFieldValue(record.FieldValues["summary"]); value != "" {
		return value
	}
	fieldKeys := matchingWorkspaceSourceLinkKeys(record.FieldValues)
	for _, key := range fieldKeys {
		if value := firstWorkspaceSourceLinkFieldValue(record.FieldValues[key]); value != "" {
			return value
		}
	}
	if value := firstWorkspaceSourceLinkValue(record.InlineProps["summary"]); value != "" {
		return value
	}
	inlineKeys := matchingWorkspaceSourceLinkKeys(record.InlineProps)
	for _, key := range inlineKeys {
		if value := firstWorkspaceSourceLinkValue(record.InlineProps[key]); value != "" {
			return value
		}
	}
	return ""
}

func matchingWorkspaceSourceLinkKeys[V any](values map[string]V) []string {
	keys := make([]string, 0)
	for key := range values {
		if key != "summary" && strings.EqualFold(strings.TrimSpace(key), "summary") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func firstWorkspaceSourceLinkFieldValue(values []codeanchor.IntelOntologyNodeFieldValue) string {
	for _, value := range values {
		if text := strings.TrimSpace(value.ValueText); text != "" {
			return text
		}
	}
	return ""
}

func firstWorkspaceSourceLinkValue(values []string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func workspaceSourceLinkResolverInput(cache *obsidian.NotePathCache, fromPath string, link obsidian.StructuredLink, markdownLinks bool) (string, bool) {
	target := strings.TrimSpace(link.Target)
	if target == "" {
		return "", false
	}
	// Markdown anchors use the shared resolver's canonical source path; inserting
	// that filename into an authored URL would reinterpret literal escapes.
	if link.Kind == obsidian.StructuredLinkWikilink && strings.TrimSpace(link.Path) == "" && strings.TrimSpace(link.Fragment) != "" {
		if cache == nil || strings.TrimSpace(fromPath) == "" {
			return "", false
		}
		target = strings.TrimSpace(fromPath) + "#" + strings.TrimSpace(link.Fragment)
	}
	switch link.Kind {
	case obsidian.StructuredLinkWikilink:
		if cache == nil {
			return "", false
		}
		if _, ok := cache.ResolveNoteTarget(target); !ok {
			return "", false
		}
		return "[[" + target + "]]", true
	case obsidian.StructuredLinkMarkdown:
		if !markdownLinks {
			return "", false
		}
		if cache == nil {
			return "", false
		}
		if _, ok := cache.ResolveMdLinkTarget(target, fromPath); !ok {
			return "", false
		}
		return "[source](" + target + ")", true
	default:
		return "", false
	}
}

func workspaceSourceLinkKind(kind obsidian.StructuredLinkKind) string {
	if kind == obsidian.StructuredLinkMarkdown {
		return "mdlink"
	}
	return "wikilink"
}

func workspaceSourceLinkAnchor(fragment string) string {
	return strings.TrimSpace(fragment)
}

func workspaceSourceLinkLimit(first int) int {
	if first <= 0 {
		return 0
	}
	if first > maxWorkspaceSourceLinks {
		return maxWorkspaceSourceLinks
	}
	return first
}

func workspaceSourceLinkResolvedTarget(ref ontology.NodeRef, authoredFragment string) string {
	target := strings.TrimSpace(ref.NotePath)
	fragment := strings.TrimSpace(authoredFragment)
	if fragment == "" {
		fragment = strings.TrimSpace(ref.Fragment)
	}
	if target == "" || fragment == "" {
		return target
	}
	return target + "#" + strings.TrimPrefix(fragment, "#")
}

func workspaceSourceLinkTargetKind(path string) string {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(path))) {
	case ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".pdf", ".mp3", ".mp4", ".mov", ".webm":
		return "attachment"
	}
	// An authored link can only become a note after Scope.Resolve returns a
	// catalog-owned NodeRef. Markdown filename suffixes are not note evidence.
	return "file"
}

func (e *executor) resolveWorkspaceSourceLink(item workspaceSourceLink, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	e.eachSelectionField(set, func(field *ast.Field) {
		key := responseKey(field)
		switch field.Name {
		case "target":
			out[key] = item.Target
		case "authoredTarget":
			out[key] = item.AuthoredTarget
		case "text":
			out[key] = emptyNil(item.Text)
		case "kind":
			out[key] = item.Kind
		case "anchor":
			out[key] = emptyNil(item.Anchor)
		case "embed":
			out[key] = item.Embed
		case "targetKind":
			out[key] = item.TargetKind
		case "resolved":
			out[key] = item.Resolved
		case "resolvedRef":
			if item.ResolvedRef == nil {
				out[key] = nil
			} else {
				out[key] = nodeRefGraphQLValue(*item.ResolvedRef)
			}
		case "title":
			out[key] = emptyNil(item.Title)
		case "preview":
			out[key] = emptyNil(item.Preview)
		default:
			e.addError(append(path, key), "field does not exist on NodeWorkspaceSourceLink")
		}
	})
	return out
}
