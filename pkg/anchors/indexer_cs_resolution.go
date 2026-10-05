//go:build cgo

package codeanchor

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *CSharpIndexer) buildResolvedImportContext(root *sitter.Node, content []byte, namespace, fallback, filePath string) csImportContext {
	local := c.buildImportContext(root, content, fallback)
	global := c.globalImportContextForFile(filePath, content)
	return mergeCSImportContexts(local, global)
}

func mergeCSImportContexts(local, global csImportContext) csImportContext {
	out := csImportContext{
		edges:         append([]ImportEdge(nil), local.edges...),
		aliases:       make(map[string]SymbolRef, len(global.aliases)+len(local.aliases)),
		importedTypes: make(map[string]SymbolRef, len(global.importedTypes)+len(local.importedTypes)),
	}
	seenNS := make(map[string]struct{}, len(local.namespaces)+len(global.namespaces))
	for _, ns := range global.namespaces {
		ns = strings.TrimSpace(ns)
		if ns == "" {
			continue
		}
		if _, ok := seenNS[ns]; ok {
			continue
		}
		seenNS[ns] = struct{}{}
		out.namespaces = append(out.namespaces, ns)
	}
	for _, ns := range local.namespaces {
		ns = strings.TrimSpace(ns)
		if ns == "" {
			continue
		}
		if _, ok := seenNS[ns]; ok {
			continue
		}
		seenNS[ns] = struct{}{}
		out.namespaces = append(out.namespaces, ns)
	}
	for k, v := range global.aliases {
		out.aliases[k] = v
	}
	for k, v := range local.aliases {
		out.aliases[k] = v
	}
	for k, v := range global.importedTypes {
		out.importedTypes[k] = v
	}
	for k, v := range local.importedTypes {
		out.importedTypes[k] = v
	}
	return out
}

func (c *CSharpIndexer) globalImportContextForFile(filePath string, content []byte) csImportContext {
	absPath := c.absolutePathFor(filePath)
	if absPath == "" {
		return csImportContext{}
	}
	projectRoot := nearestCSharpProjectRoot(filepath.Dir(absPath))
	if projectRoot == "" {
		return csImportContext{}
	}
	refresh := bytes.Contains(content, []byte("global using"))
	c.globalUsingMu.RLock()
	cached, ok := c.globalUsingByProject[projectRoot]
	c.globalUsingMu.RUnlock()
	if ok && !refresh {
		return cached
	}
	ctx := c.scanProjectGlobalUsings(projectRoot)
	c.globalUsingMu.Lock()
	if c.globalUsingByProject == nil {
		c.globalUsingByProject = make(map[string]csImportContext)
	}
	c.globalUsingByProject[projectRoot] = ctx
	c.globalUsingMu.Unlock()
	return ctx
}

func (c *CSharpIndexer) absolutePathFor(filePath string) string {
	if filePath == "" {
		return ""
	}
	if filepath.IsAbs(filePath) {
		return filePath
	}
	if c.repoRoot == "" {
		return ""
	}
	return filepath.Join(c.repoRoot, filepath.FromSlash(filePath))
}

func nearestCSharpProjectRoot(startDir string) string {
	dir := startDir
	for dir != "" && dir != "." && dir != string(filepath.Separator) {
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				if strings.HasSuffix(entry.Name(), ".csproj") {
					return dir
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func (c *CSharpIndexer) scanProjectGlobalUsings(projectRoot string) csImportContext {
	ctx := csImportContext{
		aliases:       make(map[string]SymbolRef),
		importedTypes: make(map[string]SymbolRef),
	}
	_ = filepath.WalkDir(projectRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() || !strings.HasSuffix(path, ".cs") {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Contains(content, []byte("global using")) {
			return nil
		}
		for _, raw := range extractGlobalUsingLines(content) {
			c.applyUsingDirective(&ctx, raw, false)
		}
		return nil
	})
	return ctx
}

func extractGlobalUsingLines(content []byte) []string {
	lines := strings.Split(string(content), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "global using ") && strings.HasSuffix(line, ";") {
			out = append(out, line)
		}
	}
	return out
}

func (c *CSharpIndexer) applyUsingDirective(ctx *csImportContext, raw string, includeEdge bool) {
	if ctx == nil {
		return
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	raw = strings.TrimPrefix(raw, "global ")
	raw = strings.TrimPrefix(raw, "using ")
	raw = strings.TrimSpace(strings.TrimSuffix(raw, ";"))
	if raw == "" {
		return
	}
	target := raw
	alias := ""
	staticImport := false
	if strings.HasPrefix(target, "static ") {
		staticImport = true
		target = strings.TrimSpace(strings.TrimPrefix(target, "static "))
	}
	if eq := strings.Index(target, "="); eq >= 0 {
		alias = strings.TrimSpace(target[:eq])
		target = strings.TrimSpace(target[eq+1:])
		if strings.HasPrefix(target, "static ") {
			staticImport = true
			target = strings.TrimSpace(strings.TrimPrefix(target, "static "))
		}
	}
	target = c.normalizeTypeText(target)
	if target == "" {
		return
	}
	if alias != "" {
		if ref, ok := c.resolveTypeRef(target, csImportContext{}, "", ""); ok {
			ctx.aliases[alias] = ref
		}
		return
	}
	if staticImport {
		if ref, ok := c.resolveTypeRef(target, csImportContext{}, "", ""); ok {
			ctx.importedTypes[ref.Name] = ref
		}
		return
	}
	ctx.namespaces = append(ctx.namespaces, target)
	if includeEdge {
		ctx.edges = append(ctx.edges, ImportEdge{Module: target})
	}
}

func (c *CSharpIndexer) resolveTypeChain(raw string, imports csImportContext, namespace, fallback string) (SymbolRef, bool) {
	raw = c.normalizeTypeText(raw)
	if raw == "" || isCSharpBuiltinType(raw) {
		return SymbolRef{}, false
	}
	if !strings.Contains(raw, ".") {
		return c.resolveSingleType(raw, imports, namespace, fallback)
	}
	parts := strings.Split(raw, ".")
	if len(parts) == 0 {
		return SymbolRef{}, false
	}
	if ref, ok := imports.aliases[parts[0]]; ok {
		return nestCSharpType(ref, parts[1:]), true
	}
	if ref, ok := imports.importedTypes[parts[0]]; ok {
		return nestCSharpType(ref, parts[1:]), true
	}
	if len(parts) <= 2 {
		for _, ns := range preferredCSharpNamespaces(imports.namespaces) {
			ns = strings.TrimSpace(ns)
			if ns == "" {
				continue
			}
			return nestCSharpType(SymbolRef{Lang: LangCs, Pkg: ns, Name: parts[0]}, parts[1:]), true
		}
		if namespace != "" {
			return nestCSharpType(SymbolRef{Lang: LangCs, Pkg: namespace, Name: parts[0]}, parts[1:]), true
		}
		if fallback != "" {
			return nestCSharpType(SymbolRef{Lang: LangCs, Pkg: fallback, Name: parts[0]}, parts[1:]), true
		}
	}
	pkg, name := splitFQN(raw)
	if name == "" || isCSharpBuiltinType(name) {
		return SymbolRef{}, false
	}
	if pkg == "" {
		pkg = namespace
	}
	return SymbolRef{Lang: LangCs, Pkg: pkg, Name: name}, true
}

func (c *CSharpIndexer) resolveSingleType(raw string, imports csImportContext, namespace, fallback string) (SymbolRef, bool) {
	raw = c.normalizeTypeText(raw)
	if raw == "" || isCSharpBuiltinType(raw) {
		return SymbolRef{}, false
	}
	if ref, ok := imports.aliases[raw]; ok {
		return ref, true
	}
	if ref, ok := imports.importedTypes[raw]; ok {
		return ref, true
	}
	for _, ns := range preferredCSharpNamespaces(imports.namespaces) {
		if ns == "" {
			continue
		}
		return SymbolRef{Lang: LangCs, Pkg: ns, Name: raw}, true
	}
	if namespace != "" {
		return SymbolRef{Lang: LangCs, Pkg: namespace, Name: raw}, true
	}
	if fallback != "" {
		return SymbolRef{Lang: LangCs, Pkg: fallback, Name: raw}, true
	}
	return SymbolRef{Lang: LangCs, Name: raw}, true
}

func (c *CSharpIndexer) preferLocalTypeDeclarations(root *sitter.Node, content []byte, namespace, fallback string, imports csImportContext) csImportContext {
	if imports.importedTypes == nil {
		imports.importedTypes = make(map[string]SymbolRef)
	}
	locals := make(map[string]SymbolRef)
	ambiguous := make(map[string]struct{})
	var walk func(*sitter.Node, []string)
	walk = func(node *sitter.Node, stack []string) {
		if node == nil {
			return
		}
		next := stack
		switch node.Kind() {
		case "class_declaration", "struct_declaration", "interface_declaration", "enum_declaration", "record_declaration":
			name := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), content))
			if name != "" {
				ref := SymbolRef{Lang: LangCs, Pkg: c.currentPkg(namespace, fallback, stack), Name: name}
				if prior, exists := locals[name]; exists && normalizeSymbol(prior) != normalizeSymbol(ref) {
					delete(locals, name)
					ambiguous[name] = struct{}{}
				} else if _, conflict := ambiguous[name]; !conflict {
					locals[name] = ref
				}
				next = append(append([]string(nil), stack...), name)
			}
		}
		for index := uint(0); index < node.NamedChildCount(); index++ {
			walk(node.NamedChild(index), next)
		}
	}
	walk(root, nil)
	for name, ref := range locals {
		imports.importedTypes[name] = ref
	}
	return imports
}

func nestCSharpType(ref SymbolRef, nested []string) SymbolRef {
	out := ref
	for _, name := range nested {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out = SymbolRef{
			Lang: LangCs,
			Pkg:  joinPkgStrings(out.Pkg, out.Name),
			Name: name,
		}
	}
	return out
}

func joinPkgStrings(pkg, name string) string {
	switch {
	case pkg == "":
		return name
	case name == "":
		return pkg
	default:
		return pkg + "." + name
	}
}

func (c *CSharpIndexer) memberRefsFromAccess(n *sitter.Node, content []byte, imports csImportContext, namespace, fallback, ownerFQN, filePath string) []MemberRef {
	if n == nil || isInvocationTargetNode(n) {
		return nil
	}
	if isTypeContextMemberAccessNode(n) || isIntermediateMemberAccessNode(n) {
		return nil
	}
	raw := strings.TrimSpace(nodeText(n, content))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ".")
	if len(parts) < 2 {
		return nil
	}
	head := strings.TrimSpace(parts[0])
	if head == "" {
		return nil
	}
	if _, ok := imports.aliases[head]; !ok {
		if _, ok := imports.importedTypes[head]; !ok {
			if head[0] == '_' || (head[0] >= 'a' && head[0] <= 'z') {
				return nil
			}
		}
	}
	var refs []MemberRef
	seen := make(map[string]struct{})
	for i := len(parts) - 1; i >= 1; i-- {
		typeRefs := c.resolveMemberChainTypePrefixes(strings.Join(parts[:i], "."), imports, namespace, fallback)
		if len(typeRefs) == 0 {
			continue
		}
		name := strings.TrimSpace(parts[i])
		if name == "" {
			continue
		}
		for _, typeRef := range typeRefs {
			ref := MemberRef{
				File:     filePath,
				OwnerFQN: ownerFQN,
				Sym: SymbolRef{
					Lang: LangCs,
					Pkg:  joinPkgStrings(typeRef.Pkg, typeRef.Name),
					Name: name,
				},
			}
			key := normalizeSymbol(ref.Sym)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			refs = append(refs, ref)
		}
	}
	return refs
}

func (c *CSharpIndexer) resolveMemberChainTypePrefixes(raw string, imports csImportContext, namespace, fallback string) []SymbolRef {
	raw = c.normalizeTypeText(raw)
	if raw == "" || isCSharpBuiltinType(raw) {
		return nil
	}
	if strings.Contains(raw, ".") {
		if ref, ok := c.resolveTypeChain(raw, imports, namespace, fallback); ok {
			return []SymbolRef{ref}
		}
		return nil
	}
	if ref, ok := imports.aliases[raw]; ok {
		return []SymbolRef{ref}
	}
	if ref, ok := imports.importedTypes[raw]; ok {
		return []SymbolRef{ref}
	}
	for _, ns := range preferredCSharpNamespaces(imports.namespaces) {
		ns = strings.TrimSpace(ns)
		if ns == "" {
			continue
		}
		return []SymbolRef{{Lang: LangCs, Pkg: ns, Name: raw}}
	}
	if namespace != "" {
		return []SymbolRef{{Lang: LangCs, Pkg: namespace, Name: raw}}
	}
	if fallback != "" {
		return []SymbolRef{{Lang: LangCs, Pkg: fallback, Name: raw}}
	}
	return []SymbolRef{{Lang: LangCs, Name: raw}}
}

func preferredCSharpNamespaces(namespaces []string) []string {
	if len(namespaces) == 0 {
		return nil
	}
	nonSystem := make([]string, 0, len(namespaces))
	system := make([]string, 0, len(namespaces))
	for _, ns := range namespaces {
		ns = strings.TrimSpace(ns)
		if ns == "" {
			continue
		}
		if strings.HasPrefix(ns, "System") || strings.HasPrefix(ns, "Microsoft") {
			system = append(system, ns)
			continue
		}
		nonSystem = append(nonSystem, ns)
	}
	out := make([]string, 0, len(nonSystem)+len(system))
	out = append(out, preferSpecificCSharpNamespaces(nonSystem)...)
	out = append(out, preferSpecificCSharpNamespaces(system)...)
	return out
}

func preferSpecificCSharpNamespaces(namespaces []string) []string {
	if len(namespaces) <= 1 {
		return append([]string(nil), namespaces...)
	}
	out := make([]string, 0, len(namespaces))
	for i, ns := range namespaces {
		if ns == "" {
			continue
		}
		shadowed := false
		for j, other := range namespaces {
			if i == j || other == "" {
				continue
			}
			if strings.HasPrefix(other, ns+".") {
				shadowed = true
				break
			}
		}
		if !shadowed {
			out = append(out, ns)
		}
	}
	return out
}

func isInvocationTargetNode(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	parent := n.Parent()
	if parent == nil || parent.Kind() != "invocation_expression" {
		return false
	}
	if expr := parent.ChildByFieldName("expression"); expr != nil {
		return expr == n
	}
	return parent.NamedChildCount() > 0 && parent.NamedChild(0) == n
}

func isIntermediateMemberAccessNode(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	parent := n.Parent()
	if parent == nil || parent.Kind() != "member_access_expression" {
		return false
	}
	if expr := parent.ChildByFieldName("expression"); expr != nil && expr != n {
		return false
	}
	return !isInvocationTargetNode(parent)
}

func isTypeContextMemberAccessNode(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	parent := n.Parent()
	if parent == nil {
		return false
	}
	switch parent.Kind() {
	case "object_creation_expression", "implicit_object_creation_expression", "typeof_expression":
		if typeNode := parent.ChildByFieldName("type"); typeNode != nil {
			return typeNode == n
		}
		return parent.NamedChildCount() > 0 && parent.NamedChild(0) == n
	default:
		return false
	}
}
