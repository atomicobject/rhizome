//go:build cgo

package codeanchor

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// PythonBuiltinCatalogVersion advances independently from external target
// identity; IndexerVersion remains the persisted-output rebuild trigger.
const PythonBuiltinCatalogVersion = "v1.0.0"

// pythonBuiltinExternalEvidence classifies only explicit, curated Python
// builtins. The raw association keys exactly mirror BuildSymbolRefRows.
func pythonBuiltinExternalEvidence(summary FileSummary, localBindings map[string]struct{}) ExternalEvidenceBatch {
	var batch ExternalEvidenceBatch
	seen := make(map[string]struct{})
	add := func(owner string, kind RefKind, ref SymbolRef) {
		name := strings.TrimSpace(ref.Name)
		if ref.Lang != LangPy || ref.Pkg != "builtins" || !pythonBuiltins[name] {
			return
		}
		if _, shadowed := localBindings[name]; shadowed {
			return
		}
		key := strings.Join([]string{strings.TrimSpace(owner), string(kind), string(ref.Lang), ref.Pkg, name}, "\x00")
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		target, err := NewExternalTarget(ExternalTargetIdentity{
			Ecosystem:  ExternalEcosystemPython,
			Module:     "builtins",
			SymbolPath: name,
			Kind:       ExternalTargetRuntimeBuiltin,
		})
		if err != nil {
			return
		}
		batch.Symbols = append(batch.Symbols, ExternalSymbolEvidenceInput{
			OwnerFQN: owner,
			RefKind:  kind,
			Raw: RawSymbolTargetKey{
				DstLang: LangPy,
				DstPkg:  "builtins",
				DstName: name,
				DstFQN:  "builtins." + name,
			},
			Target: target,
			Evidence: ExternalEvidence{
				Kind:       ExternalEvidenceRuntimeBuiltin,
				Confidence: ExternalConfidenceHigh,
				LocalName:  name,
			},
		})
	}
	for _, call := range summary.Calls {
		add(call.OwnerFQN, RefKindCalls, call.CalleeSymbol)
	}
	for _, typeRef := range summary.TypeRefs {
		add(typeRef.OwnerFQN, RefKindTypeRef, typeRef.TypeSym)
	}
	for _, memberRef := range summary.MemberRefs {
		add(memberRef.OwnerFQN, RefKindMemberRef, memberRef.Sym)
	}
	return batch
}

// collectPythonLocalBindings is deliberately conservative: any explicit file
// binding suppresses runtime-builtin evidence for that spelling. Imports need no
// special case because import-aware resolution already wins over builtins.
func collectPythonLocalBindings(root *sitter.Node, content []byte) map[string]struct{} {
	bindings := make(map[string]struct{})
	var addIdentifiers func(*sitter.Node)
	addIdentifiers = func(node *sitter.Node) {
		if node == nil {
			return
		}
		if node.Kind() == "identifier" {
			if name := strings.TrimSpace(text(node, content)); name != "" {
				bindings[name] = struct{}{}
			}
			return
		}
		for i := uint(0); i < node.NamedChildCount(); i++ {
			addIdentifiers(node.NamedChild(i))
		}
	}
	var walk func(*sitter.Node)
	addAlias := func(node *sitter.Node) {
		if node == nil {
			return
		}
		for _, field := range []string{"alias", "name"} {
			if alias := node.ChildByFieldName(field); alias != nil {
				addIdentifiers(alias)
				return
			}
		}
		raw := text(node, content)
		if index := strings.LastIndex(raw, " as "); index >= 0 {
			alias := strings.TrimSpace(raw[index+4:])
			alias = strings.TrimRight(alias, ":),]")
			if alias != "" && !strings.ContainsAny(alias, " .()[]{}") {
				bindings[alias] = struct{}{}
			}
		}
	}
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}
		switch node.Kind() {
		case "function_definition", "class_definition":
			addIdentifiers(node.ChildByFieldName("name"))
			if node.Kind() == "function_definition" {
				params := node.ChildByFieldName("parameters")
				if params != nil {
					for i := uint(0); i < params.NamedChildCount(); i++ {
						raw := strings.TrimSpace(text(params.NamedChild(i), content))
						raw = strings.TrimSpace(strings.TrimLeft(raw, "*"))
						raw = strings.TrimSpace(strings.SplitN(raw, "=", 2)[0])
						if colon := strings.Index(raw, ":"); colon >= 0 {
							raw = strings.TrimSpace(raw[:colon])
						}
						if raw != "" {
							bindings[raw] = struct{}{}
						}
					}
				}
			}
		case "lambda":
			addIdentifiers(node.ChildByFieldName("parameters"))
		case "assignment", "augmented_assignment", "named_expression", "for_statement", "for_in_clause":
			addIdentifiers(node.ChildByFieldName("left"))
		case "with_item", "except_clause", "as_pattern":
			addAlias(node)
		}
		for i := uint(0); i < node.NamedChildCount(); i++ {
			walk(node.NamedChild(i))
		}
	}
	walk(root)
	return bindings
}
