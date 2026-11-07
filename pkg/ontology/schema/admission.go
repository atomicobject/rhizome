package schema

import (
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
)

// Report our role code before GraphQL rejects @admit's OBJECT-only location.
func validateAdmissionLocations(sources []*ast.Source) error {
	doc, err := parser.ParseSchemas(sources...)
	if err != nil {
		return err
	}
	for _, defs := range []ast.DefinitionList{doc.Definitions, doc.Extensions} {
		for _, def := range defs {
			if dir := def.Directives.ForName("admit"); dir != nil && def.Kind != ast.Object {
				return compileError(dir.Position, "admit_node_role", def.Name, "", "@admit requires a file-backed @node type")
			}
		}
	}
	return nil
}

func compileAdmission(def *ast.Definition) ([]*NoteMatcher, error) {
	dir := def.Directives.ForName("admit")
	if dir == nil {
		return nil, nil
	}
	count := 0
	for _, directive := range def.Directives {
		if directive.Name == "admit" {
			count++
		}
	}
	if count > 1 {
		return nil, compileError(dir.Position, "duplicate_admit", def.Name, "", "@admit may appear only once per type, including extensions")
	}
	node := def.Directives.ForName("node")
	if node == nil || stringArg(node.ArgumentMap(nil)["locator"], "FILE") != "FILE" || isRecordDefinition(def) {
		return nil, compileError(dir.Position, "admit_node_role", def.Name, "", "@admit requires a file-backed @node type")
	}
	matches, err := stringListArg(dir.ArgumentMap(nil)["matches"])
	if err != nil {
		return nil, compileError(dir.Position, "invalid_admit_matcher", def.Name, "", "invalid @admit matches: %v", err)
	}
	matchers, err := compileNoteMatchers(nil, matches)
	if err != nil {
		return nil, compileError(dir.Position, "invalid_admit_matcher", def.Name, "", "invalid @admit matcher: %v", err)
	}
	return matchers, nil
}

// Admits applies the optional condition independently of selectors, including
// the selector-free default type's fallback admission.
func (t *NoteType) Admits(doc *Subject) bool {
	if t == nil || doc == nil {
		return false
	}
	if t.AdmissionMatchers == nil {
		return true
	}
	for _, matcher := range t.AdmissionMatchers {
		if matcher.Matches(doc) {
			return true
		}
	}
	return false
}
