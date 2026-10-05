//go:build cgo

package codeanchor

// Docs:
// - [Code anchors (Hub)](docs/hubs/Code anchors (Hub).md)
// - [Code anchors - matching + scopes](docs/reference/analysis/code-anchors-matching-scopes.md)

import (
	"bufio"
	"bytes"
	"log"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/atomicobject/rhizome/pkg/paths"

	sitter "github.com/tree-sitter/go-tree-sitter"
	sitterpython "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

// PythonIndexer implements LanguageIndexer using tree-sitter-python.
type PythonIndexer struct {
	parserPool  sync.Pool
	sourceRoots []string
	limits      TreeSitterLimits
}

var pythonTypeIdentRE = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*`)

var pythonTypingWrappers = map[string]bool{
	"Annotated":  true,
	"Any":        true,
	"Callable":   true,
	"ClassVar":   true,
	"Dict":       true,
	"FrozenSet":  true,
	"Generic":    true,
	"Iterable":   true,
	"Iterator":   true,
	"List":       true,
	"Literal":    true,
	"Mapping":    true,
	"Optional":   true,
	"Protocol":   true,
	"Self":       true,
	"Sequence":   true,
	"Set":        true,
	"Tuple":      true,
	"Type":       true,
	"Union":      true,
	"None":       true,
	"typing.Any": true,
}

// pythonProtocolBases are base classes that indicate an interface/protocol type.
var pythonProtocolBases = map[string]bool{
	"Protocol":                   true,
	"typing.Protocol":            true,
	"ABC":                        true,
	"abc.ABC":                    true,
	"ABCMeta":                    true,
	"abc.ABCMeta":                true,
	"typing_extensions.Protocol": true,
}

// NewPythonIndexer constructs a Python indexer with default source roots.
func NewPythonIndexer() *PythonIndexer {
	return NewPythonIndexerWithRoots([]string{"src", "lib"})
}

// NewPythonIndexerWithRoots allows configuring source roots (e.g., ["src", "packages"]).
func NewPythonIndexerWithRoots(roots []string) *PythonIndexer {
	return NewPythonIndexerWithRootsAndLimits(roots, TreeSitterLimits{})
}

// NewPythonIndexerWithRootsAndLimits allows configuring source roots and tree-sitter limits.
func NewPythonIndexerWithRootsAndLimits(roots []string, limits TreeSitterLimits) *PythonIndexer {
	idx := &PythonIndexer{
		sourceRoots: roots,
		limits:      limits,
	}
	idx.parserPool.New = func() any {
		p := sitter.NewParser()
		p.SetLanguage(sitter.NewLanguage(sitterpython.Language()))
		return p
	}
	return idx
}

// Lang returns the language identifier.
func (p *PythonIndexer) Lang() Lang { return LangPy }

func (*PythonIndexer) ReverseIndexFallbacks(deltas DefDeltas) []ReverseIndexFallback {
	symbols := append(append([]SymbolRef{}, deltas.AddedSymbols...), deltas.RemovedSymbols...)
	if len(symbols) == 0 {
		return nil
	}
	nameSuffixes := make(map[string]map[string]struct{})
	for _, ref := range symbols {
		if ref.Lang != LangPy || ref.Name == "" || ref.Pkg == "" {
			continue
		}
		dirSuffix := pythonModuleDirSuffix(ref.Pkg)
		if dirSuffix == "" {
			continue
		}
		if nameSuffixes[ref.Name] == nil {
			nameSuffixes[ref.Name] = make(map[string]struct{})
		}
		nameSuffixes[ref.Name][dirSuffix] = struct{}{}
	}
	if len(nameSuffixes) == 0 {
		return nil
	}

	fallbacks := make([]ReverseIndexFallback, 0, len(nameSuffixes))
	for name, suffixes := range nameSuffixes {
		if name == "" || len(suffixes) == 0 {
			continue
		}
		suffixesCopy := suffixes
		fallbacks = append(fallbacks, ReverseIndexFallback{
			Ref: SymbolRef{Lang: LangPy, Name: name},
			PathFilter: func(path string) bool {
				return matchesDirSuffix(path, suffixesCopy)
			},
		})
	}
	return fallbacks
}

// IndexFile parses a Python file and extracts symbols, inheritance, annotations, and calls.
func (p *PythonIndexer) IndexFile(content []byte, ref paths.CodePathRef) (FileSummary, error) {
	filePath := ref.Rel.String()
	if filePath == "" {
		filePath = ref.Abs.String()
	}

	parserAny := p.parserPool.Get()
	var parser *sitter.Parser
	if parserAny == nil {
		parser = sitter.NewParser()
		parser.SetLanguage(sitter.NewLanguage(sitterpython.Language()))
	} else {
		parser = parserAny.(*sitter.Parser)
	}

	parseTimeout := resolveTreeSitterLimits(content, p.limits)
	tree, timedOut, err := parseTreeWithTimeout(parser, content, parseTimeout)
	if err != nil {
		parser.Close()
		return FileSummary{}, err
	}
	if timedOut {
		parser.Close()
		log.Printf("codeanchor: python parse timeout for %s", filePath)
		return FileSummary{FilePath: filePath, Lang: LangPy, ParseStatus: ParseTimeout}, nil
	}
	defer p.parserPool.Put(parser)
	defer tree.Close()
	root := tree.RootNode()
	parseErrored := root.HasError()
	pkg := pythonModuleFromPath(filePath, p.sourceRoots)
	initPy := isInitPy(filePath)
	imports, importedModules := collectImportsAndAliases(root, content, pkg, initPy)
	localBindings := collectPythonLocalBindings(root, content)
	knownTypeFQNs := make(map[string]string)

	summary := FileSummary{
		FilePath: filePath,
		Lang:     LangPy,
		ParseStatus: func() ParseStatus {
			if parseErrored {
				return ParseErrored
			}
			return ParseOK
		}(),
	}
	summary.PackageDoc = pythonModuleDocstring(root, content)

	// Deduplicate and add import edges.
	seenModules := make(map[string]struct{})
	for _, mod := range importedModules {
		mod = strings.TrimSpace(mod)
		if mod == "" {
			continue
		}
		if _, ok := seenModules[mod]; ok {
			continue
		}
		seenModules[mod] = struct{}{}
		summary.Imports = append(summary.Imports, ImportEdge{Module: mod})
	}

	// For __init__.py files, extract re-exports as package-level symbols.
	// This allows anchors to target `package.Symbol` even when Symbol is defined in a submodule.
	if isInitPy(filePath) {
		p.extractReExports(root, pkg, content, &summary)
	}

	var walk func(node *sitter.Node, classStack []string, ownerFQN string, localTypeFQNs map[string]string)
	walk = func(node *sitter.Node, classStack []string, ownerFQN string, localTypeFQNs map[string]string) {
		switch node.Kind() {
		case "decorated_definition":
			decorators := collectDecorators(node, content, pkg, imports)
			// Find the underlying definition (class or function).
			for i := uint(0); i < node.NamedChildCount(); i++ {
				child := node.NamedChild(i)
				if child == nil {
					continue
				}
				if child.Kind() == "class_definition" || child.Kind() == "function_definition" {
					p.processDefinition(child, classStack, pkg, content, decorators, imports, &summary)
					if child.Kind() == "class_definition" {
						name := text(child.ChildByFieldName("name"), content)
						if name != "" {
							knownTypeFQNs[name] = pythonFQN(pkg, classStack, name)
						}
						// Walk members, but avoid re-processing the class node itself.
						walkChildren(child, append(classStack, name), func(n *sitter.Node, cs []string) {
							walk(n, cs, ownerFQN, localTypeFQNs)
						})
						return
					}
					if child.Kind() == "function_definition" {
						newOwner := pythonFQN(pkg, classStack, text(child.ChildByFieldName("name"), content))
						localTypeFQNs = pythonLocalTypeFQNs(child, content, pkg, imports, classStack)
						// Extract type refs from function signature.
						typeRefs := p.extractFunctionTypeRefs(child, content, pkg, imports, newOwner, summary.FilePath)
						summary.TypeRefs = append(summary.TypeRefs, typeRefs...)
						walkChildren(child, classStack, func(n *sitter.Node, cs []string) {
							walk(n, cs, newOwner, localTypeFQNs)
						})
						return
					}
				}
			}
			return
		case "class_definition":
			p.processDefinition(node, classStack, pkg, content, nil, imports, &summary)
			name := text(node.ChildByFieldName("name"), content)
			if name != "" {
				knownTypeFQNs[name] = pythonFQN(pkg, classStack, name)
			}
			walkChildren(node, append(classStack, name), func(n *sitter.Node, cs []string) {
				walk(n, cs, ownerFQN, localTypeFQNs)
			})
			return
		case "function_definition":
			p.processDefinition(node, classStack, pkg, content, nil, imports, &summary)
			newOwnerFQN := pythonFQN(pkg, classStack, text(node.ChildByFieldName("name"), content))
			localTypeFQNs = pythonLocalTypeFQNs(node, content, pkg, imports, classStack)
			// Extract type refs from function signature.
			typeRefs := p.extractFunctionTypeRefs(node, content, pkg, imports, newOwnerFQN, summary.FilePath)
			summary.TypeRefs = append(summary.TypeRefs, typeRefs...)
			ownerFQN = newOwnerFQN
		case "call":
			if cs := p.callSite(node, content, pkg, imports, localTypeFQNs, knownTypeFQNs, parseErrored); cs != nil {
				cs.File = summary.FilePath
				cs.OwnerFQN = ownerFQN
				summary.Calls = append(summary.Calls, *cs)
			}
		}
		walkChildren(node, classStack, func(n *sitter.Node, cs []string) { walk(n, cs, ownerFQN, localTypeFQNs) })
	}

	walk(root, nil, "", nil)
	summary.ExternalEvidence = pythonBuiltinExternalEvidence(summary, localBindings)
	summary.ExternalEvidenceReady = true
	if parseErrored {
		logParseErrorIfPoor("python", filePath, summary, "fallback call edges enabled")
	}
	return summary, nil
}

func pythonModuleDirSuffix(pkg string) string {
	if strings.TrimSpace(pkg) == "" {
		return ""
	}
	path := strings.ReplaceAll(pkg, ".", "/")
	dir := pathpkg.Dir(path)
	if dir == "." {
		return ""
	}
	return dir
}

func matchesDirSuffix(path string, suffixes map[string]struct{}) bool {
	if len(suffixes) == 0 {
		return false
	}
	dir := filepath.ToSlash(filepath.Dir(path))
	if dir == "." {
		dir = ""
	}
	for suffix := range suffixes {
		if suffix == "" {
			if dir == "" {
				return true
			}
			continue
		}
		if dir == suffix || strings.HasSuffix(dir, "/"+suffix) {
			return true
		}
	}
	return false
}

func (p *PythonIndexer) processDefinition(node *sitter.Node, classStack []string, pkg string, content []byte, decorators []AnnotationUse, imports map[string]string, summary *FileSummary) {
	if node == nil {
		return
	}
	nameNode := node.ChildByFieldName("name")
	name := text(nameNode, content)
	if name == "" {
		return
	}
	isClass := node.Kind() == "class_definition"
	kind := SymFunc
	if isClass {
		kind = SymClass
	}
	if !isClass && len(classStack) > 0 {
		kind = SymMethod
	}
	fqn := pythonFQN(pkg, classStack, name)

	// Check if this is a dataclass (for field extraction).
	isDataclass := false
	for _, dec := range decorators {
		if dec.AnnSymbol.Name == "dataclass" {
			isDataclass = true
			break
		}
	}

	// Check if this is a Protocol/ABC (interface-like class).
	isProtocol := false

	// Base classes - resolve using imports to preserve qualified names.
	if isClass {
		if super := node.ChildByFieldName("superclasses"); super != nil {
			ids := extractIdentifiers(super, content)
			for _, id := range ids {
				parentPkg, parentName := resolveSymbolRef(id, pkg, imports)
				parent := pythonFQN(parentPkg, nil, parentName)
				summary.Supers = append(summary.Supers, SuperEdge{
					ChildFQN:  fqn,
					ParentFQN: parent,
				})
				// Check if parent is a protocol/ABC base.
				if pythonProtocolBases[id] || pythonProtocolBases[parentName] || pythonProtocolBases[parent] {
					isProtocol = true
				}
			}
		} else {
			// Some grammars use argument_list without named field; scan children.
			for i := uint(0); i < node.NamedChildCount(); i++ {
				ch := node.NamedChild(i)
				if ch != nil && ch.Kind() == "argument_list" {
					ids := extractIdentifiers(ch, content)
					for _, id := range ids {
						parentPkg, parentName := resolveSymbolRef(id, pkg, imports)
						parent := pythonFQN(parentPkg, nil, parentName)
						summary.Supers = append(summary.Supers, SuperEdge{
							ChildFQN:  fqn,
							ParentFQN: parent,
						})
						// Check if parent is a protocol/ABC base.
						if pythonProtocolBases[id] || pythonProtocolBases[parentName] || pythonProtocolBases[parent] {
							isProtocol = true
						}
					}
				}
			}
		}
	}

	// Use SymInterface for Protocol/ABC classes.
	if isClass && isProtocol {
		kind = SymInterface
	}

	sym := Symbol{
		Lang: LangPy,
		Kind: kind,
		File: summary.FilePath,
		Pkg:  pkg,
		Name: name,
		FQN:  fqn,
		// Best-effort export flag (useful for module rollups).
		Exported: !strings.HasPrefix(name, "_"),
	}
	fillPythonSymbolMeta(&sym, node, content)
	summary.Symbols = append(summary.Symbols, sym)

	// Extract dataclass fields as symbols.
	if isClass && isDataclass {
		p.extractDataclassFields(node, classStack, pkg, name, content, summary)
	}

	// Decorators become annotations.
	for _, ann := range decorators {
		ann.OwnerFQN = fqn
		summary.Annotations = append(summary.Annotations, ann)
	}
}

func fillPythonSymbolMeta(sym *Symbol, decl *sitter.Node, content []byte) {
	if sym == nil || decl == nil {
		return
	}
	sym.StartByte = int64(decl.StartByte())
	sym.EndByte = int64(decl.EndByte())
	sym.StartLine = int64(decl.StartPosition().Row) + 1
	sym.EndLine = int64(decl.EndPosition().Row) + 1
	sym.Signature = pythonSignature(content, int(decl.StartByte()))
	sym.DocComment = pythonLeadingDocstring(decl, content)
}

// extractDataclassFields extracts typed field annotations from a dataclass body.
// Example: `name: str` or `count: int = 0` become SymField symbols.
func (p *PythonIndexer) extractDataclassFields(classNode *sitter.Node, classStack []string, pkg, className string, content []byte, summary *FileSummary) {
	body := classNode.ChildByFieldName("body")
	if body == nil {
		return
	}

	newClassStack := append(classStack, className)
	classFQN := pythonFQN(pkg, classStack, className)

	for i := uint(0); i < body.NamedChildCount(); i++ {
		child := body.NamedChild(i)
		if child == nil {
			continue
		}

		// Look for expression_statement containing type annotation.
		// Dataclass fields are: `name: Type` or `name: Type = default`
		if child.Kind() == "expression_statement" {
			expr := child.NamedChild(0)
			if expr == nil {
				continue
			}

			var fieldName, fieldType string
			var fieldNode *sitter.Node

			switch expr.Kind() {
			case "assignment":
				// Pattern: name: Type = value
				left := expr.ChildByFieldName("left")
				if left != nil && left.Kind() == "identifier" {
					fieldName = text(left, content)
					fieldNode = expr
					// Type annotation is on the left side for typed assignments.
					if ann := expr.ChildByFieldName("type"); ann != nil {
						fieldType = text(ann, content)
					}
				}
			case "type":
				// Pattern: name: Type (no default)
				// The tree-sitter node structure for `name: Type` is a "type" node.
				nameNode := expr.NamedChild(0)
				if nameNode != nil {
					fieldName = text(nameNode, content)
					fieldNode = expr
				}
				if expr.NamedChildCount() > 1 {
					typeNode := expr.NamedChild(1)
					if typeNode != nil {
						fieldType = text(typeNode, content)
					}
				}
			}

			if fieldName == "" || strings.HasPrefix(fieldName, "_") {
				continue
			}

			fieldFQN := pythonFQN(pkg, newClassStack, fieldName)
			fieldSig := fieldName
			if fieldType != "" {
				fieldSig = fieldName + ": " + fieldType
			}

			sym := Symbol{
				Lang:     LangPy,
				Kind:     SymField,
				File:     summary.FilePath,
				Pkg:      classFQN,
				Name:     fieldName,
				FQN:      fieldFQN,
				Exported: true,
			}
			if fieldNode != nil {
				sym.StartByte = int64(fieldNode.StartByte())
				sym.EndByte = int64(fieldNode.EndByte())
				sym.StartLine = int64(fieldNode.StartPosition().Row) + 1
				sym.EndLine = int64(fieldNode.EndPosition().Row) + 1
			}
			sym.Signature = fieldSig
			summary.Symbols = append(summary.Symbols, sym)
		}
	}
}

// isInitPy returns true if the path is an __init__.py file.
func isInitPy(path string) bool {
	base := filepath.Base(path)
	return base == "__init__.py"
}

// extractReExports extracts re-exported symbols from __init__.py files.
// When __init__.py contains `from .submodule import X` or `from .submodule import X as Y`,
// we emit X (or Y) as a symbol of the package so it can be targeted by anchors.
func (p *PythonIndexer) extractReExports(root *sitter.Node, pkg string, content []byte, summary *FileSummary) {
	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		if n.Kind() == "import_from_statement" {
			raw := text(n, content)
			reExports := parseReExports(raw, pkg)
			for _, re := range reExports {
				sym := Symbol{
					Lang:     LangPy,
					Kind:     SymFunc, // Best guess; could be class/func/const.
					File:     summary.FilePath,
					Pkg:      pkg,
					Name:     re.localName,
					FQN:      pythonFQN(pkg, nil, re.localName),
					Exported: true,
				}
				sym.Signature = "re-export from " + re.sourceFQN
				summary.Symbols = append(summary.Symbols, sym)
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(root)
}

type reExport struct {
	localName string // Name exported by this package
	sourceFQN string // FQN of the original symbol
}

// parseReExports parses `from .X import Y` or `from .X import Y as Z` statements.
func parseReExports(raw string, currentPkg string) []reExport {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "from ") {
		return nil
	}
	rest := strings.TrimSpace(strings.TrimPrefix(raw, "from "))
	fields := strings.Fields(rest)
	if len(fields) < 2 || fields[1] != "import" {
		return nil
	}
	origModule := fields[0]
	// Only process relative imports (re-exports from submodules).
	if !strings.HasPrefix(origModule, ".") {
		return nil
	}
	// Use the __init__.py resolver since this function is only called for __init__.py files.
	module := resolveRelativeModuleForInitPy(currentPkg, origModule)

	importPart := strings.TrimSpace(strings.TrimPrefix(rest, origModule))
	importPart = strings.TrimSpace(strings.TrimPrefix(importPart, "import"))

	var out []reExport
	items := strings.Split(importPart, ",")
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || item == "*" {
			continue
		}
		name, alias := splitAlias(item)
		localName := alias
		if localName == "" {
			localName = lastSegment(name)
		}
		sourceFQN := module + "." + name
		out = append(out, reExport{
			localName: localName,
			sourceFQN: sourceFQN,
		})
	}
	return out
}

func pythonSignature(content []byte, startByte int) string {
	if startByte < 0 || startByte >= len(content) {
		return ""
	}
	const lookahead = 600
	end := startByte + lookahead
	if end > len(content) {
		end = len(content)
	}
	slice := content[startByte:end]
	// Signature heuristics: first line up to ':'.
	if i := bytes.IndexByte(slice, '\n'); i >= 0 {
		slice = slice[:i]
	}
	if i := bytes.IndexByte(slice, ':'); i >= 0 {
		slice = slice[:i+1]
	}
	sig := strings.TrimSpace(string(slice))
	sig = strings.ReplaceAll(sig, "\t", " ")
	sig = strings.Join(strings.Fields(sig), " ")
	return sig
}

func pythonLeadingDocstring(decl *sitter.Node, content []byte) string {
	if decl == nil {
		return ""
	}
	body := decl.ChildByFieldName("body")
	if body == nil {
		return ""
	}
	// First statement in the body can be a docstring: expression_statement -> string.
	first := body.NamedChild(0)
	if first == nil || first.Kind() != "expression_statement" {
		return ""
	}
	strNode := first.NamedChild(0)
	if strNode == nil || strNode.Kind() != "string" {
		return ""
	}
	raw := strings.TrimSpace(text(strNode, content))
	return strings.TrimSpace(unquotePythonString(raw))
}

func pythonModuleDocstring(root *sitter.Node, content []byte) string {
	if root == nil {
		return ""
	}
	first := root.NamedChild(0)
	if first == nil || first.Kind() != "expression_statement" {
		return pythonLeadingComments(content)
	}
	strNode := first.NamedChild(0)
	if strNode == nil || strNode.Kind() != "string" {
		return pythonLeadingComments(content)
	}
	raw := strings.TrimSpace(text(strNode, content))
	return strings.TrimSpace(unquotePythonString(raw))
}

func pythonLeadingComments(content []byte) string {
	if len(content) == 0 {
		return ""
	}
	lines := strings.Split(string(content), "\n")
	var out []string
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" {
			if len(out) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(trim, "#") {
			out = append(out, strings.TrimSpace(strings.TrimPrefix(trim, "#")))
			continue
		}
		break
	}
	if len(out) == 0 {
		return ""
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func unquotePythonString(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	for _, q := range []string{`"""`, `'''`} {
		if strings.HasPrefix(raw, q) && strings.HasSuffix(raw, q) && len(raw) >= 2*len(q) {
			return raw[len(q) : len(raw)-len(q)]
		}
	}
	if len(raw) >= 2 {
		if (raw[0] == '"' && raw[len(raw)-1] == '"') || (raw[0] == '\'' && raw[len(raw)-1] == '\'') {
			return raw[1 : len(raw)-1]
		}
	}
	return raw
}

func (p *PythonIndexer) callSite(node *sitter.Node, content []byte, pkg string, imports map[string]string, localTypeFQNs map[string]string, knownTypeFQNs map[string]string, parseErrored bool) *CallSite {
	if node == nil {
		return nil
	}
	fnNode := node.ChildByFieldName("function")
	if fnNode == nil {
		return nil
	}
	ident := text(fnNode, content)
	if ident == "" {
		return nil
	}

	if localTypeFQNs != nil && !strings.Contains(ident, ".") {
		// Handle constructor-like calls on typed locals, e.g. `cls()` in a classmethod.
		if typeFQN := strings.TrimSpace(localTypeFQNs[ident]); typeFQN != "" {
			refPkg, refName := splitFQN(typeFQN)
			if refPkg != "" && refName != "" {
				return &CallSite{
					CalleeSymbol: SymbolRef{
						Lang: LangPy,
						Pkg:  refPkg,
						Name: refName,
					},
				}
			}
		}
	}

	// Prefer import-aware resolution for dotted names (e.g. `foo.bar()` where `foo` is imported).
	refPkg, refName := resolveSymbolRef(ident, pkg, imports)
	if parseErrored && !strings.Contains(ident, ".") {
		if _, ok := imports[ident]; !ok && !pythonBuiltins[ident] && refPkg == pkg {
			refPkg = ""
		}
	}
	if refPkg == "" && strings.Contains(ident, ".") {
		parts := strings.Split(ident, ".")
		// Best-effort: resolve simple receiver calls like `self.compute()` or `store.persist()`.
		if len(parts) == 2 {
			receiver := strings.TrimSpace(parts[0])
			method := strings.TrimSpace(parts[1])
			typeFQN := ""
			if receiver != "" {
				if localTypeFQNs != nil {
					typeFQN = strings.TrimSpace(localTypeFQNs[receiver])
				}
				if typeFQN == "" && knownTypeFQNs != nil {
					typeFQN = strings.TrimSpace(knownTypeFQNs[receiver])
				}
				// `super().method()` is common in Python; treat as a call on the current class.
				if typeFQN == "" && (receiver == "super()" || receiver == "super") && localTypeFQNs != nil {
					typeFQN = strings.TrimSpace(localTypeFQNs["self"])
				}
			}
			if typeFQN != "" && method != "" {
				refPkg, refName = splitFQN(typeFQN + "." + method)
			}
		}
	}
	if refPkg == "" {
		if parseErrored || len(imports) == 0 {
			return &CallSite{CalleeSymbol: SymbolRef{Lang: LangPy, Name: refName}}
		}
		return nil
	}
	ref := SymbolRef{
		Lang: LangPy,
		Name: refName,
		Pkg:  refPkg,
	}
	return &CallSite{
		CalleeSymbol: ref,
	}
}

func pythonLocalTypeFQNs(fn *sitter.Node, content []byte, pkg string, imports map[string]string, classStack []string) map[string]string {
	if fn == nil {
		return nil
	}
	out := make(map[string]string)
	if len(classStack) > 0 {
		classFQN := pythonFQN(pkg, classStack, "")
		if classFQN != "" {
			out["self"] = classFQN
			out["cls"] = classFQN
		}
	}

	params := fn.ChildByFieldName("parameters")
	if params == nil {
		if len(out) == 0 {
			return nil
		}
		return out
	}
	for i := uint(0); i < params.NamedChildCount(); i++ {
		param := params.NamedChild(i)
		if param == nil {
			continue
		}
		raw := text(param, content)
		if raw == "" {
			continue
		}
		raw = strings.TrimLeft(raw, "*")
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		// Strip defaults: `x: T = ...` or `x=...`.
		rawNoDefault := strings.SplitN(raw, "=", 2)[0]
		rawNoDefault = strings.TrimSpace(rawNoDefault)
		if rawNoDefault == "" {
			continue
		}

		namePart := rawNoDefault
		typePart := ""
		if colon := strings.Index(rawNoDefault, ":"); colon >= 0 {
			namePart = strings.TrimSpace(rawNoDefault[:colon])
			typePart = strings.TrimSpace(rawNoDefault[colon+1:])
		}
		if namePart == "" || typePart == "" {
			continue
		}

		if fqn := pythonResolveTypeFQN(typePart, pkg, imports); fqn != "" {
			out[namePart] = fqn
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// extractFunctionTypeRefs extracts type references from a function's signature.
// This includes parameter type annotations and return type annotations.
func (p *PythonIndexer) extractFunctionTypeRefs(fn *sitter.Node, content []byte, pkg string, imports map[string]string, ownerFQN, filePath string) []TypeRef {
	if fn == nil {
		return nil
	}
	var out []TypeRef
	seen := make(map[string]struct{})

	addTypeRef := func(typeExpr string) {
		fqn := pythonResolveTypeFQN(typeExpr, pkg, imports)
		if fqn == "" {
			return
		}
		if _, ok := seen[fqn]; ok {
			return
		}
		seen[fqn] = struct{}{}
		refPkg, refName := splitFQN(fqn)
		if refPkg == "" || refName == "" {
			return
		}
		out = append(out, TypeRef{
			File:     filePath,
			OwnerFQN: ownerFQN,
			TypeSym:  SymbolRef{Lang: LangPy, Pkg: refPkg, Name: refName},
		})
	}

	// Extract parameter type annotations.
	params := fn.ChildByFieldName("parameters")
	if params != nil {
		for i := uint(0); i < params.NamedChildCount(); i++ {
			param := params.NamedChild(i)
			if param == nil {
				continue
			}
			raw := text(param, content)
			if raw == "" {
				continue
			}
			raw = strings.TrimLeft(raw, "*")
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			// Strip defaults: `x: T = ...` or `x=...`.
			rawNoDefault := strings.SplitN(raw, "=", 2)[0]
			rawNoDefault = strings.TrimSpace(rawNoDefault)
			if rawNoDefault == "" {
				continue
			}
			if colon := strings.Index(rawNoDefault, ":"); colon >= 0 {
				typePart := strings.TrimSpace(rawNoDefault[colon+1:])
				if typePart != "" {
					addTypeRef(typePart)
				}
			}
		}
	}

	// Extract return type annotation.
	retType := fn.ChildByFieldName("return_type")
	if retType != nil {
		typeExpr := text(retType, content)
		if typeExpr != "" {
			addTypeRef(typeExpr)
		}
	}

	return out
}

func pythonResolveTypeFQN(typeExpr string, defaultPkg string, imports map[string]string) string {
	typeExpr = strings.TrimSpace(typeExpr)
	if typeExpr == "" {
		return ""
	}
	candidates := pythonTypeIdentRE.FindAllString(typeExpr, -1)
	if len(candidates) == 0 {
		return ""
	}
	for i := len(candidates) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(candidates[i])
		if candidate == "" {
			continue
		}
		refPkg, refName := resolveSymbolRef(candidate, defaultPkg, imports)
		if refPkg == "" || refName == "" {
			continue
		}
		if refPkg == "builtins" {
			continue
		}
		if pythonTypingWrappers[refName] || refName == "None" {
			continue
		}
		return normalizeSymbol(SymbolRef{Lang: LangPy, Pkg: refPkg, Name: refName})
	}
	return ""
}

func collectDecorators(node *sitter.Node, content []byte, pkg string, imports map[string]string) []AnnotationUse {
	var out []AnnotationUse
	for i := uint(0); i < node.NamedChildCount(); i++ {
		ch := node.NamedChild(i)
		if ch == nil || ch.Kind() != "decorator" {
			continue
		}
		name := decoratorName(ch, content)
		if name == "" {
			continue
		}
		args := parseDecoratorArgsNode(ch, content)
		refPkg, refName := resolveDecorator(name, pkg, imports)
		out = append(out, AnnotationUse{
			AnnSymbol: SymbolRef{
				Lang: LangPy,
				Pkg:  refPkg,
				Name: refName,
			},
			Args: args,
		})
	}
	return out
}

func parseDecoratorArgs(raw string) map[string]string {
	start := strings.Index(raw, "(")
	end := strings.LastIndex(raw, ")")
	if start < 0 || end <= start {
		return nil
	}
	body := strings.TrimSpace(raw[start+1 : end])
	if body == "" {
		return nil
	}
	// Skip unsupported patterns (walrus, f-strings) rather than misparse.
	if strings.Contains(body, ":=") ||
		strings.Contains(body, `f"`) || strings.Contains(body, `f'`) ||
		strings.Contains(body, `F"`) || strings.Contains(body, `F'`) {
		return nil
	}
	args := make(map[string]string)
	var current strings.Builder
	depth := 0
	inString := false
	tripleQuote := false
	var quote rune
	escaped := false
	i := 0
	runes := []rune(body)

	commit := func() {
		part := strings.TrimSpace(current.String())
		current.Reset()
		if part == "" {
			return
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		// Strip string prefixes (r, f, rf, fr, b, etc.)
		val = strings.TrimLeft(val, "rRfFbBuU")
		// Strip triple quotes or single quotes
		if strings.HasPrefix(val, `"""`) && strings.HasSuffix(val, `"""`) {
			val = val[3 : len(val)-3]
		} else if strings.HasPrefix(val, `'''`) && strings.HasSuffix(val, `'''`) {
			val = val[3 : len(val)-3]
		} else {
			val = strings.Trim(val, `"'`)
		}
		if key != "" && val != "" {
			args[key] = val
		}
	}

	for i < len(runes) {
		r := runes[i]

		if escaped {
			current.WriteRune(r)
			escaped = false
			i++
			continue
		}

		if inString {
			current.WriteRune(r)
			if r == '\\' {
				escaped = true
				i++
				continue
			}
			// Check for end of string
			if tripleQuote {
				// Look for matching triple quote
				if r == quote && i+2 < len(runes) && runes[i+1] == quote && runes[i+2] == quote {
					current.WriteRune(runes[i+1])
					current.WriteRune(runes[i+2])
					inString = false
					tripleQuote = false
					i += 3
					continue
				}
			} else {
				if r == quote {
					inString = false
				}
			}
			i++
			continue
		}

		switch {
		case r == '"' || r == '\'':
			// Check for triple quote
			if i+2 < len(runes) && runes[i+1] == r && runes[i+2] == r {
				inString = true
				tripleQuote = true
				quote = r
				current.WriteRune(r)
				current.WriteRune(runes[i+1])
				current.WriteRune(runes[i+2])
				i += 3
				continue
			}
			inString = true
			tripleQuote = false
			quote = r
			current.WriteRune(r)
		case r == '(' || r == '[' || r == '{':
			depth++
			current.WriteRune(r)
		case r == ')' || r == ']' || r == '}':
			if depth > 0 {
				depth--
			}
			current.WriteRune(r)
		case r == ',' && depth == 0:
			commit()
		default:
			current.WriteRune(r)
		}
		i++
	}
	commit()
	return args
}

// extractIdentifiers extracts base class identifiers, preserving full dotted names.
func extractIdentifiers(node *sitter.Node, content []byte) []string {
	var out []string
	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		switch n.Kind() {
		case "identifier", "dotted_name":
			if name := text(n, content); name != "" {
				// Preserve full dotted name for proper resolution
				out = append(out, name)
			}
			return // Don't descend into dotted_name children (they're parts)
		case "attribute":
			// Handle chained attribute access like module.submodule.Class
			if name := text(n, content); name != "" {
				out = append(out, name)
			}
			return
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(node)
	return out
}

func walkChildren(node *sitter.Node, classStack []string, fn func(*sitter.Node, []string)) {
	for i := uint(0); i < node.NamedChildCount(); i++ {
		fn(node.NamedChild(i), classStack)
	}
}

func text(node *sitter.Node, content []byte) string {
	if node == nil {
		return ""
	}
	start := node.StartByte()
	end := node.EndByte()
	if end <= start || int(end) > len(content) {
		return ""
	}
	return string(bytes.TrimSpace(content[start:end]))
}

// pythonModuleFromPath converts a file path to a Python module name.
// It tries to strip configured source roots to get the correct module path.
func pythonModuleFromPath(path string, roots []string) string {
	clean := filepath.ToSlash(strings.TrimPrefix(path, "./"))
	noExt := strings.TrimSuffix(clean, filepath.Ext(clean))

	// Try to strip a configured source root.
	for _, root := range roots {
		rootSlash := filepath.ToSlash(root)
		if !strings.HasSuffix(rootSlash, "/") {
			rootSlash += "/"
		}
		rootSlash = strings.TrimPrefix(rootSlash, "./")
		if strings.HasPrefix(noExt, rootSlash) {
			noExt = strings.TrimPrefix(noExt, rootSlash)
			break
		}
		// Handle absolute paths by checking if the path contains the root.
		if idx := strings.Index(noExt, rootSlash); idx >= 0 {
			noExt = noExt[idx+len(rootSlash):]
			break
		}
	}

	// Strip __init__ suffix for package modules.
	noExt = strings.TrimPrefix(noExt, "/")
	noExt = strings.TrimSuffix(noExt, "/__init__")

	parts := strings.Split(noExt, "/")
	filtered := parts[:0]
	for _, part := range parts {
		if part == "" || part == "__init__" {
			continue
		}
		filtered = append(filtered, part)
	}

	return strings.Join(filtered, ".")
}

func pythonFQN(pkg string, classStack []string, name string) string {
	parts := []string{pkg}
	if len(classStack) > 0 {
		parts = append(parts, strings.Join(classStack, "."))
	}
	parts = append(parts, name)
	return strings.Trim(strings.Join(parts, "."), ".")
}

func lastSegment(name string) string {
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

func packageFromQualified(q string) string {
	if idx := strings.LastIndex(q, "."); idx >= 0 {
		return q[:idx]
	}
	return q
}

// collectImportsAndAliases parses import statements from the AST and returns:
// - aliases: map of local name -> fully qualified module.symbol
// - modules: list of imported module names (for import edges)
// isInitPy should be true for __init__.py files (affects relative import resolution).
func collectImportsAndAliases(root *sitter.Node, content []byte, currentPkg string, isInitPy bool) (map[string]string, []string) {
	aliases := make(map[string]string)
	var modules []string
	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		switch n.Kind() {
		case "import_statement", "import_from_statement":
			raw := text(n, content)
			parseImport(raw, currentPkg, aliases, &modules, isInitPy)
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(root)
	if len(aliases) == 0 || (root != nil && root.HasError()) {
		fallbackAliases := make(map[string]string)
		var fallbackModules []string
		scanImportAliasesFallback(content, currentPkg, fallbackAliases, &fallbackModules, isInitPy)
		for alias, full := range fallbackAliases {
			if _, ok := aliases[alias]; ok {
				continue
			}
			aliases[alias] = full
		}
		if len(fallbackModules) > 0 {
			modules = append(modules, fallbackModules...)
		}
	}
	return aliases, modules
}

func scanImportAliasesFallback(content []byte, currentPkg string, aliases map[string]string, modules *[]string, isInitPy bool) {
	if len(content) == 0 {
		return
	}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "import ") || strings.HasPrefix(line, "from ") {
			parseImport(line, currentPkg, aliases, modules, isInitPy)
		}
	}
}

// parseImport parses an import statement and populates the aliases map.
// If modules is non-nil, it also collects the imported module names for import edges.
// isInitPy should be true for __init__.py files (affects relative import resolution).
func parseImport(raw string, currentPkg string, aliases map[string]string, modules *[]string, isInitPy bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "import ") {
		rest := strings.TrimSpace(strings.TrimPrefix(raw, "import "))
		parts := strings.Split(rest, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			name, alias := splitAlias(part)
			name = canonicalizeImportModule(currentPkg, name)
			if alias == "" {
				// Python binds the top-level package name for dotted imports:
				// `import foo.bar` binds `foo`, not `bar`.
				bound := name
				if dot := strings.Index(bound, "."); dot >= 0 {
					bound = bound[:dot]
				}
				alias = bound
				aliases[alias] = canonicalizeImportModule(currentPkg, bound)
			} else {
				aliases[alias] = name
			}
			// Collect the full module for import edges.
			if modules != nil && name != "" {
				*modules = append(*modules, name)
			}
		}
		return
	}
	if strings.HasPrefix(raw, "from ") {
		rest := strings.TrimSpace(strings.TrimPrefix(raw, "from "))
		fields := strings.Fields(rest)
		if len(fields) < 2 || fields[1] != "import" {
			return
		}
		origModule := fields[0]
		var module string
		if isInitPy {
			module = resolveRelativeModuleForInitPy(currentPkg, origModule)
		} else {
			module = resolveRelativeModule(currentPkg, origModule)
		}
		module = canonicalizeImportModule(currentPkg, module)
		// Collect the module for import edges (before symbol name is appended).
		if modules != nil && module != "" {
			*modules = append(*modules, module)
		}
		importPart := strings.TrimSpace(strings.TrimPrefix(rest, origModule))
		importPart = strings.TrimSpace(strings.TrimPrefix(importPart, "import"))
		items := strings.Split(importPart, ",")
		for _, item := range items {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			name, alias := splitAlias(item)
			if alias == "" {
				alias = lastSegment(name)
			}
			aliases[alias] = module + "." + name
		}
	}
}

func splitAlias(part string) (string, string) {
	if strings.Contains(part, " as ") {
		kv := strings.SplitN(part, " as ", 2)
		return strings.TrimSpace(kv[0]), strings.TrimSpace(kv[1])
	}
	return strings.TrimSpace(part), ""
}

// canonicalizeImportModule applies a small heuristic to align absolute import module names
// with the module naming implied by a source-root that includes a `src/` shim directory.
//
// Example:
// - currentPkg: "src.charm.core.badges.rules.early_bird"
// - import:     "charm.core.events.CEP.definition"
// - result:     "src.charm.core.events.CEP.definition"
//
// This helps avoid a common mismatch where code imports `charm...` but the computed module
// paths (from file layout) are `src.charm...` because the configured python root is the
// repository root (containing `src/`).
//
// Note: For deeper nesting (e.g., backend/src/charm), suffix matching in
// IntelAnchorIDsByFQNsAndLang handles the resolution at edge-creation time.
func canonicalizeImportModule(currentPkg, module string) string {
	module = strings.TrimSpace(module)
	if module == "" || strings.HasPrefix(module, ".") {
		return module
	}
	cur := strings.TrimSpace(currentPkg)
	if cur == "" {
		return module
	}
	curParts := strings.Split(cur, ".")
	modParts := strings.Split(module, ".")
	if len(curParts) >= 2 && curParts[0] == "src" && len(modParts) >= 1 && modParts[0] == curParts[1] && modParts[0] != "src" {
		return "src." + module
	}
	return module
}

func resolveDecorator(name string, pkg string, imports map[string]string) (string, string) {
	if idx := strings.Index(name, "("); idx > 0 {
		name = name[:idx]
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return pkg, name
	}
	if builtin, ok := builtinDecorator(name); ok {
		return builtin, name
	}
	return resolveSymbolRef(name, pkg, imports)
}

func resolveSymbolRef(name string, defaultPkg string, imports map[string]string) (string, string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return defaultPkg, name
	}
	if strings.Contains(name, ".") {
		parts := strings.SplitN(name, ".", 2)
		base := name
		if full, ok := imports[parts[0]]; ok {
			base = full + "." + parts[1]
		} else {
			// Unknown receiver/namespace (likely a local variable): don't guess a package.
			return "", lastSegment(name)
		}
		return packageFromQualified(base), lastSegment(base)
	}
	if full, ok := imports[name]; ok {
		return packageFromQualified(full), lastSegment(full)
	}
	if pythonBuiltins[name] {
		return "builtins", name
	}
	if defaultPkg != "" {
		// Fall back to current module/package for unresolved bare names.
		return defaultPkg, name
	}
	// Unknown package; return empty to avoid incorrect matches.
	return "", name
}

func builtinDecorator(name string) (string, bool) {
	switch name {
	case "property", "classmethod", "staticmethod":
		return "builtins", true
	case "dataclass":
		return "dataclasses", true
	}
	return "", false
}

var pythonBuiltins = map[string]bool{
	"int":           true,
	"str":           true,
	"float":         true,
	"bool":          true,
	"list":          true,
	"dict":          true,
	"set":           true,
	"tuple":         true,
	"object":        true,
	"bytes":         true,
	"BaseException": true,
	"Exception":     true,
	"type":          true,
	"len":           true,
	"range":         true,
	"print":         true,
	"enumerate":     true,
	"zip":           true,
	"map":           true,
	"filter":        true,
	"iter":          true,
	"next":          true,
}

func resolveRelativeModule(currentPkg, mod string) string {
	if !strings.HasPrefix(mod, ".") {
		return mod
	}
	dots := 0
	for dots < len(mod) && mod[dots] == '.' {
		dots++
	}
	trimmed := strings.TrimPrefix(mod, strings.Repeat(".", dots))
	base := currentPkg
	for i := 0; i < dots; i++ {
		base = packageFromQualified(base)
	}
	if trimmed == "" {
		return base
	}
	if base == "" {
		return trimmed
	}
	return base + "." + trimmed
}

// resolveRelativeModuleForInitPy is like resolveRelativeModule but for __init__.py files.
// In __init__.py, the module name is the package itself, so we go up (dots - 1) levels
// instead of dots levels.
func resolveRelativeModuleForInitPy(currentPkg, mod string) string {
	if !strings.HasPrefix(mod, ".") {
		return mod
	}
	dots := 0
	for dots < len(mod) && mod[dots] == '.' {
		dots++
	}
	trimmed := strings.TrimPrefix(mod, strings.Repeat(".", dots))
	base := currentPkg
	// For __init__.py, the module name is the package, so we go up (dots - 1) levels.
	for i := 1; i < dots; i++ {
		base = packageFromQualified(base)
	}
	if trimmed == "" {
		return base
	}
	if base == "" {
		return trimmed
	}
	return base + "." + trimmed
}

func decoratorName(dec *sitter.Node, content []byte) string {
	nameNode := dec.ChildByFieldName("name")
	if nameNode == nil && dec.NamedChildCount() > 0 {
		nameNode = dec.NamedChild(0)
	}
	name := text(nameNode, content)
	if name == "" {
		return ""
	}
	if idx := strings.Index(name, "("); idx > 0 {
		name = name[:idx]
	}
	return name
}

func parseDecoratorArgsNode(dec *sitter.Node, content []byte) map[string]string {
	argsNode := dec.ChildByFieldName("arguments")
	if argsNode == nil {
		for i := uint(0); i < dec.NamedChildCount(); i++ {
			child := dec.NamedChild(i)
			if child != nil && child.Kind() == "argument_list" {
				argsNode = child
				break
			}
		}
	}
	if argsNode == nil {
		return parseDecoratorArgs(text(dec, content))
	}
	args := make(map[string]string)
	for i := uint(0); i < argsNode.NamedChildCount(); i++ {
		arg := argsNode.NamedChild(i)
		if arg == nil {
			continue
		}
		if arg.Kind() == "keyword_argument" {
			key := text(arg.ChildByFieldName("name"), content)
			val := text(arg.ChildByFieldName("value"), content)
			if key != "" && val != "" {
				args[key] = val
			}
		}
	}
	return args
}

// PythonIndexerAvailable reports whether Python indexing is supported in this build.
func PythonIndexerAvailable() bool { return true }
