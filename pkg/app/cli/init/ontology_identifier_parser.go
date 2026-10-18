package init

import (
	"fmt"
	"strconv"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/lexer"
	"github.com/vektah/gqlparser/v2/parser"
)

func parseIdentifierAllocationContracts(name string, content []byte) (map[string]identifierDirectiveContract, error) {
	parserInput, err := identifierOntologyParserInput(name, content)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	source := &ast.Source{Name: name, Input: parserInput}
	doc, err := parser.ParseSchema(source)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	tokens, err := lexIdentifierOntology(source)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	runes := []rune(source.Input)
	contracts := map[string]identifierDirectiveContract{}
	definitions := append(append(ast.DefinitionList(nil), doc.Definitions...), doc.Extensions...)
	interfacesByType := map[string][]string{}
	interfaceFields := map[string]map[string]bool{}
	for _, definition := range definitions {
		if definition == nil || (definition.Kind != ast.Object && definition.Kind != ast.Interface) {
			continue
		}
		interfacesByType[definition.Name] = append(interfacesByType[definition.Name], definition.Interfaces...)
		if definition.Kind == ast.Interface {
			if interfaceFields[definition.Name] == nil {
				interfaceFields[definition.Name] = map[string]bool{}
			}
			for _, field := range definition.Fields {
				interfaceFields[definition.Name][field.Name] = true
			}
		}
	}
	for _, definition := range definitions {
		if definition == nil || (definition.Kind != ast.Object && definition.Kind != ast.Interface) {
			continue
		}
		for _, field := range definition.Fields {
			directives := field.Directives.ForNames("identifier")
			if len(directives) == 0 {
				continue
			}
			key := definition.Name + "." + field.Name
			if len(directives) != 1 {
				return nil, fmt.Errorf("parse %s: %s declares more than one @identifier directive", name, key)
			}
			if _, duplicate := contracts[key]; duplicate {
				return nil, fmt.Errorf("parse %s: %s is declared more than once", name, key)
			}
			directive := directives[0]
			allocation, allocationErr := parseIdentifierAllocationContract(runes, directive)
			if allocationErr != nil {
				return nil, fmt.Errorf("parse %s: %s @identifier: %w", name, key, allocationErr)
			}
			span, spanErr := identifierDirectiveTokenSpan(tokens, directive)
			if spanErr != nil {
				return nil, fmt.Errorf("parse %s: %s @identifier: %w", name, key, spanErr)
			}
			var interfaces []string
			for _, iface := range interfacesByType[definition.Name] {
				if interfaceFields[iface][field.Name] {
					interfaces = append(interfaces, iface)
				}
			}
			contracts[key] = identifierDirectiveContract{allocation: allocation, interfaces: interfaces, fieldName: field.Name, directive: directive, span: span}
		}
	}
	return contracts, nil
}

// identifierOntologyParserInput masks empty object/interface declarations as
// scalars without changing the source's rune length. Rhizome permits empty
// Section implementations because their fields are injected during full
// ontology compilation, while gqlparser's syntax parser rejects an empty field
// block. Equal-length masking keeps every untouched AST position aligned with
// the authored source used for edits.
func identifierOntologyParserInput(name string, content []byte) (string, error) {
	source := &ast.Source{Name: name, Input: string(content)}
	tokens, err := lexIdentifierOntology(source)
	if err != nil {
		return "", err
	}
	runes := []rune(source.Input)
	for index := 0; index < len(tokens); index++ {
		if tokens[index].Kind != lexer.Name || (tokens[index].Value != "type" && tokens[index].Value != "interface") {
			continue
		}
		nameIndex := index + 1
		if nameIndex >= len(tokens) || tokens[nameIndex].Kind != lexer.Name {
			continue
		}
		parenDepth := 0
		bodyStart := -1
		for cursor := nameIndex + 1; cursor < len(tokens); cursor++ {
			switch tokens[cursor].Kind {
			case lexer.ParenL:
				parenDepth++
			case lexer.ParenR:
				parenDepth--
			case lexer.BraceL:
				if parenDepth == 0 {
					bodyStart = cursor
				}
			}
			if bodyStart >= 0 {
				break
			}
			if tokens[cursor].Kind == lexer.EOF {
				break
			}
		}
		if bodyStart < 0 {
			continue
		}
		bodyEnd := bodyStart + 1
		for bodyEnd < len(tokens) && tokens[bodyEnd].Kind == lexer.Comment {
			bodyEnd++
		}
		if bodyEnd >= len(tokens) || tokens[bodyEnd].Kind != lexer.BraceR {
			continue
		}
		definitionStart := index
		if index > 0 && tokens[index-1].Kind == lexer.Name && tokens[index-1].Value == "extend" {
			definitionStart = index - 1
		}
		start := tokens[definitionStart].Pos.Start
		end := tokens[bodyEnd].Pos.End
		if !validRuneRange(start, end, len(runes)) {
			return "", fmt.Errorf("empty definition %q has invalid source positions", tokens[nameIndex].Value)
		}
		for cursor := start; cursor < end; cursor++ {
			if runes[cursor] != '\n' && runes[cursor] != '\r' {
				runes[cursor] = ' '
			}
		}
		replacement := []rune("scalar " + tokens[nameIndex].Value)
		if len(replacement) > end-start {
			return "", fmt.Errorf("empty definition %q cannot be masked without shifting source positions", tokens[nameIndex].Value)
		}
		copy(runes[start:start+len(replacement)], replacement)
		index = bodyEnd
	}
	return string(runes), nil
}

func parseIdentifierAllocationContract(content []rune, directive *ast.Directive) (identifierAllocationContract, error) {
	contract := identifierAllocationContract{
		preferred:        false,
		strategy:         "SEQUENTIAL",
		separator:        "-",
		pad:              4,
		preferredLiteral: "false",
		strategyLiteral:  "SEQUENTIAL",
		separatorLiteral: `"-"`,
		padLiteral:       "4",
	}
	seen := map[string]bool{}
	padExplicit := false
	for _, argument := range directive.Arguments {
		if seen[argument.Name] {
			return identifierAllocationContract{}, fmt.Errorf("argument %q is declared more than once", argument.Name)
		}
		seen[argument.Name] = true
		if argument.Value == nil || argument.Value.Position == nil || !validRuneRange(argument.Value.Position.Start, argument.Value.Position.End, len(content)) {
			return identifierAllocationContract{}, fmt.Errorf("argument %q has no valid source position", argument.Name)
		}
		literal := string(content[argument.Value.Position.Start:argument.Value.Position.End])
		switch argument.Name {
		case "preferred":
			if argument.Value.Kind != ast.BooleanValue {
				return identifierAllocationContract{}, fmt.Errorf("preferred must be a boolean")
			}
			preferred, err := strconv.ParseBool(argument.Value.Raw)
			if err != nil {
				return identifierAllocationContract{}, fmt.Errorf("parse preferred: %w", err)
			}
			contract.preferred = preferred
			contract.preferredLiteral = literal
		case "strategy":
			if argument.Value.Kind != ast.EnumValue {
				return identifierAllocationContract{}, fmt.Errorf("strategy must be an enum value")
			}
			contract.strategy = argument.Value.Raw
			contract.strategyLiteral = literal
		case "prefix":
			if argument.Value.Kind != ast.StringValue {
				return identifierAllocationContract{}, fmt.Errorf("prefix must be a string")
			}
			contract.prefix = argument.Value.Raw
			contract.prefixLiteral = literal
		case "separator":
			if argument.Value.Kind != ast.StringValue {
				return identifierAllocationContract{}, fmt.Errorf("separator must be a string")
			}
			contract.separator = argument.Value.Raw
			contract.separatorLiteral = literal
		case "pad":
			if argument.Value.Kind != ast.IntValue {
				return identifierAllocationContract{}, fmt.Errorf("pad must be an integer")
			}
			pad, err := strconv.Atoi(argument.Value.Raw)
			if err != nil {
				return identifierAllocationContract{}, fmt.Errorf("parse pad: %w", err)
			}
			contract.pad = pad
			contract.padLiteral = literal
			padExplicit = true
		}
	}

	// Without prefix the directive is a free-form identifier, not an allocation
	// contract. Match ontology compilation by leaving its other arguments inert.
	if !contract.enabled() {
		return identifierAllocationContract{}, nil
	}
	if contract.strategy != "SEQUENTIAL" && contract.strategy != "DATETIME" {
		return identifierAllocationContract{}, fmt.Errorf("unsupported strategy %q", contract.strategy)
	}
	if !validIdentifierPrefix(contract.prefix) {
		return identifierAllocationContract{}, fmt.Errorf("prefix %q must be 1-16 uppercase ASCII letters or digits and start with a letter", contract.prefix)
	}
	if contract.separator == "" {
		return identifierAllocationContract{}, fmt.Errorf("separator cannot be empty")
	}
	if contract.strategy == "SEQUENTIAL" {
		if contract.pad < 1 || contract.pad > 8 {
			return identifierAllocationContract{}, fmt.Errorf("pad %d out of range [1,8]", contract.pad)
		}
	} else {
		if padExplicit {
			return identifierAllocationContract{}, fmt.Errorf("pad is not valid for DATETIME identifiers")
		}
		contract.pad = 0
		contract.padLiteral = ""
	}
	return contract, nil
}

func lexIdentifierOntology(source *ast.Source) ([]lexer.Token, error) {
	reader := lexer.New(source)
	var tokens []lexer.Token
	for {
		token, err := reader.ReadToken()
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
		if token.Kind == lexer.EOF {
			return tokens, nil
		}
	}
}

func identifierDirectiveTokenSpan(tokens []lexer.Token, directive *ast.Directive) (identifierDirectiveSpan, error) {
	if directive == nil || directive.Position == nil {
		return identifierDirectiveSpan{}, fmt.Errorf("directive has no source position")
	}
	nameIndex := -1
	for index, token := range tokens {
		if token.Kind == lexer.Name && token.Value == "identifier" && token.Pos.Start == directive.Position.Start {
			nameIndex = index
			break
		}
	}
	if nameIndex < 0 {
		return identifierDirectiveSpan{}, fmt.Errorf("cannot locate directive name token")
	}
	atIndex := nameIndex - 1
	for atIndex >= 0 && tokens[atIndex].Kind == lexer.Comment {
		atIndex--
	}
	if atIndex < 0 || tokens[atIndex].Kind != lexer.At {
		return identifierDirectiveSpan{}, fmt.Errorf("cannot locate directive @ token")
	}
	span := identifierDirectiveSpan{
		start:      tokens[atIndex].Pos.Start,
		end:        tokens[nameIndex].Pos.End,
		hasComment: atIndex != nameIndex-1,
	}
	nextIndex := nameIndex + 1
	for nextIndex < len(tokens) && tokens[nextIndex].Kind == lexer.Comment {
		span.hasComment = true
		nextIndex++
	}
	if nextIndex >= len(tokens) || tokens[nextIndex].Kind != lexer.ParenL {
		return span, nil
	}
	depth := 0
	for index := nextIndex; index < len(tokens); index++ {
		switch tokens[index].Kind {
		case lexer.Comment:
			span.hasComment = true
		case lexer.ParenL:
			depth++
		case lexer.ParenR:
			depth--
			if depth == 0 {
				span.end = tokens[index].Pos.End
				return span, nil
			}
		}
	}
	return identifierDirectiveSpan{}, fmt.Errorf("identifier directive has no closing parenthesis")
}
