//go:build cgo

package codeanchor

// Docs:
// - [Code anchors (Hub)](docs/hubs/Code anchors (Hub).md)
// - [Code anchors - matching + scopes](docs/reference/analysis/code-anchors-matching-scopes.md)

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"github.com/atomicobject/rhizome/pkg/codefile"
	"github.com/atomicobject/rhizome/pkg/paths"

	sitter "github.com/tree-sitter/go-tree-sitter"
	sitterjavascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	sittertypescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

var jsxTagNamePattern = regexp.MustCompile(`(?m)<\s*([A-Za-z_][A-Za-z0-9_]*)(?:\s|/|>)`)

type tsPkgCacheEntry struct {
	pkgName string // from nearest package.json name
	pkgRoot string // directory containing that package.json
}

// TSIndexer implements LanguageIndexer for TypeScript/JavaScript (including TSX/JSX) using tree-sitter.
// It is intentionally best-effort: it prefers returning partial summaries over failing hard.

type TSIndexer struct {
	parserPool sync.Pool

	// repoRoot is the base directory used for repo-relative fallback.
	// It should typically be the repository root (or vault root if the vault is the repo).
	repoRoot string
	tailIdx  *PathTailIndex
	limits   TreeSitterLimits

	pkgMu                sync.Mutex
	pkgCache             map[string]*tsPkgCacheEntry // dir(abs) -> nearest package.json info (or nil for none)
	resolver             *tsModuleResolver
	externalVersionMu    sync.Mutex
	externalVersionCache map[string]tsExternalVersionManifest

	relationshipMu             sync.Mutex
	exportInventories          map[string]tsExportInventory
	exportResolutionMap        map[string]tsExportResolution
	exportResolutionGeneration uint64
}

// NewTSIndexerWithRoot constructs a TS/JS indexer rooted at repoRoot.
// repoRoot is normalized (absolute, symlinks resolved best-effort) for deterministic pkg strings.
func NewTSIndexerWithRoot(repoRoot string) *TSIndexer {
	return NewTSIndexerWithRootAndTailIndex(repoRoot, nil)
}

// NewTSIndexerWithRootAndTailIndex constructs a TS/JS indexer rooted at repoRoot with an optional path tail index.
func NewTSIndexerWithRootAndTailIndex(repoRoot string, tailIdx *PathTailIndex) *TSIndexer {
	return NewTSIndexerWithRootAndTailIndexAndLimits(repoRoot, tailIdx, TreeSitterLimits{})
}

// NewTSIndexerWithRootAndTailIndexAndLimits constructs a TS/JS indexer rooted at repoRoot with an optional path tail index and limits.
func NewTSIndexerWithRootAndTailIndexAndLimits(repoRoot string, tailIdx *PathTailIndex, limits TreeSitterLimits) *TSIndexer {
	idx := &TSIndexer{
		repoRoot:             paths.ResolveSymlinks(repoRoot).String(),
		tailIdx:              tailIdx,
		pkgCache:             make(map[string]*tsPkgCacheEntry),
		externalVersionCache: make(map[string]tsExternalVersionManifest),
		limits:               limits,
		exportInventories:    make(map[string]tsExportInventory),
		exportResolutionMap:  make(map[string]tsExportResolution),
	}
	idx.resolver = newTSModuleResolver(idx.repoRoot)
	idx.parserPool.New = func() any {
		return sitter.NewParser()
	}
	return idx
}

func (*TSIndexer) Lang() Lang { return LangTS }

// InvalidateModuleMetadata marks package/config resolution metadata dirty.
// The next source index or rebuild refreshes it; this does not itself reindex callers.
func (t *TSIndexer) InvalidateModuleMetadata(path string) {
	if t == nil {
		return
	}
	if t.resolver != nil {
		t.resolver.invalidateModuleMetadata(path)
	}
	t.relationshipMu.Lock()
	t.exportResolutionGeneration++
	t.exportResolutionMap = make(map[string]tsExportResolution)
	t.relationshipMu.Unlock()
	if path == "" || strings.EqualFold(filepath.Base(path), "package.json") {
		t.pkgMu.Lock()
		t.pkgCache = make(map[string]*tsPkgCacheEntry)
		t.pkgMu.Unlock()
		t.externalVersionMu.Lock()
		t.externalVersionCache = make(map[string]tsExternalVersionManifest)
		t.externalVersionMu.Unlock()
	}
}

func (t *TSIndexer) IndexFile(content []byte, ref paths.CodePathRef) (FileSummary, error) {
	relPath := ref.Rel.String()
	absPath := ref.Abs.String()
	filePath := relPath
	if filePath == "" {
		filePath = absPath
	}
	absForFS := absPath
	if absForFS == "" {
		absForFS = filePath
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	fileBase := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))

	if !codefile.IsTypeScriptJavaScriptExtension(ext) {
		return FileSummary{}, fmt.Errorf("%w: %s", ErrUnsupportedLanguage, ext)
	}

	parserAny := t.parserPool.Get()
	var parser *sitter.Parser
	if parserAny == nil {
		parser = sitter.NewParser()
	} else {
		parser = parserAny.(*sitter.Parser)
	}
	setLanguage := func(p *sitter.Parser) {
		switch ext {
		case ".ts", ".mts", ".cts":
			p.SetLanguage(sitter.NewLanguage(sittertypescript.LanguageTypescript()))
		case ".tsx":
			p.SetLanguage(sitter.NewLanguage(sittertypescript.LanguageTSX()))
		case ".js", ".jsx", ".mjs", ".cjs":
			p.SetLanguage(sitter.NewLanguage(sitterjavascript.Language()))
		}
	}
	setLanguage(parser)

	parseTimeout := resolveTreeSitterLimits(content, t.limits)
	tree, timedOut, err := parseTreeWithTimeout(parser, content, parseTimeout)
	if err != nil {
		parser.Close()
		return FileSummary{}, err
	}
	if timedOut {
		parser.Close()
		log.Printf("codeanchor: ts parse timeout for %s", filePath)
		return FileSummary{FilePath: filePath, Lang: LangTS, ParseStatus: ParseTimeout}, nil
	}
	defer t.parserPool.Put(parser)
	defer tree.Close()
	root := tree.RootNode()
	parseErrored := root.HasError()

	summary := FileSummary{
		FilePath: filePath,
		Lang:     LangTS,
		ParseStatus: func() ParseStatus {
			if parseErrored {
				return ParseErrored
			}
			return ParseOK
		}(),
	}
	summary.PackageDoc = tsFileDocComment(content)

	currentPkg := t.pkgForFile(absForFS)
	imports := t.collectImports(root, content, filepath.Dir(absForFS))
	lexicalSymbols := t.analyzeTSLexicalRelationships(root, content, currentPkg, filePath, &imports)
	summary.Symbols = append(summary.Symbols, lexicalSymbols...)

	// Deduplicate and add import edges.
	// For TypeScript, we store resolved file paths (not pkg names) since module anchors have empty FQNs.
	seenPaths := make(map[string]struct{})
	for _, path := range imports.importPaths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, ok := seenPaths[path]; ok {
			continue
		}
		seenPaths[path] = struct{}{}
		summary.Imports = append(summary.Imports, ImportEdge{Module: path})
	}

	var walk func(n *sitter.Node, inTopLevel bool, ownerFQN string)
	walk = func(n *sitter.Node, inTopLevel bool, ownerFQN string) {
		if n == nil {
			return
		}

		// childTopLevel controls whether descendants should be considered top-level.
		// The current node still uses the incoming inTopLevel value for symbol extraction.
		childTopLevel := inTopLevel
		switch n.Kind() {
		case "function_declaration", "arrow_function", "function", "function_expression", "method_definition", "class_body":
			childTopLevel = false
		}

		// Track the nearest enclosing named function-ish symbol; attribute calls to it.
		switch n.Kind() {
		case "function_declaration":
			if name := tsStableBindingName(n.ChildByFieldName("name"), content); name != "" {
				ownerFQN = (Symbol{Pkg: currentPkg, Name: name}).NormalizeFQN()
			}
		case "class_declaration", "interface_declaration", "type_alias_declaration", "enum_declaration":
			if name := tsStableBindingName(n.ChildByFieldName("name"), content); name != "" {
				ownerFQN = (Symbol{Pkg: currentPkg, Name: name}).NormalizeFQN()
			}
		case "method_definition":
			if methodName := tsStableBindingName(n.ChildByFieldName("name"), content); methodName != "" {
				if ownerFQN != "" {
					ownerFQN = ownerFQN + "." + methodName
				}
			}
		case "variable_declarator":
			// const Foo = () => {} / function() {} / memo(() => {})
			name := tsStableBindingName(n.ChildByFieldName("name"), content)
			if name != "" {
				val := n.ChildByFieldName("value")
				if val != nil {
					switch val.Kind() {
					case "arrow_function", "function", "function_expression", "call_expression":
						ownerFQN = (Symbol{Pkg: currentPkg, Name: name}).NormalizeFQN()
					}
				}
			}
		}

		switch n.Kind() {
		case "export_statement", "export_default_declaration":
			if !inTopLevel {
				break
			}
			if n.Kind() == "export_statement" {
				summary.Symbols = append(summary.Symbols, t.extractTSReExportSymbols(n, content, currentPkg, filePath)...)
				if !strings.HasPrefix(strings.TrimSpace(nodeText(n, content)), "export default") {
					break
				}
			}
			// Default imports resolve to the file-base alias, independently of the
			// declaration's name. The normal child walk retains declaration metadata.
			decl := n.ChildByFieldName("declaration")
			if decl == nil || fileBase == "" {
				break
			}
			var kind SymbolKind
			switch decl.Kind() {
			case "function_declaration":
				kind = SymFunc
			case "class_declaration":
				kind = SymClass
			}
			if kind != "" {
				summary.Symbols = append(summary.Symbols, Symbol{
					Lang: LangTS, Kind: kind, File: filePath, Pkg: currentPkg, Name: fileBase,
				})
			}
		case "function_declaration":
			name := tsStableBindingName(n.ChildByFieldName("name"), content)
			if inTopLevel && name != "" {
				sym := Symbol{Lang: LangTS, Kind: SymFunc, File: filePath, Pkg: currentPkg, Name: name}
				fillTSSymbolMeta(&sym, n, content)
				summary.Symbols = append(summary.Symbols, sym)
			}
			// Extract type refs from function signature.
			typeRefs := t.extractFunctionTypeRefs(n, content, currentPkg, ownerFQN, filePath, imports)
			summary.TypeRefs = append(summary.TypeRefs, typeRefs...)
		case "class_declaration":
			if inTopLevel {
				if name := tsStableBindingName(n.ChildByFieldName("name"), content); name != "" {
					sym := Symbol{Lang: LangTS, Kind: SymClass, File: filePath, Pkg: currentPkg, Name: name}
					fillTSSymbolMeta(&sym, n, content)
					summary.Symbols = append(summary.Symbols, sym)
				}
			}
			summary.Supers = append(summary.Supers, t.extractTSSupers(n, content, currentPkg, ownerFQN, imports)...)
		case "interface_declaration":
			if inTopLevel {
				if name := tsStableBindingName(n.ChildByFieldName("name"), content); name != "" {
					sym := Symbol{Lang: LangTS, Kind: SymInterface, File: filePath, Pkg: currentPkg, Name: name}
					fillTSSymbolMeta(&sym, n, content)
					summary.Symbols = append(summary.Symbols, sym)
				}
			}
			summary.Supers = append(summary.Supers, t.extractTSSupers(n, content, currentPkg, ownerFQN, imports)...)
		case "type_alias_declaration":
			if inTopLevel {
				if name := tsStableBindingName(n.ChildByFieldName("name"), content); name != "" {
					sym := Symbol{Lang: LangTS, Kind: SymType, File: filePath, Pkg: currentPkg, Name: name}
					fillTSSymbolMeta(&sym, n, content)
					summary.Symbols = append(summary.Symbols, sym)
				}
			}
			summary.TypeRefs = append(summary.TypeRefs, t.extractTypeRefsFromNode(tsTypeAliasValueNode(n), content, currentPkg, ownerFQN, filePath, imports)...)
		case "enum_declaration":
			if inTopLevel {
				if name := tsStableBindingName(n.ChildByFieldName("name"), content); name != "" {
					sym := Symbol{Lang: LangTS, Kind: SymEnum, File: filePath, Pkg: currentPkg, Name: name}
					fillTSSymbolMeta(&sym, n, content)
					summary.Symbols = append(summary.Symbols, sym)
				}
			}
		case "variable_declarator":
			// Capture common top-level function-ish assignments:
			//   const Foo = () => {}
			//   const Foo = function() {}
			name := tsStableBindingName(n.ChildByFieldName("name"), content)
			val := n.ChildByFieldName("value")
			if inTopLevel && name != "" && val != nil {
				switch val.Kind() {
				case "arrow_function", "function", "function_expression":
					sym := Symbol{Lang: LangTS, Kind: SymFunc, File: filePath, Pkg: currentPkg, Name: name}
					fillTSSymbolMeta(&sym, n, content)
					summary.Symbols = append(summary.Symbols, sym)
				case "call_expression":
					// Common HOC patterns:
					//   export const Foo = memo(() => ...)
					//   export const Foo = forwardRef(function Foo() {})
					if hasCallableArg(val) {
						sym := Symbol{Lang: LangTS, Kind: SymFunc, File: filePath, Pkg: currentPkg, Name: name}
						fillTSSymbolMeta(&sym, n, content)
						summary.Symbols = append(summary.Symbols, sym)
					}
				case "object":
					methodSyms, methodTypeRefs := t.extractTopLevelObjectLiteralMethods(val, content, currentPkg, filePath, imports)
					summary.Symbols = append(summary.Symbols, methodSyms...)
					summary.TypeRefs = append(summary.TypeRefs, methodTypeRefs...)
				}
			}
			if ann := tsTypeAnnotationNode(n); ann != nil {
				summary.TypeRefs = append(summary.TypeRefs, t.extractTypeRefsFromNode(ann, content, currentPkg, ownerFQN, filePath, imports)...)
			}
			// Extract type refs from arrow functions and function expressions.
			if name != "" && val != nil {
				switch val.Kind() {
				case "arrow_function", "function", "function_expression":
					funcOwner := (Symbol{Pkg: currentPkg, Name: name}).NormalizeFQN()
					typeRefs := t.extractFunctionTypeRefs(val, content, currentPkg, funcOwner, filePath, imports)
					summary.TypeRefs = append(summary.TypeRefs, typeRefs...)
				}
			}
		case "arrow_function", "function_expression":
			// Extract type refs from anonymous functions (when not handled by variable_declarator).
			typeRefs := t.extractFunctionTypeRefs(n, content, currentPkg, ownerFQN, filePath, imports)
			summary.TypeRefs = append(summary.TypeRefs, typeRefs...)
		case "method_definition":
			// Extract type refs from class methods.
			typeRefs := t.extractFunctionTypeRefs(n, content, currentPkg, ownerFQN, filePath, imports)
			summary.TypeRefs = append(summary.TypeRefs, typeRefs...)
			if tsMethodIsStatic(n, content) {
				if name := tsNameRelativeToPkg(currentPkg, ownerFQN); name != "" {
					sym := Symbol{Lang: LangTS, Kind: SymFunc, File: filePath, Pkg: currentPkg, Name: name}
					fillTSSymbolMeta(&sym, n, content)
					summary.Symbols = append(summary.Symbols, sym)
				}
			}
		case "public_field_definition", "property_signature":
			if ann := tsTypeAnnotationNode(n); ann != nil {
				summary.TypeRefs = append(summary.TypeRefs, t.extractTypeRefsFromNode(ann, content, currentPkg, ownerFQN, filePath, imports)...)
			}
		case "satisfies_expression":
			if typeNode := tsSatisfiesTypeNode(n); typeNode != nil {
				summary.TypeRefs = append(summary.TypeRefs, t.extractTypeRefsFromNode(typeNode, content, currentPkg, ownerFQN, filePath, imports)...)
			}
		case "call_expression":
			for _, cs := range t.callSitesFromCallExpr(n, content, currentPkg, imports) {
				cs.File = filePath
				cs.OwnerFQN = ownerFQN
				summary.Calls = append(summary.Calls, cs)
			}
		case "new_expression":
			// Treat `new Foo()` / `new NS.Foo()` as a "use" of the symbol.
			if cs := t.callSiteFromNewExpr(n, content, currentPkg, imports); cs != nil {
				cs.File = filePath
				cs.OwnerFQN = ownerFQN
				summary.Calls = append(summary.Calls, *cs)
			}
		case "jsx_self_closing_element", "jsx_opening_element", "jsx_element":
			if cs := t.callSiteFromJSXElement(n, content, currentPkg, imports); cs != nil {
				cs.File = filePath
				cs.OwnerFQN = ownerFQN
				summary.Calls = append(summary.Calls, *cs)
			}
		case "member_expression", "optional_chain":
			if mr := t.memberRefFromExpr(n, content, currentPkg, ownerFQN, filePath, imports); mr != nil {
				summary.MemberRefs = append(summary.MemberRefs, *mr)
			}
		}

		for i := uint(0); i < n.NamedChildCount(); i++ {
			walk(n.NamedChild(i), childTopLevel, ownerFQN)
		}
	}

	walk(root, true, "")
	summary.ExternalEvidence = t.extractTSExternalEvidence(root, content, filepath.Dir(absForFS), currentPkg, &summary)
	summary.ExternalEvidenceReady = true
	if parseErrored {
		logParseErrorIfPoor("ts", filePath, summary, "")
	}
	return summary, nil
}

func hasCallableArg(call *sitter.Node) bool {
	if call == nil {
		return false
	}
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return false
	}
	for i := uint(0); i < args.NamedChildCount(); i++ {
		arg := args.NamedChild(i)
		if arg == nil {
			continue
		}
		switch arg.Kind() {
		case "arrow_function", "function", "function_expression":
			return true
		}
	}
	return false
}

func (t *TSIndexer) extractTopLevelObjectLiteralMethods(obj *sitter.Node, content []byte, currentPkg, filePath string, imports tsImportBindings) ([]Symbol, []TypeRef) {
	if obj == nil || obj.Kind() != "object" {
		return nil, nil
	}

	var symbols []Symbol
	var typeRefs []TypeRef

	for i := uint(0); i < obj.NamedChildCount(); i++ {
		child := obj.NamedChild(i)
		if child == nil {
			continue
		}

		var (
			name string
			fn   *sitter.Node
		)

		switch child.Kind() {
		case "pair":
			name = tsObjectLiteralPropertyName(child.ChildByFieldName("key"), content)
			fn = child.ChildByFieldName("value")
			if fn == nil {
				continue
			}
			switch fn.Kind() {
			case "arrow_function", "function", "function_expression":
				// ok
			case "call_expression":
				if !hasCallableArg(fn) {
					continue
				}
			default:
				continue
			}
		case "method_definition":
			name = tsObjectLiteralPropertyName(child.ChildByFieldName("name"), content)
			fn = child
		default:
			continue
		}

		if name == "" {
			continue
		}

		sym := Symbol{Lang: LangTS, Kind: SymFunc, File: filePath, Pkg: currentPkg, Name: name}
		fillTSSymbolMeta(&sym, child, content)
		symbols = append(symbols, sym)

		ownerFQN := sym.NormalizeFQN()
		switch fn.Kind() {
		case "arrow_function", "function", "function_expression", "method_definition":
			typeRefs = append(typeRefs, t.extractFunctionTypeRefs(fn, content, currentPkg, ownerFQN, filePath, imports)...)
		}
	}

	return symbols, typeRefs
}

func tsObjectLiteralPropertyName(n *sitter.Node, content []byte) string {
	if n == nil {
		return ""
	}
	name := strings.TrimSpace(nodeText(n, content))
	name = strings.Trim(name, `"'`)
	if name == "" {
		return ""
	}
	for _, r := range name {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$') {
			return ""
		}
	}
	return name
}

func tsStableBindingName(n *sitter.Node, content []byte) string {
	return tsObjectLiteralPropertyName(n, content)
}

func tsSpan(n *sitter.Node) (sb, eb, sl, el int64) {
	if n == nil {
		return 0, 0, 0, 0
	}
	sb = int64(n.StartByte())
	eb = int64(n.EndByte())
	sl = int64(n.StartPosition().Row) + 1
	el = int64(n.EndPosition().Row) + 1
	return sb, eb, sl, el
}

func tsFileDocComment(content []byte) string {
	if len(content) == 0 {
		return ""
	}
	// Best-effort: look for a leading comment block at the top of the file.
	s := strings.TrimLeft(string(content), "\n\r\t ")
	if strings.HasPrefix(s, "/**") || strings.HasPrefix(s, "/*") {
		end := strings.Index(s, "*/")
		if end < 0 {
			return ""
		}
		doc := s[:end+2]
		return strings.TrimSpace(doc)
	}
	if strings.HasPrefix(s, "//") {
		lines := strings.Split(s, "\n")
		var out []string
		for _, line := range lines {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "//") {
				out = append(out, strings.TrimSpace(strings.TrimPrefix(trim, "//")))
				continue
			}
			break
		}
		if len(out) == 0 {
			return ""
		}
		return strings.TrimSpace(strings.Join(out, "\n"))
	}
	return ""
}

func tsLeadingJSDoc(content []byte, startByte int) string {
	if startByte <= 0 || startByte > len(content) {
		return ""
	}
	const lookback = 4000
	lo := startByte - lookback
	if lo < 0 {
		lo = 0
	}
	window := content[lo:startByte]

	// Strip trailing whitespace.
	i := len(window) - 1
	for i >= 0 && (window[i] == ' ' || window[i] == '\t' || window[i] == '\n' || window[i] == '\r') {
		i--
	}
	window = window[:i+1]

	end := bytes.LastIndex(window, []byte("*/"))
	if end < 0 {
		return ""
	}

	start := bytes.LastIndex(window[:end], []byte("/**"))
	if start < 0 {
		return ""
	}

	// Ensure no non-whitespace between comment end and decl start.
	tail := window[end+2:]
	for _, b := range tail {
		if b != ' ' && b != '\t' && b != '\n' && b != '\r' {
			return ""
		}
	}

	doc := string(window[start : end+2])
	return strings.TrimSpace(doc)
}

func tsSignature(content []byte, sb, eb int) string {
	if sb < 0 || sb >= len(content) {
		return ""
	}
	if eb <= sb {
		eb = min(sb+600, len(content))
	}
	if eb > len(content) {
		eb = len(content)
	}
	slice := content[sb:eb]

	cut := len(slice)
	if i := bytes.IndexByte(slice, '{'); i >= 0 {
		cut = min(cut, i)
	}
	if i := bytes.Index(slice, []byte("=>")); i >= 0 {
		cut = min(cut, i+2)
	}
	if i := bytes.IndexByte(slice, ';'); i >= 0 {
		cut = min(cut, i+1)
	}

	sig := strings.TrimSpace(string(slice[:cut]))
	sig = strings.ReplaceAll(sig, "\n", " ")
	sig = strings.Join(strings.Fields(sig), " ")
	return sig
}

func fillTSSymbolMeta(sym *Symbol, decl *sitter.Node, content []byte) {
	if sym == nil || decl == nil {
		return
	}
	sb, eb, sl, el := tsSpan(decl)
	sym.StartByte = sb
	sym.EndByte = eb
	sym.StartLine = sl
	sym.EndLine = el

	sym.Signature = tsSignature(content, int(decl.StartByte()), int(decl.EndByte()))
	sym.DocComment = tsLeadingJSDoc(content, int(decl.StartByte()))
}

type tsImportBindings struct {
	// ident -> resolved ref (e.g. named import, or default import when we resolved the file basename).
	idents map[string]SymbolRef
	// namespace alias -> module pkg (so NS.Foo() can resolve to {pkg: module, name: Foo}).
	namespaces map[string]string
	// importPaths tracks resolved file paths for import edges (e.g., "/abs/path/to/Button.tsx").
	// These are vault-relative or absolute paths that can be looked up as module anchors.
	importPaths []string
	// unresolved tracks local bindings whose export was absent, cyclic, or ambiguous.
	unresolved map[string]struct{}
	// memberPrefixes identifies class bindings whose static members use Class.member FQNs.
	memberPrefixes map[string]string
	// lexicalCalls records scope-aware deterministic targets by callee byte offset.
	lexicalCalls map[uint][]SymbolRef
}

func (t *TSIndexer) collectImports(root *sitter.Node, content []byte, fileDir string) tsImportBindings {
	out := tsImportBindings{
		idents:         make(map[string]SymbolRef),
		namespaces:     make(map[string]string),
		unresolved:     make(map[string]struct{}),
		memberPrefixes: make(map[string]string),
		lexicalCalls:   make(map[uint][]SymbolRef),
	}

	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		switch n.Kind() {
		case "import_statement":
			t.applyTSImportNode(n, content, fileDir, &out)
			return
		case "export_statement":
			t.applyTSExportNode(n, content, fileDir, &out)
		case "variable_declarator":
			t.applyTSModuleBinding(n, content, fileDir, &out)
		case "class_declaration":
			if name := tsStableBindingName(n.ChildByFieldName("name"), content); name != "" {
				out.memberPrefixes[name] = name
			}
		case "call_expression":
			if module, ok := tsLiteralModuleCall(n, content); ok {
				t.appendTSModuleImport(fileDir, module, &out)
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(root)
	return out
}
func (t *TSIndexer) callSitesFromCallExpr(n *sitter.Node, content []byte, currentPkg string, imports tsImportBindings) []CallSite {
	fn := n.ChildByFieldName("function")
	if fn == nil {
		return nil
	}
	refs, scoped := imports.lexicalCalls[fn.StartByte()]
	if !scoped {
		refs = t.resolveTSCallableCandidates(fn, content, currentPkg, imports)
	}
	out := make([]CallSite, 0, len(refs))
	for _, ref := range refs {
		out = append(out, CallSite{CalleeSymbol: ref})
	}
	return out
}

func (t *TSIndexer) callSiteFromNewExpr(n *sitter.Node, content []byte, currentPkg string, imports tsImportBindings) *CallSite {
	ctor := n.ChildByFieldName("constructor")
	if ctor == nil {
		ctor = n.ChildByFieldName("function")
	}
	if ctor == nil {
		return nil
	}
	if ref, ok := t.resolveTSCallTarget(ctor, content, currentPkg, imports); ok {
		return &CallSite{CalleeSymbol: ref}
	}
	return nil
}

func (t *TSIndexer) callSiteFromJSXElement(n *sitter.Node, content []byte, currentPkg string, imports tsImportBindings) *CallSite {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		// Some grammars don't expose a "name" field on this node; find the first
		// descendant that looks like a JSX name.
		var find func(*sitter.Node)
		find = func(cur *sitter.Node) {
			if cur == nil || nameNode != nil {
				return
			}
			switch cur.Kind() {
			case "jsx_identifier", "jsx_member_expression", "identifier", "member_expression":
				nameNode = cur
				return
			}
			for i := uint(0); i < cur.NamedChildCount(); i++ {
				find(cur.NamedChild(i))
				if nameNode != nil {
					return
				}
			}
		}
		find(n)
	}
	if nameNode == nil {
		// Fallback: parse node text to extract a JSX tag name when tree-sitter
		// doesn't expose a stable name node (varies by grammar/version).
		raw := nodeText(n, content)
		m := jsxTagNamePattern.FindStringSubmatch(raw)
		if len(m) < 2 {
			return nil
		}
		name := strings.TrimSpace(m[1])
		if !isJSXComponentName(name) {
			return nil
		}
		ref := t.resolveIdent(name, currentPkg, imports)
		return &CallSite{CalleeSymbol: ref}
	}
	switch nameNode.Kind() {
	case "jsx_identifier", "identifier":
		name := nodeText(nameNode, content)
		if !isJSXComponentName(name) {
			return nil
		}
		ref := t.resolveIdent(name, currentPkg, imports)
		return &CallSite{CalleeSymbol: ref}
	case "jsx_member_expression", "member_expression":
		obj := nodeText(nameNode.ChildByFieldName("object"), content)
		prop := nodeText(nameNode.ChildByFieldName("property"), content)
		if obj == "" || prop == "" || !isJSXComponentName(prop) {
			return nil
		}
		if pkg, ok := imports.namespaces[obj]; ok && pkg != "" {
			return &CallSite{CalleeSymbol: SymbolRef{Lang: LangTS, Pkg: pkg, Name: prop}}
		}
		// Best-effort: allow member usage off default import too.
		if base, ok := imports.idents[obj]; ok && base.Pkg != "" {
			return &CallSite{CalleeSymbol: SymbolRef{Lang: LangTS, Pkg: base.Pkg, Name: prop}}
		}
		return nil
	default:
		return nil
	}
}

func (t *TSIndexer) resolveIdent(ident string, currentPkg string, imports tsImportBindings) SymbolRef {
	if ref, ok := imports.idents[ident]; ok && ref.Name != "" {
		return ref
	}
	return SymbolRef{Lang: LangTS, Pkg: currentPkg, Name: ident}
}

// extractFunctionTypeRefs extracts type references from a function-like node.
// This includes parameter type annotations and return type annotations.
func (t *TSIndexer) extractFunctionTypeRefs(n *sitter.Node, content []byte, currentPkg, ownerFQN, filePath string, imports tsImportBindings) []TypeRef {
	if n == nil {
		return nil
	}
	var out []TypeRef
	seen := make(map[string]struct{})

	// Extract from parameters.
	params := n.ChildByFieldName("parameters")
	if params != nil {
		for i := uint(0); i < params.NamedChildCount(); i++ {
			param := params.NamedChild(i)
			if param == nil {
				continue
			}
			// Look for type_annotation on the parameter.
			for j := uint(0); j < param.NamedChildCount(); j++ {
				child := param.NamedChild(j)
				if child != nil && child.Kind() == "type_annotation" {
					t.appendTypeRefsFromNode(&out, seen, child, content, currentPkg, ownerFQN, filePath, imports)
				}
			}
		}
	}

	// Extract from return type.
	retType := n.ChildByFieldName("return_type")
	if retType == nil {
		for i := uint(0); i < n.NamedChildCount(); i++ {
			child := n.NamedChild(i)
			if child != nil && child.Kind() == "type_annotation" {
				retType = child
				break
			}
		}
	}
	if retType != nil {
		t.appendTypeRefsFromNode(&out, seen, retType, content, currentPkg, ownerFQN, filePath, imports)
	}

	return out
}

func (t *TSIndexer) extractTypeRefsFromNode(node *sitter.Node, content []byte, currentPkg, ownerFQN, filePath string, imports tsImportBindings) []TypeRef {
	if node == nil {
		return nil
	}
	var out []TypeRef
	t.appendTypeRefsFromNode(&out, make(map[string]struct{}), node, content, currentPkg, ownerFQN, filePath, imports)
	return out
}

func (t *TSIndexer) appendTypeRefsFromNode(out *[]TypeRef, seen map[string]struct{}, node *sitter.Node, content []byte, currentPkg, ownerFQN, filePath string, imports tsImportBindings) {
	if node == nil {
		return
	}
	switch node.Kind() {
	case "generic_type":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			t.appendTypeRefsFromNode(out, seen, nameNode, content, currentPkg, ownerFQN, filePath, imports)
		}
		if argsNode := node.ChildByFieldName("type_arguments"); argsNode != nil {
			for i := uint(0); i < argsNode.NamedChildCount(); i++ {
				t.appendTypeRefsFromNode(out, seen, argsNode.NamedChild(i), content, currentPkg, ownerFQN, filePath, imports)
			}
			return
		}
		for i := uint(0); i < node.NamedChildCount(); i++ {
			child := node.NamedChild(i)
			if child == nil || child == node.ChildByFieldName("name") {
				continue
			}
			t.appendTypeRefsFromNode(out, seen, child, content, currentPkg, ownerFQN, filePath, imports)
		}
		return
	}

	if ref, ok := t.resolveTSTypeSymbol(node, content, currentPkg, imports); ok {
		fqn := normalizeSymbol(ref)
		if fqn == "" {
			return
		}
		if _, ok := seen[fqn]; ok {
			return
		}
		seen[fqn] = struct{}{}
		*out = append(*out, TypeRef{
			File:     filePath,
			OwnerFQN: ownerFQN,
			TypeSym:  ref,
		})
		return
	}

	for i := uint(0); i < node.NamedChildCount(); i++ {
		t.appendTypeRefsFromNode(out, seen, node.NamedChild(i), content, currentPkg, ownerFQN, filePath, imports)
	}
}

func (t *TSIndexer) resolveTSCallTarget(node *sitter.Node, content []byte, currentPkg string, imports tsImportBindings) (SymbolRef, bool) {
	node = tsUnwrapExpr(node)
	if node == nil {
		return SymbolRef{}, false
	}
	switch node.Kind() {
	case "identifier":
		name := strings.TrimSpace(nodeText(node, content))
		if name == "" {
			return SymbolRef{}, false
		}
		if _, unresolved := imports.unresolved[name]; unresolved {
			return SymbolRef{}, false
		}
		return t.resolveIdent(name, currentPkg, imports), true
	case "member_expression":
		return t.resolveTSMemberSymbol(node, content, currentPkg, imports)
	default:
		return SymbolRef{}, false
	}
}

func (t *TSIndexer) resolveTSMemberSymbol(node *sitter.Node, content []byte, currentPkg string, imports tsImportBindings) (SymbolRef, bool) {
	node = tsUnwrapExpr(node)
	if node == nil {
		return SymbolRef{}, false
	}
	prop := strings.TrimSpace(nodeText(node.ChildByFieldName("property"), content))
	if prop == "" {
		return SymbolRef{}, false
	}
	obj := tsUnwrapExpr(node.ChildByFieldName("object"))
	if obj == nil {
		return SymbolRef{}, false
	}
	switch obj.Kind() {
	case "identifier":
		objName := strings.TrimSpace(nodeText(obj, content))
		if objName == "" {
			return SymbolRef{}, false
		}
		if pkg, ok := imports.namespaces[objName]; ok && pkg != "" {
			return SymbolRef{Lang: LangTS, Pkg: pkg, Name: prop}, true
		}
		if base, ok := imports.idents[objName]; ok && base.Pkg != "" {
			if prefix := imports.memberPrefixes[objName]; prefix != "" {
				return SymbolRef{Lang: LangTS, Pkg: base.Pkg, Name: prefix + "." + prop}, true
			}
			return SymbolRef{Lang: LangTS, Pkg: base.Pkg, Name: prop}, true
		}
		if prefix := imports.memberPrefixes[objName]; prefix != "" {
			return SymbolRef{Lang: LangTS, Pkg: currentPkg, Name: prefix + "." + prop}, true
		}
	case "member_expression":
		if base, ok := t.resolveTSMemberSymbol(obj, content, currentPkg, imports); ok && base.Pkg != "" {
			return SymbolRef{Lang: LangTS, Pkg: base.Pkg, Name: prop}, true
		}
	}
	return SymbolRef{}, false
}

func (t *TSIndexer) memberRefFromExpr(n *sitter.Node, content []byte, currentPkg, ownerFQN, filePath string, imports tsImportBindings) *MemberRef {
	if !tsShouldEmitMemberRef(n) {
		return nil
	}
	ref, ok := t.resolveTSMemberSymbol(n, content, currentPkg, imports)
	if !ok || strings.TrimSpace(ref.Name) == "" || strings.TrimSpace(ref.Pkg) == "" {
		return nil
	}
	return &MemberRef{
		File:     filePath,
		OwnerFQN: ownerFQN,
		Sym:      ref,
	}
}

func tsShouldEmitMemberRef(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	parent := n.Parent()
	if parent == nil {
		return true
	}
	switch parent.Kind() {
	case "call_expression":
		return parent.ChildByFieldName("function") != n
	case "new_expression":
		return parent.ChildByFieldName("constructor") != n && parent.ChildByFieldName("function") != n
	case "member_expression":
		return parent.ChildByFieldName("object") != n
	case "optional_chain":
		return parent.ChildByFieldName("expression") != n
	case "jsx_member_expression":
		return false
	default:
		return true
	}
}

func tsUnwrapExpr(node *sitter.Node) *sitter.Node {
	for node != nil {
		switch node.Kind() {
		case "optional_chain", "parenthesized_expression", "non_null_expression", "as_expression", "type_assertion", "satisfies_expression":
			if expr := node.ChildByFieldName("expression"); expr != nil {
				node = expr
				continue
			}
			if node.NamedChildCount() > 0 {
				node = node.NamedChild(0)
				continue
			}
		}
		break
	}
	return node
}

func tsTypeAnnotationNode(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	if ann := n.ChildByFieldName("type"); ann != nil {
		return ann
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		child := n.NamedChild(i)
		if child != nil && child.Kind() == "type_annotation" {
			return child
		}
	}
	return nil
}

func tsSatisfiesTypeNode(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	if typeNode := n.ChildByFieldName("type"); typeNode != nil {
		return typeNode
	}
	if n.NamedChildCount() >= 2 {
		return n.NamedChild(1)
	}
	return nil
}

func tsTypeAliasValueNode(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	if value := n.ChildByFieldName("value"); value != nil {
		return value
	}
	if typeNode := n.ChildByFieldName("type"); typeNode != nil {
		return typeNode
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		child := n.NamedChild(i)
		if child == nil || child == n.ChildByFieldName("name") {
			continue
		}
		return child
	}
	return nil
}

func (t *TSIndexer) resolveTSTypeSymbol(node *sitter.Node, content []byte, currentPkg string, imports tsImportBindings) (SymbolRef, bool) {
	node = tsUnwrapExpr(node)
	if node == nil {
		return SymbolRef{}, false
	}
	switch node.Kind() {
	case "type_identifier", "identifier":
		name := strings.TrimSpace(nodeText(node, content))
		if name == "" || tsBuiltinTypes[name] {
			return SymbolRef{}, false
		}
		return t.resolveIdent(name, currentPkg, imports), true
	case "nested_type_identifier":
		text := strings.TrimSpace(nodeText(node, content))
		if text == "" {
			return SymbolRef{}, false
		}
		parts := strings.Split(text, ".")
		if len(parts) >= 2 {
			root := strings.TrimSpace(parts[0])
			name := strings.TrimSpace(parts[len(parts)-1])
			if tsBuiltinTypes[name] || name == "" {
				return SymbolRef{}, false
			}
			if pkg, ok := imports.namespaces[root]; ok && pkg != "" {
				return SymbolRef{Lang: LangTS, Pkg: pkg, Name: name}, true
			}
			return t.resolveIdent(name, currentPkg, imports), true
		}
		return t.resolveIdent(text, currentPkg, imports), true
	}
	return SymbolRef{}, false
}

func (t *TSIndexer) extractTSSupers(n *sitter.Node, content []byte, currentPkg, childFQN string, imports tsImportBindings) []SuperEdge {
	if n == nil || strings.TrimSpace(childFQN) == "" {
		return nil
	}
	var out []SuperEdge
	seen := make(map[string]struct{})
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}
		switch node.Kind() {
		case "generic_type":
			if nameNode := node.ChildByFieldName("name"); nameNode != nil {
				if ref, ok := t.resolveTSTypeSymbol(nameNode, content, currentPkg, imports); ok {
					parentFQN := normalizeSymbol(ref)
					if parentFQN != "" {
						if _, exists := seen[parentFQN]; !exists {
							seen[parentFQN] = struct{}{}
							out = append(out, SuperEdge{ChildFQN: childFQN, ParentFQN: parentFQN})
						}
					}
				}
			}
			return
		case "type_identifier", "nested_type_identifier", "identifier":
			if ref, ok := t.resolveTSTypeSymbol(node, content, currentPkg, imports); ok {
				parentFQN := normalizeSymbol(ref)
				if parentFQN != "" {
					if _, exists := seen[parentFQN]; !exists {
						seen[parentFQN] = struct{}{}
						out = append(out, SuperEdge{ChildFQN: childFQN, ParentFQN: parentFQN})
					}
				}
			}
			return
		}
		for i := uint(0); i < node.NamedChildCount(); i++ {
			walk(node.NamedChild(i))
		}
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		child := n.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "class_heritage", "extends_clause", "implements_clause", "extends_type_clause":
			walk(child)
		}
	}
	return out
}

func (t *TSIndexer) extractTSReExportSymbols(n *sitter.Node, content []byte, currentPkg, filePath string) []Symbol {
	raw := strings.TrimSpace(nodeText(n, content))
	if raw == "" || !strings.HasPrefix(raw, "export") || !strings.Contains(raw, " from ") {
		return nil
	}
	open := strings.Index(raw, "{")
	close := strings.LastIndex(raw, "}")
	if open < 0 || close <= open {
		return nil
	}
	specs := strings.Split(raw[open+1:close], ",")
	module := trimStringLiteral(nodeText(tsDirectNamedChild(n, "string"), content))
	resolutionPath := filePath
	if !filepath.IsAbs(resolutionPath) && strings.TrimSpace(t.repoRoot) != "" {
		resolutionPath = filepath.Join(t.repoRoot, filepath.FromSlash(resolutionPath))
	}
	target, targetLocal := t.resolveModuleFile(filepath.Dir(resolutionPath), module)
	var out []Symbol
	seen := make(map[string]struct{})
	for _, spec := range specs {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		importedName := strings.TrimSpace(strings.TrimPrefix(spec, "type "))
		name := importedName
		if strings.Contains(spec, " as ") {
			parts := strings.SplitN(spec, " as ", 2)
			importedName = strings.TrimSpace(strings.TrimPrefix(parts[0], "type "))
			name = strings.TrimSpace(parts[1])
		}
		importedName = strings.TrimSpace(strings.Trim(importedName, `"'`))
		name = strings.TrimSpace(strings.TrimPrefix(name, "type "))
		name = strings.TrimSpace(strings.Trim(name, `"'`))
		if !isTSStableName(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		symbol := Symbol{Lang: LangTS, Kind: SymField, File: filePath, Pkg: currentPkg, Name: name}
		if targetLocal {
			if ref, ok := t.resolveTSBarrelExport(target, importedName); ok {
				symbol.Signature = "re-export from " + normalizeSymbol(ref)
			}
		}
		out = append(out, symbol)
	}
	return out
}

func (t *TSIndexer) resolveTSBarrelExport(barrelFile, exportName string) (SymbolRef, bool) {
	return t.resolveTSExport(barrelFile, exportName, make(map[string]bool))
}

func isTSStableName(name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	for _, r := range name {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$') {
			return false
		}
	}
	return true
}

// tsBuiltinTypes are TypeScript/JavaScript builtin types that shouldn't create edges.
var tsBuiltinTypes = map[string]bool{
	"string":    true,
	"number":    true,
	"boolean":   true,
	"void":      true,
	"null":      true,
	"undefined": true,
	"never":     true,
	"any":       true,
	"unknown":   true,
	"object":    true,
	"symbol":    true,
	"bigint":    true,
	"String":    true,
	"Number":    true,
	"Boolean":   true,
	"Object":    true,
	"Symbol":    true,
	"Function":  true,
	"Array":     true,
	"Promise":   true,
	"Map":       true,
	"Set":       true,
	"Date":      true,
	"Error":     true,
	"RegExp":    true,
	"JSON":      true,
}

func isJSXComponentName(name string) bool {
	if name == "" {
		return false
	}
	r, _ := utf8FirstRune(name)
	return unicode.IsUpper(r)
}

func utf8FirstRune(s string) (rune, int) {
	for _, r := range s {
		return r, 1
	}
	return 0, 0
}

func nodeText(node *sitter.Node, content []byte) string {
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

func trimStringLiteral(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '`' && s[len(s)-1] == '`') {
			return s[1 : len(s)-1]
		}
	}
	return strings.Trim(s, "\"'")
}

func (t *TSIndexer) modulePkgAndDefaultName(module, target string, local bool) (pkg string, defaultName string) {
	module = strings.TrimSpace(module)
	if module == "" {
		return "", ""
	}
	if local {
		pkg = t.pkgForFile(target)
		defaultName = strings.TrimSuffix(filepath.Base(target), filepath.Ext(target))
		return pkg, defaultName
	}
	// Unresolved bare modules keep their import specifier as pkg.
	if !(strings.HasPrefix(module, ".") || strings.HasPrefix(module, "/")) {
		return module, ""
	}

	return module, ""
}

func (t *TSIndexer) resolveModuleFile(fileDir, module string) (string, bool) {
	if t != nil && t.resolver != nil {
		if resolved, ok := t.resolver.resolve(fileDir, module); ok {
			return resolved, true
		}
	}
	if t != nil && t.tailIdx != nil && isAliasLikeModuleSpecifier(module) {
		return t.resolveByTailIndex(fileDir, module)
	}
	return "", false
}

func isAliasLikeModuleSpecifier(module string) bool {
	// Support common tsconfig alias patterns:
	// - @/... (e.g., @/components/Button)
	// - @word/... where word is a path-like alias (e.g., @components/ui/dialog)
	//
	// Avoid treating npm scopes like "@scope/pkg" as alias paths.
	// npm scoped packages: @angular/core, @types/react, @emotion/react
	// Path aliases: @components/ui/dialog, @features/auth/Login

	if strings.HasPrefix(module, "@/") {
		return true
	}

	if !strings.HasPrefix(module, "@") {
		return false
	}

	// Count path segments. npm packages have 1-2 segments max (e.g., @scope/pkg).
	// Path aliases typically have more (e.g., @components/ui/dialog = 3 segments).
	parts := strings.Split(module, "/")
	if len(parts) >= 3 {
		// 3+ segments strongly suggests a path alias, not npm package.
		return true
	}

	// For 2-segment cases, heuristic: common npm scope prefixes.
	// If the first part (after @) looks like a path word (components, features, etc.),
	// treat it as an alias.
	if len(parts) == 2 {
		scope := strings.TrimPrefix(parts[0], "@")
		// Common path alias prefixes.
		pathAliasHints := []string{"components", "features", "utils", "lib", "src", "app", "pages", "hooks", "services", "portals", "assets", "styles"}
		for _, hint := range pathAliasHints {
			if strings.EqualFold(scope, hint) {
				return true
			}
		}
	}

	return false
}

func (t *TSIndexer) resolveByTailIndex(fileDir string, module string) (string, bool) {
	if t == nil || t.tailIdx == nil {
		return "", false
	}
	tail := strings.TrimSpace(module)
	if tail == "" {
		return "", false
	}
	tail = filepath.ToSlash(tail)
	tail = strings.TrimPrefix(tail, "./")
	tail = strings.TrimPrefix(tail, "/")
	for strings.HasPrefix(tail, "../") {
		tail = strings.TrimPrefix(tail, "../")
	}
	tail = strings.TrimPrefix(tail, "@/") // common alias style: @/components/Button
	// Also handle @alias/path style: @components/ui/dialog -> ui/dialog
	if strings.HasPrefix(tail, "@") {
		if idx := strings.Index(tail, "/"); idx > 0 {
			tail = tail[idx+1:] // Strip @alias prefix
		}
	}
	tail = strings.TrimPrefix(tail, "/")
	if tail == "" {
		return "", false
	}

	prefer := t.preferRootsForDir(fileDir)
	allowed := codefile.TypeScriptJavaScriptExtensions()
	maxSeg := 5
	parts := strings.Split(tail, "/")
	filtered := parts[:0]
	for _, p := range parts {
		if p == "" {
			continue
		}
		filtered = append(filtered, p)
	}
	if len(filtered) == 0 {
		return "", false
	}

	// Try last 1..maxSeg segments, from most-specific to least-specific.
	for segs := min(maxSeg, len(filtered)); segs >= 1; segs-- {
		key := strings.Join(filtered[len(filtered)-segs:], "/")
		if match, ok := t.tailIdx.Query(TailQuery{Tail: key, AllowedExts: allowed, PreferUnder: prefer}); ok {
			// The tail index stores relative paths (from vault root).
			// Return an absolute path for consistent pkgForFile handling.
			if !filepath.IsAbs(match) && t.repoRoot != "" {
				match = filepath.Join(t.repoRoot, match)
			}
			if t.resolver != nil {
				if resolved, valid := t.resolver.regularFile(match); valid {
					return resolved, true
				}
			}
		}
	}
	return "", false
}

func (t *TSIndexer) preferRootsForDir(dir string) []string {
	dir = paths.ResolveSymlinks(dir).String()
	if dir == "" {
		return nil
	}
	// Prefer staying within the nearest package.json root (monorepo-friendly).
	if ent, ok := t.nearestPackageJSON(dir); ok && ent != nil && ent.pkgRoot != "" {
		return []string{ent.pkgRoot}
	}
	if t.repoRoot != "" {
		return []string{t.repoRoot}
	}
	if root := findRepoRoot(dir); root != "" {
		return []string{root}
	}
	return nil
}

func (t *TSIndexer) pkgForDir(dir string) string {
	dir = paths.ResolveSymlinks(dir).String()
	if dir == "" {
		return ""
	}

	// 1) Prefer nearest package.json name + path-within-package.
	if ent, ok := t.nearestPackageJSON(dir); ok && ent != nil && ent.pkgName != "" && ent.pkgRoot != "" {
		rootPaths, err := paths.NewVaultPaths(ent.pkgRoot)
		if err != nil {
			return ent.pkgName
		}
		rel, err := rootPaths.RelStrict(dir)
		if err != nil {
			return ent.pkgName
		}
		relPath := strings.TrimPrefix(rel.String(), "./")
		relPath = strings.TrimPrefix(relPath, "/")
		if relPath == "" || relPath == "." {
			return ent.pkgName
		}
		return ent.pkgName + "/" + relPath
	}

	// 2) Fall back to repo-root-relative directory.
	root := t.repoRoot
	if root == "" {
		root = findRepoRoot(dir)
	}
	if root != "" {
		rootPaths, err := paths.NewVaultPaths(root)
		if err == nil {
			if rel, err := rootPaths.RelStrict(dir); err == nil {
				relPath := strings.TrimPrefix(rel.String(), "./")
				relPath = strings.TrimPrefix(relPath, "/")
				if relPath == "" || relPath == "." {
					return filepath.Base(root)
				}
				return relPath
			}
		}
	}

	// 3) Last resort: absolute dir (still deterministic within one machine).
	return filepath.ToSlash(dir)
}

func (t *TSIndexer) pkgForFile(filePath string) string {
	filePath = paths.ResolveSymlinks(filePath).String()
	if filePath == "" {
		return ""
	}
	dirPkg := t.pkgForDir(filepath.Dir(filePath))
	base := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	if base == "" {
		return dirPkg
	}
	if dirPkg == "" {
		return base
	}
	return dirPkg + "/" + base
}

func (t *TSIndexer) nearestPackageJSON(startDir string) (*tsPkgCacheEntry, bool) {
	startDir = paths.ResolveSymlinks(startDir).String()
	if startDir == "" {
		return nil, false
	}

	t.pkgMu.Lock()
	if ent, ok := t.pkgCache[startDir]; ok {
		t.pkgMu.Unlock()
		return ent, true
	}
	t.pkgMu.Unlock()

	repoRoot := t.repoRoot
	if repoRoot == "" {
		repoRoot = findRepoRoot(startDir)
	}
	repoPaths, _ := paths.NewVaultPaths(repoRoot)

	cur := startDir
	for {
		// Stop at repo root if we have one.
		if repoPaths.Root() != "" {
			if _, err := repoPaths.RelStrict(cur); err != nil {
				break
			}
		}

		pj := filepath.Join(cur, "package.json")
		if data, err := os.ReadFile(pj); err == nil {
			if name := parsePackageJSONName(data); name != "" {
				ent := &tsPkgCacheEntry{pkgName: name, pkgRoot: cur}
				t.pkgMu.Lock()
				t.pkgCache[startDir] = ent
				t.pkgMu.Unlock()
				return ent, true
			}
		}

		parent := filepath.Dir(cur)
		if parent == cur || parent == "" {
			break
		}
		cur = parent
	}

	t.pkgMu.Lock()
	t.pkgCache[startDir] = nil
	t.pkgMu.Unlock()
	return nil, true
}

func parsePackageJSONName(data []byte) string {
	type pkg struct {
		Name string `json:"name"`
	}
	var p pkg
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	// package.json can be large; ignore errors from strict decoding and fall back.
	if err := dec.Decode(&p); err == nil && strings.TrimSpace(p.Name) != "" {
		return strings.TrimSpace(p.Name)
	}
	// Non-strict fallback.
	p = pkg{}
	if err := json.Unmarshal(data, &p); err != nil {
		return ""
	}
	return strings.TrimSpace(p.Name)
}

func findRepoRoot(start string) string {
	cur := paths.ResolveSymlinks(start).String()
	for {
		if cur == "" {
			return ""
		}
		if fi, err := os.Stat(filepath.Join(cur, ".git")); err == nil && fi.IsDir() {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}

// TSIndexerAvailable reports whether TS/JS indexing is supported in this build.
func TSIndexerAvailable() bool { return true }
