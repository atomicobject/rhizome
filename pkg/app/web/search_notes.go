package web

import (
	"context"
	"sort"
	"strings"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
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

	candidatePaths, allowedTypes, err := s.searchCandidatePaths(ctx, typeFilter, pathPrefix, tag)
	if err != nil {
		return NoteSearchResponse{}, err
	}

	vault := staticVaultManager{def: s.cfg.VaultDef}
	matches, err := actions.ListFiles(vault, &obsidian.Note{}, actions.ListParams{
		Expression:     expr,
		SessionStore:   s.runtime.Intel(),
		CandidatePaths: candidatePaths,
	})
	if err != nil {
		return NoteSearchResponse{}, err
	}

	typeByPath := map[string]string{}
	if store := s.runtime.Intel(); store != nil {
		rows, err := store.OntologyTypesByPaths(ctx, matches)
		if err != nil {
			return NoteSearchResponse{}, err
		}
		for path, row := range rows {
			typeByPath[path] = row.TypeName
		}
	}

	tagByPath, err := s.noteTagsByPaths(ctx, matches)
	if err != nil {
		return NoteSearchResponse{}, err
	}

	issuesByPath := map[string]bool{}
	if typeFilter == pseudoTypeIssues {
		_, defs, err := s.ontologyContext()
		if err != nil {
			return NoteSearchResponse{}, err
		}
		if store := s.runtime.Intel(); store != nil && defs != nil && defs.schema != nil {
			scope := noderead.NewService(s.cfg.VaultDef, &obsidian.Note{}, store, defs.schema).NewScope(ctx, noderead.ScopeOptions{})
			assessments, err := scope.AssessmentsByPaths(ctx, matches)
			if err != nil {
				return NoteSearchResponse{}, err
			}
			issuesByPath, _, _, _, err = assessmentIssueDetails(ctx, defs.schema, assessments, scope.AssessmentsByPaths)
			if err != nil {
				return NoteSearchResponse{}, err
			}
		}
	}

	filtered := make([]NoteSearchMatch, 0, len(matches))
	for _, path := range matches {
		if pathPrefix != "" && !strings.HasPrefix(path, pathPrefix) {
			continue
		}

		resolvedType := typeByPath[path]
		tags := tagByPath[path]
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

		filtered = append(filtered, NoteSearchMatch{
			Path:         path,
			Title:        searchResultTitle(path),
			ResolvedType: resolvedType,
			Snippet:      searchResultTitle(path),
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

func (s *Server) searchCandidatePaths(ctx context.Context, typeFilter, pathPrefix, tag string) ([]string, map[string]struct{}, error) {
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
		paths, interfaceTypes, isInterface, err := s.interfaceSearchScope(ctx, typeFilter)
		if err != nil {
			return nil, nil, err
		}
		if isInterface {
			allowedTypes = interfaceTypes
		} else {
			paths, err = store.OntologyPathsByType(ctx, typeFilter, 0)
			if err != nil {
				return nil, nil, err
			}
			allowedTypes = map[string]struct{}{strings.ToLower(typeFilter): {}}
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

// interfaceSearchScope resolves an interface through the canonical node-read
// type listing, then narrows the search boundary to file-backed note roots.
// Embedded implementors remain valid ontology instances but are not standalone
// notes and therefore cannot be candidates for /search/notes.
func (s *Server) interfaceSearchScope(ctx context.Context, typeName string) ([]string, map[string]struct{}, bool, error) {
	defs, err := s.ontologyDefinitions()
	if err != nil {
		return nil, nil, false, err
	}
	if defs == nil || defs.schema == nil || defs.schema.Interfaces[typeName] == nil {
		return nil, nil, false, nil
	}

	result, err := s.nodeReadScope(ctx, defs).TypeInstances(ctx, noderead.TypeInstancesRequest{TypeName: typeName})
	if err != nil {
		return nil, nil, true, err
	}
	paths := make([]string, 0, len(result.Items))
	allowedTypes := make(map[string]struct{})
	seenPaths := make(map[string]struct{}, len(result.Items))
	for _, item := range result.Items {
		noteType := defs.schema.Types[item.ResolvedType]
		if noteType == nil || noteType.Role != ontology.TypeRoleNote || item.Ref.Kind != ontology.NodeKindNote {
			continue
		}
		allowedTypes[strings.ToLower(item.ResolvedType)] = struct{}{}
		if _, seen := seenPaths[item.NotePath]; seen {
			continue
		}
		seenPaths[item.NotePath] = struct{}{}
		paths = append(paths, item.NotePath)
	}
	sort.Strings(paths)
	return paths, allowedTypes, true, nil
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
