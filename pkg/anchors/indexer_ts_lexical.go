//go:build cgo

package codeanchor

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

type tsLexicalScope struct {
	parent    *tsLexicalScope
	path      []string
	bindings  map[string][]SymbolRef
	ambiguous map[string]struct{}
}

func (s *tsLexicalScope) declare(name string, refs []SymbolRef) {
	if name == "" || len(refs) == 0 {
		return
	}
	if _, exists := s.bindings[name]; exists {
		delete(s.bindings, name)
		s.ambiguous[name] = struct{}{}
		return
	}
	if _, ambiguous := s.ambiguous[name]; ambiguous {
		return
	}
	s.bindings[name] = refs
}

func (s *tsLexicalScope) lookup(name string) ([]SymbolRef, bool) {
	for current := s; current != nil; current = current.parent {
		if _, ambiguous := current.ambiguous[name]; ambiguous {
			return nil, false
		}
		if refs, ok := current.bindings[name]; ok {
			return refs, true
		}
	}
	return nil, false
}

func (s *tsLexicalScope) blocked(name string) bool {
	for current := s; current != nil; current = current.parent {
		if _, ambiguous := current.ambiguous[name]; ambiguous {
			return true
		}
		if _, declared := current.bindings[name]; declared {
			return false
		}
	}
	return false
}

func (t *TSIndexer) analyzeTSLexicalRelationships(root *sitter.Node, content []byte, currentPkg, filePath string, imports *tsImportBindings) []Symbol {
	if root == nil || imports == nil {
		return nil
	}
	program := &tsLexicalScope{bindings: make(map[string][]SymbolRef), ambiguous: make(map[string]struct{})}
	return t.analyzeTSLexicalScope(root, content, currentPkg, filePath, imports, program)
}

func (t *TSIndexer) analyzeTSLexicalScope(root *sitter.Node, content []byte, currentPkg, filePath string, imports *tsImportBindings, scope *tsLexicalScope) []Symbol {
	var symbols []Symbol
	type symbolCandidate struct {
		name string
		ref  SymbolRef
		node *sitter.Node
	}
	var candidates []symbolCandidate

	// First collect callable declarations so calls are independent of declaration order.
	tsWalkCurrentLexicalScope(root, func(node *sitter.Node) {
		name, callable := tsLexicalCallableDeclaration(node, content)
		if !callable {
			return
		}
		qualified := strings.Join(append(append([]string{}, scope.path...), name), ".")
		ref := SymbolRef{Lang: LangTS, Pkg: currentPkg, Name: qualified}
		scope.declare(name, []SymbolRef{ref})
		if len(scope.path) > 0 {
			candidates = append(candidates, symbolCandidate{name: name, ref: ref, node: node})
		}
	})
	for _, candidate := range candidates {
		refs, ok := scope.bindings[candidate.name]
		if !ok || len(refs) != 1 || refs[0] != candidate.ref {
			continue
		}
		sym := Symbol{Lang: LangTS, Kind: SymFunc, File: filePath, Pkg: currentPkg, Name: candidate.ref.Name}
		fillTSSymbolMeta(&sym, candidate.node, content)
		symbols = append(symbols, sym)
	}

	// Then collect aliases whose expressions have one or more statically known branches.
	tsWalkCurrentLexicalScope(root, func(node *sitter.Node) {
		if node.Kind() != "variable_declarator" {
			return
		}
		name := tsStableBindingName(node.ChildByFieldName("name"), content)
		value := node.ChildByFieldName("value")
		if name == "" || value == nil || tsIsFunctionNode(value) {
			return
		}
		if refs := t.tsKnownLexicalCandidates(value, content, currentPkg, *imports, scope); len(refs) > 0 {
			scope.declare(name, refs)
		}
	})

	// Record exact targets for calls in this scope. An entry suppresses generic
	// current-package fallback, which is essential for partial alias branches.
	tsWalkCurrentLexicalScope(root, func(node *sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		fn := node.ChildByFieldName("function")
		if fn == nil {
			return
		}
		if refs := t.tsKnownLexicalCandidates(fn, content, currentPkg, *imports, scope); len(refs) > 0 {
			imports.lexicalCalls[fn.StartByte()] = refs
		} else if fn.Kind() == "identifier" && scope.blocked(strings.TrimSpace(nodeText(fn, content))) {
			imports.lexicalCalls[fn.StartByte()] = nil
		}
	})

	// Function bodies create child lexical scopes with scope-qualified identities.
	tsWalkCurrentLexicalScope(root, func(node *sitter.Node) {
		name, callable := tsLexicalCallableDeclaration(node, content)
		if !callable {
			return
		}
		body := tsLexicalCallableBody(node)
		if body == nil {
			return
		}
		child := &tsLexicalScope{
			parent:    scope,
			path:      append(append([]string{}, scope.path...), name),
			bindings:  make(map[string][]SymbolRef),
			ambiguous: make(map[string]struct{}),
		}
		symbols = append(symbols, t.analyzeTSLexicalScope(body, content, currentPkg, filePath, imports, child)...)
	})

	return symbols
}

func tsWalkCurrentLexicalScope(root *sitter.Node, visit func(*sitter.Node)) {
	var walk func(*sitter.Node, bool)
	walk = func(node *sitter.Node, isRoot bool) {
		if node == nil {
			return
		}
		visit(node)
		if !isRoot && tsIsLexicalCallableDeclarationNode(node) {
			return
		}
		for i := uint(0); i < node.NamedChildCount(); i++ {
			walk(node.NamedChild(i), false)
		}
	}
	walk(root, true)
}

func tsIsLexicalCallableDeclarationNode(node *sitter.Node) bool {
	if node == nil {
		return false
	}
	if node.Kind() == "function_declaration" {
		return true
	}
	return node.Kind() == "variable_declarator" && tsIsFunctionNode(node.ChildByFieldName("value"))
}

func tsLexicalCallableDeclaration(node *sitter.Node, content []byte) (string, bool) {
	if node == nil {
		return "", false
	}
	switch node.Kind() {
	case "function_declaration":
		name := tsStableBindingName(node.ChildByFieldName("name"), content)
		return name, name != ""
	case "variable_declarator":
		value := node.ChildByFieldName("value")
		if !tsIsFunctionNode(value) {
			return "", false
		}
		name := tsStableBindingName(node.ChildByFieldName("name"), content)
		return name, name != ""
	default:
		return "", false
	}
}

func tsIsFunctionNode(node *sitter.Node) bool {
	if node == nil {
		return false
	}
	switch node.Kind() {
	case "arrow_function", "function", "function_expression":
		return true
	default:
		return false
	}
}

func tsLexicalCallableBody(node *sitter.Node) *sitter.Node {
	if node == nil {
		return nil
	}
	if node.Kind() == "variable_declarator" {
		node = node.ChildByFieldName("value")
	}
	if node == nil {
		return nil
	}
	if body := node.ChildByFieldName("body"); body != nil {
		return body
	}
	return node
}

func (t *TSIndexer) tsKnownLexicalCandidates(node *sitter.Node, content []byte, currentPkg string, imports tsImportBindings, scope *tsLexicalScope) []SymbolRef {
	node = tsUnwrapExpr(node)
	if node == nil {
		return nil
	}
	var refs []SymbolRef
	switch node.Kind() {
	case "identifier":
		name := strings.TrimSpace(nodeText(node, content))
		if local, ok := scope.lookup(name); ok {
			refs = local
		} else if imported, ok := imports.idents[name]; ok {
			refs = []SymbolRef{imported}
		}
	case "member_expression":
		if ref, ok := t.resolveTSMemberSymbol(node, content, currentPkg, imports); ok {
			refs = []SymbolRef{ref}
		}
	case "ternary_expression", "conditional_expression":
		refs = append(refs, t.tsKnownLexicalCandidates(node.ChildByFieldName("consequence"), content, currentPkg, imports, scope)...)
		refs = append(refs, t.tsKnownLexicalCandidates(node.ChildByFieldName("alternative"), content, currentPkg, imports, scope)...)
	case "binary_expression":
		switch tsBinaryOperator(node) {
		case "??", "||":
			refs = append(refs, t.tsKnownLexicalCandidates(node.ChildByFieldName("left"), content, currentPkg, imports, scope)...)
			refs = append(refs, t.tsKnownLexicalCandidates(node.ChildByFieldName("right"), content, currentPkg, imports, scope)...)
		case "&&":
			refs = append(refs, t.tsKnownLexicalCandidates(node.ChildByFieldName("right"), content, currentPkg, imports, scope)...)
		}
	}
	return dedupeTSSymbolRefs(refs)
}

func dedupeTSSymbolRefs(refs []SymbolRef) []SymbolRef {
	seen := make(map[SymbolRef]struct{}, len(refs))
	out := make([]SymbolRef, 0, len(refs))
	for _, ref := range refs {
		if ref.Pkg == "" || ref.Name == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func tsMethodIsStatic(node *sitter.Node, content []byte) bool {
	if node == nil || node.Kind() != "method_definition" {
		return false
	}
	raw := strings.TrimSpace(nodeText(node, content))
	return strings.HasPrefix(raw, "static ") || strings.HasPrefix(raw, "public static ") || strings.HasPrefix(raw, "private static ") || strings.HasPrefix(raw, "protected static ")
}

func tsNameRelativeToPkg(pkg, fqn string) string {
	prefix := strings.TrimSuffix(pkg, ".") + "."
	return strings.TrimPrefix(fqn, prefix)
}
