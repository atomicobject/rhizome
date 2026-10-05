package pushdown

import (
	"context"
	"fmt"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// LinkTargetCandidateLimit bounds catalog work for one target type (including
// interface implementors). Above this limit resolution is explicitly unknown.
const LinkTargetCandidateLimit = 5000

// CatalogLinkTargetStore is the indexed read surface needed to resolve a
// link-filter value to a target of the field's declared type.
type CatalogLinkTargetStore interface {
	OntologyNodesByTypePlan(context.Context, codeanchor.OntologyNodeQueryPlan) ([]codeanchor.IntelOntologyNode, error)
	OntologyNodeFieldValuesByNodeIDs(context.Context, []string, []string) ([]codeanchor.IntelOntologyNodeFieldValue, error)
	CurrentNotePropertyValues(context.Context, []string, []string, semdb.NotePropertySource) ([]semdb.NotePropertyValueRow, error)
}

type catalogNodesByPaths interface {
	OntologyNodesByPaths(context.Context, []string) ([]codeanchor.IntelOntologyNode, error)
}

// CatalogLinkResolver is request-scoped. A target type's bounded candidate
// set is read once even when several eq/in values or fields use it.
type CatalogLinkResolver struct {
	schema *ontology.Schema
	store  CatalogLinkTargetStore
	cache  map[string]catalogLinkCandidates
}

func NewCatalogLinkResolver(schema *ontology.Schema, store CatalogLinkTargetStore) *CatalogLinkResolver {
	return &CatalogLinkResolver{schema: schema, store: store, cache: make(map[string]catalogLinkCandidates)}
}

type catalogLinkCandidates struct {
	byPath        map[string]struct{}
	titleByPath   map[string]string
	aliasesByPath map[string][]string
	pathCache     *obsidian.NotePathCache
	incomplete    bool
}

func (r *CatalogLinkResolver) ResolveLinkTarget(ctx context.Context, field *ontology.Field, raw string) (string, Warning) {
	target := UnwrapWikilink(raw)
	if target == "" {
		return "", Warning{Code: "link_filter_unresolved", Message: "link filter target is empty"}
	}
	if r == nil || r.schema == nil || r.store == nil || field == nil {
		return "", Warning{Code: "link_filter_resolution_failed", Message: "link filter target resolver is unavailable"}
	}
	// The indexed path lookup works for every authored note provider. An exact
	// canonical note path wins over a same-text title or alias, and remains
	// usable when alias discovery is too broad to prove uniqueness.
	if path, warning, handled := r.canonicalPathTarget(ctx, field, target); handled {
		return path, warning
	}
	candidates, ok := r.cache[field.TypeName]
	if !ok {
		var err error
		candidates, err = r.loadCandidates(ctx, field.TypeName)
		if err != nil {
			return "", Warning{Code: "link_filter_resolution_failed", Message: err.Error(), Path: field.Name}
		}
		r.cache[field.TypeName] = candidates
	}
	if candidates.incomplete {
		return "", Warning{
			Code: "link_filter_resolution_incomplete", Path: field.Name,
			Message: fmt.Sprintf("link filter target %q cannot be resolved uniquely: %s has more than %d indexed candidates; supply the canonical note path or use a narrower target type", raw, field.TypeName, LinkTargetCandidateLimit),
		}
	}
	matches := map[string]struct{}{}
	add := func(path string) {
		if path = strings.TrimSpace(path); path != "" {
			matches[path] = struct{}{}
		}
	}
	for _, path := range PathPredicateVariants(target) {
		if _, ok := candidates.byPath[path]; ok {
			add(path)
		}
	}
	if candidates.pathCache != nil {
		if resolved, ok := candidates.pathCache.ResolveNoteTarget(target); ok {
			add(resolved.Path)
		}
	}
	want := normalizedLinkTarget(target)
	for path, title := range candidates.titleByPath {
		if normalizedLinkTarget(title) == want {
			add(path)
		}
	}
	for path, aliases := range candidates.aliasesByPath {
		for _, alias := range aliases {
			if normalizedLinkTarget(alias) == want {
				add(path)
			}
		}
	}
	switch len(matches) {
	case 0:
		return "", Warning{Code: "link_filter_unresolved", Message: fmt.Sprintf("link filter input %q did not resolve to a unique %s target", raw, field.TypeName), Path: field.Name}
	case 1:
		for path := range matches {
			return path, Warning{}
		}
	}
	return "", Warning{Code: "link_filter_ambiguous", Message: fmt.Sprintf("link filter input %q resolved to multiple %s targets", raw, field.TypeName), Path: field.Name}
}

func (r *CatalogLinkResolver) canonicalPathTarget(ctx context.Context, field *ontology.Field, target string) (string, Warning, bool) {
	store, ok := r.store.(catalogNodesByPaths)
	if !ok {
		return "", Warning{}, false
	}
	variants := PathPredicateVariants(target)
	if len(variants) == 0 {
		return "", Warning{}, false
	}
	rows, err := store.OntologyNodesByPaths(ctx, variants)
	if err != nil {
		return "", Warning{Code: "link_filter_resolution_failed", Message: err.Error(), Path: field.Name}, true
	}
	allowed := make(map[string]struct{})
	for _, name := range r.targetTypeNames(field.TypeName) {
		allowed[name] = struct{}{}
	}
	for _, row := range rows {
		if _, ok := allowed[row.TypeName]; ok && row.NodeKind == string(ontology.NodeKindNote) && row.NotePath == variants[0] {
			return row.NotePath, Warning{}, true
		}
	}
	return "", Warning{}, false
}

func normalizedLinkTarget(value string) string {
	return strings.ToLower(strings.TrimSpace(UnwrapWikilink(value)))
}

func (r *CatalogLinkResolver) loadCandidates(ctx context.Context, targetType string) (catalogLinkCandidates, error) {
	out := catalogLinkCandidates{
		byPath: map[string]struct{}{}, titleByPath: map[string]string{}, aliasesByPath: map[string][]string{},
	}
	typeNames := r.targetTypeNames(targetType)
	if len(typeNames) == 0 {
		return out, nil
	}
	rows, err := r.store.OntologyNodesByTypePlan(ctx, codeanchor.OntologyNodeQueryPlan{
		TypeNames: typeNames, Limit: LinkTargetCandidateLimit + 1,
	})
	if err != nil {
		return out, err
	}
	if len(rows) > LinkTargetCandidateLimit {
		out.incomplete = true
		return out, nil
	}
	nodeIDs := make([]string, 0, len(rows))
	paths := make([]string, 0, len(rows))
	for _, row := range rows {
		if path := strings.TrimSpace(row.NotePath); path != "" {
			if _, exists := out.byPath[path]; !exists {
				paths = append(paths, path)
			}
			out.byPath[path] = struct{}{}
			if title := strings.TrimSpace(row.Title); title != "" {
				out.titleByPath[path] = title
			}
		}
		if nodeID := strings.TrimSpace(row.NodeID); nodeID != "" {
			nodeIDs = append(nodeIDs, nodeID)
		}
	}
	values, err := r.store.OntologyNodeFieldValuesByNodeIDs(ctx, nodeIDs, []string{"aliases", "alias", "name", "title"})
	if err != nil {
		return out, err
	}
	for _, value := range values {
		if _, ok := out.byPath[value.NotePath]; !ok || value.ValueText == "" {
			continue
		}
		switch value.FieldName {
		case "name", "title":
			if out.titleByPath[value.NotePath] == "" {
				out.titleByPath[value.NotePath] = value.ValueText
			}
		default:
			out.aliasesByPath[value.NotePath] = append(out.aliasesByPath[value.NotePath], value.ValueText)
		}
	}
	// This path-scoped metadata read avoids CurrentNoteAliases' vault-wide scan.
	if len(paths) > 0 {
		aliases, err := r.store.CurrentNotePropertyValues(ctx, paths, []string{"aliases"}, semdb.NotePropertySourceFrontmatter)
		if err != nil {
			return out, err
		}
		for _, alias := range aliases {
			if _, ok := out.byPath[alias.NotePath]; ok && alias.ValueText != "" {
				out.aliasesByPath[alias.NotePath] = append(out.aliasesByPath[alias.NotePath], alias.ValueText)
			}
		}
		sort.Strings(paths)
		out.pathCache = obsidian.BuildNotePathCacheWithAliases(paths, out.aliasesByPath)
	}
	return out, nil
}

func (r *CatalogLinkResolver) targetTypeNames(target string) []string {
	if noteType := r.schema.Types[target]; noteType != nil {
		return []string{noteType.Name}
	}
	var out []string
	for name, noteType := range r.schema.Types {
		if noteType == nil {
			continue
		}
		if ontology.TypeMatchesOrImplements(r.schema, name, target) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
