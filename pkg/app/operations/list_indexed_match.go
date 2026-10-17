package operations

import (
	"context"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/notemeta"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func evaluateExpressionMatchesWithStore(ctx context.Context, store *semdb.Store, expr *InputExpression, candidatePaths []string) ([]string, error) {
	if expr == nil || store == nil {
		return nil, nil
	}
	planned := buildPlannedExpression(expr)
	// Prefer the store-backed planner so broad boolean/find queries can shrink to
	// indexed candidate sets before we touch note metadata rows in Go.
	if matches, ok, err := evaluatePlannedExpressionMatchesWithStore(ctx, store, planned, candidatePaths); err != nil {
		return nil, err
	} else if ok {
		return matches, nil
	}
	pathsList := candidatePaths
	var err error
	if pathsList == nil {
		pathsList, err = store.CurrentNoteMetadataPaths(ctx)
		if err != nil {
			return nil, err
		}
	}
	rowsByPath := map[string]semdb.NoteMetadataRow{}
	if expressionNeedsRows(expr) {
		rowsByPath, err = store.CurrentNoteMetadataRowsByPaths(ctx, pathsList)
		if err != nil {
			return nil, err
		}
	}
	universe := make(map[string]struct{}, len(pathsList))
	for _, path := range pathsList {
		universe[path] = struct{}{}
	}
	matches, err := evalExpressionSet(ctx, store, expr, rowsByPath, universe)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(matches))
	for path := range matches {
		out = append(out, path)
	}
	sort.Strings(out)
	return out, nil
}

func expressionNeedsRows(expr *InputExpression) bool {
	if expr == nil {
		return false
	}
	switch expr.Type {
	case exprLeaf:
		return expr.Input != nil && (expr.Input.Type == InputTypeFind || expr.Input.Type == InputTypeFile)
	case exprAnd, exprOr:
		return expressionNeedsRows(expr.Left) || expressionNeedsRows(expr.Right)
	case exprNot:
		return expressionNeedsRows(expr.Left)
	default:
		return false
	}
}

func evalExpressionSet(ctx context.Context, store *semdb.Store, expr *InputExpression, rowsByPath map[string]semdb.NoteMetadataRow, universe map[string]struct{}) (map[string]struct{}, error) {
	if expr == nil {
		return map[string]struct{}{}, nil
	}
	switch expr.Type {
	case exprLeaf:
		return evalLeafSet(ctx, store, expr.Input, rowsByPath, universe), nil
	case exprAnd:
		left, err := evalExpressionSet(ctx, store, expr.Left, rowsByPath, universe)
		if err != nil {
			return nil, err
		}
		right, err := evalExpressionSet(ctx, store, expr.Right, rowsByPath, universe)
		if err != nil {
			return nil, err
		}
		return intersectPathSets(left, right), nil
	case exprOr:
		left, err := evalExpressionSet(ctx, store, expr.Left, rowsByPath, universe)
		if err != nil {
			return nil, err
		}
		right, err := evalExpressionSet(ctx, store, expr.Right, rowsByPath, universe)
		if err != nil {
			return nil, err
		}
		return unionPathSets(left, right), nil
	case exprNot:
		child, err := evalExpressionSet(ctx, store, expr.Left, rowsByPath, universe)
		if err != nil {
			return nil, err
		}
		return subtractPathSet(universe, child), nil
	default:
		return map[string]struct{}{}, nil
	}
}

func evalLeafSet(ctx context.Context, store *semdb.Store, input *ListInput, rowsByPath map[string]semdb.NoteMetadataRow, universe map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{})
	if input == nil {
		return out
	}
	switch input.Type {
	case InputTypeFile:
		for path := range universe {
			if matchFilePath(path, input.Value) {
				out[path] = struct{}{}
			}
		}
	case InputTypeFind:
		for path, row := range rowsByPath {
			if obsidian.FuzzyMatch(input.Value, path) || obsidian.FuzzyMatch(input.Value, row.Title) {
				out[path] = struct{}{}
			}
		}
		// Alias shortcut: a `find:` token that matches a note's
		// frontmatter alias (e.g. `find:SPEC-001`) resolves directly to
		// the owning note even when fuzzy matching would miss the long
		// filename. The DB stores aliases under `value_norm`
		// (lowercased + trimmed by notemeta.NormalizePropertyValue at index
		// time), so we normalize the token the same way before the
		// lookup — otherwise mixed-case inputs like `SPEC-001` never
		// hit alias rows. This is a find/search shortcut only; wikilink
		// target resolution follows Obsidian and does not resolve aliases.
		if token := strings.TrimSpace(input.Value); token != "" {
			if aliasPaths, err := store.CurrentNotePathsByPropertyValue(ctx, "aliases", notemeta.NormalizePropertyValue(token), semdb.NotePropertySourceFrontmatter); err == nil {
				for _, path := range aliasPaths {
					out[path] = struct{}{}
				}
			}
		}
	case InputTypeTag:
		if pathsList, err := store.CurrentNotePathsByTag(ctx, strings.ToLower(strings.TrimSpace(strings.TrimPrefix(input.Value, "#")))); err == nil {
			for _, path := range pathsList {
				out[path] = struct{}{}
			}
		}
	case InputTypeProperty:
		if pathsList, err := store.CurrentNotePathsByPropertyValue(ctx, strings.ToLower(strings.TrimSpace(input.Property)), notemeta.NormalizePropertyValue(input.Value), 0); err == nil {
			for _, path := range pathsList {
				out[path] = struct{}{}
			}
		}
	}
	return out
}

func unionPathSets(left, right map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(left)+len(right))
	for path := range left {
		out[path] = struct{}{}
	}
	for path := range right {
		out[path] = struct{}{}
	}
	return out
}

func intersectPathSets(left, right map[string]struct{}) map[string]struct{} {
	if len(left) > len(right) {
		left, right = right, left
	}
	out := make(map[string]struct{})
	for path := range left {
		if _, ok := right[path]; ok {
			out[path] = struct{}{}
		}
	}
	return out
}

func subtractPathSet(universe, remove map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(universe))
	for path := range universe {
		if _, ok := remove[path]; ok {
			continue
		}
		out[path] = struct{}{}
	}
	return out
}
