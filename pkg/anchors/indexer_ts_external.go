//go:build cgo

package codeanchor

import (
	"path/filepath"
	"regexp"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

type tsExternalBinding struct {
	module, imported, local string
	kind                    ExternalEvidenceKind
	version                 *ExternalVersionProvenance
}

var (
	tsCJSDestructurePattern = regexp.MustCompile(`^\s*(?:(?:const|let|var)\s+)?\{([^}]+)\}\s*=\s*require\(\s*(["'][^"']+["'])\s*\)`)
	tsCJSDefaultPattern     = regexp.MustCompile(`^\s*(?:(?:const|let|var)\s+)?([A-Za-z_$][\w$]*)\s*=\s*require\(\s*(["'][^"']+["'])\s*\)\.default\b`)
	tsCJSNamespacePattern   = regexp.MustCompile(`^\s*(?:(?:const|let|var)\s+)?([A-Za-z_$][\w$]*)\s*=\s*require\(\s*(["'][^"']+["'])\s*\)`)
)

// extractTSExternalEvidence emits only syntax-proven associations. Resolution
// remains local-first: any repository module, workspace package, or configured
// path alias is excluded even when its current source target is missing.
func (t *TSIndexer) extractTSExternalEvidence(root *sitter.Node, content []byte, fileDir, currentPkg string, summary *FileSummary) ExternalEvidenceBatch {
	bindings, imports := t.collectTSExternalBindings(root, content, fileDir)
	shadowed := tsDeclaredRuntimeNames(root, content)
	normalizeTSExternalDefaultRefs(bindings, summary)
	appendTSCJSBindingRefs(root, content, currentPkg, bindings, summary)
	t.appendTSRuntimeGlobalRefs(root, content, currentPkg, shadowed, summary)
	rows := BuildSymbolRefRows(summary.FilePath, *summary)
	batch := ExternalEvidenceBatch{Imports: imports}
	seen := make(map[string]struct{})
	for _, row := range rows {
		binding, ok := matchTSExternalBinding(row, bindings)
		if ok {
			targetKind := ExternalTargetSymbol
			if isNodeBuiltinModule(binding.module) {
				targetKind = ExternalTargetRuntimeBuiltin
			}
			symbolPath := row.DstName
			target, err := NewExternalTarget(ExternalTargetIdentity{Ecosystem: ExternalEcosystemNPM, Module: binding.module, SymbolPath: symbolPath, Kind: targetKind})
			if err == nil {
				input := externalSymbolInput(row, target, binding.kind, binding.imported, binding.local, binding.version)
				appendUniqueTSExternalSymbol(&batch, seen, input)
			}
			continue
		}
		if row.DstPkg == currentPkg {
			if _, global := tsRuntimeGlobalCalls[row.DstName]; global && !shadowed[row.DstName] {
				target, _ := NewExternalTarget(ExternalTargetIdentity{Ecosystem: ExternalEcosystemNPM, Module: "javascript", SymbolPath: row.DstName, Kind: ExternalTargetRuntimeGlobal})
				appendUniqueTSExternalSymbol(&batch, seen, externalSymbolInput(row, target, ExternalEvidenceRuntimeGlobal, row.DstName, row.DstName, nil))
			}
		}
		if row.DstPkg == "javascript" {
			target, _ := NewExternalTarget(ExternalTargetIdentity{Ecosystem: ExternalEcosystemNPM, Module: "javascript", SymbolPath: row.DstName, Kind: ExternalTargetRuntimeGlobal})
			appendUniqueTSExternalSymbol(&batch, seen, externalSymbolInput(row, target, ExternalEvidenceRuntimeGlobal, row.DstName, strings.Split(row.DstName, ".")[0], nil))
		}
	}
	return batch
}

// normalizeTSExternalDefaultRefs moves direct default-bound uses onto their
// public raw identity before BuildSymbolRefRows. This keeps the raw dictionary,
// N:1 external map, and import target aligned on module.default while leaving
// receiver members (for example React.createElement) on their member identity.
func normalizeTSExternalDefaultRefs(bindings []tsExternalBinding, summary *FileSummary) {
	defaults := make(map[string]struct{})
	for _, binding := range bindings {
		if binding.kind != ExternalEvidenceESMDefault && binding.kind != ExternalEvidenceCJSDefault {
			continue
		}
		if binding.module != "" && binding.local != "" {
			defaults[binding.module+"\x00"+binding.local] = struct{}{}
		}
	}
	normalize := func(ref *SymbolRef) {
		if ref == nil {
			return
		}
		if _, ok := defaults[ref.Pkg+"\x00"+ref.Name]; ok {
			ref.Name = "default"
		}
	}
	for i := range summary.Calls {
		normalize(&summary.Calls[i].CalleeSymbol)
	}
	for i := range summary.TypeRefs {
		normalize(&summary.TypeRefs[i].TypeSym)
	}
	for i := range summary.MemberRefs {
		normalize(&summary.MemberRefs[i].Sym)
	}
}

func appendTSCJSBindingRefs(root *sitter.Node, content []byte, currentPkg string, bindings []tsExternalBinding, summary *FileSummary) {
	byLocal := make(map[string]tsExternalBinding)
	for _, binding := range bindings {
		if strings.HasPrefix(string(binding.kind), "cjs_") && binding.local != "" {
			byLocal[binding.local] = binding
		}
	}
	for _, call := range append([]CallSite(nil), summary.Calls...) {
		binding, ok := byLocal[call.CalleeSymbol.Name]
		if !ok || call.CalleeSymbol.Pkg != currentPkg {
			continue
		}
		name := binding.imported
		if name == "" {
			name = binding.local
		}
		summary.Calls = append(summary.Calls, CallSite{File: call.File, OwnerFQN: call.OwnerFQN, CalleeSymbol: SymbolRef{Lang: LangTS, Pkg: binding.module, Name: name}})
	}
	for _, typeRef := range append([]TypeRef(nil), summary.TypeRefs...) {
		binding, ok := byLocal[typeRef.TypeSym.Name]
		if !ok || typeRef.TypeSym.Pkg != currentPkg {
			continue
		}
		name := binding.imported
		if name == "" {
			name = binding.local
		}
		summary.TypeRefs = append(summary.TypeRefs, TypeRef{File: typeRef.File, OwnerFQN: typeRef.OwnerFQN, TypeSym: SymbolRef{Lang: LangTS, Pkg: binding.module, Name: name}})
	}
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}
		if node.Kind() == "call_expression" || node.Kind() == "new_expression" {
			callee := node.ChildByFieldName("function")
			if node.Kind() == "new_expression" {
				callee = node.ChildByFieldName("constructor")
				if callee == nil {
					callee = node.ChildByFieldName("function")
				}
			}
			callee = tsUnwrapExpr(callee)
			if callee != nil && callee.Kind() == "member_expression" {
				object := tsUnwrapExpr(callee.ChildByFieldName("object"))
				property := strings.TrimSpace(nodeText(callee.ChildByFieldName("property"), content))
				if object != nil && object.Kind() == "identifier" && isTSStableName(property) {
					if binding, ok := byLocal[strings.TrimSpace(nodeText(object, content))]; ok {
						summary.Calls = append(summary.Calls, CallSite{File: summary.FilePath, OwnerFQN: tsExternalOwner(node, content, currentPkg), CalleeSymbol: SymbolRef{Lang: LangTS, Pkg: binding.module, Name: property}})
					}
				}
			}
		}
		for i := uint(0); i < node.NamedChildCount(); i++ {
			walk(node.NamedChild(i))
		}
	}
	walk(root)
}

func externalSymbolInput(row SymbolRefRow, target ExternalTarget, kind ExternalEvidenceKind, imported, local string, version *ExternalVersionProvenance) ExternalSymbolEvidenceInput {
	return ExternalSymbolEvidenceInput{OwnerFQN: row.OwnerFQN, RefKind: row.RefKind,
		Raw:    RawSymbolTargetKey{DstLang: row.DstLang, DstPkg: row.DstPkg, DstName: row.DstName, DstFQN: row.DstFQN},
		Target: target, Evidence: ExternalEvidence{Kind: kind, Confidence: ExternalConfidenceHigh, ImportedName: imported, LocalName: local, Version: version}}
}

func appendUniqueTSExternalSymbol(batch *ExternalEvidenceBatch, seen map[string]struct{}, input ExternalSymbolEvidenceInput) {
	key := strings.Join([]string{input.OwnerFQN, string(input.RefKind), string(input.Raw.DstLang), input.Raw.DstPkg, input.Raw.DstName}, "\x00")
	if _, exists := seen[key]; exists {
		return
	}
	seen[key] = struct{}{}
	batch.Symbols = append(batch.Symbols, input)
}

func matchTSExternalBinding(row SymbolRefRow, bindings []tsExternalBinding) (tsExternalBinding, bool) {
	for _, binding := range bindings {
		if row.DstPkg == binding.module && (row.DstName == binding.local || row.DstName == "default") &&
			(binding.kind == ExternalEvidenceESMDefault || binding.kind == ExternalEvidenceCJSDefault) {
			return binding, true
		}
	}
	for _, binding := range bindings {
		if row.DstPkg != binding.module || row.DstName != binding.imported {
			continue
		}
		if binding.kind == ExternalEvidenceESMNamed || binding.kind == ExternalEvidenceESMType || binding.kind == ExternalEvidenceCJSNamed || binding.kind == ExternalEvidenceCJSType {
			return binding, true
		}
	}
	for _, binding := range bindings {
		if row.DstPkg == binding.module && row.DstName == binding.local &&
			(binding.kind == ExternalEvidenceESMNamespace || binding.kind == ExternalEvidenceCJSNamespace) {
			return binding, true
		}
	}
	// Receiver-member refs carry the public member in DstName rather than the
	// local default/namespace binding. Only receiver-shaped bindings may claim
	// that package fallback; named/type bindings must match their exported name.
	for _, binding := range bindings {
		if row.DstPkg == binding.module &&
			(binding.kind == ExternalEvidenceESMDefault || binding.kind == ExternalEvidenceCJSDefault ||
				binding.kind == ExternalEvidenceESMNamespace || binding.kind == ExternalEvidenceCJSNamespace) {
			return binding, true
		}
	}
	return tsExternalBinding{}, false
}

func (t *TSIndexer) collectTSExternalBindings(root *sitter.Node, content []byte, fileDir string) ([]tsExternalBinding, []ExternalImportEvidenceInput) {
	var bindings []tsExternalBinding
	var imports []ExternalImportEvidenceInput
	ordinal := 0
	add := func(binding tsExternalBinding) {
		if binding.module == "" || t.tsSpecifierMayBeLocal(fileDir, binding.module) {
			return
		}
		binding.version = t.tsExternalVersionProvenance(fileDir, binding.module)
		bindings = append(bindings, binding)
		targetKind := ExternalTargetModule
		symbol := ""
		if binding.imported != "" {
			targetKind, symbol = ExternalTargetSymbol, ExternalSymbolPath(binding.kind, binding.imported)
		}
		if isNodeBuiltinModule(binding.module) {
			targetKind = ExternalTargetRuntimeBuiltin
		}
		target, err := NewExternalTarget(ExternalTargetIdentity{Ecosystem: ExternalEcosystemNPM, Module: binding.module, SymbolPath: symbol, Kind: targetKind})
		if err != nil {
			return
		}
		imports = append(imports, ExternalImportEvidenceInput{Module: binding.module, BindingOrdinal: ordinal, Target: target,
			Evidence: ExternalEvidence{Kind: binding.kind, Confidence: ExternalConfidenceHigh, ImportedName: binding.imported, LocalName: binding.local, Version: binding.version}})
		ordinal++
	}
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}
		switch node.Kind() {
		case "import_statement":
			for _, binding := range tsESMExternalBindings(node, content) {
				add(binding)
			}
			return
		case "variable_declarator":
			for _, binding := range tsCJSExternalBindings(nodeText(node, content)) {
				add(binding)
			}
			return
		case "expression_statement":
			raw := strings.TrimSpace(nodeText(node, content))
			if module := tsSideEffectRequire(raw); module != "" {
				add(tsExternalBinding{module: module, kind: ExternalEvidenceCJSSideEffect})
			}
			return
		}
		for i := uint(0); i < node.NamedChildCount(); i++ {
			walk(node.NamedChild(i))
		}
	}
	walk(root)
	return bindings, imports
}

func tsESMExternalBindings(node *sitter.Node, content []byte) []tsExternalBinding {
	module := trimStringLiteral(nodeText(tsDirectNamedChild(node, "string"), content))
	if module == "" {
		return nil
	}
	clause := tsDirectNamedChild(node, "import_clause")
	if clause == nil {
		return []tsExternalBinding{{module: module, kind: ExternalEvidenceESMSideEffect}}
	}
	statementTypeOnly := strings.HasPrefix(strings.TrimSpace(nodeText(node, content)), "import type ")
	var out []tsExternalBinding
	for i := uint(0); i < clause.NamedChildCount(); i++ {
		child := clause.NamedChild(i)
		switch child.Kind() {
		case "identifier":
			local := strings.TrimSpace(nodeText(child, content))
			out = append(out, tsExternalBinding{module: module, imported: "default", local: local, kind: ExternalEvidenceESMDefault})
		case "namespace_import":
			ident := tsFirstDescendant(child, "identifier")
			out = append(out, tsExternalBinding{module: module, local: strings.TrimSpace(nodeText(ident, content)), kind: ExternalEvidenceESMNamespace})
		case "named_imports":
			for j := uint(0); j < child.NamedChildCount(); j++ {
				spec := child.NamedChild(j)
				if spec == nil || spec.Kind() != "import_specifier" {
					continue
				}
				names := tsDirectIdentifiers(spec, content)
				if len(names) == 0 {
					continue
				}
				kind := ExternalEvidenceESMNamed
				if statementTypeOnly || strings.HasPrefix(strings.TrimSpace(nodeText(spec, content)), "type ") {
					kind = ExternalEvidenceESMType
				}
				out = append(out, tsExternalBinding{module: module, imported: names[0], local: names[len(names)-1], kind: kind})
			}
		}
	}
	return out
}

func tsCJSExternalBindings(raw string) []tsExternalBinding {
	if match := tsCJSDestructurePattern.FindStringSubmatch(raw); len(match) > 0 {
		module := trimStringLiteral(match[2])
		var out []tsExternalBinding
		for _, part := range strings.Split(match[1], ",") {
			names := strings.Split(strings.TrimSpace(part), ":")
			imported := strings.TrimSpace(names[0])
			local := imported
			if len(names) > 1 {
				local = strings.TrimSpace(names[1])
			}
			if isTSStableName(imported) && isTSStableName(local) {
				out = append(out, tsExternalBinding{module: module, imported: imported, local: local, kind: ExternalEvidenceCJSNamed})
			}
		}
		return out
	}
	if match := tsCJSDefaultPattern.FindStringSubmatch(raw); len(match) > 0 {
		return []tsExternalBinding{{module: trimStringLiteral(match[2]), imported: "default", local: match[1], kind: ExternalEvidenceCJSDefault}}
	}
	if match := tsCJSNamespacePattern.FindStringSubmatch(raw); len(match) > 0 {
		return []tsExternalBinding{{module: trimStringLiteral(match[2]), local: match[1], kind: ExternalEvidenceCJSNamespace}}
	}
	return nil
}

func tsSideEffectRequire(raw string) string {
	if !strings.HasPrefix(raw, "require(") {
		return ""
	}
	open, close := strings.Index(raw, "("), strings.LastIndex(raw, ")")
	if open < 0 || close <= open {
		return ""
	}
	arg := strings.TrimSpace(raw[open+1 : close])
	if len(arg) < 2 || (arg[0] != '\'' && arg[0] != '"') || arg[len(arg)-1] != arg[0] {
		return ""
	}
	return trimStringLiteral(arg)
}

func (t *TSIndexer) tsSpecifierMayBeLocal(fileDir, module string) bool {
	if _, ok := t.resolveModuleFile(fileDir, module); ok {
		return true
	}
	if strings.HasPrefix(module, ".") || strings.HasPrefix(module, "#") || filepath.IsAbs(module) {
		return true
	}
	if t.resolver != nil {
		if _, match := t.resolver.resolveConfigPaths(fileDir, module); match != tsConfigMatchNone {
			return true
		}
		name, _ := splitTSPackageSpecifier(module)
		if _, ok := t.resolver.workspacePackage(name); ok {
			return true
		}
	}
	return false
}

// JSRuntimeGlobalCatalogVersion versions classifier knowledge independently of
// stored target identity. Bump it whenever the curated global vocabulary moves.
const JSRuntimeGlobalCatalogVersion = "1.0.0"

var tsRuntimeGlobalCalls = map[string]struct{}{
	"fetch": {}, "queueMicrotask": {}, "setInterval": {}, "setTimeout": {}, "clearInterval": {}, "clearTimeout": {}, "structuredClone": {},
	"Promise": {}, "URL": {}, "URLSearchParams": {}, "Map": {}, "Set": {}, "WeakMap": {}, "WeakSet": {}, "Date": {}, "RegExp": {},
	"Error": {}, "EvalError": {}, "RangeError": {}, "ReferenceError": {}, "SyntaxError": {}, "TypeError": {}, "URIError": {}, "AggregateError": {},
	"Object": {}, "Array": {}, "String": {}, "Number": {}, "Boolean": {}, "BigInt": {}, "Symbol": {},
}

var tsRuntimeGlobalNamespaces = map[string]struct{}{"console": {}, "JSON": {}, "Math": {}, "Object": {}, "Reflect": {}, "Atomics": {}}

func (t *TSIndexer) appendTSRuntimeGlobalRefs(root *sitter.Node, content []byte, currentPkg string, shadowed map[string]bool, summary *FileSummary) {
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}
		if node.Kind() == "member_expression" {
			obj := node.ChildByFieldName("object")
			prop := node.ChildByFieldName("property")
			namespace := nodeText(obj, content)
			if _, curated := tsRuntimeGlobalNamespaces[namespace]; curated && !shadowed[namespace] && isTSStableName(nodeText(prop, content)) {
				summary.MemberRefs = append(summary.MemberRefs, MemberRef{File: summary.FilePath, OwnerFQN: tsExternalOwner(node, content, currentPkg), Sym: SymbolRef{Lang: LangTS, Pkg: "javascript", Name: namespace + "." + nodeText(prop, content)}})
			}
		}
		for i := uint(0); i < node.NamedChildCount(); i++ {
			walk(node.NamedChild(i))
		}
	}
	walk(root)
}

func tsDeclaredRuntimeNames(root *sitter.Node, content []byte) map[string]bool {
	out := make(map[string]bool)
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}
		switch node.Kind() {
		case "variable_declarator", "function_declaration", "class_declaration", "formal_parameter", "required_parameter", "optional_parameter":
			if name := tsStableBindingName(node.ChildByFieldName("name"), content); name != "" {
				out[name] = true
			}
		case "formal_parameters":
			collectTSParameterIdentifiers(node, content, out)
		case "import_clause":
			for _, name := range tsDirectIdentifiers(node, content) {
				out[name] = true
			}
		}
		for i := uint(0); i < node.NamedChildCount(); i++ {
			walk(node.NamedChild(i))
		}
	}
	walk(root)
	return out
}

func collectTSParameterIdentifiers(node *sitter.Node, content []byte, out map[string]bool) {
	if node == nil {
		return
	}
	switch node.Kind() {
	case "identifier", "shorthand_property_identifier_pattern":
		if name := strings.TrimSpace(nodeText(node, content)); isTSStableName(name) {
			out[name] = true
		}
		return
	case "required_parameter", "optional_parameter", "formal_parameter":
		collectTSParameterIdentifiers(node.ChildByFieldName("name"), content, out)
		collectTSParameterIdentifiers(node.ChildByFieldName("pattern"), content, out)
		return
	case "assignment_pattern", "object_assignment_pattern":
		collectTSParameterIdentifiers(node.ChildByFieldName("left"), content, out)
		return
	case "pair_pattern":
		collectTSParameterIdentifiers(node.ChildByFieldName("value"), content, out)
		return
	case "type_annotation":
		return
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		collectTSParameterIdentifiers(node.NamedChild(i), content, out)
	}
}

func tsExternalOwner(node *sitter.Node, content []byte, currentPkg string) string {
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		if parent.Kind() == "function_declaration" {
			if name := tsStableBindingName(parent.ChildByFieldName("name"), content); name != "" {
				return (Symbol{Pkg: currentPkg, Name: name}).NormalizeFQN()
			}
		}
	}
	return ""
}
