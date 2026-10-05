//go:build cgo

package codeanchor

// Docs:
// - [Code anchors (Hub)](docs/hubs/Code anchors (Hub).md)
// - [Code anchors - matching + scopes](docs/reference/analysis/code-anchors-matching-scopes.md)

import (
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/paths"
	sitter "github.com/tree-sitter/go-tree-sitter"
	sittercsharp "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go"
)

// CSharpIndexer implements LanguageIndexer for C# using tree-sitter.
// It is intentionally best-effort: it prefers returning partial summaries over failing hard.
type CSharpIndexer struct {
	parserPool           sync.Pool
	repoRoot             string
	limits               TreeSitterLimits
	globalUsingMu        sync.RWMutex
	globalUsingByProject map[string]csImportContext
}

type csImportContext struct {
	edges         []ImportEdge
	aliases       map[string]SymbolRef
	importedTypes map[string]SymbolRef
	namespaces    []string
}

type csMethodSignature struct {
	params     []SymbolRef
	returnType SymbolRef
}

// NewCSharpIndexer constructs a C# indexer without a repo root (uses path-based fallbacks).
func NewCSharpIndexer() *CSharpIndexer {
	return NewCSharpIndexerWithRoot("")
}

// NewCSharpIndexerWithRoot constructs a C# indexer rooted at repoRoot.
// repoRoot is normalized for deterministic package fallbacks.
func NewCSharpIndexerWithRoot(repoRoot string) *CSharpIndexer {
	return NewCSharpIndexerWithRootAndLimits(repoRoot, TreeSitterLimits{})
}

// NewCSharpIndexerWithRootAndLimits constructs a C# indexer rooted at repoRoot with tree-sitter limits.
func NewCSharpIndexerWithRootAndLimits(repoRoot string, limits TreeSitterLimits) *CSharpIndexer {
	idx := &CSharpIndexer{
		repoRoot:             paths.ResolveSymlinks(repoRoot).String(),
		limits:               limits,
		globalUsingByProject: make(map[string]csImportContext),
	}
	idx.parserPool.New = func() any {
		p := sitter.NewParser()
		p.SetLanguage(sitter.NewLanguage(sittercsharp.Language()))
		return p
	}
	return idx
}

func (*CSharpIndexer) Lang() Lang { return LangCs }

func (c *CSharpIndexer) IndexFile(content []byte, ref paths.CodePathRef) (FileSummary, error) {
	filePath := ref.Rel.String()
	if filePath == "" {
		filePath = ref.Abs.String()
	}
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext != ".cs" {
		return FileSummary{}, ErrUnsupportedLanguage
	}
	parserAny := c.parserPool.Get()
	var parser *sitter.Parser
	if parserAny == nil {
		parser = sitter.NewParser()
		parser.SetLanguage(sitter.NewLanguage(sittercsharp.Language()))
	} else {
		parser = parserAny.(*sitter.Parser)
	}

	parseTimeout := resolveTreeSitterLimits(content, c.limits)
	tree, timedOut, err := parseTreeWithTimeout(parser, content, parseTimeout)
	if err != nil {
		parser.Close()
		return FileSummary{}, err
	}
	if timedOut {
		parser.Close()
		log.Printf("codeanchor: csharp parse timeout for %s", filePath)
		return FileSummary{FilePath: filePath, Lang: LangCs, ParseStatus: ParseTimeout}, nil
	}
	defer c.parserPool.Put(parser)
	defer tree.Close()
	root := tree.RootNode()
	parseErrored := root.HasError()
	summary := c.summarizeTree(filePath, content, root, newIdentitySourceMap())
	recoveredOK := false
	if parseErrored {
		recovered, ok := c.recoverSummary(parser, filePath, content, root, parseTimeout)
		if ok {
			summary = mergeCSharpSummaries(summary, recovered, newIdentitySourceMap())
			recoveredOK = true
		}
	}
	summary.ParseStatus = ParseOK
	if parseErrored {
		if recoveredOK {
			summary.ParseStatus = ParseRecovered
		} else {
			summary.ParseStatus = ParseErrored
			logParseErrorIfPoor("csharp", filePath, summary, "recovery unavailable or insufficient")
		}
	}
	return summary, nil
}

func (c *CSharpIndexer) summarizeTree(filePath string, content []byte, root *sitter.Node, mapper transformedSourceMap) FileSummary {
	namespace := c.namespaceForFile(root, content)
	fallbackPkg := c.fallbackPkg(filePath)
	imports := c.buildResolvedImportContext(root, content, namespace, fallbackPkg, filePath)
	imports = c.preferLocalTypeDeclarations(root, content, namespace, fallbackPkg, imports)
	generated := c.isGeneratedFile(filePath, content)
	summary := FileSummary{
		FilePath: filePath,
		Lang:     LangCs,
	}
	summary.Imports = imports.edges

	var typeStack []string
	var memberTypeStack []map[string]SymbolRef
	var methodSigStack []map[string]csMethodSignature
	methodBudget := 400
	memberBudget := 0
	annotationBudget := 200
	methodCount := 0
	var walk func(n *sitter.Node, ownerFQN string, env csTypeEnv)
	walk = func(n *sitter.Node, ownerFQN string, env csTypeEnv) {
		if n == nil {
			return
		}
		switch n.Kind() {
		case "class_declaration", "struct_declaration", "interface_declaration", "enum_declaration", "record_declaration":
			name := nodeText(n.ChildByFieldName("name"), content)
			if name == "" {
				break
			}
			pkg := c.currentPkg(namespace, fallbackPkg, typeStack)
			kind := SymClass
			switch n.Kind() {
			case "struct_declaration":
				kind = SymStruct
			case "interface_declaration":
				kind = SymInterface
			case "enum_declaration":
				kind = SymEnum
			}
			sym := Symbol{
				Lang:      LangCs,
				Kind:      kind,
				File:      filePath,
				Pkg:       pkg,
				Name:      name,
				StartLine: mapper.OriginalLine(int64(n.StartPosition().Row) + 1),
				EndLine:   mapper.OriginalLine(int64(n.EndPosition().Row) + 1),
			}
			summary.Symbols = append(summary.Symbols, sym)
			owner := c.joinPkg(pkg, name)
			summary.Annotations = append(summary.Annotations, c.collectAttributes(n, imports, namespace, fallbackPkg, owner, content, generated, annotationBudget-len(summary.Annotations))...)
			summary.TypeRefs = append(summary.TypeRefs, c.attributeTypeRefs(n, imports, owner, namespace, fallbackPkg, content, filePath, generated, annotationBudget-len(summary.TypeRefs))...)
			summary.Supers = append(summary.Supers, c.baseEdges(n, sym.NormalizeFQN(), namespace, fallbackPkg, content)...)
			summary.TypeRefs = append(summary.TypeRefs, c.baseTypeRefs(n, imports, owner, namespace, fallbackPkg, content, filePath)...)

			typeStack = append(typeStack, name)
			memberTypes := c.collectTypeMemberTypes(n, imports, namespace, fallbackPkg, content)
			memberTypeStack = append(memberTypeStack, memberTypes)
			methodSigStack = append(methodSigStack, c.collectTypeMethodSignatures(n, imports, namespace, fallbackPkg, content))
			env.thisType = SymbolRef{Lang: LangCs, Pkg: pkg, Name: name}
			env.thisMembers = memberTypes
			env.thisMethods = methodSigStack[len(methodSigStack)-1]
			for i := uint(0); i < n.NamedChildCount(); i++ {
				walk(n.NamedChild(i), owner, env)
			}
			methodSigStack = methodSigStack[:len(methodSigStack)-1]
			memberTypeStack = memberTypeStack[:len(memberTypeStack)-1]
			typeStack = typeStack[:len(typeStack)-1]
			return
		case "method_declaration":
			if generated && methodCount >= methodBudget {
				return
			}
			name := nodeText(n.ChildByFieldName("name"), content)
			if name == "" {
				break
			}
			methodCount++
			pkg := c.currentPkg(namespace, fallbackPkg, typeStack)
			sym := Symbol{
				Lang:      LangCs,
				Kind:      SymMethod,
				File:      filePath,
				Pkg:       pkg,
				Name:      name,
				StartLine: mapper.OriginalLine(int64(n.StartPosition().Row) + 1),
				EndLine:   mapper.OriginalLine(int64(n.EndPosition().Row) + 1),
			}
			summary.Symbols = append(summary.Symbols, sym)
			owner := sym.NormalizeFQN()
			summary.Annotations = append(summary.Annotations, c.collectAttributes(n, imports, namespace, fallbackPkg, owner, content, generated, annotationBudget-len(summary.Annotations))...)
			summary.TypeRefs = append(summary.TypeRefs, c.attributeTypeRefs(n, imports, owner, namespace, fallbackPkg, content, filePath, generated, annotationBudget-len(summary.TypeRefs))...)
			summary.TypeRefs = append(summary.TypeRefs, c.methodSignatureTypeRefs(n, imports, owner, namespace, fallbackPkg, content, filePath)...)
			methodEnv := env.clone()
			c.addParameterTypes(n, imports, namespace, fallbackPkg, content, &summary, owner, filePath, &methodEnv)
			if ref, ok := c.symbolRefFromTypeNode(n.ChildByFieldName("type"), imports, namespace, fallbackPkg, content); ok {
				methodEnv.returnType = ref
			}
			for i := uint(0); i < n.NamedChildCount(); i++ {
				walk(n.NamedChild(i), owner, methodEnv)
			}
			return
		case "constructor_declaration":
			if generated && methodCount >= methodBudget {
				return
			}
			name := nodeText(n.ChildByFieldName("name"), content)
			if name == "" && len(typeStack) > 0 {
				name = typeStack[len(typeStack)-1]
			}
			if name != "" {
				methodCount++
				pkg := c.currentPkg(namespace, fallbackPkg, typeStack)
				sym := Symbol{
					Lang:      LangCs,
					Kind:      SymMethod,
					File:      filePath,
					Pkg:       pkg,
					Name:      name,
					StartLine: mapper.OriginalLine(int64(n.StartPosition().Row) + 1),
					EndLine:   mapper.OriginalLine(int64(n.EndPosition().Row) + 1),
				}
				summary.Symbols = append(summary.Symbols, sym)
				owner := sym.NormalizeFQN()
				summary.Annotations = append(summary.Annotations, c.collectAttributes(n, imports, namespace, fallbackPkg, owner, content, generated, annotationBudget-len(summary.Annotations))...)
				summary.TypeRefs = append(summary.TypeRefs, c.attributeTypeRefs(n, imports, owner, namespace, fallbackPkg, content, filePath, generated, annotationBudget-len(summary.TypeRefs))...)
				summary.TypeRefs = append(summary.TypeRefs, c.methodSignatureTypeRefs(n, imports, owner, namespace, fallbackPkg, content, filePath)...)
				ctorEnv := env.clone()
				c.addParameterTypes(n, imports, namespace, fallbackPkg, content, &summary, owner, filePath, &ctorEnv)
				ctorEnv.returnType = env.thisType
				for i := uint(0); i < n.NamedChildCount(); i++ {
					walk(n.NamedChild(i), owner, ctorEnv)
				}
				return
			}
		case "delegate_declaration":
			name := nodeText(n.ChildByFieldName("name"), content)
			if name == "" {
				break
			}
			pkg := c.currentPkg(namespace, fallbackPkg, typeStack)
			sym := Symbol{
				Lang:      LangCs,
				Kind:      SymType,
				File:      filePath,
				Pkg:       pkg,
				Name:      name,
				StartLine: mapper.OriginalLine(int64(n.StartPosition().Row) + 1),
				EndLine:   mapper.OriginalLine(int64(n.EndPosition().Row) + 1),
			}
			summary.Symbols = append(summary.Symbols, sym)
			owner := sym.NormalizeFQN()
			summary.Annotations = append(summary.Annotations, c.collectAttributes(n, imports, namespace, fallbackPkg, owner, content, generated, annotationBudget-len(summary.Annotations))...)
			summary.TypeRefs = append(summary.TypeRefs, c.attributeTypeRefs(n, imports, owner, namespace, fallbackPkg, content, filePath, generated, annotationBudget-len(summary.TypeRefs))...)
			summary.TypeRefs = append(summary.TypeRefs, c.methodSignatureTypeRefs(n, imports, owner, namespace, fallbackPkg, content, filePath)...)
		case "operator_declaration", "conversion_operator_declaration":
			if generated && methodCount >= methodBudget {
				return
			}
			name := c.operatorName(n, content)
			if name == "" {
				break
			}
			methodCount++
			pkg := c.currentPkg(namespace, fallbackPkg, typeStack)
			sym := Symbol{
				Lang:      LangCs,
				Kind:      SymMethod,
				File:      filePath,
				Pkg:       pkg,
				Name:      name,
				StartLine: mapper.OriginalLine(int64(n.StartPosition().Row) + 1),
				EndLine:   mapper.OriginalLine(int64(n.EndPosition().Row) + 1),
			}
			summary.Symbols = append(summary.Symbols, sym)
			owner := sym.NormalizeFQN()
			summary.Annotations = append(summary.Annotations, c.collectAttributes(n, imports, namespace, fallbackPkg, owner, content, generated, annotationBudget-len(summary.Annotations))...)
			summary.TypeRefs = append(summary.TypeRefs, c.attributeTypeRefs(n, imports, owner, namespace, fallbackPkg, content, filePath, generated, annotationBudget-len(summary.TypeRefs))...)
			summary.TypeRefs = append(summary.TypeRefs, c.methodSignatureTypeRefs(n, imports, owner, namespace, fallbackPkg, content, filePath)...)
			operatorEnv := env.clone()
			c.addParameterTypes(n, imports, namespace, fallbackPkg, content, &summary, owner, filePath, &operatorEnv)
			if ref, ok := c.symbolRefFromTypeNode(n.ChildByFieldName("return_type"), imports, namespace, fallbackPkg, content); ok {
				operatorEnv.returnType = ref
			}
			for i := uint(0); i < n.NamedChildCount(); i++ {
				walk(n.NamedChild(i), owner, operatorEnv)
			}
			return
		case "indexer_declaration":
			if generated && methodCount >= methodBudget {
				return
			}
			methodCount++
			pkg := c.currentPkg(namespace, fallbackPkg, typeStack)
			sym := Symbol{
				Lang:      LangCs,
				Kind:      SymMethod,
				File:      filePath,
				Pkg:       pkg,
				Name:      "this[]",
				StartLine: mapper.OriginalLine(int64(n.StartPosition().Row) + 1),
				EndLine:   mapper.OriginalLine(int64(n.EndPosition().Row) + 1),
			}
			summary.Symbols = append(summary.Symbols, sym)
			owner := sym.NormalizeFQN()
			summary.Annotations = append(summary.Annotations, c.collectAttributes(n, imports, namespace, fallbackPkg, owner, content, generated, annotationBudget-len(summary.Annotations))...)
			summary.TypeRefs = append(summary.TypeRefs, c.attributeTypeRefs(n, imports, owner, namespace, fallbackPkg, content, filePath, generated, annotationBudget-len(summary.TypeRefs))...)
			summary.TypeRefs = append(summary.TypeRefs, c.methodSignatureTypeRefs(n, imports, owner, namespace, fallbackPkg, content, filePath)...)
			indexerEnv := env.clone()
			c.addParameterTypes(n, imports, namespace, fallbackPkg, content, &summary, owner, filePath, &indexerEnv)
			if ref, ok := c.symbolRefFromTypeNode(n.ChildByFieldName("type"), imports, namespace, fallbackPkg, content); ok {
				indexerEnv.returnType = ref
			}
			for i := uint(0); i < n.NamedChildCount(); i++ {
				walk(n.NamedChild(i), owner, indexerEnv)
			}
			return
		case "property_declaration":
			if generated && memberBudget <= 0 {
				break
			}
			if generated {
				memberBudget--
			}
			name := nodeText(n.ChildByFieldName("name"), content)
			if name == "" {
				break
			}
			pkg := c.currentPkg(namespace, fallbackPkg, typeStack)
			owner := c.joinPkg(pkg, name)
			summary.Symbols = append(summary.Symbols, Symbol{
				Lang:      LangCs,
				Kind:      SymField,
				File:      filePath,
				Pkg:       pkg,
				Name:      name,
				StartLine: mapper.OriginalLine(int64(n.StartPosition().Row) + 1),
				EndLine:   mapper.OriginalLine(int64(n.EndPosition().Row) + 1),
			})
			summary.TypeRefs = append(summary.TypeRefs, c.ownedTypeRefsFromNode(n.ChildByFieldName("type"), imports, ownerFQNOr(ownerFQN, c.currentTypeOwner(namespace, fallbackPkg, typeStack)), namespace, fallbackPkg, filePath, content)...)
			summary.TypeRefs = append(summary.TypeRefs, c.attributeTypeRefs(n, imports, owner, namespace, fallbackPkg, content, filePath, generated, annotationBudget-len(summary.TypeRefs))...)
			if len(memberTypeStack) > 0 {
				if ref, ok := c.symbolRefFromTypeNode(n.ChildByFieldName("type"), imports, namespace, fallbackPkg, content); ok {
					memberTypeStack[len(memberTypeStack)-1][name] = ref
				}
			}
			c.addTargetTypedInitializerRefs(n, n.ChildByFieldName("type"), imports, ownerFQN, namespace, fallbackPkg, content, filePath, &summary)
		case "field_declaration":
			if generated && memberBudget <= 0 {
				break
			}
			if generated {
				memberBudget--
			}
			pkg := c.currentPkg(namespace, fallbackPkg, typeStack)
			owner := ownerFQNOr(ownerFQN, c.currentTypeOwner(namespace, fallbackPkg, typeStack))
			typeNode := c.firstTypeNode(n)
			summary.TypeRefs = append(summary.TypeRefs, c.ownedTypeRefsFromNode(typeNode, imports, owner, namespace, fallbackPkg, filePath, content)...)
			for _, name := range c.fieldNames(n, content) {
				summary.Symbols = append(summary.Symbols, Symbol{
					Lang:      LangCs,
					Kind:      SymField,
					File:      filePath,
					Pkg:       pkg,
					Name:      name,
					StartLine: mapper.OriginalLine(int64(n.StartPosition().Row) + 1),
					EndLine:   mapper.OriginalLine(int64(n.EndPosition().Row) + 1),
				})
				if len(memberTypeStack) > 0 {
					if ref, ok := c.symbolRefFromTypeNode(typeNode, imports, namespace, fallbackPkg, content); ok {
						memberTypeStack[len(memberTypeStack)-1][name] = ref
					}
				}
			}
			c.addTargetTypedInitializerRefs(n, typeNode, imports, ownerFQN, namespace, fallbackPkg, content, filePath, &summary)
		case "event_declaration", "event_field_declaration":
			if generated && memberBudget <= 0 {
				break
			}
			if generated {
				memberBudget--
			}
			pkg := c.currentPkg(namespace, fallbackPkg, typeStack)
			owner := ownerFQNOr(ownerFQN, c.currentTypeOwner(namespace, fallbackPkg, typeStack))
			typeNode := c.firstTypeNode(n)
			summary.TypeRefs = append(summary.TypeRefs, c.ownedTypeRefsFromNode(typeNode, imports, owner, namespace, fallbackPkg, filePath, content)...)
			names := c.fieldNames(n, content)
			if len(names) == 0 {
				if name := nodeText(n.ChildByFieldName("name"), content); name != "" {
					names = []string{name}
				}
			}
			for _, name := range names {
				summary.Symbols = append(summary.Symbols, Symbol{
					Lang:      LangCs,
					Kind:      SymField,
					File:      filePath,
					Pkg:       pkg,
					Name:      name,
					StartLine: mapper.OriginalLine(int64(n.StartPosition().Row) + 1),
					EndLine:   mapper.OriginalLine(int64(n.EndPosition().Row) + 1),
				})
				if len(memberTypeStack) > 0 {
					if ref, ok := c.symbolRefFromTypeNode(typeNode, imports, namespace, fallbackPkg, content); ok {
						memberTypeStack[len(memberTypeStack)-1][name] = ref
					}
				}
			}
		case "invocation_expression":
			if c.addNameOfRefs(n, content, imports, ownerFQN, namespace, fallbackPkg, filePath, &summary) {
				break
			}
			if call := c.callFromInvocation(n, content, imports, namespace, fallbackPkg, typeStack, env); call != nil {
				call.OwnerFQN = ownerFQN
				call.File = filePath
				summary.Calls = append(summary.Calls, *call)
				c.addTargetTypedArgumentRefs(n, call.CalleeSymbol, ownerFQN, namespace, fallbackPkg, content, filePath, &summary, env)
			}
		case "typeof_expression":
			if typeNode := n.ChildByFieldName("type"); typeNode != nil {
				summary.TypeRefs = append(summary.TypeRefs, c.ownedTypeRefsFromNode(typeNode, imports, ownerFQN, namespace, fallbackPkg, filePath, content)...)
			} else {
				for i := uint(0); i < n.NamedChildCount(); i++ {
					summary.TypeRefs = append(summary.TypeRefs, c.ownedTypeRefsFromNode(n.NamedChild(i), imports, ownerFQN, namespace, fallbackPkg, filePath, content)...)
				}
			}
		case "member_access_expression":
			summary.MemberRefs = append(summary.MemberRefs, c.memberRefsFromAccess(n, content, imports, namespace, fallbackPkg, ownerFQN, filePath)...)
		case "object_creation_expression":
			c.addObjectCreationRefs(n, imports, ownerFQN, namespace, fallbackPkg, content, filePath, &summary, true)
		case "implicit_object_creation_expression":
			c.addObjectCreationRefs(n, imports, ownerFQN, namespace, fallbackPkg, content, filePath, &summary, false)
		case "local_declaration_statement":
			typeNode := c.firstTypeNode(n)
			summary.TypeRefs = append(summary.TypeRefs, c.ownedTypeRefsFromNode(typeNode, imports, ownerFQN, namespace, fallbackPkg, filePath, content)...)
			if ref, ok := c.symbolRefFromTypeNode(typeNode, imports, namespace, fallbackPkg, content); ok {
				for _, name := range c.fieldNames(n, content) {
					env.locals[name] = ref
				}
			} else if initRef, ok := c.inferExpressionType(c.localInitializerNode(n), imports, namespace, fallbackPkg, env, content); ok {
				for _, name := range c.fieldNames(n, content) {
					env.locals[name] = initRef
				}
			}
			c.addTargetTypedInitializerRefs(n, typeNode, imports, ownerFQN, namespace, fallbackPkg, content, filePath, &summary)
		case "assignment_expression":
			c.propagateAssignmentType(n, imports, namespace, fallbackPkg, env, content)
			c.addTargetTypedAssignmentRefs(n, imports, ownerFQN, namespace, fallbackPkg, content, filePath, &summary, env)
		case "return_statement":
			c.addTargetTypedReturnRefs(n, ownerFQN, namespace, fallbackPkg, content, filePath, &summary, env)
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walk(n.NamedChild(i), ownerFQN, env)
		}
	}
	walk(root, "", newCsTypeEnv())
	summary.Imports = dedupeCSharpImports(summary.Imports)
	summary.TypeRefs = dedupeCSharpTypeRefs(summary.TypeRefs)
	summary.MemberRefs = dedupeCSharpMemberRefs(summary.MemberRefs)
	summary.Calls = dedupeCSharpCalls(summary.Calls)
	summary.Symbols = dedupeCSharpSymbols(summary.Symbols)
	return summary
}

func (c *CSharpIndexer) fieldNames(n *sitter.Node, content []byte) []string {
	if n == nil {
		return nil
	}
	var names []string
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}
		if node.Kind() == "variable_declarator" {
			if name := nodeText(node.ChildByFieldName("name"), content); name != "" {
				names = append(names, name)
				return
			}
			for i := uint(0); i < node.NamedChildCount(); i++ {
				child := node.NamedChild(i)
				if child != nil && child.Kind() == "identifier" {
					if name := nodeText(child, content); name != "" {
						names = append(names, name)
						return
					}
				}
			}
		}
		for i := uint(0); i < node.NamedChildCount(); i++ {
			walk(node.NamedChild(i))
		}
	}
	walk(n)
	return names
}

func (c *CSharpIndexer) recoverSummary(parser *sitter.Parser, filePath string, content []byte, root *sitter.Node, parseTimeout time.Duration) (FileSummary, bool) {
	rewritten := content
	mapper := newIdentitySourceMap()
	changedAny := false
	currentRoot := root
	var recoveryTree *sitter.Tree

	for pass := 0; pass < 3; pass++ {
		errorSpans := collectErrorSpans(currentRoot)
		if len(errorSpans) == 0 && !changedAny {
			errorSpans = []byteSpan{{start: 0, end: len(rewritten)}}
		}
		next, nextMapper, changed := rewriteCollectionExpressions(rewritten, errorSpans)
		if !changed {
			if !changedAny {
				return FileSummary{}, false
			}
			break
		}
		rewritten = next
		mapper = nextMapper
		changedAny = true

		if recoveryTree != nil {
			recoveryTree.Close()
			recoveryTree = nil
		}
		var timedOut bool
		var err error
		recoveryTree, timedOut, err = parseTreeWithTimeout(parser, rewritten, parseTimeout)
		if err != nil || timedOut || recoveryTree == nil {
			return FileSummary{}, false
		}
		currentRoot = recoveryTree.RootNode()
		if !currentRoot.HasError() {
			break
		}
	}

	if recoveryTree == nil {
		return FileSummary{}, false
	}
	defer recoveryTree.Close()
	return c.summarizeTree(filePath, rewritten, recoveryTree.RootNode(), mapper), true
}

func (c *CSharpIndexer) namespaceForFile(root *sitter.Node, content []byte) string {
	if root == nil {
		return ""
	}
	var ns string
	var find func(n *sitter.Node)
	find = func(n *sitter.Node) {
		if n == nil || ns != "" {
			return
		}
		switch n.Kind() {
		case "namespace_declaration", "file_scoped_namespace_declaration":
			if name := nodeText(n.ChildByFieldName("name"), content); name != "" {
				ns = name
				return
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			find(n.NamedChild(i))
			if ns != "" {
				return
			}
		}
	}
	find(root)
	return ns
}

func (c *CSharpIndexer) fallbackPkg(filePath string) string {
	dir := filepath.Dir(filePath)
	if dir == "" || dir == "." {
		return ""
	}
	if c.repoRoot == "" {
		return "cs:" + filepath.ToSlash(dir)
	}
	rootPaths, err := paths.NewVaultPaths(c.repoRoot)
	if err != nil {
		return "cs:" + filepath.ToSlash(dir)
	}
	rel, err := rootPaths.RelStrict(dir)
	if err != nil || rel.String() == "" {
		return "cs"
	}
	return "cs:" + rel.String()
}

func (c *CSharpIndexer) currentPkg(namespace, fallback string, typeStack []string) string {
	parts := make([]string, 0, 2+len(typeStack))
	if namespace != "" {
		parts = append(parts, namespace)
	} else if fallback != "" {
		parts = append(parts, fallback)
	}
	parts = append(parts, typeStack...)
	return strings.Trim(strings.Join(parts, "."), ".")
}

func (c *CSharpIndexer) callFromInvocation(n *sitter.Node, content []byte, imports csImportContext, namespace, fallback string, typeStack []string, env csTypeEnv) *CallSite {
	target := n.ChildByFieldName("expression")
	if target == nil && n.NamedChildCount() > 0 {
		target = n.NamedChild(0)
	}
	if target == nil {
		return nil
	}
	switch target.Kind() {
	case "identifier":
		name := nodeText(target, content)
		if name == "" {
			return nil
		}
		if name == "nameof" {
			return nil
		}
		pkg := c.currentPkg(namespace, fallback, typeStack)
		if sig, ok := env.thisMethods[name]; ok && sig.returnType.Name != "" {
			pkg = sig.returnType.Pkg
		}
		return &CallSite{CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: pkg, Name: name}}
	case "qualified_name":
		full := nodeText(target, content)
		if full == "" {
			return nil
		}
		pkg, name := c.splitQualified(full, namespace, fallback)
		if name == "" {
			return nil
		}
		return &CallSite{CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: pkg, Name: name}}
	case "member_access_expression":
		recv := nodeText(target.ChildByFieldName("expression"), content)
		method := nodeText(target.ChildByFieldName("name"), content)
		if method == "" && target.NamedChildCount() > 0 {
			method = nodeText(target.NamedChild(target.NamedChildCount()-1), content)
		}
		if recv == "" && target.NamedChildCount() > 1 {
			recv = nodeText(target.NamedChild(0), content)
		}
		if method == "" {
			return nil
		}
		pkg := c.pkgForReceiver(recv, imports, namespace, fallback, typeStack, env)
		if pkg == "" {
			return nil
		}
		return &CallSite{CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: pkg, Name: method}}
	default:
		name := nodeText(target, content)
		if name == "" {
			return nil
		}
		pkg := ""
		if strings.Contains(name, ".") {
			pkg, name = c.splitQualified(name, namespace, fallback)
			if name == "" {
				return nil
			}
		} else {
			pkg = c.currentPkg(namespace, fallback, typeStack)
		}
		return &CallSite{CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: pkg, Name: name}}
	}
}

func (c *CSharpIndexer) pkgForReceiver(recv string, imports csImportContext, namespace, fallback string, typeStack []string, env csTypeEnv) string {
	recv = strings.TrimSpace(recv)
	if recv == "" {
		return c.currentPkg(namespace, fallback, typeStack)
	}
	if recv == "this" && env.thisType.Name != "" {
		return c.joinPkg(env.thisType.Pkg, env.thisType.Name)
	}
	if strings.HasPrefix(recv, "this.") {
		member := strings.TrimPrefix(recv, "this.")
		if ref, ok := env.thisMembers[member]; ok {
			return c.joinPkg(ref.Pkg, ref.Name)
		}
	}
	if ref, ok := env.locals[recv]; ok {
		return c.joinPkg(ref.Pkg, ref.Name)
	}
	if ref, ok := env.thisMembers[recv]; ok {
		return c.joinPkg(ref.Pkg, ref.Name)
	}
	// If receiver already contains dots, treat it as a full package path.
	if strings.Contains(recv, ".") {
		if ref, ok := c.resolveTypeChain(recv, imports, namespace, fallback); ok {
			return c.joinPkg(ref.Pkg, ref.Name)
		}
		pkg, name := c.splitQualified(recv, namespace, fallback)
		if name == "" {
			return recv
		}
		return c.joinPkg(pkg, name)
	}
	if recv[0] == '_' || (recv[0] >= 'a' && recv[0] <= 'z') {
		return ""
	}
	if ref, ok := imports.resolveSimpleType(recv, namespace, fallback); ok {
		return c.joinPkg(ref.Pkg, ref.Name)
	}
	if namespace != "" {
		return namespace + "." + recv
	}
	if fallback != "" {
		return fallback + "." + recv
	}
	return recv
}

func (c *CSharpIndexer) addNameOfRefs(n *sitter.Node, content []byte, imports csImportContext, ownerFQN, namespace, fallback, filePath string, summary *FileSummary) bool {
	if n == nil || summary == nil {
		return false
	}
	target := n.ChildByFieldName("expression")
	if target == nil && n.NamedChildCount() > 0 {
		target = n.NamedChild(0)
	}
	if target == nil || target.Kind() != "identifier" || nodeText(target, content) != "nameof" {
		return false
	}
	args := n.ChildByFieldName("arguments")
	if args == nil {
		for i := uint(0); i < n.NamedChildCount(); i++ {
			child := n.NamedChild(i)
			if child != nil && child.Kind() == "argument_list" {
				args = child
				break
			}
		}
	}
	if args == nil || args.NamedChildCount() == 0 {
		return true
	}
	arg := args.NamedChild(0)
	if arg == nil {
		return true
	}
	raw := strings.TrimSpace(nodeText(arg, content))
	if raw == "" {
		return true
	}
	if refs := c.memberRefsFromAccess(arg, content, imports, namespace, fallback, ownerFQN, filePath); len(refs) > 0 {
		summary.MemberRefs = append(summary.MemberRefs, refs...)
		if typeRef, ok := c.resolveTypeChain(strings.TrimSpace(nodeText(arg.ChildByFieldName("expression"), content)), imports, namespace, fallback); ok {
			summary.TypeRefs = append(summary.TypeRefs, TypeRef{
				File:     filePath,
				OwnerFQN: ownerFQN,
				TypeSym:  typeRef,
			})
		}
		return true
	}
	if ref, ok := c.resolveTypeChain(raw, imports, namespace, fallback); ok {
		summary.TypeRefs = append(summary.TypeRefs, TypeRef{
			File:     filePath,
			OwnerFQN: ownerFQN,
			TypeSym:  ref,
		})
	}
	return true
}

func (c *CSharpIndexer) collectAttributes(n *sitter.Node, imports csImportContext, namespace, fallback, ownerFQN string, content []byte, generated bool, budget int) []AnnotationUse {
	if n == nil {
		return nil
	}
	if generated && budget <= 0 {
		return nil
	}
	var anns []AnnotationUse
	var walk func(cur *sitter.Node)
	walk = func(cur *sitter.Node) {
		if cur == nil {
			return
		}
		if cur.Kind() == "attribute" {
			if generated && budget <= 0 {
				return
			}
			name := nodeText(cur.ChildByFieldName("name"), content)
			if name == "" {
				name = nodeText(cur, content)
			}
			if name == "" {
				return
			}
			name = strings.TrimSuffix(name, "Attribute")
			ref, ok := c.resolveTypeRef(name, imports, namespace, fallback)
			if !ok {
				return
			}
			anns = append(anns, AnnotationUse{
				OwnerFQN:  ownerFQN,
				AnnSymbol: ref,
				Args:      nil,
			})
			budget--
		}
		for i := uint(0); i < cur.NamedChildCount(); i++ {
			walk(cur.NamedChild(i))
		}
	}
	walk(n)
	return anns
}

func (c *CSharpIndexer) baseEdges(n *sitter.Node, childFQN, namespace, fallback string, content []byte) []SuperEdge {
	if n == nil {
		return nil
	}
	baseList := n.ChildByFieldName("base_list")
	if baseList == nil {
		for i := uint(0); i < n.NamedChildCount(); i++ {
			child := n.NamedChild(i)
			if child != nil && child.Kind() == "base_list" {
				baseList = child
				break
			}
		}
	}
	if baseList == nil {
		return nil
	}
	raw := nodeText(baseList, content)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	var edges []SuperEdge
	for _, part := range parts {
		part = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), ":"))
		if part == "" {
			continue
		}
		pkg, name := c.splitQualified(part, namespace, fallback)
		if name == "" {
			continue
		}
		parent := Symbol{Pkg: pkg, Name: name}.NormalizeFQN()
		edges = append(edges, SuperEdge{
			ChildFQN:  childFQN,
			ParentFQN: parent,
		})
	}
	return edges
}

func (c *CSharpIndexer) splitQualified(raw, namespace, fallback string) (pkg, name string) {
	// Drop generic arguments.
	if lt := strings.Index(raw, "<"); lt > 0 {
		raw = raw[:lt]
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	if strings.Contains(raw, ".") {
		pkg, name = splitFQN(raw)
		if pkg == "" {
			pkg = namespace
		}
		return pkg, name
	}
	if namespace != "" {
		return namespace, raw
	}
	if fallback != "" {
		return fallback, raw
	}
	return "", raw
}

func (c *CSharpIndexer) joinPkg(pkg, name string) string {
	switch {
	case pkg == "":
		return name
	case name == "":
		return pkg
	default:
		return pkg + "." + name
	}
}

type csTypeEnv struct {
	locals      map[string]SymbolRef
	thisMembers map[string]SymbolRef
	thisMethods map[string]csMethodSignature
	thisType    SymbolRef
	returnType  SymbolRef
}

func newCsTypeEnv() csTypeEnv {
	return csTypeEnv{
		locals:      make(map[string]SymbolRef),
		thisMembers: make(map[string]SymbolRef),
		thisMethods: make(map[string]csMethodSignature),
	}
}

func (e csTypeEnv) clone() csTypeEnv {
	out := csTypeEnv{
		locals:      make(map[string]SymbolRef, len(e.locals)),
		thisMembers: make(map[string]SymbolRef, len(e.thisMembers)),
		thisMethods: make(map[string]csMethodSignature, len(e.thisMethods)),
		thisType:    e.thisType,
		returnType:  e.returnType,
	}
	for k, v := range e.locals {
		out.locals[k] = v
	}
	for k, v := range e.thisMembers {
		out.thisMembers[k] = v
	}
	for k, v := range e.thisMethods {
		out.thisMethods[k] = v
	}
	return out
}

func ownerFQNOr(owner, fallback string) string {
	if strings.TrimSpace(owner) != "" {
		return owner
	}
	return strings.TrimSpace(fallback)
}

func (c *CSharpIndexer) currentTypeOwner(namespace, fallback string, typeStack []string) string {
	if len(typeStack) == 0 {
		return ""
	}
	name := typeStack[len(typeStack)-1]
	return (Symbol{Pkg: c.currentPkg(namespace, fallback, typeStack[:len(typeStack)-1]), Name: name}).NormalizeFQN()
}

func (c *CSharpIndexer) collectTypeMemberTypes(n *sitter.Node, imports csImportContext, namespace, fallback string, content []byte) map[string]SymbolRef {
	memberTypes := make(map[string]SymbolRef)
	if n == nil {
		return memberTypes
	}
	record := func(name string, typeNode *sitter.Node) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		ref, ok := c.symbolRefFromTypeNode(typeNode, imports, namespace, fallback, content)
		if !ok {
			return
		}
		memberTypes[name] = ref
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		child := n.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "property_declaration":
			record(nodeText(child.ChildByFieldName("name"), content), child.ChildByFieldName("type"))
		case "field_declaration", "event_declaration", "event_field_declaration":
			typeNode := c.firstTypeNode(child)
			for _, name := range c.fieldNames(child, content) {
				record(name, typeNode)
			}
			if name := nodeText(child.ChildByFieldName("name"), content); name != "" {
				record(name, typeNode)
			}
		}
	}
	return memberTypes
}

func (c *CSharpIndexer) collectTypeMethodSignatures(n *sitter.Node, imports csImportContext, namespace, fallback string, content []byte) map[string]csMethodSignature {
	out := make(map[string]csMethodSignature)
	if n == nil {
		return out
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		child := n.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "method_declaration":
			name := nodeText(child.ChildByFieldName("name"), content)
			if name == "" {
				continue
			}
			sig := csMethodSignature{}
			if ref, ok := c.symbolRefFromTypeNode(child.ChildByFieldName("type"), imports, namespace, fallback, content); ok {
				sig.returnType = ref
			}
			sig.params = c.parameterTypeRefs(child, imports, namespace, fallback, content)
			out[name] = sig
		}
	}
	return out
}

func (c *CSharpIndexer) methodSignatureTypeRefs(n *sitter.Node, imports csImportContext, ownerFQN, namespace, fallback string, content []byte, filePath string) []TypeRef {
	var out []TypeRef
	out = append(out, c.ownedTypeRefsFromNode(n.ChildByFieldName("type"), imports, ownerFQN, namespace, fallback, filePath, content)...)
	if n.Kind() == "conversion_operator_declaration" {
		out = append(out, c.ownedTypeRefsFromNode(n.ChildByFieldName("return_type"), imports, ownerFQN, namespace, fallback, filePath, content)...)
	}
	return out
}

func (c *CSharpIndexer) addParameterTypes(n *sitter.Node, imports csImportContext, namespace, fallback string, content []byte, summary *FileSummary, ownerFQN, filePath string, env *csTypeEnv) {
	if n == nil || env == nil {
		return
	}
	var walk func(*sitter.Node)
	walk = func(cur *sitter.Node) {
		if cur == nil {
			return
		}
		if cur.Kind() == "parameter" {
			typeNode := cur.ChildByFieldName("type")
			summary.TypeRefs = append(summary.TypeRefs, c.ownedTypeRefsFromNode(typeNode, imports, ownerFQN, namespace, fallback, filePath, content)...)
			if ref, ok := c.symbolRefFromTypeNode(typeNode, imports, namespace, fallback, content); ok {
				if name := nodeText(cur.ChildByFieldName("name"), content); name != "" {
					env.locals[name] = ref
				}
			}
			return
		}
		for i := uint(0); i < cur.NamedChildCount(); i++ {
			walk(cur.NamedChild(i))
		}
	}
	if params := n.ChildByFieldName("parameters"); params != nil {
		walk(params)
		return
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		child := n.NamedChild(i)
		if child != nil && (child.Kind() == "parameter_list" || child.Kind() == "bracketed_parameter_list") {
			walk(child)
		}
	}
}

func (c *CSharpIndexer) addTargetTypedInitializerRefs(n, typeNode *sitter.Node, imports csImportContext, ownerFQN, namespace, fallback string, content []byte, filePath string, summary *FileSummary) {
	if n == nil || typeNode == nil || summary == nil {
		return
	}
	ref, ok := c.symbolRefFromTypeNode(typeNode, imports, namespace, fallback, content)
	if !ok {
		return
	}
	var walk func(*sitter.Node)
	walk = func(cur *sitter.Node) {
		if cur == nil {
			return
		}
		switch cur.Kind() {
		case "implicit_object_creation_expression":
			summary.TypeRefs = append(summary.TypeRefs, TypeRef{
				File:     filePath,
				OwnerFQN: ownerFQN,
				TypeSym:  ref,
			})
			summary.Calls = append(summary.Calls, CallSite{
				File:         filePath,
				OwnerFQN:     ownerFQN,
				CalleeSymbol: ref,
			})
			return
		case "object_creation_expression":
			return
		}
		for i := uint(0); i < cur.NamedChildCount(); i++ {
			walk(cur.NamedChild(i))
		}
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		walk(n.NamedChild(i))
	}
}

func (c *CSharpIndexer) addObjectCreationRefs(n *sitter.Node, imports csImportContext, ownerFQN, namespace, fallback string, content []byte, filePath string, summary *FileSummary, explicit bool) {
	if n == nil || summary == nil {
		return
	}
	typeNode := n.ChildByFieldName("type")
	if !explicit && typeNode == nil {
		return
	}
	ref, ok := c.symbolRefFromTypeNode(typeNode, imports, namespace, fallback, content)
	if !ok {
		return
	}
	summary.TypeRefs = append(summary.TypeRefs, TypeRef{
		File:     filePath,
		OwnerFQN: ownerFQN,
		TypeSym:  ref,
	})
	summary.Calls = append(summary.Calls, CallSite{
		File:         filePath,
		OwnerFQN:     ownerFQN,
		CalleeSymbol: ref,
	})
}

func (c *CSharpIndexer) attributeTypeRefs(n *sitter.Node, imports csImportContext, ownerFQN, namespace, fallback string, content []byte, filePath string, generated bool, budget int) []TypeRef {
	anns := c.collectAttributes(n, imports, namespace, fallback, ownerFQN, content, generated, budget)
	if len(anns) == 0 {
		return nil
	}
	refs := make([]TypeRef, 0, len(anns))
	for _, ann := range anns {
		ref := ann.AnnSymbol
		if !strings.HasSuffix(ref.Name, "Attribute") {
			ref.Name += "Attribute"
		}
		refs = append(refs, TypeRef{File: filePath, OwnerFQN: ownerFQN, TypeSym: ref})
	}
	return refs
}

func (c *CSharpIndexer) baseTypeRefs(n *sitter.Node, imports csImportContext, ownerFQN, namespace, fallback string, content []byte, filePath string) []TypeRef {
	baseList := n.ChildByFieldName("base_list")
	if baseList == nil {
		for i := uint(0); i < n.NamedChildCount(); i++ {
			child := n.NamedChild(i)
			if child != nil && child.Kind() == "base_list" {
				baseList = child
				break
			}
		}
	}
	if baseList == nil {
		return nil
	}
	var refs []TypeRef
	for _, ref := range c.typeRefsFromNode(baseList, imports, namespace, fallback, content) {
		refs = append(refs, TypeRef{File: filePath, OwnerFQN: ownerFQN, TypeSym: ref})
	}
	return refs
}

func (c *CSharpIndexer) ownedTypeRefsFromNode(typeNode *sitter.Node, imports csImportContext, ownerFQN, namespace, fallback, filePath string, content []byte) []TypeRef {
	refs := c.typeRefsFromNode(typeNode, imports, namespace, fallback, content)
	if len(refs) == 0 {
		return nil
	}
	out := make([]TypeRef, 0, len(refs))
	for _, ref := range refs {
		out = append(out, TypeRef{
			File:     filePath,
			OwnerFQN: ownerFQN,
			TypeSym:  ref,
		})
	}
	return out
}

func (c *CSharpIndexer) symbolRefFromTypeNode(typeNode *sitter.Node, imports csImportContext, namespace, fallback string, content []byte) (SymbolRef, bool) {
	refs := c.typeRefsFromNode(typeNode, imports, namespace, fallback, content)
	if len(refs) == 0 {
		return SymbolRef{}, false
	}
	return refs[0], true
}

func (c *CSharpIndexer) typeRefsFromNode(typeNode *sitter.Node, imports csImportContext, namespace, fallback string, content []byte) []SymbolRef {
	if typeNode == nil {
		return nil
	}
	var refs []SymbolRef
	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		switch n.Kind() {
		case "qualified_name", "alias_qualified_name", "identifier", "predefined_type":
			if ref, ok := c.typeRefFromText(nodeText(n, content), imports, namespace, fallback); ok {
				refs = append(refs, ref)
			}
			return
		case "generic_name":
			base := nodeText(n.ChildByFieldName("name"), content)
			if base == "" && n.NamedChildCount() > 0 {
				base = nodeText(n.NamedChild(0), content)
			}
			if ref, ok := c.typeRefFromText(base, imports, namespace, fallback); ok {
				refs = append(refs, ref)
			}
			raw := strings.TrimSpace(nodeText(n, content))
			if lt := strings.Index(raw, "<"); lt >= 0 && strings.HasSuffix(raw, ">") {
				for _, part := range splitCSharpTypeArgs(raw[lt+1 : len(raw)-1]) {
					if ref, ok := c.typeRefFromText(part, imports, namespace, fallback); ok {
						refs = append(refs, ref)
					}
				}
			}
			for i := uint(0); i < n.NamedChildCount(); i++ {
				child := n.NamedChild(i)
				if child != nil && child.Kind() != "identifier" {
					walk(child)
				}
			}
			return
		case "array_type", "nullable_type":
			if child := n.ChildByFieldName("element"); child != nil {
				walk(child)
				return
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(typeNode)
	return dedupeTypeSymbolRefs(refs)
}

func (c *CSharpIndexer) typeRefFromText(raw string, imports csImportContext, namespace, fallback string) (SymbolRef, bool) {
	raw = c.normalizeTypeText(raw)
	if raw == "" || isCSharpBuiltinType(raw) {
		return SymbolRef{}, false
	}
	return c.resolveTypeRef(raw, imports, namespace, fallback)
}

func (c *CSharpIndexer) normalizeTypeText(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "global::")
	raw = strings.TrimPrefix(raw, "ref ")
	raw = strings.TrimPrefix(raw, "out ")
	raw = strings.TrimPrefix(raw, "in ")
	raw = strings.TrimPrefix(raw, "params ")
	raw = strings.TrimPrefix(raw, "scoped ")
	raw = strings.TrimPrefix(raw, "readonly ")
	raw = strings.TrimPrefix(raw, "static ")
	raw = strings.TrimSuffix(raw, "?")
	raw = strings.TrimSpace(raw)
	if idx := strings.Index(raw, "<"); idx >= 0 {
		raw = raw[:idx]
	}
	raw = strings.TrimSuffix(raw, "[]")
	raw = strings.TrimSpace(raw)
	return raw
}

func isCSharpBuiltinType(raw string) bool {
	switch strings.TrimSpace(raw) {
	case "", "void", "string", "int", "long", "short", "byte", "bool", "char", "float", "double", "decimal", "object", "dynamic", "nint", "nuint", "var":
		return true
	default:
		return false
	}
}

func splitCSharpTypeArgs(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var (
		out   []string
		start int
		depth int
	)
	for i, r := range raw {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				part := strings.TrimSpace(raw[start:i])
				if part != "" {
					out = append(out, part)
				}
				start = i + 1
			}
		}
	}
	last := strings.TrimSpace(raw[start:])
	if last != "" {
		out = append(out, last)
	}
	return out
}

func (c *CSharpIndexer) firstTypeNode(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	if typeNode := n.ChildByFieldName("type"); typeNode != nil {
		return typeNode
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		child := n.NamedChild(i)
		if child == nil {
			continue
		}
		if child.Kind() == "variable_declaration" {
			if typeNode := child.ChildByFieldName("type"); typeNode != nil {
				return typeNode
			}
			for j := uint(0); j < child.NamedChildCount(); j++ {
				grandchild := child.NamedChild(j)
				if grandchild == nil || grandchild.Kind() == "variable_declarator" {
					continue
				}
				switch grandchild.Kind() {
				case "qualified_name", "alias_qualified_name", "generic_name", "array_type", "nullable_type", "identifier", "predefined_type":
					return grandchild
				}
			}
		}
	}
	return nil
}

func (c *CSharpIndexer) operatorName(n *sitter.Node, content []byte) string {
	if n == nil {
		return ""
	}
	if n.Kind() == "conversion_operator_declaration" {
		modifier := "op_Explicit"
		raw := strings.TrimSpace(nodeText(n, content))
		if strings.HasPrefix(raw, "implicit") || strings.Contains(raw, " implicit operator ") {
			modifier = "op_Implicit"
		}
		if ref, ok := c.symbolRefFromTypeNode(n.ChildByFieldName("type"), csImportContext{}, "", "", content); ok {
			return modifier + "_" + ref.Name
		}
		return modifier
	}
	raw := strings.TrimSpace(nodeText(n, content))
	for _, tok := range []string{"+", "-", "*", "/", "==", "!=", "true", "false", ">", "<", ">=", "<=", "%", "&", "|", "^", "<<", ">>"} {
		if strings.Contains(raw, " operator "+tok) {
			return "operator " + tok
		}
	}
	return "operator"
}

func dedupeCSharpImports(in []ImportEdge) []ImportEdge {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]ImportEdge, 0, len(in))
	for _, imp := range in {
		key := strings.TrimSpace(imp.Module)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ImportEdge{Module: key})
	}
	return out
}

func dedupeTypeSymbolRefs(in []SymbolRef) []SymbolRef {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]SymbolRef, 0, len(in))
	for _, ref := range in {
		key := strings.Join([]string{string(ref.Lang), ref.Pkg, ref.Name}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func dedupeCSharpTypeRefs(in []TypeRef) []TypeRef {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]TypeRef, 0, len(in))
	for _, ref := range in {
		key := strings.Join([]string{ref.File, ref.OwnerFQN, string(ref.TypeSym.Lang), ref.TypeSym.Pkg, ref.TypeSym.Name}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func dedupeCSharpMemberRefs(in []MemberRef) []MemberRef {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]MemberRef, 0, len(in))
	for _, ref := range in {
		key := strings.Join([]string{ref.File, ref.OwnerFQN, string(ref.Sym.Lang), ref.Sym.Pkg, ref.Sym.Name}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func dedupeCSharpCalls(in []CallSite) []CallSite {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]CallSite, 0, len(in))
	for _, call := range in {
		key := strings.Join([]string{call.File, call.OwnerFQN, string(call.CalleeSymbol.Lang), call.CalleeSymbol.Pkg, call.CalleeSymbol.Name}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, call)
	}
	return out
}

func dedupeCSharpSymbols(in []Symbol) []Symbol {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]Symbol, 0, len(in))
	for _, sym := range in {
		key := strings.Join([]string{string(sym.Lang), string(sym.Kind), sym.File, sym.Pkg, sym.Name}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, sym)
	}
	return out
}

func (c *CSharpIndexer) buildImportContext(root *sitter.Node, content []byte, fallback string) csImportContext {
	ctx := csImportContext{
		aliases:       make(map[string]SymbolRef),
		importedTypes: make(map[string]SymbolRef),
	}
	if root == nil {
		return ctx
	}
	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		if n.Kind() == "using_directive" {
			c.applyUsingDirective(&ctx, nodeText(n, content), true)
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(root)
	return ctx
}

func (ctx csImportContext) resolveSimpleType(raw, namespace, fallback string) (SymbolRef, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return SymbolRef{}, false
	}
	if ref, ok := ctx.aliases[raw]; ok {
		return ref, true
	}
	if ref, ok := ctx.importedTypes[raw]; ok {
		return ref, true
	}
	for _, ns := range preferredCSharpNamespaces(ctx.namespaces) {
		if ns == "" || strings.HasPrefix(ns, "System") || strings.HasPrefix(ns, "Microsoft") {
			continue
		}
		return SymbolRef{Lang: LangCs, Pkg: ns, Name: raw}, true
	}
	if namespace != "" {
		return SymbolRef{Lang: LangCs, Pkg: namespace, Name: raw}, true
	}
	for _, ns := range preferredCSharpNamespaces(ctx.namespaces) {
		if ns == "" || (!strings.HasPrefix(ns, "System") && !strings.HasPrefix(ns, "Microsoft")) {
			continue
		}
		return SymbolRef{Lang: LangCs, Pkg: ns, Name: raw}, true
	}
	if fallback != "" {
		return SymbolRef{Lang: LangCs, Pkg: fallback, Name: raw}, true
	}
	return SymbolRef{}, false
}

func (c *CSharpIndexer) resolveTypeRef(raw string, imports csImportContext, namespace, fallback string) (SymbolRef, bool) {
	return c.resolveTypeChain(raw, imports, namespace, fallback)
}

func (c *CSharpIndexer) parameterTypeRefs(n *sitter.Node, imports csImportContext, namespace, fallback string, content []byte) []SymbolRef {
	var out []SymbolRef
	if n == nil {
		return out
	}
	var walk func(*sitter.Node)
	walk = func(cur *sitter.Node) {
		if cur == nil {
			return
		}
		if cur.Kind() == "parameter" {
			if ref, ok := c.symbolRefFromTypeNode(cur.ChildByFieldName("type"), imports, namespace, fallback, content); ok {
				out = append(out, ref)
			}
			return
		}
		for i := uint(0); i < cur.NamedChildCount(); i++ {
			walk(cur.NamedChild(i))
		}
	}
	if params := n.ChildByFieldName("parameters"); params != nil {
		walk(params)
		return out
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		child := n.NamedChild(i)
		if child != nil && (child.Kind() == "parameter_list" || child.Kind() == "bracketed_parameter_list") {
			walk(child)
		}
	}
	return out
}

func (c *CSharpIndexer) localInitializerNode(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	var found *sitter.Node
	var walk func(*sitter.Node)
	walk = func(cur *sitter.Node) {
		if cur == nil || found != nil {
			return
		}
		if cur.Kind() == "equals_value_clause" {
			for i := uint(0); i < cur.NamedChildCount(); i++ {
				child := cur.NamedChild(i)
				if child != nil {
					found = child
					return
				}
			}
		}
		for i := uint(0); i < cur.NamedChildCount(); i++ {
			walk(cur.NamedChild(i))
		}
	}
	walk(n)
	return found
}

func (c *CSharpIndexer) inferExpressionType(n *sitter.Node, imports csImportContext, namespace, fallback string, env csTypeEnv, content []byte) (SymbolRef, bool) {
	if n == nil {
		return SymbolRef{}, false
	}
	switch n.Kind() {
	case "identifier":
		name := nodeText(n, content)
		if ref, ok := env.locals[name]; ok {
			return ref, true
		}
		if ref, ok := env.thisMembers[name]; ok {
			return ref, true
		}
		if ref, ok := imports.resolveSimpleType(name, namespace, fallback); ok {
			return ref, true
		}
	case "member_access_expression":
		raw := nodeText(n, content)
		if strings.HasPrefix(raw, "this.") {
			member := strings.TrimPrefix(raw, "this.")
			if ref, ok := env.thisMembers[member]; ok {
				return ref, true
			}
		}
		if ref, ok := env.thisMembers[raw]; ok {
			return ref, true
		}
	case "object_creation_expression":
		return c.symbolRefFromTypeNode(n.ChildByFieldName("type"), imports, namespace, fallback, content)
	case "implicit_object_creation_expression":
		return env.returnType, env.returnType.Name != ""
	}
	return SymbolRef{}, false
}

func (c *CSharpIndexer) propagateAssignmentType(n *sitter.Node, imports csImportContext, namespace, fallback string, env csTypeEnv, content []byte) {
	if n == nil {
		return
	}
	left := n.ChildByFieldName("left")
	right := n.ChildByFieldName("right")
	if left == nil || right == nil {
		if n.NamedChildCount() >= 2 {
			left = n.NamedChild(0)
			right = n.NamedChild(1)
		}
	}
	if left == nil || right == nil {
		return
	}
	ref, ok := c.inferExpressionType(right, imports, namespace, fallback, env, content)
	if !ok {
		return
	}
	name := strings.TrimSpace(nodeText(left, content))
	if name == "" {
		return
	}
	if strings.HasPrefix(name, "this.") {
		env.thisMembers[strings.TrimPrefix(name, "this.")] = ref
		return
	}
	if strings.HasPrefix(name, "_") || (name[0] >= 'a' && name[0] <= 'z') {
		env.locals[name] = ref
	}
}

func (c *CSharpIndexer) addTargetTypedAssignmentRefs(n *sitter.Node, imports csImportContext, ownerFQN, namespace, fallback string, content []byte, filePath string, summary *FileSummary, env csTypeEnv) {
	if n == nil {
		return
	}
	left := n.ChildByFieldName("left")
	right := n.ChildByFieldName("right")
	if left == nil || right == nil {
		if n.NamedChildCount() >= 2 {
			left = n.NamedChild(0)
			right = n.NamedChild(1)
		}
	}
	if right == nil {
		return
	}
	ref, ok := c.inferExpressionType(left, imports, namespace, fallback, env, content)
	if !ok {
		return
	}
	c.addTargetTypedNewRefsForExpression(right, ref, ownerFQN, filePath, summary)
}

func (c *CSharpIndexer) addTargetTypedReturnRefs(n *sitter.Node, ownerFQN, namespace, fallback string, content []byte, filePath string, summary *FileSummary, env csTypeEnv) {
	if n == nil || env.returnType.Name == "" {
		return
	}
	if expr := n.ChildByFieldName("expression"); expr != nil {
		c.addTargetTypedNewRefsForExpression(expr, env.returnType, ownerFQN, filePath, summary)
		return
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		c.addTargetTypedNewRefsForExpression(n.NamedChild(i), env.returnType, ownerFQN, filePath, summary)
	}
}

func (c *CSharpIndexer) addTargetTypedArgumentRefs(n *sitter.Node, callee SymbolRef, ownerFQN, namespace, fallback string, content []byte, filePath string, summary *FileSummary, env csTypeEnv) {
	if n == nil {
		return
	}
	sig, ok := env.thisMethods[callee.Name]
	if !ok || len(sig.params) == 0 {
		return
	}
	var argsNode *sitter.Node
	for i := uint(0); i < n.NamedChildCount(); i++ {
		child := n.NamedChild(i)
		if child != nil && child.Kind() == "argument_list" {
			argsNode = child
			break
		}
	}
	if argsNode == nil {
		return
	}
	argIndex := 0
	for i := uint(0); i < argsNode.NamedChildCount() && argIndex < len(sig.params); i++ {
		arg := argsNode.NamedChild(i)
		if arg == nil {
			continue
		}
		c.addTargetTypedNewRefsForExpression(arg, sig.params[argIndex], ownerFQN, filePath, summary)
		argIndex++
	}
}

func (c *CSharpIndexer) addTargetTypedNewRefsForExpression(n *sitter.Node, ref SymbolRef, ownerFQN, filePath string, summary *FileSummary) {
	if n == nil || ref.Name == "" {
		return
	}
	var walk func(*sitter.Node)
	walk = func(cur *sitter.Node) {
		if cur == nil {
			return
		}
		if cur.Kind() == "implicit_object_creation_expression" {
			summary.TypeRefs = append(summary.TypeRefs, TypeRef{File: filePath, OwnerFQN: ownerFQN, TypeSym: ref})
			summary.Calls = append(summary.Calls, CallSite{File: filePath, OwnerFQN: ownerFQN, CalleeSymbol: ref})
			return
		}
		for i := uint(0); i < cur.NamedChildCount(); i++ {
			walk(cur.NamedChild(i))
		}
	}
	walk(n)
}

func (c *CSharpIndexer) isGeneratedFile(filePath string, content []byte) bool {
	base := filepath.Base(filePath)
	switch {
	case strings.HasSuffix(base, ".Designer.cs"),
		strings.HasSuffix(base, ".generated.cs"),
		strings.HasSuffix(base, "AssemblyInfo.cs"),
		strings.Contains(base, "ModelSnapshot"):
		return true
	}
	head := strings.ToLower(string(content))
	if len(head) > 2048 {
		head = head[:2048]
	}
	return strings.Contains(head, "<auto-generated") ||
		strings.Contains(head, "// <auto-generated") ||
		strings.Contains(head, "[generatedcode")
}
