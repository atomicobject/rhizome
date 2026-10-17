package web

import (
	"context"
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"sort"
	"strings"
)

type staticVaultManager struct {
	def obsidian.VaultDefinition
}

func (v staticVaultManager) DefaultName() (string, error) { return v.def.Name, nil }
func (v staticVaultManager) SetDefaultName(string) error  { return nil }
func (v staticVaultManager) Path() (string, error)        { return v.def.BasePath(), nil }
func (v staticVaultManager) Definition() (obsidian.VaultDefinition, error) {
	return v.def, nil
}

func (s *Server) searchNotes(ctx context.Context, query, typeFilter, pathPrefix, tag string, limit, offset int) (NoteSearchResponse, error) {
	if limit <= 0 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}
	query = strings.TrimSpace(query)
	typeFilter = strings.TrimSpace(typeFilter)
	pathPrefix = strings.Trim(strings.TrimSpace(pathPrefix), "/")
	tag = strings.TrimPrefix(strings.TrimSpace(tag), "#")

	_, expr, err := actions.ParseSearchQueryWithExpression(query)
	if err != nil {
		return NoteSearchResponse{}, err
	}

	defs, err := s.ontologyDefinitions()
	if err != nil {
		return NoteSearchResponse{}, err
	}
	var schema *ontoschema.Schema
	if defs != nil {
		schema = defs.schema
	}
	nodeScope := noderead.NewService(s.cfg.VaultDef, s.defaultOntologyNoteReader(), s.runtime.Intel(), schema).NewScope(noderead.ScopeOptions{})
	candidatePaths, allowedTypes, err := s.searchCandidatePaths(ctx, nodeScope, schema, typeFilter, pathPrefix, tag)
	if err != nil {
		return NoteSearchResponse{}, err
	}

	vault := staticVaultManager{def: s.cfg.VaultDef}
	matches, err := actions.ListFiles(vault, s.defaultOntologyNoteReader(), actions.ListParams{
		Expression:     expr,
		SessionStore:   s.runtime.Intel(),
		CandidatePaths: candidatePaths,
	})
	if err != nil {
		return NoteSearchResponse{}, err
	}

	refs := make([]ontology.NodeRef, 0, len(matches))
	for _, path := range matches {
		refs = append(refs, ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote})
	}
	records, err := nodeScope.Hydrate(ctx, refs, noderead.HydrateOptions{Profile: noderead.HydrateSummary})
	if err != nil {
		return NoteSearchResponse{}, err
	}
	recordByPath := make(map[string]noderead.NodeRecord, len(records))
	for _, record := range records {
		recordByPath[record.Ref.NotePath] = record
	}
	issuesByPath := map[string]bool{}
	if typeFilter == pseudoTypeIssues && schema != nil {
		assessments, err := nodeScope.AssessmentsByPaths(ctx, matches)
		if err != nil {
			return NoteSearchResponse{}, err
		}
		issuesByPath, _, _, _, err = assessmentIssueDetails(ctx, schema, assessments, nodeScope.AssessmentsByPaths)
		if err != nil {
			return NoteSearchResponse{}, err
		}
	}

	filtered := make([]NoteSearchMatch, 0, len(matches))
	for _, path := range matches {
		if pathPrefix != "" && !strings.HasPrefix(path, pathPrefix) {
			continue
		}

		record, ok := recordByPath[path]
		if !ok {
			continue
		}
		resolvedType := record.TypeName
		if ontoschema.OntologyTypeVisibility(resolvedType) == ontoschema.TypeVisibilityInternal {
			resolvedType = ""
		}
		tags := record.Tags
		switch typeFilter {
		case "", pseudoTypeAll:
		case pseudoTypeIssues:
			if !issuesByPath[path] {
				continue
			}
		default:
			if _, ok := allowedTypes[strings.ToLower(resolvedType)]; !ok {
				continue
			}
		}
		if tag != "" && !noteHasTag(tags, tag) {
			continue
		}

		title := firstNonEmpty(record.Title, searchResultTitle(path))
		filtered = append(filtered, NoteSearchMatch{
			Path:         path,
			Title:        title,
			ResolvedType: resolvedType,
			Snippet:      title,
			Tags:         tags,
		})
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Title != filtered[j].Title {
			return filtered[i].Title < filtered[j].Title
		}
		return filtered[i].Path < filtered[j].Path
	})

	total := len(filtered)
	if offset >= total {
		return NoteSearchResponse{
			Query:  query,
			Offset: offset,
			Limit:  limit,
			Count:  0,
			Total:  total,
		}, nil
	}
	page := filtered[offset:]
	if len(page) > limit {
		page = page[:limit]
	}
	return NoteSearchResponse{
		Query:   query,
		Offset:  offset,
		Limit:   limit,
		Count:   len(page),
		Total:   total,
		Matches: page,
	}, nil
}

func searchResultTitle(path string) string {
	if title, ok := (&obsidian.Note{}).Title(path); ok && strings.TrimSpace(title) != "" {
		return title
	}
	return path
}

func noteHasTag(tags []string, target string) bool {
	needle := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(target, "#")))
	if needle == "" {
		return false
	}
	for _, tag := range tags {
		if strings.ToLower(strings.TrimSpace(strings.TrimPrefix(tag, "#"))) == needle {
			return true
		}
	}
	return false
}

func (s *Server) searchCandidatePaths(ctx context.Context, scope *noderead.Scope, schema *ontoschema.Schema, typeFilter, pathPrefix, tag string) ([]string, map[string]struct{}, error) {
	store := s.runtime.Intel()
	if store == nil {
		return nil, nil, nil
	}
	var candidate []string
	var allowedTypes map[string]struct{}
	intersect := func(next []string) {
		if len(next) == 0 {
			candidate = []string{}
			return
		}
		sort.Strings(next)
		if candidate == nil {
			candidate = next
			return
		}
		candidate = intersectSortedStringSlices(candidate, next)
	}

	switch typeFilter {
	case "", pseudoTypeAll, pseudoTypeIssues:
	default:
		result, err := scope.TypeInstances(ctx, noderead.TypeInstancesRequest{TypeName: typeFilter})
		if err != nil {
			return nil, nil, err
		}
		paths := make([]string, 0, len(result.Items))
		allowedTypes = make(map[string]struct{})
		seenPaths := make(map[string]bool, len(result.Items))
		for _, item := range result.Items {
			if schema == nil || (item.Ref.Kind != "" && item.Ref.Kind != ontology.NodeKindNote) {
				continue
			}
			noteType := schema.Types[item.ResolvedType]
			if noteType == nil || noteType.Role != ontoschema.TypeRoleNote {
				continue
			}
			allowedTypes[strings.ToLower(item.ResolvedType)] = struct{}{}
			if !seenPaths[item.NotePath] {
				seenPaths[item.NotePath] = true
				paths = append(paths, item.NotePath)
			}
		}
		intersect(paths)
	}
	if pathPrefix != "" {
		paths, err := store.CurrentNotePathsByPathPrefix(ctx, pathPrefix)
		if err != nil {
			return nil, nil, err
		}
		intersect(paths)
	}
	if tag != "" {
		paths, err := store.CurrentNotePathsByTag(ctx, strings.ToLower(strings.TrimSpace(strings.TrimPrefix(tag, "#"))))
		if err != nil {
			return nil, nil, err
		}
		intersect(paths)
	}
	return candidate, allowedTypes, nil
}

func intersectSortedStringSlices(left, right []string) []string {
	if len(left) == 0 || len(right) == 0 {
		return []string{}
	}
	out := make([]string, 0, min(len(left), len(right)))
	i, j := 0, 0
	for i < len(left) && j < len(right) {
		switch {
		case left[i] == right[j]:
			out = append(out, left[i])
			i++
			j++
		case left[i] < right[j]:
			i++
		default:
			j++
		}
	}
	return out
}
