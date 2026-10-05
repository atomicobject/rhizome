//go:build cgo

package codeanchor

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (t *TSIndexer) applyTSImportNode(node *sitter.Node, content []byte, fileDir string, out *tsImportBindings) {
	if node == nil || node.Kind() != "import_statement" {
		return
	}
	source := tsDirectNamedChild(node, "string")
	module := trimStringLiteral(nodeText(source, content))
	if module == "" {
		return
	}
	target, local := t.resolveModuleFile(fileDir, module)
	if local {
		out.importPaths = append(out.importPaths, target)
	}
	pkg, defaultName := t.modulePkgAndDefaultName(module, target, local)
	clause := tsDirectNamedChild(node, "import_clause")
	if clause == nil {
		return
	}
	for i := uint(0); i < clause.NamedChildCount(); i++ {
		child := clause.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "identifier":
			alias := strings.TrimSpace(nodeText(child, content))
			if alias == "" {
				continue
			}
			name := alias
			if defaultName != "" {
				name = defaultName
			}
			out.idents[alias] = SymbolRef{Lang: LangTS, Pkg: pkg, Name: name}
		case "namespace_import":
			if ident := tsFirstDescendant(child, "identifier"); ident != nil {
				alias := strings.TrimSpace(nodeText(ident, content))
				if alias != "" {
					out.namespaces[alias] = pkg
				}
			}
		case "named_imports":
			t.applyTSNamedImports(child, content, target, local, pkg, out)
		}
	}
}

func (t *TSIndexer) applyTSNamedImports(named *sitter.Node, content []byte, target string, local bool, pkg string, out *tsImportBindings) {
	for i := uint(0); i < named.NamedChildCount(); i++ {
		spec := named.NamedChild(i)
		if spec == nil || spec.Kind() != "import_specifier" {
			continue
		}
		names := tsDirectIdentifiers(spec, content)
		if len(names) == 0 {
			continue
		}
		imported := names[0]
		alias := names[len(names)-1]
		if local {
			if ref, ok := t.resolveTSBarrelExport(target, imported); ok {
				out.idents[alias] = ref
				if t.tsFileDeclaresClass(target, imported) {
					out.memberPrefixes[alias] = ref.Name
				}
			} else {
				out.unresolved[alias] = struct{}{}
			}
			continue
		}
		out.idents[alias] = SymbolRef{Lang: LangTS, Pkg: pkg, Name: imported}
	}
}

func (t *TSIndexer) tsFileDeclaresClass(path, name string) bool {
	if path == "" || !isTSStableName(name) {
		return false
	}
	return t.tsExportsForFile(path).classes[name]
}

func (t *TSIndexer) applyTSExportNode(node *sitter.Node, content []byte, fileDir string, out *tsImportBindings) {
	if source := tsDirectNamedChild(node, "string"); source != nil {
		t.appendTSModuleImport(fileDir, trimStringLiteral(nodeText(source, content)), out)
	}
}

func tsDirectNamedChild(node *sitter.Node, kind string) *sitter.Node {
	if node == nil {
		return nil
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		if child := node.NamedChild(i); child != nil && child.Kind() == kind {
			return child
		}
	}
	return nil
}

func tsFirstDescendant(node *sitter.Node, kind string) *sitter.Node {
	if node == nil {
		return nil
	}
	if node.Kind() == kind {
		return node
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		if found := tsFirstDescendant(node.NamedChild(i), kind); found != nil {
			return found
		}
	}
	return nil
}

func tsDirectIdentifiers(node *sitter.Node, content []byte) []string {
	var names []string
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child != nil && child.Kind() == "identifier" {
			if name := strings.TrimSpace(nodeText(child, content)); isTSStableName(name) {
				names = append(names, name)
			}
		}
	}
	return names
}
