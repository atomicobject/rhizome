package schema

import (
	"fmt"
	"github.com/bmatcuk/doublestar/v4"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type matchInputType int

const (
	matchInputFile matchInputType = iota
	matchInputTag
	matchInputFind
	matchInputProperty
	matchInputHas
)

type matchInput struct {
	Type     matchInputType
	Value    string
	Property string
	// Exact marks a quoted property value: it matches a frontmatter string as
	// authored, with no inline fields and no case, space, or link folding.
	Exact bool
}

type matchExprType int

const (
	matchExprLeaf matchExprType = iota
	matchExprAnd
	matchExprOr
	matchExprNot
)

type matchExpression struct {
	Type  matchExprType
	Input *matchInput
	Left  *matchExpression
	Right *matchExpression
}

func compileNoteMatchers(pathsList, matches []string) ([]*NoteMatcher, error) {
	out := make([]*NoteMatcher, 0, len(pathsList)+len(matches))
	for _, pattern := range pathsList {
		if strings.TrimSpace(pattern) == "" {
			continue
		}
		out = append(out, &NoteMatcher{Raw: pattern, PathPrefix: filepath.ToSlash(pattern)})
	}
	for _, raw := range matches {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		expr, err := parseMatchExpression(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, &NoteMatcher{Raw: raw, Expression: expr})
	}
	return out, nil
}

func (m *NoteMatcher) Matches(doc *Subject) bool {
	if m == nil || doc == nil {
		return false
	}
	if m.Expression != nil {
		return evalMatchExpression(m.Expression, doc)
	}
	ok, err := doublestar.Match(m.PathPrefix, filepath.ToSlash(doc.Path))
	return err == nil && ok
}

func parseMatchExpression(raw string) (*matchExpression, error) {
	tokens := insertImplicitOrs(tokenizeMatchArgs([]string{raw}))
	if len(tokens) == 0 {
		return nil, fmt.Errorf("empty match expression")
	}
	parser := matchExpressionParser{tokens: tokens}
	expr, err := parser.parseExpression()
	if err != nil {
		return nil, err
	}
	if parser.pos != len(parser.tokens) {
		return nil, fmt.Errorf("unexpected token %q", parser.tokens[parser.pos])
	}
	return expr, nil
}

type matchExpressionParser struct {
	tokens []string
	pos    int
}

func (p *matchExpressionParser) parseExpression() (*matchExpression, error) {
	return p.parseOr()
}

func (p *matchExpressionParser) parseOr() (*matchExpression, error) {
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
		left = &matchExpression{Type: matchExprOr, Left: left, Right: right}
	}
	return left, nil
}

func (p *matchExpressionParser) parseAnd() (*matchExpression, error) {
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
		left = &matchExpression{Type: matchExprAnd, Left: left, Right: right}
	}
	return left, nil
}

func (p *matchExpressionParser) parseUnary() (*matchExpression, error) {
	if p.peekIsOperator("not") {
		p.pos++
		child, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &matchExpression{Type: matchExprNot, Left: child}, nil
	}
	return p.parsePrimary()
}

func (p *matchExpressionParser) parsePrimary() (*matchExpression, error) {
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
	if classifyMatchToken(token) == "operator" {
		return nil, fmt.Errorf("unexpected operator %q", token)
	}
	p.pos++
	input, err := parseMatchInput(token)
	if err != nil {
		return nil, err
	}
	return &matchExpression{Type: matchExprLeaf, Input: &input}, nil
}

func (p *matchExpressionParser) match(tok string) bool {
	if p.pos < len(p.tokens) && p.tokens[p.pos] == tok {
		p.pos++
		return true
	}
	return false
}

func (p *matchExpressionParser) peekIsOperator(op string) bool {
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

func evalMatchExpression(expr *matchExpression, doc *Subject) bool {
	if expr == nil || doc == nil {
		return false
	}
	switch expr.Type {
	case matchExprLeaf:
		return evalMatchInput(expr.Input, doc)
	case matchExprAnd:
		return evalMatchExpression(expr.Left, doc) && evalMatchExpression(expr.Right, doc)
	case matchExprOr:
		return evalMatchExpression(expr.Left, doc) || evalMatchExpression(expr.Right, doc)
	case matchExprNot:
		return !evalMatchExpression(expr.Left, doc)
	default:
		return false
	}
}

func evalMatchInput(input *matchInput, doc *Subject) bool {
	if input == nil || doc == nil {
		return false
	}
	switch input.Type {
	case matchInputFile:
		return matchSelectorPath(doc.Path, input.Value)
	case matchInputFind:
		return obsidian.FuzzyMatch(input.Value, doc.Path) || obsidian.FuzzyMatch(input.Value, doc.Title)
	case matchInputTag:
		return hasTag(doc.Tags, input.Value)
	case matchInputProperty:
		if input.Exact {
			return docFrontmatterHasExactValue(doc, input.Property, input.Value)
		}
		return docPropertyHasValue(doc, input.Property, input.Value)
	case matchInputHas:
		for key, value := range doc.Frontmatter {
			if strings.EqualFold(strings.TrimSpace(key), input.Property) && value != nil && value != "" {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func hasTag(tags []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(target, "#")))
	if target == "" {
		return false
	}
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(tag, "#")))
		if tag == target || strings.HasPrefix(tag, target+"/") {
			return true
		}
	}
	return false
}

func matchSelectorPath(notePath, input string) bool {
	normalizedInputPath := string(paths.Normalize(input))
	if normalizedInputPath == "*" {
		return true
	}
	normalizedNotePath := string(paths.Normalize(notePath))
	dirPrefix := normalizedInputPath + "/"
	return normalizedNotePath == normalizedInputPath || strings.HasPrefix(normalizedNotePath, dirPrefix)
}

func docPropertyHasValue(doc *Subject, property, target string) bool {
	if doc == nil || strings.TrimSpace(property) == "" || strings.TrimSpace(target) == "" {
		return false
	}
	targetNorm := normalizeMatchPropertyValue(target)
	propKey := strings.ToLower(strings.TrimSpace(property))
	checkValues := func(vals []string) bool {
		for _, v := range vals {
			if normalizeMatchPropertyValue(v) == targetNorm {
				return true
			}
		}
		return false
	}
	for k, v := range doc.Frontmatter {
		if strings.ToLower(strings.TrimSpace(k)) != propKey {
			continue
		}
		if checkValues(obsidian.AnalyzePropertyValue(v).Values) {
			return true
		}
	}
	for k, vals := range doc.Inline {
		if strings.ToLower(strings.TrimSpace(k)) != propKey {
			continue
		}
		if checkValues(vals) {
			return true
		}
	}
	return false
}

// docFrontmatterHasExactValue reports whether a frontmatter property's string
// scalar, or a string element of its list, equals target byte for byte.
func docFrontmatterHasExactValue(doc *Subject, property, target string) bool {
	for key, value := range doc.Frontmatter {
		if !strings.EqualFold(strings.TrimSpace(key), strings.TrimSpace(property)) {
			continue
		}
		switch value := value.(type) {
		case string:
			if value == target {
				return true
			}
		case []string:
			if slices.Contains(value, target) {
				return true
			}
		case []any:
			if slices.Contains(value, any(target)) {
				return true
			}
		}
	}
	return false
}

func normalizeMatchPropertyValue(v string) string {
	val := strings.ToLower(strings.TrimSpace(v))
	if strings.HasPrefix(val, "[[") && strings.HasSuffix(val, "]]") {
		val = strings.TrimSuffix(strings.TrimPrefix(val, "[["), "]]")
	}
	if strings.Contains(val, "|") {
		parts := strings.SplitN(val, "|", 2)
		val = strings.TrimSpace(parts[0])
	}
	return val
}

func parseMatchInput(arg string) (matchInput, error) {
	if property, ok := strings.CutPrefix(arg, "has:"); ok {
		property = strings.TrimSpace(property)
		if property == "" {
			return matchInput{}, fmt.Errorf("invalid presence input %q", arg)
		}
		return matchInput{Type: matchInputHas, Property: property}, nil
	}
	if strings.HasPrefix(arg, "tag:") {
		tag := strings.Trim(strings.TrimPrefix(arg, "tag:"), "\"")
		if tag == "" || tag == "*" {
			return matchInput{}, fmt.Errorf("invalid tag value in %q", arg)
		}
		return matchInput{Type: matchInputTag, Value: tag}, nil
	}
	if strings.HasPrefix(arg, "find:") {
		val := strings.Trim(strings.TrimPrefix(arg, "find:"), "\"")
		if val == "" || val == "*" {
			return matchInput{}, fmt.Errorf("invalid find value in %q", arg)
		}
		return matchInput{Type: matchInputFind, Value: val}, nil
	}
	if strings.Contains(arg, ":") {
		parts := strings.SplitN(arg, ":", 2)
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if key == "" || val == "" || val == "*" {
			return matchInput{}, fmt.Errorf("invalid property input %q", arg)
		}
		exact := len(val) >= 2 && strings.HasPrefix(val, `"`) && strings.HasSuffix(val, `"`)
		if exact {
			val = val[1 : len(val)-1]
		}
		return matchInput{Type: matchInputProperty, Property: key, Value: val, Exact: exact}, nil
	}
	return matchInput{Type: matchInputFile, Value: arg}, nil
}

func tokenizeMatchArgs(args []string) []string {
	var tokens []string
	for _, arg := range args {
		fields := matchExpressionTokens(arg)
		if containsMatchOperatorWord(arg) || (len(fields) > 0 && (fields[0] == "(" || fields[0] == ")")) {
			tokens = append(tokens, fields...)
		} else if arg != "" {
			tokens = append(tokens, arg)
		}
	}
	return tokens
}

func matchExpressionTokens(raw string) []string {
	var tokens []string
	var value strings.Builder
	quoted := false
	flush := func() {
		if value.Len() > 0 {
			tokens = append(tokens, value.String())
			value.Reset()
		}
	}
	for _, r := range raw {
		if r == '"' {
			quoted = !quoted
		}
		switch {
		case !quoted && (r == '(' || r == ')'):
			flush()
			tokens = append(tokens, string(r))
		case !quoted && unicode.IsSpace(r):
			flush()
		default:
			value.WriteRune(r)
		}
	}
	flush()
	return tokens
}

// containsMatchOperatorWord reports whether a selector piece is an operator
// expression rather than one literal input such as a path with spaces.
// Uppercase AND, OR, and NOT always mark an expression; lowercase words only
// do alongside other operator evidence, so "Notes/Rock and Roll" stays a path.
func containsMatchOperatorWord(piece string) bool {
	for _, f := range matchExpressionTokens(piece) {
		switch f {
		case "AND", "OR", "NOT":
			return true
		}
	}
	colonCount := strings.Count(piece, ":")
	hasParens := strings.Contains(piece, "(") || strings.Contains(piece, ")")
	if colonCount < 2 && !hasParens && !strings.Contains(piece, "&&") && !strings.Contains(piece, "||") {
		return false
	}
	for _, f := range matchExpressionTokens(piece) {
		switch strings.ToUpper(f) {
		case "AND", "OR", "NOT", "&&", "||", "!":
			return true
		}
	}
	return false
}

func insertImplicitOrs(tokens []string) []string {
	var result []string
	for i, tok := range tokens {
		kind := classifyMatchToken(tok)
		if i > 0 {
			prevKind := classifyMatchToken(tokens[i-1])
			if (prevKind == "operand" || prevKind == "rparen") && (kind == "operand" || kind == "lparen") {
				result = append(result, "OR")
			}
		}
		result = append(result, tok)
	}
	return result
}

func classifyMatchToken(tok string) string {
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
