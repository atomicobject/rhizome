//go:build cgo

package codeanchor

// Docs:
// - [Code anchors (Hub)](docs/hubs/Code anchors (Hub).md)

import (
	"log"
	"regexp"
	"strings"
	"sync"

	"github.com/atomicobject/rhizome/pkg/paths"
	sitter "github.com/tree-sitter/go-tree-sitter"
	sitterphp "github.com/tree-sitter/tree-sitter-php/bindings/go"
)

// phpIdentifierRe matches PHP identifiers (optionally fully qualified with backslashes).
// Used to filter string-literal arguments that look like callee names — see phpCollectCalls.
var phpIdentifierRe = regexp.MustCompile(`^\\?[a-zA-Z_][a-zA-Z0-9_]*(?:\\[a-zA-Z_][a-zA-Z0-9_]*)*$`)
var phpNewAssignmentRe = regexp.MustCompile(`^\s*\$([a-zA-Z_][a-zA-Z0-9_]*)\s*=\s*new\s+(\\?[a-zA-Z_][a-zA-Z0-9_]*(?:\\[a-zA-Z_][a-zA-Z0-9_]*)*)`)
var phpNewExpressionRe = regexp.MustCompile(`^\s*new\s+(\\?[a-zA-Z_][a-zA-Z0-9_]*(?:\\[a-zA-Z_][a-zA-Z0-9_]*)*)`)

// PHPIndexer implements LanguageIndexer for PHP using tree-sitter.
// Targets PHP 7.4+. Traits are indexed as SymClass (v1; no new SymKind).
// Global-scope (procedural) files are fully supported via SymFunc.
type PHPIndexer struct {
	parserPool  sync.Pool
	sourceRoots []string
	limits      TreeSitterLimits
}

type phpImportContext map[string]string

type phpClassContext struct {
	fqn           string
	propertyTypes map[string]SymbolRef
}

// NewPHPIndexer constructs a PHP indexer with default source roots.
func NewPHPIndexer() *PHPIndexer {
	return NewPHPIndexerWithRootsAndLimits([]string{"src", "app", "lib"}, TreeSitterLimits{})
}

// NewPHPIndexerWithRootsAndLimits constructs a PHP indexer with explicit roots and limits.
func NewPHPIndexerWithRootsAndLimits(roots []string, limits TreeSitterLimits) *PHPIndexer {
	idx := &PHPIndexer{sourceRoots: roots, limits: limits}
	idx.parserPool.New = func() any {
		p := sitter.NewParser()
		p.SetLanguage(sitter.NewLanguage(sitterphp.LanguagePHP()))
		return p
	}
	return idx
}

// Lang returns the language identifier.
func (*PHPIndexer) Lang() Lang { return LangPhp }

// IndexFile parses a PHP file and extracts symbols, calls, and imports.
func (idx *PHPIndexer) IndexFile(content []byte, ref paths.CodePathRef) (FileSummary, error) {
	filePath := ref.Rel.String()
	if filePath == "" {
		filePath = ref.Abs.String()
	}

	parserAny := idx.parserPool.Get()
	var parser *sitter.Parser
	if parserAny == nil {
		parser = sitter.NewParser()
		parser.SetLanguage(sitter.NewLanguage(sitterphp.LanguagePHP()))
	} else {
		parser = parserAny.(*sitter.Parser)
	}

	parseTimeout := resolveTreeSitterLimits(content, idx.limits)
	tree, timedOut, err := parseTreeWithTimeout(parser, content, parseTimeout)
	if err != nil {
		parser.Close()
		return FileSummary{}, err
	}
	if timedOut {
		parser.Close()
		log.Printf("codeanchor: php parse timeout for %s", filePath)
		return FileSummary{FilePath: filePath, Lang: LangPhp, ParseStatus: ParseTimeout}, nil
	}
	defer idx.parserPool.Put(parser)
	defer tree.Close()

	root := tree.RootNode()
	hasError := root.HasError()

	var (
		namespace   string
		symbols     []Symbol
		supers      []SuperEdge
		annotations []AnnotationUse
		calls       []CallSite
		imports     []ImportEdge
		typeRefs    []TypeRef
		memberRefs  []MemberRef
	)
	importCtx := make(phpImportContext)

	// Walk top-level program node (tree-sitter-php wraps in "program")
	src := content
	var walkNode func(node *sitter.Node, ownerFQN string, classCtx *phpClassContext)
	walkNode = func(node *sitter.Node, ownerFQN string, classCtx *phpClassContext) {
		kind := node.Kind()
		switch kind {
		case "namespace_definition":
			if nameNode := node.ChildByFieldName("name"); nameNode != nil {
				namespace = phpNodeText(nameNode, src)
			}

		case "namespace_use_declaration":
			// use Vendor\Package\Class[ as Alias];
			phpCollectUseImports(node, src, &imports)
			phpCollectUseImportBindings(node, src, importCtx)

		case "include_expression", "require_expression",
			"include_once_expression", "require_once_expression":
			// include/require <expr>. The argument may be a bare string ('foo.php'),
			// a concat chain (ABSPATH . 'wp-cron.php', __DIR__ . '/inc/' . $name), or
			// a parenthesized form. phpExtractIncludePath joins literal fragments and
			// substitutes a placeholder for unresolved fragments (constants, variables)
			// so dynamic includes still surface a partial-info edge.
			if node.NamedChildCount() > 0 {
				if path, ok := phpExtractIncludePath(node.NamedChild(0), src); ok && path != "" {
					imports = append(imports, ImportEdge{Module: path})
				}
			}

		case "class_declaration", "trait_declaration":
			name := phpNodeText(node.ChildByFieldName("name"), src)
			if name == "" {
				break
			}
			fqn := phpFQN(namespace, name)
			doc := phpDocBefore(node, src)
			sym := Symbol{
				Lang:       LangPhp,
				Kind:       SymClass,
				File:       filePath,
				Name:       name,
				Pkg:        namespace,
				FQN:        fqn,
				DocComment: doc,
				Signature:  phpSignature(kind, name),
			}
			sym.StartLine = int64(node.StartPosition().Row) + 1
			sym.EndLine = int64(node.EndPosition().Row) + 1
			sym.StartByte = int64(node.StartByte())
			sym.EndByte = int64(node.EndByte())
			symbols = append(symbols, sym)
			annotations = append(annotations, phpCollectAttributes(node, src, fqn, namespace, importCtx)...)
			supers = append(supers, phpCollectSupers(node, src, fqn, namespace, importCtx)...)
			nextClassCtx := &phpClassContext{
				fqn:           fqn,
				propertyTypes: phpCollectClassPropertyTypes(node, src, namespace, importCtx),
			}
			// walk body
			if body := node.ChildByFieldName("body"); body != nil {
				for i := uint(0); i < body.NamedChildCount(); i++ {
					walkNode(body.NamedChild(i), fqn, nextClassCtx)
				}
			}
			return

		case "interface_declaration":
			name := phpNodeText(node.ChildByFieldName("name"), src)
			if name == "" {
				break
			}
			fqn := phpFQN(namespace, name)
			doc := phpDocBefore(node, src)
			sym := Symbol{
				Lang: LangPhp, Kind: SymInterface,
				File: filePath,
				Name: name, Pkg: namespace, FQN: fqn, DocComment: doc,
				Signature: "interface " + name,
			}
			sym.StartLine = int64(node.StartPosition().Row) + 1
			sym.EndLine = int64(node.EndPosition().Row) + 1
			sym.StartByte = int64(node.StartByte())
			sym.EndByte = int64(node.EndByte())
			symbols = append(symbols, sym)
			annotations = append(annotations, phpCollectAttributes(node, src, fqn, namespace, importCtx)...)
			supers = append(supers, phpCollectSupers(node, src, fqn, namespace, importCtx)...)
			if body := node.ChildByFieldName("body"); body != nil {
				for i := uint(0); i < body.NamedChildCount(); i++ {
					walkNode(body.NamedChild(i), fqn, &phpClassContext{fqn: fqn, propertyTypes: map[string]SymbolRef{}})
				}
			}
			return

		case "enum_declaration":
			name := phpNodeText(node.ChildByFieldName("name"), src)
			if name == "" {
				break
			}
			fqn := phpFQN(namespace, name)
			doc := phpDocBefore(node, src)
			sym := Symbol{
				Lang: LangPhp, Kind: SymEnum,
				File: filePath,
				Name: name, Pkg: namespace, FQN: fqn, DocComment: doc,
				Signature: "enum " + name,
			}
			sym.StartLine = int64(node.StartPosition().Row) + 1
			sym.EndLine = int64(node.EndPosition().Row) + 1
			sym.StartByte = int64(node.StartByte())
			sym.EndByte = int64(node.EndByte())
			symbols = append(symbols, sym)
			annotations = append(annotations, phpCollectAttributes(node, src, fqn, namespace, importCtx)...)
			symbols = append(symbols, phpCollectEnumCases(node, src, fqn, filePath)...)
			return

		case "function_definition":
			name := phpNodeText(node.ChildByFieldName("name"), src)
			if name == "" {
				break
			}
			fqn := phpFQN(namespace, name)
			pkg := namespace
			if ownerFQN != "" {
				fqn = ownerFQN + "::" + name
				pkg = ownerFQN
			}
			doc := phpDocBefore(node, src)
			kind2 := SymFunc
			if ownerFQN != "" {
				kind2 = SymMethod
			}
			sym := Symbol{
				Lang: LangPhp, Kind: kind2,
				File: filePath,
				Name: name, Pkg: pkg, FQN: fqn, DocComment: doc,
				Signature: "function " + name,
			}
			sym.StartLine = int64(node.StartPosition().Row) + 1
			sym.EndLine = int64(node.EndPosition().Row) + 1
			sym.StartByte = int64(node.StartByte())
			sym.EndByte = int64(node.EndByte())
			symbols = append(symbols, sym)
			annotations = append(annotations, phpCollectAttributes(node, src, fqn, namespace, importCtx)...)
			typeRefs = append(typeRefs, phpCollectSignatureTypeRefs(node, src, fqn, filePath, namespace, importCtx)...)
			// collect calls inside body
			if body := node.ChildByFieldName("body"); body != nil {
				phpCollectCalls(body, src, fqn, filePath, namespace, importCtx, classCtx, map[string]SymbolRef{}, &calls, &memberRefs, false)
			}
			return

		case "property_declaration":
			if ownerFQN != "" {
				for i := uint(0); i < node.NamedChildCount(); i++ {
					ch := node.NamedChild(i)
					if ch.Kind() == "property_element" {
						if varNode := ch.ChildByFieldName("name"); varNode != nil {
							name := strings.TrimPrefix(phpNodeText(varNode, src), "$")
							if name != "" {
								sym := Symbol{
									Lang: LangPhp, Kind: SymField,
									File: filePath,
									Name: name, Pkg: ownerFQN,
									FQN: ownerFQN + "::$" + name,
								}
								sym.StartLine = int64(node.StartPosition().Row) + 1
								sym.EndLine = int64(node.EndPosition().Row) + 1
								sym.StartByte = int64(node.StartByte())
								sym.EndByte = int64(node.EndByte())
								symbols = append(symbols, sym)
							}
						}
					}
				}
				typeRefs = append(typeRefs, phpCollectOwnedTypeRefs(node, src, ownerFQN, filePath, namespace, importCtx)...)
			}

		case "method_declaration":
			name := phpNodeText(node.ChildByFieldName("name"), src)
			if name == "" {
				break
			}
			fqn := name
			pkg := namespace
			if ownerFQN != "" {
				fqn = ownerFQN + "::" + name
				pkg = ownerFQN
			}
			doc := phpDocBefore(node, src)
			sym := Symbol{
				Lang: LangPhp, Kind: SymMethod,
				File: filePath,
				Name: name, Pkg: pkg, FQN: fqn, DocComment: doc,
				Signature: "function " + name,
			}
			sym.StartLine = int64(node.StartPosition().Row) + 1
			sym.EndLine = int64(node.EndPosition().Row) + 1
			sym.StartByte = int64(node.StartByte())
			sym.EndByte = int64(node.EndByte())
			symbols = append(symbols, sym)
			annotations = append(annotations, phpCollectAttributes(node, src, fqn, namespace, importCtx)...)
			typeRefs = append(typeRefs, phpCollectSignatureTypeRefs(node, src, fqn, filePath, namespace, importCtx)...)
			if body := node.ChildByFieldName("body"); body != nil {
				localTypes := phpCollectPromotedParameterTypes(node, src, namespace, importCtx)
				phpCollectCalls(body, src, fqn, filePath, namespace, importCtx, classCtx, localTypes, &calls, &memberRefs, false)
			}
			return
		}

		// default: recurse into children
		for i := uint(0); i < node.NamedChildCount(); i++ {
			walkNode(node.NamedChild(i), ownerFQN, classCtx)
		}
	}

	walkNode(root, "", nil)

	// WHY: the structured walker invokes phpCollectCalls only from inside function_definition
	// and method_declaration switch cases, so file-scope statements (e.g. WordPress's
	// default-filters.php with 600 lines of top-level add_action() / add_filter()) never get
	// their calls extracted. This second pass walks the root with stopAtBoundaries=true to pick
	// up file-scope calls while skipping into function/method/class bodies (already collected
	// by the structured walker). namespace is fully resolved at this point because the
	// structured walker visits namespace_definition before any code statements in well-formed
	// PHP. The file-scope owner FQN is the namespace (or empty string for unnamespaced files),
	// symmetric with how free functions are already attributed.
	phpCollectCalls(root, src, namespace, filePath, namespace, importCtx, nil, map[string]SymbolRef{}, &calls, &memberRefs, true)

	status := ParseOK
	if hasError {
		status = ParseErrored
	}

	return FileSummary{
		FilePath:    filePath,
		Lang:        LangPhp,
		Hash:        "",
		Symbols:     symbols,
		Supers:      supers,
		Annotations: annotations,
		Calls:       calls,
		Imports:     imports,
		TypeRefs:    typeRefs,
		MemberRefs:  memberRefs,
		ParseStatus: status,
	}, nil
}

// phpCollectCalls walks a node subtree collecting function/method call expressions.
//
// When stopAtBoundaries is true, the walk returns early on class/interface/trait/function/method
// declaration nodes. This is the file-scope pass mode: those subtrees are already collected by
// the structured walker's per-function/method phpCollectCalls(body, fqn, ...) invocations, so
// recursing into them would double-emit. PHP closures use distinct node kinds
// (anonymous_function_creation_expression, arrow_function) so they are intentionally NOT in the
// skip set — closure-body calls continue to inherit the lexical owner FQN.
func phpCollectCalls(node *sitter.Node, src []byte, ownerFQN, filePath, namespace string, imports phpImportContext, classCtx *phpClassContext, localTypes map[string]SymbolRef, calls *[]CallSite, memberRefs *[]MemberRef, stopAtBoundaries bool) {
	if node == nil {
		return
	}
	kind := node.Kind()
	if stopAtBoundaries {
		switch kind {
		case "function_definition", "method_declaration",
			"class_declaration", "interface_declaration", "trait_declaration":
			return
		}
	}
	if kind == "assignment_expression" {
		phpRecordAssignmentType(node, src, namespace, imports, localTypes)
	}
	if kind == "function_call_expression" {
		if fn := node.ChildByFieldName("function"); fn != nil {
			name := phpNodeText(fn, src)
			if name != "" {
				*calls = append(*calls, CallSite{
					File:     filePath,
					OwnerFQN: ownerFQN,
					CalleeSymbol: SymbolRef{
						Lang: LangPhp,
						Name: phpBaseName(name),
						Pkg:  phpPkgFromQualified(name),
					},
				})
			}
		}
		// String-argument callback resolution: registration APIs like add_action('init', 'my_handler'),
		// Route::get('/foo', 'FooController@show'), call_user_func('handler') pass the callee by string
		// name. The handler would otherwise be a graph orphan with no incoming edges. Match by AST shape
		// (string literal whose contents look like a PHP identifier), not by registration function name —
		// false positives are acceptable since they only manifest if a same-named symbol happens to be
		// indexed.
		phpCollectStringArgCallees(node, src, ownerFQN, filePath, calls)
	} else if kind == "member_call_expression" || kind == "scoped_call_expression" || kind == "nullsafe_member_call_expression" {
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name := phpNodeText(nameNode, src)
			if name != "" {
				// Member=true: the callee is a class member (method/static), so
				// normalizeSymbol joins Pkg and Name with `::`, matching the FQN
				// the indexer stored for the method (e.g. Acme\Theme\Foo::bar).
				ref := SymbolRef{Lang: LangPhp, Name: name, Member: true}
				if receiver, ok := phpResolveCallReceiver(node, src, namespace, imports, classCtx, localTypes); ok {
					ref.Pkg = receiver.Pkg
					if receiver.Pkg != "" {
						ref.Pkg += `\`
					}
					ref.Pkg += receiver.Name
				}
				*calls = append(*calls, CallSite{
					File:         filePath,
					OwnerFQN:     ownerFQN,
					CalleeSymbol: ref,
				})
			}
		}
	} else if kind == "scoped_property_access_expression" || kind == "class_constant_access_expression" {
		if ref, ok := phpMemberRefFromScopedAccess(node, src, namespace, imports); ok {
			*memberRefs = append(*memberRefs, MemberRef{
				File:     filePath,
				OwnerFQN: ownerFQN,
				Sym:      ref,
			})
		}
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		phpCollectCalls(node.NamedChild(i), src, ownerFQN, filePath, namespace, imports, classCtx, localTypes, calls, memberRefs, stopAtBoundaries)
	}
}

func phpRecordAssignmentType(node *sitter.Node, src []byte, namespace string, imports phpImportContext, localTypes map[string]SymbolRef) {
	if localTypes == nil {
		return
	}
	left := node.ChildByFieldName("left")
	right := node.ChildByFieldName("right")
	if left == nil || right == nil || left.Kind() != "variable_name" {
		if match := phpNewAssignmentRe.FindStringSubmatch(phpNodeText(node, src)); len(match) == 3 {
			if ref, ok := phpResolveTypeText(match[2], namespace, imports); ok {
				localTypes[match[1]] = ref
			}
		}
		return
	}
	ref, ok := phpTypeFromObjectCreation(right, src, namespace, imports)
	if !ok {
		return
	}
	name := strings.TrimPrefix(phpNodeText(left, src), "$")
	if name != "" {
		localTypes[name] = ref
	}
}

func phpResolveCallReceiver(node *sitter.Node, src []byte, namespace string, imports phpImportContext, classCtx *phpClassContext, localTypes map[string]SymbolRef) (SymbolRef, bool) {
	switch node.Kind() {
	case "scoped_call_expression":
		return phpResolveTypeNode(node.ChildByFieldName("scope"), src, namespace, imports)
	case "member_call_expression", "nullsafe_member_call_expression":
		return phpResolveReceiverExpr(node.ChildByFieldName("object"), src, namespace, imports, classCtx, localTypes)
	}
	return SymbolRef{}, false
}

func phpResolveReceiverExpr(node *sitter.Node, src []byte, namespace string, imports phpImportContext, classCtx *phpClassContext, localTypes map[string]SymbolRef) (SymbolRef, bool) {
	if node == nil {
		return SymbolRef{}, false
	}
	switch node.Kind() {
	case "variable_name":
		name := strings.TrimPrefix(phpNodeText(node, src), "$")
		if ref, ok := localTypes[name]; ok {
			return ref, true
		}
	case "member_access_expression", "nullsafe_member_access_expression":
		obj := node.ChildByFieldName("object")
		nameNode := node.ChildByFieldName("name")
		if classCtx != nil && obj != nil && nameNode != nil && phpNodeText(obj, src) == "$this" {
			name := strings.TrimPrefix(phpNodeText(nameNode, src), "$")
			if ref, ok := classCtx.propertyTypes[name]; ok {
				return ref, true
			}
		}
	case "object_creation_expression":
		return phpTypeFromObjectCreation(node, src, namespace, imports)
	case "scoped_call_expression":
		return phpResolveTypeNode(node.ChildByFieldName("scope"), src, namespace, imports)
	}
	return SymbolRef{}, false
}

func phpTypeFromObjectCreation(node *sitter.Node, src []byte, namespace string, imports phpImportContext) (SymbolRef, bool) {
	if node == nil || node.Kind() != "object_creation_expression" {
		return SymbolRef{}, false
	}
	if typeNode := node.ChildByFieldName("type"); typeNode != nil {
		return phpResolveTypeNode(typeNode, src, namespace, imports)
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		ch := node.NamedChild(i)
		if phpLooksLikeTypeNode(ch) {
			return phpResolveTypeNode(ch, src, namespace, imports)
		}
	}
	if match := phpNewExpressionRe.FindStringSubmatch(phpNodeText(node, src)); len(match) == 2 {
		return phpResolveTypeText(match[1], namespace, imports)
	}
	return SymbolRef{}, false
}

func phpMemberRefFromScopedAccess(node *sitter.Node, src []byte, namespace string, imports phpImportContext) (SymbolRef, bool) {
	scopeNode := node.ChildByFieldName("scope")
	nameNode := node.ChildByFieldName("name")
	if scopeNode == nil || nameNode == nil {
		text := phpNodeText(node, src)
		parts := strings.Split(text, "::")
		if len(parts) != 2 {
			return SymbolRef{}, false
		}
		scope, ok := phpResolveTypeText(parts[0], namespace, imports)
		if !ok {
			return SymbolRef{}, false
		}
		name := strings.TrimPrefix(strings.TrimSpace(parts[1]), "$")
		if name == "" || name == "class" {
			return SymbolRef{}, false
		}
		pkg := scope.Pkg
		if pkg != "" {
			pkg += `\`
		}
		pkg += scope.Name
		// Scoped access (Foo::CONST, Foo::$prop, Foo::method) is a class member;
		// normalizeSymbol must join with `::` to match the stored method/property FQN.
		return SymbolRef{Lang: LangPhp, Pkg: pkg, Name: name, Member: true}, true
	}
	scope, ok := phpResolveTypeNode(scopeNode, src, namespace, imports)
	if !ok {
		return SymbolRef{}, false
	}
	name := phpNodeText(nameNode, src)
	name = strings.TrimPrefix(name, "$")
	if name == "" || name == "class" {
		return SymbolRef{}, false
	}
	pkg := scope.Pkg
	if pkg != "" {
		pkg += `\`
	}
	pkg += scope.Name
	return SymbolRef{Lang: LangPhp, Pkg: pkg, Name: name, Member: true}, true
}

// phpCollectStringArgCallees inspects a function_call_expression's arguments and emits
// CallSite entries for string-literal arguments that look like PHP identifiers. This is
// the registration-API pattern: add_action('init', 'my_handler'), Route::get('/x', 'Ctrl@m'),
// call_user_func('handler'). Without this, the named handler appears as a graph orphan even
// though the registration site clearly references it. Detection is purely AST-shape based;
// false positives only manifest when a same-named symbol happens to be indexed.
func phpCollectStringArgCallees(callNode *sitter.Node, src []byte, ownerFQN, filePath string, calls *[]CallSite) {
	args := callNode.ChildByFieldName("arguments")
	if args == nil {
		return
	}
	for i := uint(0); i < args.NamedChildCount(); i++ {
		arg := args.NamedChild(i)
		// tree-sitter-php wraps each call argument in an `argument` node whose first
		// named child is the actual expression. Unwrap when present, but also accept
		// direct string children for grammar resilience.
		candidate := arg
		if arg.Kind() == "argument" && arg.NamedChildCount() > 0 {
			candidate = arg.NamedChild(0)
		}
		switch candidate.Kind() {
		case "string", "encapsed_string":
		default:
			continue
		}
		val := strings.Trim(phpNodeText(candidate, src), `'"`)
		if val == "" || !phpIdentifierRe.MatchString(val) {
			continue
		}
		*calls = append(*calls, CallSite{
			File:     filePath,
			OwnerFQN: ownerFQN,
			CalleeSymbol: SymbolRef{
				Lang: LangPhp,
				Name: phpBaseName(val),
				Pkg:  phpPkgFromQualified(val),
			},
		})
	}
}

// phpExtractIncludePath joins literal fragments of an include/require argument expression
// into a single path string. Unresolved fragments (constants like ABSPATH, magic constants
// like __DIR__, variables, function calls) are substituted with `{...}` placeholders so
// dynamic includes still surface partial information for graph density. Returns the joined
// path and true if any fragment was extractable; "", false if the shape is unrecognized.
//
// Examples (left = argument source, right = returned path):
//
//	'foo.php'                              -> foo.php
//	ABSPATH . 'wp-cron.php'                -> {ABSPATH}wp-cron.php
//	__DIR__ . '/inc/' . 'helpers.php'      -> {__DIR__}/inc/helpers.php
//	__DIR__ . '/inc/' . $name              -> {__DIR__}/inc/{var}
func phpExtractIncludePath(node *sitter.Node, src []byte) (string, bool) {
	if node == nil {
		return "", false
	}
	switch node.Kind() {
	case "string", "encapsed_string":
		return strings.Trim(phpNodeText(node, src), `'"`), true
	case "binary_expression":
		// Only "." (concatenation) is meaningful for include paths.
		op := node.ChildByFieldName("operator")
		if op == nil || phpNodeText(op, src) != "." {
			return "", false
		}
		ls, lok := phpExtractIncludePath(node.ChildByFieldName("left"), src)
		rs, rok := phpExtractIncludePath(node.ChildByFieldName("right"), src)
		if !lok && !rok {
			return "", false
		}
		return ls + rs, true
	case "parenthesized_expression":
		if node.NamedChildCount() > 0 {
			return phpExtractIncludePath(node.NamedChild(0), src)
		}
		return "", false
	case "name":
		// Constant reference (ABSPATH, WP_PLUGIN_DIR) or magic constant (__DIR__, __FILE__).
		// We can't resolve constants statically, so emit a placeholder; downstream tooling
		// can still grep for the literal portion or surface the constant name.
		txt := phpNodeText(node, src)
		if txt == "" {
			return "", false
		}
		return "{" + txt + "}", true
	case "variable_name":
		return "{var}", true
	case "function_call_expression", "member_call_expression",
		"scoped_call_expression", "nullsafe_member_call_expression",
		"subscript_expression":
		return "{expr}", true
	}
	return "", false
}

// phpCollectUseImports extracts module names from a namespace_use_declaration.
// Handles both:
//
//	use Vendor\Package\Class;
//	use Vendor\Package\{ClassA, ClassB};
//	use Vendor\Package\Class as Alias;
func phpCollectUseImports(node *sitter.Node, src []byte, imports *[]ImportEdge) {
	// Look for a leading qualified prefix (only present in grouped form: use Vendor\Pkg\{A, B};).
	// In flat form (use Vendor\Pkg\A;), the full name is inside namespace_use_clause itself.
	var prefix string
	for i := uint(0); i < node.NamedChildCount(); i++ {
		ch := node.NamedChild(i)
		if ch.Kind() == "namespace_name" && i+1 < node.NamedChildCount() {
			// Heuristic: namespace_name appears as a prefix only when followed by a use_group.
			next := node.NamedChild(i + 1)
			if next != nil && next.Kind() == "namespace_use_group" {
				prefix = phpNodeText(ch, src)
				break
			}
		}
	}
	var visit func(n *sitter.Node)
	visit = func(n *sitter.Node) {
		if n == nil {
			return
		}
		if n.Kind() == "namespace_use_clause" {
			// The first child is the qualified module name.
			var mod string
			for i := uint(0); i < n.NamedChildCount(); i++ {
				ch := n.NamedChild(i)
				if ch.Kind() == "qualified_name" || ch.Kind() == "name" || ch.Kind() == "namespace_name" {
					mod = phpNodeText(ch, src)
					break
				}
			}
			if mod != "" {
				if prefix != "" {
					mod = prefix + `\` + mod
				}
				*imports = append(*imports, ImportEdge{Module: mod})
			}
			return
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			visit(n.NamedChild(i))
		}
	}
	visit(node)
}

func phpCollectUseImportBindings(node *sitter.Node, src []byte, imports phpImportContext) {
	for _, binding := range phpUseBindings(node, src) {
		alias := binding.alias
		if alias == "" {
			alias = phpBaseName(binding.module)
		}
		if alias != "" {
			imports[alias] = strings.TrimPrefix(binding.module, `\`)
		}
	}
}

type phpUseBinding struct {
	module string
	alias  string
}

func phpUseBindings(node *sitter.Node, src []byte) []phpUseBinding {
	var prefix string
	for i := uint(0); i < node.NamedChildCount(); i++ {
		ch := node.NamedChild(i)
		if ch.Kind() == "namespace_name" && i+1 < node.NamedChildCount() {
			next := node.NamedChild(i + 1)
			if next != nil && next.Kind() == "namespace_use_group" {
				prefix = phpNodeText(ch, src)
				break
			}
		}
	}
	var bindings []phpUseBinding
	var visit func(n *sitter.Node)
	visit = func(n *sitter.Node) {
		if n == nil {
			return
		}
		if n.Kind() == "namespace_use_clause" {
			var mod string
			var alias string
			for i := uint(0); i < n.NamedChildCount(); i++ {
				ch := n.NamedChild(i)
				switch ch.Kind() {
				case "qualified_name", "name", "namespace_name":
					if mod == "" {
						mod = phpNodeText(ch, src)
					} else {
						alias = phpNodeText(ch, src)
					}
				}
			}
			if mod != "" {
				if prefix != "" {
					mod = prefix + `\` + mod
				}
				bindings = append(bindings, phpUseBinding{module: mod, alias: alias})
			}
			return
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			visit(n.NamedChild(i))
		}
	}
	visit(node)
	return bindings
}

func phpCollectSupers(node *sitter.Node, src []byte, childFQN, namespace string, imports phpImportContext) []SuperEdge {
	var out []SuperEdge
	var visit func(n *sitter.Node)
	visit = func(n *sitter.Node) {
		if n == nil {
			return
		}
		if n.Kind() == "base_clause" || n.Kind() == "class_interface_clause" {
			for _, ref := range phpCollectTypeRefsFromNode(n, src, namespace, imports) {
				out = append(out, SuperEdge{ChildFQN: childFQN, ParentFQN: phpTypeRefFQN(ref)})
			}
			return
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			visit(n.NamedChild(i))
		}
	}
	visit(node)
	return dedupePHPSupers(out)
}

func phpCollectAttributes(node *sitter.Node, src []byte, ownerFQN, namespace string, imports phpImportContext) []AnnotationUse {
	var out []AnnotationUse
	var visit func(n *sitter.Node)
	visit = func(n *sitter.Node) {
		if n == nil {
			return
		}
		if n.Kind() == "attribute" {
			ref, ok := phpResolveTypeNode(n.ChildByFieldName("name"), src, namespace, imports)
			if !ok {
				for i := uint(0); i < n.NamedChildCount(); i++ {
					ch := n.NamedChild(i)
					if phpLooksLikeTypeNode(ch) {
						ref, ok = phpResolveTypeNode(ch, src, namespace, imports)
						break
					}
				}
			}
			if ok {
				out = append(out, AnnotationUse{
					OwnerFQN:  ownerFQN,
					AnnSymbol: ref,
					Args:      phpAttributeArgs(n, src),
				})
			}
			return
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			visit(n.NamedChild(i))
		}
	}
	if attrs := node.ChildByFieldName("attributes"); attrs != nil {
		visit(attrs)
	} else {
		for i := uint(0); i < node.NamedChildCount(); i++ {
			ch := node.NamedChild(i)
			if ch.Kind() == "attribute_group" || ch.Kind() == "attribute_list" || ch.Kind() == "attribute" {
				visit(ch)
			}
		}
	}
	return out
}

func phpAttributeArgs(node *sitter.Node, src []byte) map[string]string {
	argsNode := node.ChildByFieldName("arguments")
	if argsNode == nil {
		argsNode = phpFindFirstKind(node, "arguments")
	}
	if argsNode == nil {
		return nil
	}
	args := make(map[string]string)
	pos := 0
	for i := uint(0); i < argsNode.NamedChildCount(); i++ {
		arg := argsNode.NamedChild(i)
		text := strings.TrimSpace(phpNodeText(arg, src))
		if text == "" {
			continue
		}
		key := ""
		value := text
		if colon := strings.Index(text, ":"); colon > 0 {
			key = strings.TrimSpace(text[:colon])
			value = strings.TrimSpace(text[colon+1:])
		} else {
			key = string(rune('0' + pos))
			pos++
		}
		value = strings.Trim(value, `"'[] `)
		if key != "" && value != "" {
			args[key] = value
		}
	}
	if len(args) == 0 {
		return nil
	}
	return args
}

func phpFindFirstKind(node *sitter.Node, kind string) *sitter.Node {
	if node == nil {
		return nil
	}
	if node.Kind() == kind {
		return node
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		if found := phpFindFirstKind(node.NamedChild(i), kind); found != nil {
			return found
		}
	}
	return nil
}

func phpCollectEnumCases(node *sitter.Node, src []byte, enumFQN, filePath string) []Symbol {
	var out []Symbol
	var visit func(n *sitter.Node)
	visit = func(n *sitter.Node) {
		if n == nil {
			return
		}
		if n.Kind() == "enum_case" {
			name := phpNodeText(n.ChildByFieldName("name"), src)
			if name != "" {
				sym := Symbol{
					Lang: LangPhp, Kind: SymField,
					File: filePath,
					Name: name, Pkg: enumFQN,
					FQN: enumFQN + "::" + name,
				}
				sym.StartLine = int64(n.StartPosition().Row) + 1
				sym.EndLine = int64(n.EndPosition().Row) + 1
				sym.StartByte = int64(n.StartByte())
				sym.EndByte = int64(n.EndByte())
				out = append(out, sym)
			}
			return
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			visit(n.NamedChild(i))
		}
	}
	visit(node)
	return out
}

func phpCollectSignatureTypeRefs(node *sitter.Node, src []byte, ownerFQN, filePath, namespace string, imports phpImportContext) []TypeRef {
	var out []TypeRef
	if params := node.ChildByFieldName("parameters"); params != nil {
		out = append(out, phpCollectOwnedTypeRefs(params, src, ownerFQN, filePath, namespace, imports)...)
	}
	if ret := node.ChildByFieldName("type"); ret != nil {
		out = append(out, phpTypeRefsForOwner(ret, src, ownerFQN, filePath, namespace, imports)...)
	}
	return dedupePHPTypeRefs(out)
}

func phpCollectOwnedTypeRefs(node *sitter.Node, src []byte, ownerFQN, filePath, namespace string, imports phpImportContext) []TypeRef {
	return dedupePHPTypeRefs(phpTypeRefsForOwner(node, src, ownerFQN, filePath, namespace, imports))
}

func phpTypeRefsForOwner(node *sitter.Node, src []byte, ownerFQN, filePath, namespace string, imports phpImportContext) []TypeRef {
	var out []TypeRef
	for _, ref := range phpCollectTypeRefsFromNode(node, src, namespace, imports) {
		out = append(out, TypeRef{File: filePath, OwnerFQN: ownerFQN, TypeSym: ref})
	}
	return out
}

func phpCollectClassPropertyTypes(node *sitter.Node, src []byte, namespace string, imports phpImportContext) map[string]SymbolRef {
	out := make(map[string]SymbolRef)
	var visit func(n *sitter.Node)
	visit = func(n *sitter.Node) {
		if n == nil {
			return
		}
		switch n.Kind() {
		case "property_declaration":
			ref, ok := phpFirstTypeRef(n, src, namespace, imports)
			if !ok {
				return
			}
			for i := uint(0); i < n.NamedChildCount(); i++ {
				ch := n.NamedChild(i)
				if ch.Kind() == "property_element" {
					name := strings.TrimPrefix(phpNodeText(ch.ChildByFieldName("name"), src), "$")
					if name != "" {
						out[name] = ref
					}
				}
			}
		case "method_declaration":
			if phpNodeText(n.ChildByFieldName("name"), src) != "__construct" {
				return
			}
			for name, ref := range phpCollectPromotedParameterTypes(n, src, namespace, imports) {
				out[name] = ref
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			visit(n.NamedChild(i))
		}
	}
	visit(node)
	return out
}

func phpCollectPromotedParameterTypes(node *sitter.Node, src []byte, namespace string, imports phpImportContext) map[string]SymbolRef {
	out := make(map[string]SymbolRef)
	params := node.ChildByFieldName("parameters")
	if params == nil {
		return out
	}
	var visit func(n *sitter.Node)
	visit = func(n *sitter.Node) {
		if n == nil {
			return
		}
		if n.Kind() == "simple_parameter" || n.Kind() == "property_promotion_parameter" || n.Kind() == "parameter" {
			ref, ok := phpFirstTypeRef(n, src, namespace, imports)
			if ok {
				name := phpFindFirstVariableName(n, src)
				if name != "" {
					out[name] = ref
				}
			}
			return
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			visit(n.NamedChild(i))
		}
	}
	visit(params)
	return out
}

func phpFindFirstVariableName(node *sitter.Node, src []byte) string {
	if node == nil {
		return ""
	}
	if node.Kind() == "variable_name" {
		return strings.TrimPrefix(phpNodeText(node, src), "$")
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		if name := phpFindFirstVariableName(node.NamedChild(i), src); name != "" {
			return name
		}
	}
	return ""
}

func phpFirstTypeRef(node *sitter.Node, src []byte, namespace string, imports phpImportContext) (SymbolRef, bool) {
	refs := phpCollectTypeRefsFromNode(node, src, namespace, imports)
	if len(refs) == 0 {
		return SymbolRef{}, false
	}
	return refs[0], true
}

func phpCollectTypeRefsFromNode(node *sitter.Node, src []byte, namespace string, imports phpImportContext) []SymbolRef {
	seen := map[string]struct{}{}
	var out []SymbolRef
	var visit func(n *sitter.Node)
	visit = func(n *sitter.Node) {
		if n == nil {
			return
		}
		if phpLooksLikeTypeNode(n) {
			if ref, ok := phpResolveTypeNode(n, src, namespace, imports); ok {
				key := symbolRefKey(ref)
				if _, exists := seen[key]; !exists {
					seen[key] = struct{}{}
					out = append(out, ref)
				}
			}
			return
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			visit(n.NamedChild(i))
		}
	}
	visit(node)
	return out
}

func phpLooksLikeTypeNode(node *sitter.Node) bool {
	if node == nil {
		return false
	}
	switch node.Kind() {
	case "named_type", "qualified_name", "namespace_name":
		return true
	case "name":
		return true
	}
	return false
}

func phpResolveTypeNode(node *sitter.Node, src []byte, namespace string, imports phpImportContext) (SymbolRef, bool) {
	if node == nil {
		return SymbolRef{}, false
	}
	return phpResolveTypeText(phpNodeText(node, src), namespace, imports)
}

func phpResolveTypeText(raw string, namespace string, imports phpImportContext) (SymbolRef, bool) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, `\`)
	raw = strings.TrimSuffix(raw, "::class")
	raw = strings.TrimPrefix(raw, "?")
	if raw == "" || phpIsBuiltinType(raw) || strings.ContainsAny(raw, "$'\"") {
		return SymbolRef{}, false
	}
	if strings.Contains(raw, `\`) {
		pkg, name, _ := splitPhpSymbol(raw)
		return SymbolRef{Lang: LangPhp, Pkg: pkg, Name: name}, name != ""
	}
	if fqn, ok := imports[raw]; ok {
		pkg, name, _ := splitPhpSymbol(fqn)
		return SymbolRef{Lang: LangPhp, Pkg: pkg, Name: name}, name != ""
	}
	if namespace != "" {
		return SymbolRef{Lang: LangPhp, Pkg: namespace, Name: raw}, true
	}
	return SymbolRef{Lang: LangPhp, Name: raw}, true
}

func phpTypeRefFQN(ref SymbolRef) string {
	if ref.Pkg == "" {
		return ref.Name
	}
	return ref.Pkg + `\` + ref.Name
}

func phpIsBuiltinType(raw string) bool {
	switch strings.ToLower(strings.TrimPrefix(raw, `?`)) {
	case "array", "bool", "boolean", "callable", "float", "int", "integer",
		"iterable", "mixed", "never", "null", "object", "self", "static",
		"string", "true", "false", "void":
		return true
	}
	return false
}

func dedupePHPTypeRefs(in []TypeRef) []TypeRef {
	seen := map[string]struct{}{}
	out := make([]TypeRef, 0, len(in))
	for _, ref := range in {
		key := ref.File + "|" + ref.OwnerFQN + "|" + symbolRefKey(ref.TypeSym)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func dedupePHPSupers(in []SuperEdge) []SuperEdge {
	seen := map[string]struct{}{}
	out := make([]SuperEdge, 0, len(in))
	for _, edge := range in {
		key := edge.ChildFQN + "|" + edge.ParentFQN
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, edge)
	}
	return out
}

func phpNodeText(node *sitter.Node, src []byte) string {
	if node == nil {
		return ""
	}
	return string(src[node.StartByte():node.EndByte()])
}

func phpFQN(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + `\` + name
}

func phpSignature(kind, name string) string {
	switch kind {
	case "trait_declaration":
		return "trait " + name
	default:
		return "class " + name
	}
}

// phpDocBefore returns the PHPDoc comment immediately preceding node, if any.
func phpDocBefore(node *sitter.Node, src []byte) string {
	if node == nil {
		return ""
	}
	prev := node.PrevNamedSibling()
	if prev == nil {
		return ""
	}
	if prev.Kind() == "comment" {
		txt := phpNodeText(prev, src)
		if strings.HasPrefix(txt, "/**") {
			return txt
		}
	}
	return ""
}

func phpBaseName(qualified string) string {
	if idx := strings.LastIndexAny(qualified, `\:`); idx >= 0 {
		return qualified[idx+1:]
	}
	return qualified
}

func phpPkgFromQualified(qualified string) string {
	if idx := strings.LastIndex(qualified, `\`); idx >= 0 {
		return qualified[:idx]
	}
	return ""
}
