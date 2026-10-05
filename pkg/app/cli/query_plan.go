package actions

import (
	"context"
	"sort"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type plannedLeafStrategy int

const (
	plannedLeafUnknown plannedLeafStrategy = iota
	plannedLeafUniverse
	plannedLeafPath
	plannedLeafTag
	plannedLeafProperty
	plannedLeafIndexedFind
	plannedLeafFallbackFind
)

type plannedExpression struct {
	Type     exprType
	Input    *ListInput
	Left     *plannedExpression
	Right    *plannedExpression
	Strategy plannedLeafStrategy
}

func buildPlannedExpression(expr *InputExpression) *plannedExpression {
	if expr == nil {
		return nil
	}
	planned := &plannedExpression{
		Type:  expr.Type,
		Input: expr.Input,
	}
	switch expr.Type {
	case exprLeaf:
		planned.Strategy = classifyLeafStrategy(expr.Input)
	case exprAnd, exprOr:
		planned.Left = buildPlannedExpression(expr.Left)
		planned.Right = buildPlannedExpression(expr.Right)
	case exprNot:
		planned.Left = buildPlannedExpression(expr.Left)
	}
	return planned
}

func classifyLeafStrategy(input *ListInput) plannedLeafStrategy {
	if input == nil {
		return plannedLeafUnknown
	}
	switch input.Type {
	case InputTypeFile:
		if stringValue(input.Value) == "*" {
			return plannedLeafUniverse
		}
		return plannedLeafPath
	case InputTypeTag:
		return plannedLeafTag
	case InputTypeProperty:
		return plannedLeafProperty
	case InputTypeFind:
		if isIndexedFindCandidate(input.Value) {
			return plannedLeafIndexedFind
		}
		return plannedLeafFallbackFind
	default:
		return plannedLeafUnknown
	}
}

func isIndexedFindCandidate(pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	if strings.ContainsAny(pattern, "*?") || strings.Count(pattern, "/") > 1 {
		return false
	}
	return true
}

func evaluatePlannedExpressionMatchesWithStore(ctx context.Context, store *semdb.Store, expr *plannedExpression, candidatePaths []string) ([]string, bool, error) {
	if expr == nil || store == nil {
		return nil, false, nil
	}
	pathsList := candidatePaths
	if pathsList == nil {
		// The planner still needs a universe for OR/NOT semantics. Callers can pass
		// a narrower candidate set so fallback find validation never expands back to
		// the full note corpus.
		var err error
		pathsList, err = store.CurrentNoteMetadataPaths(ctx)
		if err != nil {
			return nil, false, err
		}
	}
	universe := make(map[string]struct{}, len(pathsList))
	for _, path := range pathsList {
		universe[path] = struct{}{}
	}

	state := &storePlannerState{
		store:    store,
		universe: universe,
	}
	rootCandidate := map[string]struct{}(nil)
	if candidatePaths != nil {
		rootCandidate = universe
	}
	matches, ok, err := evalPlannedExpressionSet(ctx, state, expr, rootCandidate)
	if err != nil || !ok {
		return nil, ok, err
	}
	out := pathSetToSortedSlice(matches)
	return out, true, nil
}

type storePlannerState struct {
	store      *semdb.Store
	universe   map[string]struct{}
	rowsByPath map[string]semdb.NoteMetadataRow
}

func (s *storePlannerState) metadataRows(ctx context.Context, candidates map[string]struct{}) (map[string]semdb.NoteMetadataRow, error) {
	if len(candidates) == 0 {
		return map[string]semdb.NoteMetadataRow{}, nil
	}
	paths := pathSetToSortedSlice(candidates)
	rows, err := s.store.CurrentNoteMetadataRowsByPaths(ctx, paths)
	if err != nil {
		return nil, err
	}
	if s.rowsByPath == nil {
		s.rowsByPath = make(map[string]semdb.NoteMetadataRow, len(rows))
	}
	for path, row := range rows {
		s.rowsByPath[path] = row
	}
	return rows, nil
}

func evalPlannedExpressionSet(ctx context.Context, state *storePlannerState, expr *plannedExpression, candidate map[string]struct{}) (map[string]struct{}, bool, error) {
	if expr == nil {
		return map[string]struct{}{}, true, nil
	}
	switch expr.Type {
	case exprLeaf:
		return evalPlannedLeafSet(ctx, state, expr, candidate)
	case exprAnd:
		leftFirst, rightSecond := expr.Left, expr.Right
		if leafSelectivityRank(expr.Right) < leafSelectivityRank(expr.Left) {
			leftFirst, rightSecond = expr.Right, expr.Left
		}
		left, ok, err := evalPlannedExpressionSet(ctx, state, leftFirst, candidate)
		if err != nil || !ok {
			return nil, ok, err
		}
		return evalPlannedExpressionSet(ctx, state, rightSecond, left)
	case exprOr:
		left, ok, err := evalPlannedExpressionSet(ctx, state, expr.Left, candidate)
		if err != nil || !ok {
			return nil, ok, err
		}
		remaining := candidate
		if candidate != nil {
			remaining = subtractPathSet(candidate, left)
		}
		right, ok, err := evalPlannedExpressionSet(ctx, state, expr.Right, remaining)
		if err != nil || !ok {
			return nil, ok, err
		}
		return unionPathSets(left, right), true, nil
	case exprNot:
		base := candidate
		if base == nil {
			base = state.universe
		}
		child, ok, err := evalPlannedExpressionSet(ctx, state, expr.Left, base)
		if err != nil || !ok {
			return nil, ok, err
		}
		return subtractPathSet(base, child), true, nil
	default:
		return nil, false, nil
	}
}

func evalPlannedLeafSet(ctx context.Context, state *storePlannerState, expr *plannedExpression, candidate map[string]struct{}) (map[string]struct{}, bool, error) {
	if expr == nil || expr.Input == nil {
		return map[string]struct{}{}, true, nil
	}
	var (
		paths []string
		err   error
		ok    = true
	)
	switch expr.Strategy {
	case plannedLeafUniverse:
		if candidate != nil {
			return clonePathSet(candidate), true, nil
		}
		return clonePathSet(state.universe), true, nil
	case plannedLeafPath:
		paths, err = state.store.CurrentNotePathsByPathPrefix(ctx, expr.Input.Value)
	case plannedLeafTag:
		paths, err = state.store.CurrentNotePathsByTag(ctx, strings.ToLower(strings.TrimSpace(strings.TrimPrefix(expr.Input.Value, "#"))))
	case plannedLeafProperty:
		paths, err = state.store.CurrentNotePathsByPropertyValue(ctx, strings.ToLower(strings.TrimSpace(expr.Input.Property)), normalizePropertyValue(expr.Input.Value), 0)
	case plannedLeafIndexedFind:
		var indexed bool
		paths, indexed, err = state.store.CurrentNotePathsByFindPattern(ctx, expr.Input.Value)
		ok = indexed
	case plannedLeafFallbackFind:
		ok = false
	default:
		ok = false
	}
	if err != nil {
		return nil, false, err
	}
	if !ok {
		fallbackBase := candidate
		if fallbackBase == nil {
			fallbackBase = state.universe
		}
		return evalFallbackFindSet(ctx, state, expr.Input, fallbackBase)
	}

	result := sliceToPathSet(paths)
	if candidate != nil {
		result = intersectPathSets(result, candidate)
	}
	if expr.Strategy == plannedLeafIndexedFind {
		return validateFindCandidates(ctx, state, expr.Input.Value, result, candidate)
	}
	return result, true, nil
}

func evalFallbackFindSet(ctx context.Context, state *storePlannerState, input *ListInput, base map[string]struct{}) (map[string]struct{}, bool, error) {
	rows, err := state.metadataRows(ctx, base)
	if err != nil {
		return nil, false, err
	}
	out := make(map[string]struct{})
	for path := range base {
		row := rows[path]
		if obsidian.FuzzyMatch(input.Value, path) || obsidian.FuzzyMatch(input.Value, row.Title) {
			out[path] = struct{}{}
		}
	}
	if input != nil && input.Type == InputTypeFind {
		if token := strings.TrimSpace(input.Value); token != "" {
			aliasPaths, err := state.store.CurrentNotePathsByPropertyValue(ctx, "aliases", normalizePropertyValue(token), semdb.NotePropertySourceFrontmatter)
			if err == nil {
				for _, path := range aliasPaths {
					if candidateAllows(base, path) {
						out[path] = struct{}{}
					}
				}
			}
		}
	}
	return out, true, nil
}

func validateFindCandidates(ctx context.Context, state *storePlannerState, pattern string, candidates map[string]struct{}, outerCandidate map[string]struct{}) (map[string]struct{}, bool, error) {
	rows, err := state.metadataRows(ctx, candidates)
	if err != nil {
		return nil, false, err
	}
	out := make(map[string]struct{}, len(candidates))
	for path := range candidates {
		row := rows[path]
		if obsidian.FuzzyMatch(pattern, path) || obsidian.FuzzyMatch(pattern, row.Title) {
			out[path] = struct{}{}
		}
	}
	if token := strings.TrimSpace(pattern); token != "" {
		aliasPaths, err := state.store.CurrentNotePathsByPropertyValue(ctx, "aliases", normalizePropertyValue(token), semdb.NotePropertySourceFrontmatter)
		if err == nil {
			for _, path := range aliasPaths {
				if candidateAllows(outerCandidate, path) {
					out[path] = struct{}{}
				}
			}
		}
	}
	return out, true, nil
}

func leafSelectivityRank(expr *plannedExpression) int {
	if expr == nil {
		return 100
	}
	if expr.Type != exprLeaf {
		switch expr.Type {
		case exprNot:
			return 90
		case exprOr:
			return 80
		default:
			return 70
		}
	}
	switch expr.Strategy {
	case plannedLeafPath:
		return 1
	case plannedLeafTag:
		return 2
	case plannedLeafProperty:
		return 3
	case plannedLeafIndexedFind:
		return 4
	case plannedLeafFallbackFind:
		return 10
	case plannedLeafUniverse:
		return 100
	default:
		return 50
	}
}

func sliceToPathSet(paths []string) map[string]struct{} {
	out := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		out[path] = struct{}{}
	}
	return out
}

func clonePathSet(paths map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(paths))
	for path := range paths {
		out[path] = struct{}{}
	}
	return out
}

func pathSetToSortedSlice(paths map[string]struct{}) []string {
	out := make([]string, 0, len(paths))
	for path := range paths {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func candidateAllows(candidate map[string]struct{}, path string) bool {
	if candidate == nil {
		return true
	}
	_, ok := candidate[path]
	return ok
}

func stringValue(value string) string {
	return strings.TrimSpace(value)
}
