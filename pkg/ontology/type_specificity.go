package ontology

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

type selectorAtomKind string

const (
	selectorAtomPath     selectorAtomKind = "path"
	selectorAtomTag      selectorAtomKind = "tag"
	selectorAtomProperty selectorAtomKind = "property"
	selectorAtomFind     selectorAtomKind = "find"
)

type selectorAtom struct {
	Kind     selectorAtomKind
	Raw      string
	Path     string
	Property string
	Value    string
}

type selectorClause struct {
	Raw        string
	Atoms      []selectorAtom
	Comparable bool
}

func mostSpecificCandidate(doc *noteDoc, candidateNames []string, schema *Schema) string {
	if doc == nil || schema == nil || len(candidateNames) < 2 {
		return ""
	}

	clausesByCandidate := make(map[string][]selectorClause, len(candidateNames))
	for _, name := range candidateNames {
		noteType := schema.Types[name]
		if noteType == nil {
			continue
		}
		clausesByCandidate[name] = matchingSelectorClauses(noteType, doc)
	}

	winners := make([]string, 0, len(candidateNames))
	for _, left := range candidateNames {
		dominatesAll := true
		for _, right := range candidateNames {
			if left == right {
				continue
			}
			if !candidateStrictlyRefines(clausesByCandidate[left], clausesByCandidate[right]) {
				dominatesAll = false
				break
			}
		}
		if dominatesAll {
			winners = append(winners, left)
		}
	}
	if len(winners) != 1 {
		return ""
	}
	return winners[0]
}

func matchingSelectorClauses(noteType *NoteType, doc *noteDoc) []selectorClause {
	if noteType == nil || doc == nil {
		return nil
	}
	out := make([]selectorClause, 0, len(noteType.Paths)+len(noteType.Matches))
	for _, pattern := range noteType.Paths {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		ok, err := doublestar.Match(pattern, filepath.ToSlash(doc.Path))
		if err != nil || !ok {
			continue
		}
		out = append(out, selectorClause{
			Raw:        pattern,
			Comparable: true,
			Atoms: []selectorAtom{{
				Kind: selectorAtomPath,
				Raw:  pattern,
				Path: pattern,
			}},
		})
	}
	for _, raw := range noteType.Matches {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		expr, err := parseMatchExpression(raw)
		if err != nil {
			continue
		}
		clauses, comparable := selectorClausesFromExpression(expr)
		if len(clauses) == 0 {
			continue
		}
		for _, clause := range clauses {
			if !selectorClauseMatchesDoc(clause, doc) {
				continue
			}
			clause.Raw = raw
			clause.Comparable = comparable && clause.Comparable
			out = append(out, clause)
		}
	}
	return out
}

func selectorClausesFromExpression(expr *matchExpression) ([]selectorClause, bool) {
	if expr == nil {
		return nil, false
	}
	switch expr.Type {
	case matchExprLeaf:
		atom, ok := selectorAtomFromInput(expr.Input)
		if !ok {
			return []selectorClause{{Comparable: false}}, false
		}
		return []selectorClause{{
			Comparable: true,
			Atoms:      []selectorAtom{atom},
		}}, true
	case matchExprOr:
		left, leftComparable := selectorClausesFromExpression(expr.Left)
		right, rightComparable := selectorClausesFromExpression(expr.Right)
		return append(left, right...), leftComparable && rightComparable
	case matchExprAnd:
		left, leftComparable := selectorClausesFromExpression(expr.Left)
		right, rightComparable := selectorClausesFromExpression(expr.Right)
		if len(left) == 0 || len(right) == 0 {
			return nil, leftComparable && rightComparable
		}
		out := make([]selectorClause, 0, len(left)*len(right))
		for _, l := range left {
			for _, r := range right {
				clause := selectorClause{
					Comparable: l.Comparable && r.Comparable,
					Atoms:      append(append([]selectorAtom{}, l.Atoms...), r.Atoms...),
				}
				out = append(out, clause)
			}
		}
		return out, leftComparable && rightComparable
	case matchExprNot:
		return []selectorClause{{Comparable: false}}, false
	default:
		return nil, false
	}
}

func selectorAtomFromInput(input *matchInput) (selectorAtom, bool) {
	if input == nil {
		return selectorAtom{}, false
	}
	switch input.Type {
	case matchInputFile:
		return selectorAtom{
			Kind: selectorAtomPath,
			Raw:  input.Value,
			Path: input.Value,
		}, true
	case matchInputTag:
		return selectorAtom{
			Kind:  selectorAtomTag,
			Raw:   input.Value,
			Value: strings.ToLower(strings.TrimSpace(strings.TrimPrefix(input.Value, "#"))),
		}, true
	case matchInputProperty:
		return selectorAtom{
			Kind:     selectorAtomProperty,
			Raw:      input.Property + ":" + input.Value,
			Property: strings.ToLower(strings.TrimSpace(input.Property)),
			Value:    normalizeMatchPropertyValue(input.Value),
		}, true
	case matchInputFind:
		return selectorAtom{
			Kind:  selectorAtomFind,
			Raw:   input.Value,
			Value: strings.ToLower(strings.TrimSpace(input.Value)),
		}, true
	default:
		return selectorAtom{}, false
	}
}

func selectorClauseMatchesDoc(clause selectorClause, doc *noteDoc) bool {
	for _, atom := range clause.Atoms {
		if !selectorAtomMatchesDoc(atom, doc) {
			return false
		}
	}
	return true
}

func selectorAtomMatchesDoc(atom selectorAtom, doc *noteDoc) bool {
	switch atom.Kind {
	case selectorAtomPath:
		if containsGlob(atom.Path) {
			ok, err := doublestar.Match(atom.Path, filepath.ToSlash(doc.Path))
			return err == nil && ok
		}
		return matchSelectorPath(doc.Path, atom.Path)
	case selectorAtomTag:
		return evalMatchInput(&matchInput{Type: matchInputTag, Value: atom.Value}, doc)
	case selectorAtomProperty:
		return docPropertyHasValue(doc, atom.Property, atom.Value)
	case selectorAtomFind:
		return evalMatchInput(&matchInput{Type: matchInputFind, Value: atom.Value}, doc)
	default:
		return false
	}
}

func candidateStrictlyRefines(leftClauses, rightClauses []selectorClause) bool {
	if len(leftClauses) == 0 || len(rightClauses) == 0 {
		return false
	}
	anyComparable := false
	for _, left := range leftClauses {
		if !left.Comparable {
			return false
		}
		anyComparable = true
		leftStrict := false
		for _, right := range rightClauses {
			if !right.Comparable {
				continue
			}
			if selectorClauseStrictlyRefines(left, right) {
				leftStrict = true
				break
			}
		}
		if !leftStrict {
			return false
		}
	}
	return anyComparable
}

func selectorClauseStrictlyRefines(left, right selectorClause) bool {
	if !left.Comparable || !right.Comparable {
		return false
	}
	used := make([]bool, len(left.Atoms))
	strict := false
	for _, rightAtom := range right.Atoms {
		matchIndex := -1
		matchStrict := false
		for i, leftAtom := range left.Atoms {
			if used[i] {
				continue
			}
			covered, isStrict := selectorAtomCovers(leftAtom, rightAtom)
			if !covered {
				continue
			}
			matchIndex = i
			matchStrict = isStrict
			break
		}
		if matchIndex == -1 {
			return false
		}
		used[matchIndex] = true
		strict = strict || matchStrict
	}
	if strict {
		return true
	}
	return len(left.Atoms) > len(right.Atoms)
}

func selectorAtomCovers(left, right selectorAtom) (bool, bool) {
	if left.Kind != right.Kind {
		return false, false
	}
	switch left.Kind {
	case selectorAtomPath:
		if left.Path == right.Path {
			return true, false
		}
		if pathSelectorStrictlyRefines(left.Path, right.Path) {
			return true, true
		}
		return false, false
	case selectorAtomTag:
		return left.Value == right.Value, false
	case selectorAtomProperty:
		if left.Property != right.Property {
			return false, false
		}
		return left.Value == right.Value, false
	case selectorAtomFind:
		return left.Value == right.Value, false
	default:
		return false, false
	}
}

func pathSelectorStrictlyRefines(left, right string) bool {
	left = filepath.ToSlash(strings.TrimSpace(left))
	right = filepath.ToSlash(strings.TrimSpace(right))
	if left == "" || right == "" || left == right {
		return false
	}

	leftPrefix := selectorFixedPrefix(left)
	rightPrefix := selectorFixedPrefix(right)
	if leftPrefix == "" || rightPrefix == "" || leftPrefix == rightPrefix {
		return false
	}

	if !strings.HasPrefix(leftPrefix, rightPrefix) {
		return false
	}
	if strings.HasSuffix(rightPrefix, "/") {
		return true
	}
	return strings.HasPrefix(leftPrefix, rightPrefix+"/")
}

func selectorFixedPrefix(raw string) string {
	raw = filepath.ToSlash(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	if !containsGlob(raw) {
		if filepath.Ext(raw) != "" {
			return raw
		}
		if strings.HasSuffix(raw, "/") {
			return raw
		}
		return raw + "/"
	}
	idx := strings.IndexFunc(raw, func(r rune) bool {
		return slices.Contains([]rune{'*', '?', '[', '{'}, r)
	})
	if idx == -1 {
		return raw
	}
	prefix := raw[:idx]
	if prefix == "" {
		return ""
	}
	if strings.LastIndex(prefix, "/") >= 0 {
		return prefix[:strings.LastIndex(prefix, "/")+1]
	}
	return prefix
}

func containsGlob(raw string) bool {
	return strings.ContainsAny(raw, "*?[{")
}
