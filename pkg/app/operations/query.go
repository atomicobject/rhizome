package operations

// Docs: [List + prompt matching DSL](docs/reference/domain/List + prompt matching DSL.md)

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type exprType int

const (
	exprLeaf exprType = iota
	exprAnd
	exprOr
	exprNot
)

// InputExpression represents a boolean expression composed of ListInputs.
type InputExpression struct {
	Type  exprType
	Input *ListInput
	Left  *InputExpression
	Right *InputExpression
}

// CanonicalInputExpression returns a stable semantic representation for
// request identities. The expression's node type is intentionally private, so
// callers must not attempt to JSON-encode the struct directly.
func CanonicalInputExpression(expr *InputExpression) string {
	if expr == nil {
		return ""
	}
	switch expr.Type {
	case exprLeaf:
		if expr.Input == nil {
			return "leaf:nil"
		}
		return "leaf:" + strconv.Itoa(int(expr.Input.Type)) + ":" + strings.TrimSpace(expr.Input.Property) + ":" + strings.TrimSpace(expr.Input.Value)
	case exprAnd:
		children := []string{CanonicalInputExpression(expr.Left), CanonicalInputExpression(expr.Right)}
		sort.Strings(children)
		return "and(" + strings.Join(children, ",") + ")"
	case exprOr:
		children := []string{CanonicalInputExpression(expr.Left), CanonicalInputExpression(expr.Right)}
		sort.Strings(children)
		return "or(" + strings.Join(children, ",") + ")"
	case exprNot:
		return "not(" + CanonicalInputExpression(expr.Left) + ")"
	default:
		return "invalid"
	}
}

// ExpressionInfo summarizes an input expression tree.
type ExpressionInfo struct {
	HasNot      bool
	HasOr       bool
	HasFind     bool
	HasTag      bool
	HasProperty bool
	FileInputs  []string
}

type parseConfig struct {
	bareInputType    InputType
	implicitOperator string
}

var defaultParseConfig = parseConfig{
	bareInputType:    InputTypeFile,
	implicitOperator: "OR",
}

var searchQueryParseConfig = parseConfig{
	bareInputType:    InputTypeFind,
	implicitOperator: "AND",
}

// AnalyzeExpression walks an input expression tree and reports its structure and leaves.
func AnalyzeExpression(expr *InputExpression) ExpressionInfo {
	var info ExpressionInfo
	var walk func(node *InputExpression)
	walk = func(node *InputExpression) {
		if node == nil {
			return
		}
		switch node.Type {
		case exprLeaf:
			if node.Input == nil {
				return
			}
			switch node.Input.Type {
			case InputTypeFile:
				info.FileInputs = append(info.FileInputs, node.Input.Value)
			case InputTypeFind:
				info.HasFind = true
			case InputTypeTag:
				info.HasTag = true
			case InputTypeProperty:
				info.HasProperty = true
			}
		case exprAnd:
			walk(node.Left)
			walk(node.Right)
		case exprOr:
			info.HasOr = true
			walk(node.Left)
			walk(node.Right)
		case exprNot:
			info.HasNot = true
			walk(node.Left)
		default:
			walk(node.Left)
			walk(node.Right)
		}
	}
	walk(expr)
	return info
}

// ParseInputsWithExpression parses args into ListInputs and a boolean expression tree.
// Operators: AND/OR/NOT (case-insensitive), with parentheses for grouping.
// When no operator is provided between terms, OR is assumed for compatibility.
func ParseInputsWithExpression(args []string) ([]ListInput, *InputExpression, error) {
	if len(args) == 0 {
		return nil, nil, nil
	}

	rawTokens := tokenizeArgs(args)
	return parseTokens(rawTokens, defaultParseConfig)
}

// ParseSearchQueryWithExpression parses a freeform search string into the same
// boolean DSL used by list, but defaults bare text to find: and ANDs adjacent
// clauses together unless the query says otherwise.
func ParseSearchQueryWithExpression(query string) ([]ListInput, *InputExpression, error) {
	rawTokens := tokenizeQueryString(query)
	rawTokens = coalesceBareOperands(rawTokens)
	return parseTokens(rawTokens, searchQueryParseConfig)
}

func parseTokens(rawTokens []string, cfg parseConfig) ([]ListInput, *InputExpression, error) {
	if len(rawTokens) == 0 {
		return nil, nil, nil
	}

	tokens := insertImplicitOperators(rawTokens, cfg.implicitOperator)

	parser := expressionParser{
		tokens: tokens,
		cfg:    cfg,
	}
	expr, err := parser.parseExpression()
	if err != nil {
		return nil, nil, err
	}
	if parser.pos != len(parser.tokens) {
		return nil, nil, fmt.Errorf("unexpected token %q", parser.tokens[parser.pos])
	}

	return parser.inputs, expr, nil
}

func parseSingleInputWithConfig(arg string, cfg parseConfig) (ListInput, error) {
	if strings.HasPrefix(arg, "tag:") {
		tag := strings.TrimPrefix(arg, "tag:")
		tag = trimMatchingDoubleQuotes(tag)

		if tag == "" || tag == "*" {
			return ListInput{}, fmt.Errorf("invalid tag value in %q: tag cannot be empty or a wildcard (*)", arg)
		}

		return ListInput{
			Type:  InputTypeTag,
			Value: tag,
		}, nil
	}

	if strings.HasPrefix(arg, "find:") {
		searchTerm := strings.TrimPrefix(arg, "find:")
		searchTerm = trimMatchingDoubleQuotes(searchTerm)

		if searchTerm == "" || searchTerm == "*" {
			return ListInput{}, fmt.Errorf("invalid find value in %q: find cannot be empty or a wildcard (*)", arg)
		}

		return ListInput{
			Type:  InputTypeFind,
			Value: searchTerm,
		}, nil
	}

	if strings.Contains(arg, ":") {
		parts := strings.SplitN(arg, ":", 2)
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		if key == "" || val == "" || val == "*" {
			return ListInput{}, fmt.Errorf("invalid property input %q: both key and value are required", arg)
		}

		return ListInput{
			Type:     InputTypeProperty,
			Value:    val,
			Property: key,
		}, nil
	}

	return ListInput{
		Type:  cfg.bareInputType,
		Value: trimMatchingDoubleQuotes(arg),
	}, nil
}

func trimMatchingDoubleQuotes(value string) string {
	if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") && len(value) >= 2 {
		return strings.Trim(value, "\"")
	}
	return value
}
