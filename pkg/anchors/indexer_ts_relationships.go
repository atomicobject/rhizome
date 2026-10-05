//go:build cgo

package codeanchor

import (
	"path/filepath"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (t *TSIndexer) appendTSModuleImport(fileDir, module string, out *tsImportBindings) {
	if resolved, ok := t.resolveModuleFile(fileDir, module); ok {
		out.importPaths = append(out.importPaths, resolved)
	}
}

func tsLiteralModuleCall(call *sitter.Node, content []byte) (string, bool) {
	if call == nil || call.Kind() != "call_expression" {
		return "", false
	}
	fn := call.ChildByFieldName("function")
	if fn == nil && call.NamedChildCount() > 0 {
		fn = call.NamedChild(0)
	}
	callee := strings.TrimSpace(nodeText(fn, content))
	if callee != "import" && callee != "require" {
		return "", false
	}
	args := call.ChildByFieldName("arguments")
	if args == nil {
		for i := uint(0); i < call.NamedChildCount(); i++ {
			if child := call.NamedChild(i); child != nil && child.Kind() == "arguments" {
				args = child
				break
			}
		}
	}
	if args == nil || args.NamedChildCount() != 1 {
		return "", false
	}
	arg := args.NamedChild(0)
	if arg == nil || arg.Kind() != "string" {
		return "", false
	}
	module := trimStringLiteral(nodeText(arg, content))
	return module, module != ""
}

func tsModuleCallFromValue(value *sitter.Node, content []byte) (*sitter.Node, string, bool) {
	for value != nil {
		switch value.Kind() {
		case "await_expression", "parenthesized_expression", "non_null_expression", "as_expression", "satisfies_expression":
			if expr := value.ChildByFieldName("expression"); expr != nil {
				value = expr
				continue
			}
			if value.NamedChildCount() > 0 {
				value = value.NamedChild(0)
				continue
			}
		}
		break
	}
	module, ok := tsLiteralModuleCall(value, content)
	return value, module, ok
}

func (t *TSIndexer) applyTSModuleBinding(decl *sitter.Node, content []byte, fileDir string, out *tsImportBindings) {
	if decl == nil {
		return
	}
	_, module, ok := tsModuleCallFromValue(decl.ChildByFieldName("value"), content)
	if !ok {
		return
	}
	target, ok := t.resolveModuleFile(fileDir, module)
	if !ok {
		return
	}
	pkg := t.pkgForFile(target)
	defaultName := strings.TrimSuffix(filepath.Base(target), filepath.Ext(target))
	name := decl.ChildByFieldName("name")
	if name == nil {
		return
	}
	switch name.Kind() {
	case "identifier":
		alias := strings.TrimSpace(nodeText(name, content))
		if alias != "" {
			ref, resolved := t.resolveTSBarrelExport(target, "default")
			if !resolved {
				ref = SymbolRef{Lang: LangTS, Pkg: pkg, Name: defaultName}
			}
			out.idents[alias] = ref
			out.namespaces[alias] = pkg
		}
	case "object_pattern":
		for _, binding := range tsDestructuredModuleBindings(name, content) {
			if ref, resolved := t.resolveTSBarrelExport(target, binding.exported); resolved {
				out.idents[binding.local] = ref
			} else {
				out.idents[binding.local] = SymbolRef{Lang: LangTS, Pkg: pkg, Name: binding.exported}
			}
		}
	}
}

type tsModuleBinding struct {
	exported string
	local    string
}

func tsDestructuredModuleBindings(pattern *sitter.Node, content []byte) []tsModuleBinding {
	var out []tsModuleBinding
	if pattern == nil || pattern.Kind() != "object_pattern" {
		return out
	}
	for i := uint(0); i < pattern.NamedChildCount(); i++ {
		child := pattern.NamedChild(i)
		if child == nil {
			continue
		}
		var exported, local string
		switch child.Kind() {
		case "shorthand_property_identifier_pattern", "identifier":
			exported = strings.TrimSpace(nodeText(child, content))
			local = exported
		case "pair_pattern", "pair":
			key := child.ChildByFieldName("key")
			if key == nil {
				key = child.ChildByFieldName("property")
			}
			value := child.ChildByFieldName("value")
			exported = strings.TrimSpace(nodeText(key, content))
			local = strings.TrimSpace(nodeText(value, content))
			if exported == "" || local == "" {
				names := tsDirectIdentifiers(child, content)
				if len(names) > 0 {
					exported, local = names[0], names[len(names)-1]
				}
			}
		}
		if isTSStableName(exported) && isTSStableName(local) {
			out = append(out, tsModuleBinding{exported: exported, local: local})
		}
	}
	return out
}

func (t *TSIndexer) resolveTSCallableCandidates(node *sitter.Node, content []byte, currentPkg string, imports tsImportBindings) []SymbolRef {
	node = tsUnwrapExpr(node)
	if node == nil {
		return nil
	}
	switch node.Kind() {
	case "ternary_expression", "conditional_expression":
		return t.resolveTSCallableBranches(node, content, currentPkg, imports, "consequence", "alternative")
	case "binary_expression":
		switch tsBinaryOperator(node) {
		case "??", "||":
			return t.resolveTSCallableBranches(node, content, currentPkg, imports, "left", "right")
		case "&&":
			return t.resolveTSCallableBranches(node, content, currentPkg, imports, "right")
		}
	}
	if ref, ok := t.resolveTSCallTarget(node, content, currentPkg, imports); ok {
		return []SymbolRef{ref}
	}
	return nil
}

func tsBinaryOperator(node *sitter.Node) string {
	if node == nil || node.Kind() != "binary_expression" {
		return ""
	}
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil || child.IsNamed() {
			continue
		}
		switch child.Kind() {
		case "??", "||", "&&":
			return child.Kind()
		}
	}
	return ""
}

func (t *TSIndexer) resolveTSCallableBranches(node *sitter.Node, content []byte, currentPkg string, imports tsImportBindings, fields ...string) []SymbolRef {
	var out []SymbolRef
	seen := make(map[SymbolRef]struct{})
	for _, field := range fields {
		branch := node.ChildByFieldName(field)
		for _, ref := range t.resolveTSCallableCandidates(branch, content, currentPkg, imports) {
			if _, ok := seen[ref]; ok {
				continue
			}
			seen[ref] = struct{}{}
			out = append(out, ref)
		}
	}
	return out
}
