package ontology

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/bmatcuk/doublestar/v4"
)

type matchInputType int

const (
	matchInputFile matchInputType = iota
	matchInputTag
	matchInputFind
	matchInputProperty
)

type matchInput struct {
	Type     matchInputType
	Value    string
	Property string
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

func (m *NoteMatcher) matches(doc *noteDoc) bool {
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

func evalMatchExpression(expr *matchExpression, doc *noteDoc) bool {
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

func evalMatchInput(input *matchInput, doc *noteDoc) bool {
	if input == nil || doc == nil {
		return false
	}
	switch input.Type {
	case matchInputFile:
		return matchSelectorPath(doc.Path, input.Value)
	case matchInputFind:
		return obsidian.FuzzyMatch(input.Value, doc.Path) || obsidian.FuzzyMatch(input.Value, doc.Title)
	case matchInputTag:
		if len(doc.Tags) > 0 {
			return hasTag(doc.Tags, input.Value)
		}
		return obsidian.HasAnyTags(doc.Content, []string{input.Value})
	case matchInputProperty:
		return docPropertyHasValue(doc, input.Property, input.Value)
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

func docPropertyHasValue(doc *noteDoc, property, target string) bool {
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
		return matchInput{Type: matchInputProperty, Property: key, Value: val}, nil
	}
	return matchInput{Type: matchInputFile, Value: arg}, nil
}

func tokenizeMatchArgs(args []string) []string {
	var tokens []string
	for _, arg := range args {
		for _, piece := range splitMatchParens(arg) {
			if piece == "" {
				continue
			}
			if containsMatchOperatorWord(piece) {
				for _, field := range strings.Fields(piece) {
					if field != "" {
						tokens = append(tokens, field)
					}
				}
				continue
			}
			tokens = append(tokens, piece)
		}
	}
	return tokens
}

// containsMatchOperatorWord reports whether a selector piece is an operator
// expression rather than one literal input such as a path with spaces.
// Uppercase AND, OR, and NOT always mark an expression; lowercase words only
// do alongside other operator evidence, so "Notes/Rock and Roll" stays a path.
func containsMatchOperatorWord(piece string) bool {
	for _, f := range strings.Fields(piece) {
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
	for _, f := range strings.Fields(piece) {
		switch strings.ToUpper(f) {
		case "AND", "OR", "NOT", "&&", "||", "!":
			return true
		}
	}
	return false
}

func splitMatchParens(token string) []string {
	if strings.Contains(token, " ") && !containsMatchOperatorWord(token) {
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
