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

func expressionSelectsProvider(expression ast.Expr, selectors map[string]struct{}, constants map[string]string) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok && callSelectsProvider(call, selectors, constants) {
			found = true
			return false
		}
		binary, ok := node.(*ast.BinaryExpr)
		if !ok || (binary.Op != token.EQL && binary.Op != token.NEQ) {
			return true
		}
		if selectionComparison(binary.X, binary.Y, selectors, constants) || selectionComparison(binary.Y, binary.X, selectors, constants) {
			found = true
		}
		return !found
	})
	return found
}

func callSelectsProvider(call *ast.CallExpr, selectors map[string]struct{}, constants map[string]string) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch selector.Sel.Name {
	case "HasSuffix":
		for _, argument := range call.Args {
			if value, ok := providerSelectorValue(argument, selectors, constants); ok && strings.HasPrefix(value, ".") {
				return true
			}
		}
	case "EqualFold":
		for i, argument := range call.Args {
			for j, other := range call.Args {
				if i != j && selectionComparison(argument, other, selectors, constants) {
					return true
				}
			}
		}
	}
	return false
}

func expressionsSelectProvider(expressions []ast.Expr, tag ast.Expr, selectors map[string]struct{}, constants map[string]string) bool {
	for _, expression := range expressions {
		if selectionComparison(expression, tag, selectors, constants) {
			return true
		}
	}
	return false
}

func selectionComparison(selector, discriminator ast.Expr, selectors map[string]struct{}, constants map[string]string) bool {
	value, ok := providerSelectorValue(selector, selectors, constants)
	return ok && (strings.HasPrefix(value, ".") || expressionMentionsFormat(discriminator))
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

func providerSelectorValue(expression ast.Expr, selectors map[string]struct{}, constants map[string]string) (string, bool) {
	var value string
	switch expression := expression.(type) {
	case *ast.BasicLit:
		if expression.Kind != token.STRING {
			return "", false
		}
		value = strings.Trim(expression.Value, "\"`")
	case *ast.Ident:
		value = constants[expression.Name]
	default:
		return "", false
	}
	value = strings.ToLower(value)
	_, ok := selectors[value]
	return value, ok
}
