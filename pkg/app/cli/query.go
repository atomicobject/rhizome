package actions

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

// ParseInputs keeps the original signature while delegating to the expression parser.
func ParseInputs(args []string) ([]ListInput, error) {
	inputs, _, err := ParseInputsWithExpression(args)
	return inputs, err
}

type expressionParser struct {
	tokens []string
	pos    int
	inputs []ListInput
	cfg    parseConfig
}

func (p *expressionParser) parseExpression() (*InputExpression, error) {
	return p.parseOr()
}

func (p *expressionParser) parseOr() (*InputExpression, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}

	for p.peekIsOperator("or") {
		p.pos++
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &InputExpression{
			Type:  exprOr,
			Left:  left,
			Right: right,
		}
	}
	return left, nil
}

func (p *expressionParser) parseAnd() (*InputExpression, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}

	for p.peekIsOperator("and") {
		p.pos++
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &InputExpression{
			Type:  exprAnd,
			Left:  left,
			Right: right,
		}
	}
	return left, nil
}

func (p *expressionParser) parseUnary() (*InputExpression, error) {
	if p.peekIsOperator("not") {
		p.pos++
		child, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &InputExpression{
			Type: exprNot,
			Left: child,
		}, nil
	}
	return p.parsePrimary()
}

func (p *expressionParser) parsePrimary() (*InputExpression, error) {
	if p.match("(") {
		expr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if !p.match(")") {
			return nil, fmt.Errorf("expected ')'")
		}
		return expr, nil
	}

	if p.pos >= len(p.tokens) {
		return nil, fmt.Errorf("unexpected end of expression")
	}

	token := p.tokens[p.pos]
	if classifyToken(token) == "operator" {
		return nil, fmt.Errorf("unexpected operator %q", token)
	}
	p.pos++
	input, err := parseSingleInputWithConfig(token, p.cfg)
	if err != nil {
		return nil, err
	}
	p.inputs = append(p.inputs, input)

	return &InputExpression{
		Type:  exprLeaf,
		Input: &input,
	}, nil
}

func (p *expressionParser) match(tok string) bool {
	if p.pos < len(p.tokens) && p.tokens[p.pos] == tok {
		p.pos++
		return true
	}
	return false
}

func (p *expressionParser) peekIsOperator(op string) bool {
	if p.pos >= len(p.tokens) {
		return false
	}
	tok := strings.ToLower(p.tokens[p.pos])
	switch op {
	case "and":
		return tok == "and" || tok == "&&"
	case "or":
		return tok == "or" || tok == "||"
	case "not":
		return tok == "not" || tok == "!"
	default:
		return false
	}
}

func tokenizeArgs(args []string) []string {
	var tokens []string
	for _, arg := range args {
		for _, piece := range splitParens(arg) {
			if piece == "" {
				continue
			}
			if containsOperatorWord(piece) {
				for _, field := range strings.Fields(piece) {
					if field == "" {
						continue
					}
					tokens = append(tokens, field)
				}
				continue
			}
			tokens = append(tokens, piece)
		}
	}
	return tokens
}

func tokenizeQueryString(query string) []string {
	var (
		tokens []string
		buf    strings.Builder
		quote  rune
	)
	flush := func() {
		if buf.Len() == 0 {
			return
		}
		tokens = append(tokens, buf.String())
		buf.Reset()
	}

	for _, r := range query {
		switch {
		case quote != 0:
			buf.WriteRune(r)
			if r == quote {
				quote = 0
			}
		case r == '"':
			buf.WriteRune(r)
			quote = r
		case r == '(' || r == ')':
			flush()
			tokens = append(tokens, string(r))
		case r == '!' || r == '\t' || r == '\n' || r == '\r' || r == ' ':
			flush()
			if r == '!' {
				tokens = append(tokens, "!")
			}
		default:
			buf.WriteRune(r)
		}
	}
	flush()
	return tokens
}

// containsOperatorWord checks if a string contains boolean operators that need
// to be split into separate tokens. We use heuristics to avoid expensive field
// splitting on simple inputs: a single colon indicates a simple pattern like
// "tag:foo" which won't contain operators. Two or more colons (e.g., "tag:foo AND tag:bar")
// or parentheses/symbolic operators suggest a compound expression worth scanning.
func containsOperatorWord(piece string) bool {
	colonCount := strings.Count(piece, ":")
	hasParens := strings.Contains(piece, "(") || strings.Contains(piece, ")")
	if colonCount < 2 && !hasParens && !strings.Contains(piece, "&&") && !strings.Contains(piece, "||") {
		return false
	}
	fields := strings.Fields(piece)
	for _, f := range fields {
		switch strings.ToUpper(f) {
		case "AND", "OR", "NOT", "&&", "||", "!":
			return true
		}
	}
	return false
}

// splitParens separates leading/trailing parentheses from a token without
// splitting parentheses that appear in the middle of a value.
func splitParens(token string) []string {
	if strings.Contains(token, " ") && !containsOperatorWord(token) {
		return []string{token}
	}

	var tokens []string
	leading := token
	for len(leading) > 0 && (leading[0] == '(' || leading[0] == ')') {
		tokens = append(tokens, string(leading[0]))
		leading = leading[1:]
	}

	trailingParens := ""
	for len(leading) > 0 && (leading[len(leading)-1] == '(' || leading[len(leading)-1] == ')') {
		trailingParens = string(leading[len(leading)-1]) + trailingParens
		leading = leading[:len(leading)-1]
	}

	if strings.TrimSpace(leading) != "" {
		tokens = append(tokens, leading)
	}

	for i := 0; i < len(trailingParens); i++ {
		tokens = append(tokens, string(trailingParens[i]))
	}
	return tokens
}

func coalesceBareOperands(tokens []string) []string {
	var (
		out []string
		buf []string
	)
	flush := func() {
		if len(buf) == 0 {
			return
		}
		out = append(out, strings.Join(buf, " "))
		buf = nil
	}

	for _, tok := range tokens {
		switch classifyToken(tok) {
		case "operator", "lparen", "rparen":
			flush()
			out = append(out, tok)
		default:
			if strings.Contains(tok, ":") {
				flush()
				out = append(out, tok)
				continue
			}
			buf = append(buf, trimMatchingDoubleQuotes(tok))
		}
	}
	flush()
	return out
}

// insertImplicitOperators inserts the configured operator between adjacent
// operands/groups when no explicit operator is provided.
func insertImplicitOperators(tokens []string, operator string) []string {
	if strings.TrimSpace(operator) == "" {
		operator = "OR"
	}
	var result []string
	for i, tok := range tokens {
		kind := classifyToken(tok)
		if i > 0 {
			prevKind := classifyToken(tokens[i-1])
			if (prevKind == "operand" || prevKind == "rparen") && (kind == "operand" || kind == "lparen") {
				result = append(result, strings.ToUpper(operator))
			}
		}
		result = append(result, tok)
	}
	return result
}

func classifyToken(tok string) string {
	switch strings.ToLower(tok) {
	case "and", "or", "not", "&&", "||", "!":
		return "operator"
	case "(":
		return "lparen"
	case ")":
		return "rparen"
	default:
		return "operand"
	}
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
