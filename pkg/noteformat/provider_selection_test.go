package noteformat_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestProviderSelectionControlFlow(t *testing.T) {
	t.Parallel()
	selectors := map[string]struct{}{"markdown": {}, ".md": {}}
	tests := []struct {
		name       string
		path       string
		source     string
		violations int
	}{
		{name: "FormatID literal", source: `package p; func f(formatID string) { if formatID == "markdown" {} }`, violations: 1},
		{name: "FormatID constant", source: `package p; const markdown = "markdown"; func f(formatID string) { if formatID == markdown {} }`, violations: 1},
		{name: "aliased FormatID conversion", source: `package p; import formats "github.com/atomicobject/rhizome/pkg/noteformat"; func f(provider string) { if provider == formats.FormatID("markdown") {} }`, violations: 1},
		{name: "nested string and parenthesized FormatID", source: `package p; import formats "github.com/atomicobject/rhizome/pkg/noteformat"; const md = "markdown"; func f(provider string) { if provider == string((formats.FormatID((md)))) {} }`, violations: 1},
		{name: "typed switch case", source: `package p; import formats "github.com/atomicobject/rhizome/pkg/noteformat"; func f(provider string) { switch provider { case formats.FormatID("markdown"): } }`, violations: 1},
		{name: "unrelated FormatID call", source: `package p; import formats "example.com/other"; func f(formatID string) { if formatID == formats.FormatID("markdown") {} }`},
		{name: "arbitrary function stays unresolved", source: `package p; func value(string) string { return "other" }; func f(formatID string) { if formatID == value("markdown") {} }`},
		{name: "nonconstant string conversion stays unresolved", source: `package p; func value() string { return "markdown" }; func f(formatID string) { if formatID == string(value()) {} }`},
		{name: "nonconstant FormatID conversion stays unresolved", source: `package p; import formats "github.com/atomicobject/rhizome/pkg/noteformat"; func value() string { return "markdown" }; func f(formatID string) { if formatID == formats.FormatID(value()) {} }`},
		{name: "ordinary string comparison", source: `package p; func f(message string) { if message == string("markdown") {} }`},
		{name: "shadowed builtin conversion", source: `package p; func string(value string) string { return "other" }; func f(formatID string) { if formatID == string("markdown") {} }`},
		{name: "shadowed provider alias", source: `package p; import formats "github.com/atomicobject/rhizome/pkg/noteformat"; func f(formatID string) { formats := struct{ FormatID func(string) string }{}; if formatID == formats.FormatID("markdown") {} }`},
		{name: "extension literal", source: `package p; import "strings"; func f(path string) { if strings.HasSuffix(path, ".md") {} }`, violations: 1},
		{name: "extension constant", source: `package p; import "strings"; const markdownExtension = ".md"; func f(path string) { if strings.HasSuffix(path, markdownExtension) {} }`, violations: 1},
		{name: "ordinary returned extension", source: `package p; func f() string { return ".md" }`},
		{name: "messages docs and SQL text", source: "package p; const message = `select '.md' from docs`; func f() string { return message }"},
		{name: "approved boundary", path: "pkg/app/codeintel/ingest.go", source: `package p; func validateMarkdownCodeAnchorSyntax(formatID string) { if formatID == "markdown" {} }`},
		{name: "other agent surface function stays guarded", path: "pkg/app/cli/init/agent_surfaces.go", source: `package p; import "strings"; func selectNote(path string) { if strings.HasSuffix(path, ".md") {} }`, violations: 1},
		{name: "same function name elsewhere stays guarded", source: `package p; import "strings"; func pruneStaleSkillReferences(path string) { if strings.HasSuffix(path, ".md") {} }`, violations: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), test.name+".go", test.source, 0)
			if err != nil {
				t.Fatal(err)
			}
			path := test.path
			if path == "" {
				path = "pkg/consumer/consumer.go"
			}
			violations := providerSelectionControlFlow(file, path, selectors)
			if len(violations) != test.violations {
				t.Fatalf("got %d violations (%v), want %d", len(violations), violations, test.violations)
			}
		})
	}
}

func expressionSelectsProvider(expression ast.Expr, selectors map[string]struct{}, constants map[string]string, aliases map[string]struct{}) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok && callSelectsProvider(call, selectors, constants, aliases) {
			found = true
			return false
		}
		binary, ok := node.(*ast.BinaryExpr)
		if !ok || (binary.Op != token.EQL && binary.Op != token.NEQ) {
			return true
		}
		if selectionComparison(binary.X, binary.Y, selectors, constants, aliases) || selectionComparison(binary.Y, binary.X, selectors, constants, aliases) {
			found = true
		}
		return !found
	})
	return found
}

func callSelectsProvider(call *ast.CallExpr, selectors map[string]struct{}, constants map[string]string, aliases map[string]struct{}) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch selector.Sel.Name {
	case "HasSuffix":
		for _, argument := range call.Args {
			if value, ok := providerSelectorValue(argument, selectors, constants, aliases); ok && strings.HasPrefix(value, ".") {
				return true
			}
		}
	case "EqualFold":
		for i, argument := range call.Args {
			for j, other := range call.Args {
				if i != j && selectionComparison(argument, other, selectors, constants, aliases) {
					return true
				}
			}
		}
	}
	return false
}

func expressionsSelectProvider(expressions []ast.Expr, tag ast.Expr, selectors map[string]struct{}, constants map[string]string, aliases map[string]struct{}) bool {
	for _, expression := range expressions {
		if selectionComparison(expression, tag, selectors, constants, aliases) {
			return true
		}
	}
	return false
}

func selectionComparison(selector, discriminator ast.Expr, selectors map[string]struct{}, constants map[string]string, aliases map[string]struct{}) bool {
	value, ok := providerSelectorValue(selector, selectors, constants, aliases)
	return ok && (strings.HasPrefix(value, ".") || expressionMentionsFormat(discriminator) || providerFormatConversion(selector, aliases))
}

func expressionMentionsFormat(expression ast.Expr) bool {
	mentioned := false
	ast.Inspect(expression, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if ok && strings.Contains(strings.ToLower(identifier.Name), "format") {
			mentioned = true
		}
		return !mentioned
	})
	return mentioned
}

func providerSelectorValue(expression ast.Expr, selectors map[string]struct{}, constants map[string]string, aliases map[string]struct{}) (string, bool) {
	var value string
	switch expression := expression.(type) {
	case *ast.BasicLit:
		if expression.Kind != token.STRING {
			return "", false
		}
		value = strings.Trim(expression.Value, "\"`")
	case *ast.Ident:
		value = constants[expression.Name]
	case *ast.ParenExpr:
		return providerSelectorValue(expression.X, selectors, constants, aliases)
	case *ast.CallExpr:
		argument, _, ok := providerConversionArgument(expression, aliases)
		if !ok {
			return "", false
		}
		return providerSelectorValue(argument, selectors, constants, aliases)
	default:
		return "", false
	}
	value = strings.ToLower(value)
	_, ok := selectors[value]
	return value, ok
}

func providerFormatConversion(expression ast.Expr, aliases map[string]struct{}) bool {
	for {
		if parens, ok := expression.(*ast.ParenExpr); ok {
			expression = parens.X
			continue
		}
		argument, typed, ok := providerConversionArgument(expression, aliases)
		if !ok || typed {
			return typed
		}
		expression = argument
	}
}

func providerConversionArgument(expression ast.Expr, aliases map[string]struct{}) (ast.Expr, bool, bool) {
	call, ok := expression.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || call.Ellipsis.IsValid() {
		return nil, false, false
	}
	callee := call.Fun
	for {
		parens, ok := callee.(*ast.ParenExpr)
		if !ok {
			break
		}
		callee = parens.X
	}
	if name, ok := callee.(*ast.Ident); ok && name.Name == "string" && name.Obj == nil {
		return call.Args[0], false, true
	}
	if name, ok := callee.(*ast.SelectorExpr); ok && name.Sel.Name == "FormatID" {
		if alias, ok := name.X.(*ast.Ident); ok && (alias.Obj == nil || alias.Obj.Kind == ast.Pkg) {
			if _, known := aliases[alias.Name]; known {
				return call.Args[0], true, true
			}
		}
	}
	return nil, false, false
}
