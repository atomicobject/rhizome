package codeanchor

// Docs:
// - [Code anchors (Hub)](docs/hubs/Code anchors (Hub).md)
// - [Code anchors - matching + scopes](docs/reference/analysis/code-anchors-matching-scopes.md)

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// GoIndexer implements LanguageIndexer for Go using the standard library.
// It is intentionally best-effort: it will return a partial summary when possible.
type GoIndexer struct {
	mu           sync.Mutex
	moduleByRoot map[string]string // moduleRoot(abs) -> modulePath (from go.mod)
	rootByDir    map[string]string // dir(abs) -> moduleRoot(abs)
}

func NewGoIndexer() *GoIndexer {
	return &GoIndexer{
		moduleByRoot: make(map[string]string),
		rootByDir:    make(map[string]string),
	}
}

func (*GoIndexer) Lang() Lang { return LangGo }

func (*GoIndexer) ReverseIndexFallbacks(deltas DefDeltas) []ReverseIndexFallback {
	symbols := append(append([]SymbolRef{}, deltas.AddedSymbols...), deltas.RemovedSymbols...)
	if len(symbols) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	var fallbacks []ReverseIndexFallback
	for _, ref := range symbols {
		if ref.Lang != LangGo || ref.Name == "" {
			continue
		}
		if !goMethodPkg(ref.Pkg) {
			continue
		}
		key := symbolRefKey(SymbolRef{Lang: LangGo, Name: ref.Name})
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		fallbacks = append(fallbacks, ReverseIndexFallback{
			Ref: SymbolRef{Lang: LangGo, Name: ref.Name},
		})
	}
	return fallbacks
}

func (g *GoIndexer) IndexFile(content []byte, ref paths.CodePathRef) (FileSummary, error) {
	filePath := ref.Rel.String()
	if filePath == "" {
		filePath = ref.Abs.String()
	}
	absPath := ref.Abs.String()
	summary := FileSummary{
		FilePath: filePath,
		Lang:     LangGo,
	}

	importPath, _ := g.importPathForFile(absPath)
	if importPath == "" {
		// Fall back: best-effort package name (still stable within a vault index).
		importPath = "go:" + filepath.ToSlash(filepath.Dir(filePath))
	}

	fset := token.NewFileSet()
	// Best-effort parse: ParseFile may return both a *ast.File and an error.
	astFile, err := parser.ParseFile(fset, filePath, content, parser.AllErrors|parser.ParseComments)
	if astFile == nil {
		// No usable AST.
		return summary, err
	}
	if astFile.Name != nil && strings.TrimSpace(astFile.Name.Name) != "" {
		directory := filepath.ToSlash(filepath.Dir(filePath))
		if directory == "" {
			directory = "."
		}
		summary.GoPackage = &GoPackageKey{
			ImportPath:   importPath,
			Directory:    directory,
			PackageName:  astFile.Name.Name,
			BuildVariant: CurrentGoBuildVariant(),
		}
	}

	// Extract package-level doc comment (comments before package declaration).
	if astFile.Doc != nil {
		summary.PackageDoc = strings.TrimSpace(astFile.Doc.Text())
	}

	imports := collectGoImports(astFile)
	localTypes := collectGoLocalTypeNames(astFile)

	// Symbols: types + functions (+ methods).
	for _, decl := range astFile.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || ts.Name == nil || ts.Name.Name == "" {
					continue
				}
				kind := SymClass
				typeFQN := importPath + "." + ts.Name.Name
				switch t := ts.Type.(type) {
				case *ast.StructType:
					kind = SymStruct
					// Extract struct embeddings (for baseClass: anchors).
					summary.Supers = append(summary.Supers, extractGoStructEmbeddings(t, typeFQN, importPath, imports)...)
					// Extract struct fields as symbols.
					summary.Symbols = append(summary.Symbols, extractGoStructFields(t, fset, content, ts.Name.Name, importPath, filePath)...)
				case *ast.InterfaceType:
					kind = SymInterface
					// Extract interface embeddings (for baseClass: anchors).
					summary.Supers = append(summary.Supers, extractGoInterfaceEmbeddings(t, typeFQN, importPath, imports)...)
				}
				sym := Symbol{
					Lang: LangGo,
					Kind: kind,
					File: filePath,
					Pkg:  importPath,
					Name: ts.Name.Name,
					FQN:  typeFQN,
					// Exported is a stable signal for module rollups and symbol prioritization.
					Exported: ast.IsExported(ts.Name.Name),
				}
				if doc := goDocForTypeSpec(d, ts); doc != "" {
					sym.DocComment = doc
				}
				sym.Signature = goSignatureForTypeSpec(fset, ts.Name.Name, ts.Type)
				setGoSymbolSpan(&sym, fset, content, goSpanForTypeSpec(d, ts))
				summary.Symbols = append(summary.Symbols, sym)
			}
		case *ast.FuncDecl:
			if d.Name == nil || d.Name.Name == "" {
				continue
			}
			span := goSpanForFuncDecl(d)
			doc := goDocForFuncDecl(d)
			sig := goSignatureForFuncDecl(fset, d)
			if d.Recv == nil {
				sym := Symbol{
					Lang:       LangGo,
					Kind:       SymFunc,
					File:       filePath,
					Pkg:        importPath,
					Name:       d.Name.Name,
					FQN:        importPath + "." + d.Name.Name,
					Exported:   ast.IsExported(d.Name.Name),
					Signature:  sig,
					DocComment: doc,
				}
				setGoSymbolSpan(&sym, fset, content, span)
				summary.Symbols = append(summary.Symbols, sym)
				continue
			}
			recv := receiverTypeName(d.Recv)
			if recv == "" {
				continue
			}
			// Methods: represent as "<importPath>.<ReceiverType>.<Method>" by storing:
			// Pkg="<importPath>.<ReceiverType>", Name="<Method>".
			methodPkg := importPath + "." + recv
			sym := Symbol{
				Lang:       LangGo,
				Kind:       SymMethod,
				File:       filePath,
				Pkg:        methodPkg,
				Name:       d.Name.Name,
				FQN:        methodPkg + "." + d.Name.Name,
				Exported:   ast.IsExported(d.Name.Name),
				Signature:  sig,
				DocComment: doc,
			}
			setGoSymbolSpan(&sym, fset, content, span)
			summary.Symbols = append(summary.Symbols, sym)
		}
	}

	// Call-sites: used by function anchors (definition + references).
	var nodeStack []ast.Node
	var ownerStack []string
	ast.Inspect(astFile, func(n ast.Node) bool {
		if n == nil {
			if len(nodeStack) == 0 {
				return false
			}
			last := nodeStack[len(nodeStack)-1]
			nodeStack = nodeStack[:len(nodeStack)-1]
			if _, ok := last.(*ast.FuncDecl); ok && len(ownerStack) > 0 {
				ownerStack = ownerStack[:len(ownerStack)-1]
			}
			return true
		}

		nodeStack = append(nodeStack, n)
		if fd, ok := n.(*ast.FuncDecl); ok {
			recv := ""
			if fd.Recv != nil {
				recv = receiverTypeName(fd.Recv)
			}
			owner := ""
			if fd.Name != nil && fd.Name.Name != "" {
				if recv != "" {
					owner = (Symbol{Pkg: importPath + "." + recv, Name: fd.Name.Name}).NormalizeFQN()
				} else {
					owner = (Symbol{Pkg: importPath, Name: fd.Name.Name}).NormalizeFQN()
				}
			}
			ownerStack = append(ownerStack, owner)
			return true
		}

		currentOwner := ""
		if len(ownerStack) > 0 {
			currentOwner = ownerStack[len(ownerStack)-1]
		}

		switch node := n.(type) {
		case *ast.CallExpr:
			ref, ok := resolveGoCallExpr(node.Fun, importPath, imports, localTypes)
			if ok && ref.Name != "" {
				summary.Calls = append(summary.Calls, CallSite{
					File:     filePath,
					OwnerFQN: currentOwner,
					CalleeSymbol: SymbolRef{
						Lang: LangGo,
						Pkg:  ref.Pkg,
						Name: ref.Name,
					},
				})
			}
		case *ast.CompositeLit:
			// Treat composite literals (Type{} / pkg.Type{}) as a "use" of the type symbol.
			// This lets `symbol:` anchors for structs/types attach to both definitions and common usage sites.
			ref, ok := resolveGoTypeExpr(node.Type, importPath, imports)
			if ok && ref.Name != "" {
				summary.Calls = append(summary.Calls, CallSite{
					File:     filePath,
					OwnerFQN: currentOwner,
					CalleeSymbol: SymbolRef{
						Lang: LangGo,
						Pkg:  ref.Pkg,
						Name: ref.Name,
					},
				})
			}
		}

		return true
	})

	return summary, err
}

func goMethodPkg(pkg string) bool {
	if strings.TrimSpace(pkg) == "" {
		return false
	}
	lastSlash := strings.LastIndex(pkg, "/")
	lastDot := strings.LastIndex(pkg, ".")
	return lastDot > lastSlash
}

type goSpan struct {
	start token.Pos
	end   token.Pos
}

func goSpanForFuncDecl(d *ast.FuncDecl) goSpan {
	if d == nil {
		return goSpan{}
	}
	start := d.Pos()
	if d.Doc != nil && len(d.Doc.List) > 0 {
		start = d.Doc.Pos()
	}
	return goSpan{start: start, end: d.End()}
}

func goSpanForTypeSpec(d *ast.GenDecl, ts *ast.TypeSpec) goSpan {
	if ts == nil {
		return goSpan{}
	}
	start := ts.Pos()
	// Prefer the closest doc comment to the type spec.
	if ts.Doc != nil && len(ts.Doc.List) > 0 {
		start = ts.Doc.Pos()
	} else if d != nil && d.Doc != nil && len(d.Doc.List) > 0 {
		start = d.Doc.Pos()
	}
	end := ts.End()
	// If the enclosing decl spans multiple specs, use the spec span rather than the whole decl.
	return goSpan{start: start, end: end}
}

func goDocForFuncDecl(d *ast.FuncDecl) string {
	if d == nil || d.Doc == nil {
		return ""
	}
	return strings.TrimSpace(d.Doc.Text())
}

func goDocForTypeSpec(d *ast.GenDecl, ts *ast.TypeSpec) string {
	if ts != nil && ts.Doc != nil {
		if s := strings.TrimSpace(ts.Doc.Text()); s != "" {
			return s
		}
	}
	if d != nil && d.Doc != nil {
		return strings.TrimSpace(d.Doc.Text())
	}
	return ""
}

func goSignatureForFuncDecl(fset *token.FileSet, d *ast.FuncDecl) string {
	if fset == nil || d == nil {
		return ""
	}
	if d.Name == nil || d.Name.Name == "" {
		return ""
	}
	tmp := *d
	// Ensure the signature is comment-free; doc text is stored separately in DocComment.
	tmp.Doc = nil
	tmp.Body = nil
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, &tmp); err != nil {
		return d.Name.Name
	}
	return strings.TrimSpace(buf.String())
}

func goSignatureForTypeSpec(fset *token.FileSet, name string, expr ast.Expr) string {
	if fset == nil || strings.TrimSpace(name) == "" || expr == nil {
		return ""
	}
	spec := &ast.TypeSpec{Name: ast.NewIdent(name), Type: expr}
	decl := &ast.GenDecl{Tok: token.TYPE, Specs: []ast.Spec{spec}}
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, decl); err != nil {
		return "type " + name
	}
	return strings.TrimSpace(buf.String())
}

func setGoSymbolSpan(sym *Symbol, fset *token.FileSet, content []byte, sp goSpan) {
	if sym == nil || fset == nil || sp.start == token.NoPos || sp.end == token.NoPos {
		return
	}
	f := fset.File(sp.start)
	if f == nil {
		return
	}
	startOff := f.Offset(sp.start)
	endOff := f.Offset(sp.end)
	if startOff < 0 || endOff <= startOff || endOff > len(content) {
		return
	}
	startPos := fset.Position(sp.start)
	endPos := fset.Position(sp.end)
	sym.StartByte = int64(startOff)
	sym.EndByte = int64(endOff)
	sym.StartLine = int64(startPos.Line)
	sym.EndLine = int64(endPos.Line)
}

func (g *GoIndexer) importPathForFile(absPath string) (string, bool) {
	if absPath == "" {
		return "", false
	}
	dir := filepath.Dir(absPath)
	root, ok := g.moduleRootForDir(dir)
	if !ok {
		return "", false
	}
	modPath := g.modulePathForRoot(root)
	if modPath == "" {
		return "", false
	}
	rootPaths, err := paths.NewVaultPaths(root)
	if err != nil {
		return modPath, true
	}
	rel, err := rootPaths.RelStrict(dir)
	if err != nil {
		return modPath, true
	}
	relPath := strings.TrimPrefix(rel.String(), "./")
	relPath = strings.TrimPrefix(relPath, "/")
	if relPath == "" || relPath == "." {
		return modPath, true
	}
	return path.Join(modPath, relPath), true
}

func (g *GoIndexer) moduleRootForDir(dir string) (string, bool) {
	dir = paths.ResolveSymlinks(dir).String()
	g.mu.Lock()
	if root, ok := g.rootByDir[dir]; ok {
		g.mu.Unlock()
		return root, true
	}
	g.mu.Unlock()

	root, ok := findGoModRoot(dir)
	if !ok {
		return "", false
	}
	g.mu.Lock()
	g.rootByDir[dir] = root
	g.mu.Unlock()
	return root, true
}

func (g *GoIndexer) modulePathForRoot(root string) string {
	root = paths.ResolveSymlinks(root).String()
	g.mu.Lock()
	if p, ok := g.moduleByRoot[root]; ok {
		g.mu.Unlock()
		return p
	}
	g.mu.Unlock()

	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	modPath := parseGoModModulePath(data)
	g.mu.Lock()
	g.moduleByRoot[root] = modPath
	g.mu.Unlock()
	return modPath
}

func findGoModRoot(startDir string) (string, bool) {
	cur := paths.ResolveSymlinks(startDir).String()
	for {
		if cur == "" {
			return "", false
		}
		if fi, err := os.Stat(filepath.Join(cur, "go.mod")); err == nil && !fi.IsDir() {
			return cur, true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", false
		}
		cur = parent
	}
}

var goModModuleRe = regexp.MustCompile(`(?m)^\s*module\s+([^\s]+)\s*$`)

func parseGoModModulePath(goMod []byte) string {
	// Trim UTF-8 BOM if present (rare, but easy to support).
	goMod = bytes.TrimPrefix(goMod, []byte{0xEF, 0xBB, 0xBF})
	m := goModModuleRe.FindSubmatch(goMod)
	if len(m) != 2 {
		return ""
	}
	return strings.TrimSpace(string(m[1]))
}

func collectGoImports(f *ast.File) map[string]string {
	out := make(map[string]string)
	for _, imp := range f.Imports {
		if imp == nil || imp.Path == nil {
			continue
		}
		p, err := strconv.Unquote(strings.TrimSpace(imp.Path.Value))
		if err != nil || p == "" {
			continue
		}
		alias := ""
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		if alias == "" {
			alias = path.Base(p)
		}
		// Skip blank/dot imports (no stable selector name).
		if alias == "_" || alias == "." || alias == "" {
			continue
		}
		out[alias] = p
	}
	return out
}

func collectGoLocalTypeNames(f *ast.File) map[string]struct{} {
	out := make(map[string]struct{})
	if f == nil {
		return out
	}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd == nil || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts == nil || ts.Name == nil || ts.Name.Name == "" {
				continue
			}
			out[ts.Name.Name] = struct{}{}
		}
	}
	return out
}

func resolveGoCallExpr(fun ast.Expr, currentImportPath string, imports map[string]string, localTypes map[string]struct{}) (SymbolRef, bool) {
	// Unwrap generic instantiations: foo[T](), pkg.Foo[T](), foo[T, U]().
	fun = unwrapGoGenericCallee(fun)

	switch f := fun.(type) {
	case *ast.Ident:
		// Local call: Foo()
		if f.Name == "" {
			return SymbolRef{}, false
		}
		if goPredeclaredCalls[f.Name] {
			return SymbolRef{}, false
		}
		return SymbolRef{Lang: LangGo, Pkg: currentImportPath, Name: f.Name}, true
	case *ast.SelectorExpr:
		// Qualified call: pkgAlias.Foo() OR method-like call: x.Foo() OR method expression: T.Foo()
		pkgIdent, ok := f.X.(*ast.Ident)
		if !ok || pkgIdent == nil || pkgIdent.Name == "" || f.Sel == nil || f.Sel.Name == "" {
			return SymbolRef{}, false
		}
		if impPath, ok := imports[pkgIdent.Name]; ok {
			return SymbolRef{Lang: LangGo, Pkg: impPath, Name: f.Sel.Name}, true
		}
		// Method expression on a local type: T.Foo() → resolve to <importPath>.<T>.Foo
		if localTypes != nil {
			if _, ok := localTypes[pkgIdent.Name]; ok {
				return SymbolRef{Lang: LangGo, Pkg: currentImportPath + "." + pkgIdent.Name, Name: f.Sel.Name}, true
			}
		}
		// Best-effort: method call on a value (x.Foo()). We can't resolve receiver type without go/types.
		// Emit a symbol-only reference; resolver can use the file's package import path as a constraint.
		return SymbolRef{Lang: LangGo, Pkg: "", Name: f.Sel.Name}, true
	default:
		// We intentionally do not try to resolve method calls (x.Foo()) without type info.
		return SymbolRef{}, false
	}
}

func resolveGoTypeExpr(expr ast.Expr, currentImportPath string, imports map[string]string) (SymbolRef, bool) {
	expr = unwrapGoGenericType(expr)
	// Unwrap pointer types: *T -> T, *pkg.T -> pkg.T
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	switch t := expr.(type) {
	case *ast.Ident:
		if t.Name == "" {
			return SymbolRef{}, false
		}
		return SymbolRef{Lang: LangGo, Pkg: currentImportPath, Name: t.Name}, true
	case *ast.SelectorExpr:
		pkgIdent, ok := t.X.(*ast.Ident)
		if !ok || pkgIdent == nil || pkgIdent.Name == "" || t.Sel == nil || t.Sel.Name == "" {
			return SymbolRef{}, false
		}
		if impPath, ok := imports[pkgIdent.Name]; ok {
			return SymbolRef{Lang: LangGo, Pkg: impPath, Name: t.Sel.Name}, true
		}
		return SymbolRef{}, false
	default:
		// We intentionally do not attempt to resolve complex type expressions (maps, slices, etc.).
		return SymbolRef{}, false
	}
}

var goPredeclaredCalls = map[string]bool{
	// Built-in functions.
	"append":  true,
	"cap":     true,
	"clear":   true,
	"close":   true,
	"complex": true,
	"copy":    true,
	"delete":  true,
	"imag":    true,
	"len":     true,
	"make":    true,
	"max":     true,
	"min":     true,
	"new":     true,
	"panic":   true,
	"print":   true,
	"println": true,
	"real":    true,
	"recover": true,
	// Predeclared types used as conversions.
	"any":        true,
	"bool":       true,
	"byte":       true,
	"comparable": true,
	"complex64":  true,
	"complex128": true,
	"error":      true,
	"float32":    true,
	"float64":    true,
	"int":        true,
	"int8":       true,
	"int16":      true,
	"int32":      true,
	"int64":      true,
	"rune":       true,
	"string":     true,
	"uint":       true,
	"uint8":      true,
	"uint16":     true,
	"uint32":     true,
	"uint64":     true,
	"uintptr":    true,
}

func unwrapGoGenericCallee(expr ast.Expr) ast.Expr {
	switch e := expr.(type) {
	case *ast.IndexExpr:
		return unwrapGoGenericCallee(e.X)
	case *ast.IndexListExpr:
		return unwrapGoGenericCallee(e.X)
	default:
		return expr
	}
}

func unwrapGoGenericType(expr ast.Expr) ast.Expr {
	switch e := expr.(type) {
	case *ast.IndexExpr:
		return unwrapGoGenericType(e.X)
	case *ast.IndexListExpr:
		return unwrapGoGenericType(e.X)
	default:
		return expr
	}
}

func receiverTypeName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) == 0 || recv.List[0] == nil {
		return ""
	}
	// Receivers can be: T, *T, pkg.T, *pkg.T, or generic instantiation forms.
	return typeNameFromExpr(recv.List[0].Type)
}

// extractGoInterfaceEmbeddings returns SuperEdge entries for embedded interfaces.
// e.g., type Writer interface { io.Writer } -> SuperEdge{ChildFQN: "pkg.Writer", ParentFQN: "io.Writer"}
func extractGoInterfaceEmbeddings(iface *ast.InterfaceType, typeFQN, importPath string, imports map[string]string) []SuperEdge {
	if iface == nil || iface.Methods == nil {
		return nil
	}
	var edges []SuperEdge
	for _, field := range iface.Methods.List {
		if field == nil {
			continue
		}
		// Embedded interfaces have no names (anonymous field).
		if len(field.Names) > 0 {
			continue
		}
		// Resolve the embedded type.
		ref, ok := resolveGoTypeExpr(field.Type, importPath, imports)
		if !ok || ref.Name == "" {
			continue
		}
		parentFQN := ref.Name
		if ref.Pkg != "" {
			parentFQN = ref.Pkg + "." + ref.Name
		}
		edges = append(edges, SuperEdge{
			ChildFQN:  typeFQN,
			ParentFQN: parentFQN,
		})
	}
	return edges
}

// extractGoStructEmbeddings returns SuperEdge entries for embedded structs.
// e.g., type Service struct { *BaseService } -> SuperEdge{ChildFQN: "pkg.Service", ParentFQN: "pkg.BaseService"}
func extractGoStructEmbeddings(st *ast.StructType, typeFQN, importPath string, imports map[string]string) []SuperEdge {
	if st == nil || st.Fields == nil {
		return nil
	}
	var edges []SuperEdge
	for _, field := range st.Fields.List {
		if field == nil {
			continue
		}
		// Embedded structs have no names (anonymous field).
		if len(field.Names) > 0 {
			continue
		}
		// Resolve the embedded type (unwrap pointer if needed).
		ref, ok := resolveGoTypeExpr(field.Type, importPath, imports)
		if !ok || ref.Name == "" {
			continue
		}
		parentFQN := ref.Name
		if ref.Pkg != "" {
			parentFQN = ref.Pkg + "." + ref.Name
		}
		edges = append(edges, SuperEdge{
			ChildFQN:  typeFQN,
			ParentFQN: parentFQN,
		})
	}
	return edges
}

// extractGoStructFields returns Symbol entries for exported struct fields.
// e.g., type Task struct { ID string } -> Symbol{Kind: SymField, Name: "ID", ...}
func extractGoStructFields(st *ast.StructType, fset *token.FileSet, content []byte, typeName, importPath, filePath string) []Symbol {
	if st == nil || st.Fields == nil {
		return nil
	}
	var symbols []Symbol
	typePkg := importPath + "." + typeName
	for _, field := range st.Fields.List {
		if field == nil {
			continue
		}
		// Skip embedded fields (no names) and unexported fields.
		for _, name := range field.Names {
			if name == nil || name.Name == "" {
				continue
			}
			// Only include exported fields.
			if !ast.IsExported(name.Name) {
				continue
			}
			sym := Symbol{
				Lang:     LangGo,
				Kind:     SymField,
				File:     filePath,
				Pkg:      typePkg,
				Name:     name.Name,
				FQN:      typePkg + "." + name.Name,
				Exported: true,
			}
			// Build signature from field type.
			if field.Type != nil {
				var buf bytes.Buffer
				if err := format.Node(&buf, fset, field.Type); err == nil {
					sym.Signature = name.Name + " " + buf.String()
				} else {
					sym.Signature = name.Name
				}
			}
			// Set span for the field.
			if name.Pos() != token.NoPos && field.End() != token.NoPos {
				setGoSymbolSpan(&sym, fset, content, goSpan{start: name.Pos(), end: field.End()})
			}
			symbols = append(symbols, sym)
		}
	}
	return symbols
}

func typeNameFromExpr(expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return typeNameFromExpr(t.X)
	case *ast.SelectorExpr:
		// pkg.T -> use T (we don't want the alias)
		if t.Sel != nil {
			return t.Sel.Name
		}
		return ""
	case *ast.IndexExpr:
		// Generic receiver: T[P] -> base name T
		return typeNameFromExpr(t.X)
	case *ast.IndexListExpr:
		return typeNameFromExpr(t.X)
	default:
		return ""
	}
}
