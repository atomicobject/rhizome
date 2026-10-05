//go:build cgo

package codeanchor

import (
	"os"
	"path/filepath"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	sitterjavascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	sittertypescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

type tsExportRoute struct {
	imported string
	module   string
}

type tsExportInventory struct {
	direct     map[string]string
	classes    map[string]bool
	explicit   map[string][]tsExportRoute
	namespaces map[string]string
	stars      []string
	ok         bool
	stamp      tsFileStamp
}

type tsExportResolution struct {
	ref  SymbolRef
	ok   bool
	deps map[string]tsFileStamp
}

type tsFileStamp struct {
	size    int64
	modTime int64
}

func (t *TSIndexer) resolveTSExport(file, exportName string, visiting map[string]bool) (SymbolRef, bool) {
	file = strings.TrimSpace(file)
	exportName = strings.TrimSpace(strings.TrimPrefix(exportName, "type "))
	if file == "" || !isTSStableName(exportName) {
		return SymbolRef{}, false
	}
	cacheKey := file + "\x00" + exportName
	rootCall := len(visiting) == 0
	var generation uint64
	if rootCall {
		t.relationshipMu.Lock()
		cached, ok := t.exportResolutionMap[cacheKey]
		generation = t.exportResolutionGeneration
		t.relationshipMu.Unlock()
		if ok && tsExportResolutionFresh(cached) {
			return cached.ref, cached.ok
		}
	}
	ref, ok := t.resolveTSExportUncached(file, exportName, visiting)
	if rootCall {
		if ok {
			deps := make(map[string]tsFileStamp)
			for key := range visiting {
				if strings.HasPrefix(key, "dep\x00") {
					path := strings.TrimPrefix(key, "dep\x00")
					if stamp, exists := tsStatFile(path); exists {
						deps[path] = stamp
					}
				}
			}
			t.relationshipMu.Lock()
			if generation == t.exportResolutionGeneration {
				t.exportResolutionMap[cacheKey] = tsExportResolution{ref: ref, ok: true, deps: deps}
			}
			t.relationshipMu.Unlock()
		} else {
			t.relationshipMu.Lock()
			if generation == t.exportResolutionGeneration {
				delete(t.exportResolutionMap, cacheKey)
			}
			t.relationshipMu.Unlock()
		}
	}
	return ref, ok
}

func (t *TSIndexer) resolveTSExportUncached(file, exportName string, visiting map[string]bool) (SymbolRef, bool) {
	key := file + "\x00" + exportName
	if visiting[key] {
		return SymbolRef{}, false
	}
	visiting[key] = true
	visiting["dep\x00"+file] = true
	defer delete(visiting, key)

	inv := t.tsExportsForFile(file)
	if !inv.ok {
		return SymbolRef{}, false
	}
	if name, ok := inv.direct[exportName]; ok {
		return SymbolRef{Lang: LangTS, Pkg: t.pkgForFile(file), Name: name}, true
	}

	var explicit []SymbolRef
	for _, route := range inv.explicit[exportName] {
		target, ok := t.resolveModuleFile(filepath.Dir(file), route.module)
		if !ok {
			continue
		}
		if ref, ok := t.resolveTSExport(target, route.imported, visiting); ok {
			explicit = appendUniqueTSSymbolRef(explicit, ref)
		}
	}
	if module, ok := inv.namespaces[exportName]; ok {
		if target, resolved := t.resolveModuleFile(filepath.Dir(file), module); resolved {
			explicit = appendUniqueTSSymbolRef(explicit, SymbolRef{Lang: LangTS, Pkg: t.pkgForFile(target), Name: exportName})
		}
	}
	if len(explicit) == 1 {
		return explicit[0], true
	}
	if len(explicit) > 1 {
		return SymbolRef{}, false
	}

	var star []SymbolRef
	for _, module := range inv.stars {
		target, ok := t.resolveModuleFile(filepath.Dir(file), module)
		if !ok {
			continue
		}
		if ref, ok := t.resolveTSExport(target, exportName, visiting); ok {
			star = appendUniqueTSSymbolRef(star, ref)
		}
	}
	if len(star) == 1 {
		return star[0], true
	}
	return SymbolRef{}, false
}

func (t *TSIndexer) tsExportsForFile(file string) tsExportInventory {
	stamp, statOK := tsStatFile(file)
	t.relationshipMu.Lock()
	if cached, ok := t.exportInventories[file]; ok {
		if statOK && cached.stamp == stamp {
			t.relationshipMu.Unlock()
			return cached
		}
		delete(t.exportInventories, file)
	}
	t.relationshipMu.Unlock()

	inv := t.parseTSExportInventory(file, stamp)
	t.relationshipMu.Lock()
	if cached, ok := t.exportInventories[file]; ok {
		t.relationshipMu.Unlock()
		return cached
	}
	t.exportInventories[file] = inv
	t.relationshipMu.Unlock()
	return inv
}

func (t *TSIndexer) parseTSExportInventory(file string, stamp tsFileStamp) tsExportInventory {
	inv := tsExportInventory{
		direct:     make(map[string]string),
		classes:    make(map[string]bool),
		explicit:   make(map[string][]tsExportRoute),
		namespaces: make(map[string]string),
		stamp:      stamp,
	}
	content, err := os.ReadFile(file)
	if err != nil {
		return inv
	}
	parser := sitter.NewParser()
	defer parser.Close()
	switch strings.ToLower(filepath.Ext(file)) {
	case ".js", ".jsx", ".mjs", ".cjs":
		parser.SetLanguage(sitter.NewLanguage(sitterjavascript.Language()))
	case ".tsx":
		parser.SetLanguage(sitter.NewLanguage(sittertypescript.LanguageTSX()))
	default:
		parser.SetLanguage(sitter.NewLanguage(sittertypescript.LanguageTypescript()))
	}
	tree, timedOut, err := parseTreeWithTimeout(parser, content, resolveTreeSitterLimits(content, t.limits))
	if err != nil || timedOut || tree == nil {
		return inv
	}
	defer tree.Close()
	root := tree.RootNode()
	for i := uint(0); i < root.NamedChildCount(); i++ {
		node := root.NamedChild(i)
		if node == nil || node.Kind() != "export_statement" {
			continue
		}
		inventoryTSExportNode(&inv, node, content, file)
	}
	inv.ok = true
	return inv
}

func tsStatFile(file string) (tsFileStamp, bool) {
	info, err := os.Stat(file)
	if err != nil || !info.Mode().IsRegular() {
		return tsFileStamp{}, false
	}
	return tsFileStamp{size: info.Size(), modTime: info.ModTime().UnixNano()}, true
}

func tsExportResolutionFresh(resolution tsExportResolution) bool {
	if len(resolution.deps) == 0 {
		return false
	}
	for file, expected := range resolution.deps {
		actual, ok := tsStatFile(file)
		if !ok || actual != expected {
			return false
		}
	}
	return true
}

func inventoryTSExportNode(inv *tsExportInventory, node *sitter.Node, content []byte, file string) {
	source := trimStringLiteral(nodeText(tsDirectNamedChild(node, "string"), content))
	if clause := tsDirectNamedChild(node, "export_clause"); clause != nil {
		for i := uint(0); i < clause.NamedChildCount(); i++ {
			spec := clause.NamedChild(i)
			if spec == nil || spec.Kind() != "export_specifier" {
				continue
			}
			names := tsDirectIdentifiers(spec, content)
			if len(names) == 0 {
				continue
			}
			imported, exported := names[0], names[len(names)-1]
			if source == "" {
				inv.direct[exported] = imported
			} else {
				inv.explicit[exported] = append(inv.explicit[exported], tsExportRoute{imported: imported, module: source})
			}
		}
		return
	}
	if namespace := tsDirectNamedChild(node, "namespace_export"); namespace != nil {
		if ident := tsFirstDescendant(namespace, "identifier"); ident != nil && source != "" {
			inv.namespaces[strings.TrimSpace(nodeText(ident, content))] = source
		}
		return
	}
	if source != "" {
		inv.stars = append(inv.stars, source)
		return
	}
	declaration := firstTSExportDeclaration(node)
	if declaration == nil {
		return
	}
	raw := strings.TrimSpace(nodeText(node, content))
	if strings.HasPrefix(raw, "export default") {
		name := tsStableBindingName(declaration.ChildByFieldName("name"), content)
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		}
		inv.direct["default"] = name
		if declaration.Kind() == "class_declaration" {
			inv.classes["default"] = true
		}
	}
	collectTSExportDeclarationNames(inv.direct, declaration, content)
	if declaration.Kind() == "class_declaration" {
		if name := tsStableBindingName(declaration.ChildByFieldName("name"), content); name != "" {
			inv.classes[name] = true
		}
	}
}

func firstTSExportDeclaration(node *sitter.Node) *sitter.Node {
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "function_declaration", "class_declaration", "interface_declaration", "type_alias_declaration", "enum_declaration", "lexical_declaration", "variable_declaration":
			return child
		}
	}
	return nil
}

func collectTSExportDeclarationNames(direct map[string]string, declaration *sitter.Node, content []byte) {
	if name := tsStableBindingName(declaration.ChildByFieldName("name"), content); name != "" {
		direct[name] = name
	}
	for i := uint(0); i < declaration.NamedChildCount(); i++ {
		child := declaration.NamedChild(i)
		if child != nil && child.Kind() == "variable_declarator" {
			if name := tsStableBindingName(child.ChildByFieldName("name"), content); name != "" {
				direct[name] = name
			}
		}
	}
}

func appendUniqueTSSymbolRef(refs []SymbolRef, ref SymbolRef) []SymbolRef {
	for _, existing := range refs {
		if existing == ref {
			return refs
		}
	}
	return append(refs, ref)
}
