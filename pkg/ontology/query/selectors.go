package query

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/diag"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/vektah/gqlparser/v2/ast"
)

func (e *executor) findTypedNotes(ctx context.Context, queryText, typeFilter string, first int) ([]*noteRecord, error) {
	rows, err := e.loaders.selectorMetadataRows(ctx)
	if err != nil {
		return nil, err
	}
	var matches []scoredMatch
	queryLower := strings.ToLower(strings.TrimSpace(queryText))
	tokens := strings.Fields(queryLower)
	for _, row := range rows {
		combined := strings.ToLower(strings.TrimSpace(row.Title) + " " + filepath.ToSlash(row.Path))
		score := 0.0
		if strings.Contains(combined, queryLower) {
			score += 1
		}
		for _, token := range tokens {
			if strings.Contains(combined, token) {
				score += 0.4
			}
		}
		if score == 0 {
			continue
		}
		matches = append(matches, scoredMatch{path: row.Path, score: score})
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].path < matches[j].path
	})
	return e.filterTypedRecords(ctx, scoredPaths(matches), typeFilter, first)
}

func (e *executor) propertyTypedNotes(ctx context.Context, args map[string]any, typeFilter string, first int) ([]*noteRecord, error) {
	name := strings.TrimSpace(stringValue(args["name"]))
	value := strings.TrimSpace(stringValue(args["value"]))
	source := PropertySource(strings.ToUpper(strings.TrimSpace(stringValue(args["source"]))))
	if source == "" {
		source = PropertySourceAny
	}
	sourceFilter := semdb.NotePropertySource(0)
	switch source {
	case PropertySourceFrontmatter:
		sourceFilter = semdb.NotePropertySourceFrontmatter
	case PropertySourceInline:
		sourceFilter = semdb.NotePropertySourceInline
	}
	pathsList, err := e.deps.Store.CurrentNotePathsByPropertyValue(ctx, strings.ToLower(name), notemeta.NormalizePropertyValue(value), sourceFilter)
	if err != nil {
		return nil, err
	}
	return e.filterTypedRecords(ctx, pathsList, typeFilter, first)
}

func (e *executor) semanticNotes(ctx context.Context, queryTerms []string, first int) ([]*noteRecord, error) {
	if e.deps.SemanticSearcher == nil {
		return nil, fmt.Errorf("semantic search is unavailable")
	}
	var results []semantic.Result
	for _, queryText := range queryTerms {
		found, err := e.deps.SemanticSearcher.Search(ctx, semantic.SearchRequest{
			QueryText: queryText,
			Filters:   semantic.SearchFilters{Types: []string{"note"}},
			K:         max(first*4, 40),
		})
		if err != nil {
			return nil, err
		}
		results = append(results, found...)
	}
	pathsList := make([]string, 0, len(results))
	seen := make(map[string]struct{}, len(results))
	scoresByPath := make(map[string]float64, len(results))
	for _, result := range results {
		notePath := strings.TrimSpace(result.NoteID)
		if notePath == "" {
			notePath = strings.TrimSpace(result.Path)
		}
		if notePath == "" {
			continue
		}
		if existing, ok := scoresByPath[notePath]; !ok || result.Score > existing {
			scoresByPath[notePath] = result.Score
		}
		if _, ok := seen[notePath]; ok {
			continue
		}
		seen[notePath] = struct{}{}
		pathsList = append(pathsList, notePath)
	}
	records, err := e.filterTypedRecords(ctx, pathsList, "", first)
	if err != nil {
		return nil, err
	}
	return recordsWithSemanticScores(records, scoresByPath), nil
}

// semanticTypedNotes is a typed-slice survey, not a fallback for unbounded or
// failed queries. Docs: [[ontology-graphql-query-contract#^SPEC-0042-US5]]
func (e *executor) semanticTypedNotes(ctx context.Context, queryTerms []string, typeFilter string, first int) ([]*noteRecord, error) {
	if e.deps.SemanticSearcher == nil {
		return nil, fmt.Errorf("semantic search is unavailable")
	}
	results, err := e.deps.SemanticSearcher.SurveyNodesByType(ctx, semantic.SurveyNodesByTypeRequest{
		QueryTerms: queryTerms,
		TypeNames:  e.possibleConcreteTypes(typeFilter),
		K:          first,
	})
	if err != nil {
		return nil, err
	}
	pathsList := make([]string, 0, len(results))
	scoresByPath := make(map[string]float64, len(results))
	for _, result := range results {
		notePath := strings.TrimSpace(result.NoteID)
		if notePath == "" {
			notePath = strings.TrimSpace(result.Path)
		}
		if notePath == "" {
			continue
		}
		pathsList = append(pathsList, notePath)
		if existing, ok := scoresByPath[notePath]; !ok || result.Score > existing {
			scoresByPath[notePath] = result.Score
		}
	}
	records, err := e.filterTypedRecords(ctx, pathsList, typeFilter, first)
	if err != nil {
		return nil, err
	}
	return recordsWithSemanticScores(records, scoresByPath), nil
}

func (e *executor) filterTypedRecords(ctx context.Context, pathsList []string, typeFilter string, first int) ([]*noteRecord, error) {
	typeRows, err := e.loaders.ontologyTypesByPaths(ctx, pathsList)
	if err != nil {
		return nil, err
	}
	filteredPaths := make([]string, 0, len(pathsList))
	for _, notePath := range pathsList {
		row, ok := typeRows[notePath]
		if !ok {
			continue
		}
		if typeFilter != "" && !e.typeMatchesOrImplements(row.TypeName, typeFilter) {
			continue
		}
		filteredPaths = append(filteredPaths, notePath)
		if first > 0 && len(filteredPaths) >= first {
			break
		}
	}
	if len(filteredPaths) == 0 {
		return []*noteRecord{}, nil
	}
	records, err := e.loaders.recordsForPaths(ctx, filteredPaths)
	if err != nil {
		return nil, err
	}
	out := make([]*noteRecord, 0, len(records))
	for _, record := range records {
		row, ok := typeRows[record.Path]
		if !ok {
			continue
		}
		record.TypeName = row.TypeName
		out = append(out, record)
	}
	return out, nil
}

func recordsWithSemanticScores(records []*noteRecord, scoresByPath map[string]float64) []*noteRecord {
	if len(records) == 0 {
		return records
	}
	out := make([]*noteRecord, 0, len(records))
	for _, record := range records {
		if record == nil {
			out = append(out, record)
			continue
		}
		score, ok := scoresByPath[record.Path]
		if !ok {
			out = append(out, record)
			continue
		}
		scored := *record
		scoreCopy := score
		scored.Score = &scoreCopy
		out = append(out, &scored)
	}
	return out
}

func argsWithFirst(args map[string]any, first int) map[string]any {
	out := make(map[string]any, len(args)+1)
	for key, value := range args {
		out[key] = value
	}
	out["first"] = first
	return out
}

func trimRecordsForPage(records []*noteRecord, first int) ([]*noteRecord, bool) {
	if first <= 0 || len(records) <= first {
		return records, false
	}
	return records[:first], true
}

func pageTypedRootRecords[T any](records []T, first, offset int, dropOffset bool) ([]T, bool) {
	if dropOffset {
		if offset >= len(records) {
			return records[:0], false
		}
		records = records[offset:]
	}
	if len(records) > first {
		return records[:first], true
	}
	return records, false
}

func newTypedRootCoverage(first, offset, returned int, hasMore, noLookahead, candidateBound bool) typedRootCoverage {
	coverage := typedRootCoverage{RequestedFirst: first, ReturnedCount: returned, Offset: offset, Status: "complete"}
	if hasMore {
		coverage.Status = "capped"
		coverage.HasMore = &hasMore
		next := offset + returned
		coverage.NextOffset = &next
	} else if candidateBound {
		coverage.Status = "unknown_candidate_bound"
	} else if noLookahead && returned == first {
		coverage.Status = "unknown_page_bound"
		next := offset + returned
		coverage.NextOffset = &next
	} else {
		coverage.HasMore = &hasMore
	}
	return coverage
}

func (e *executor) resolveNodeList(ctx context.Context, records []*noteRecord, set ast.SelectionSet, path []string) []any {
	e.prefetchAssessments(ctx, records)
	e.executeNodeReadPlan(ctx, records, set)
	records = e.loaders.refreshRecords(records)
	out := make([]any, 0, len(records))
	for _, record := range records {
		out = append(out, e.resolveNodeSelectionSet(ctx, record, set, path))
	}
	return out
}

func (e *executor) resolveSectionList(ctx context.Context, records []*sectionRecord, set ast.SelectionSet, path []string) []any {
	refs := make([]ontology.NodeRef, 0, len(records))
	for _, record := range records {
		if record != nil {
			refs = append(refs, e.sectionNodeRef(record))
		}
	}
	e.prefetchRelationCounts(ctx, refs, set)
	out := make([]any, 0, len(records))
	for _, record := range records {
		out = append(out, e.resolveSectionSelectionSet(ctx, record, set, path))
	}
	return out
}

type pageInfo struct {
	QueryShapeVersion string
	RequestedFirst    int
	ReturnedCount     int
	Truncated         bool
	MaxFirst          int
}

func (e *executor) resolveNoteConnectionSelectionSet(ctx context.Context, records []*noteRecord, warnings []diag.Warning, first int, truncated bool, set ast.SelectionSet, path []string) map[string]any {
	if warnings == nil {
		warnings = []diag.Warning{}
	}
	out := make(map[string]any)
	for _, selection := range set {
		field, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(field)
		switch field.Name {
		case "nodes":
			out[key] = e.resolveNodeList(ctx, records, field.SelectionSet, append(path, key))
		case "pageInfo":
			out[key] = e.resolvePageInfoSelectionSet(pageInfo{QueryShapeVersion: "v1", RequestedFirst: first, ReturnedCount: len(records), Truncated: truncated, MaxFirst: publicRootFirstMax}, field.SelectionSet, append(path, key))
		case "warnings":
			out[key] = e.resolveWarnings(warnings, field.SelectionSet, append(path, key))
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on NoteConnection", field.Name))
		}
	}
	return out
}

func (e *executor) resolveSearchSelectionSet(ctx context.Context, field *ast.Field, path []string) map[string]any {
	args := e.fieldArgs(field)
	queryTerms := semanticArgList(args["query"])
	first := boundedInt(args["first"], 20, nestedFieldFirstMax)
	typeFilter := strings.TrimSpace(stringValue(args["type"]))
	warnings := []diag.Warning{}
	var records []*noteRecord
	fetchFirst := first + 1
	switch {
	case len(queryTerms) == 0:
		warnings = append(warnings, diag.Warning{Code: "missing_query", Message: "search requires at least one query term"})
	case e.deps.NoteSearcher == nil:
		warnings = append(warnings, diag.Warning{Code: "search_unavailable", Message: "search is unavailable"})
	default:
		var noteTypes []string
		if typeFilter != "" {
			noteTypes = e.possibleConcreteTypes(typeFilter)
		}
		response, err := e.deps.NoteSearcher.SearchNotes(ctx, NoteSearchRequest{Queries: queryTerms, NoteTypes: noteTypes, First: fetchFirst})
		warnings = append(warnings, response.Warnings...)
		if err != nil {
			warnings = append(warnings, diag.Warning{Code: "search_failed", Message: err.Error()})
			break
		}
		pathsList := make([]string, 0, len(response.Hits))
		scoresByPath := make(map[string]float64, len(response.Hits))
		for _, hit := range response.Hits {
			pathsList = append(pathsList, hit.Path)
			scoresByPath[hit.Path] = hit.Score
		}
		found, err := e.filterTypedRecords(ctx, pathsList, typeFilter, fetchFirst)
		if err != nil {
			warnings = append(warnings, diag.Warning{Code: "search_failed", Message: err.Error()})
			break
		}
		records = recordsWithSemanticScores(found, scoresByPath)
	}
	records, truncated := trimRecordsForPage(records, first)
	out := make(map[string]any)
	for _, selection := range field.SelectionSet {
		current, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(current)
		switch current.Name {
		case "nodes":
			out[key] = e.resolveNodeList(ctx, records, current.SelectionSet, append(path, key))
		case "pageInfo":
			out[key] = e.resolvePageInfoSelectionSet(pageInfo{QueryShapeVersion: "v1", RequestedFirst: first, ReturnedCount: len(records), Truncated: truncated, MaxFirst: nestedFieldFirstMax}, current.SelectionSet, append(path, key))
		case "query":
			out[key] = stringsSliceAny(queryTerms)
		case "warnings":
			out[key] = e.resolveWarnings(warnings, current.SelectionSet, append(path, key))
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on NodeSearchResult", current.Name))
		}
	}
	return out
}

func (e *executor) resolvePageInfoSelectionSet(info pageInfo, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	for _, selection := range set {
		field, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(field)
		switch field.Name {
		case "queryShapeVersion":
			out[key] = firstNonEmpty(info.QueryShapeVersion, "v1")
		case "requestedFirst":
			out[key] = info.RequestedFirst
		case "returnedCount":
			out[key] = info.ReturnedCount
		case "truncated":
			out[key] = info.Truncated
		case "maxFirst":
			out[key] = info.MaxFirst
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on PageInfo", field.Name))
		}
	}
	return out
}
