package structured

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

type yamlParser struct {
	source      sourceIndex
	diagnostics []Diagnostic
}

var yamlErrorLine = regexp.MustCompile(`line ([0-9]+)`)

// ParseYAML parses exactly one document with the versioned SafeLoader profile.
// Unsupported syntax is inspected as nodes, never decoded or dereferenced.
func ParseYAML(data []byte) (*Value, []Diagnostic) {
	p := yamlParser{source: indexSource(data, true)}
	if !utf8.Valid(data) {
		return nil, []Diagnostic{{Code: "yaml_parse_error", Message: "invalid UTF-8", Start: p.source.position(0)}}
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	err := decoder.Decode(&doc)
	if err == io.EOF {
		return &Value{kind: Null, start: p.source.position(len(data))}, nil
	}
	if err != nil {
		return nil, []Diagnostic{p.parseError(err)}
	}
	root := p.value(doc.Content[0], "", 0)
	var second yaml.Node
	if err = decoder.Decode(&second); err == nil {
		pos := p.source.at(second.Line, second.Column)
		p.diagnostics = append(p.diagnostics, Diagnostic{Code: "yaml_multiple_documents", Message: "multiple YAML documents", Start: pos})
	} else if err != io.EOF {
		p.diagnostics = append(p.diagnostics, p.parseError(err))
	}
	if len(p.diagnostics) > 0 {
		return nil, p.diagnostics
	}
	return root, nil
}
func (p *yamlParser) parseError(err error) Diagnostic {
	d := Diagnostic{Code: "yaml_parse_error", Message: err.Error()}
	if m := yamlErrorLine.FindStringSubmatch(err.Error()); len(m) > 0 {
		line, _ := strconv.Atoi(m[1])
		d.Start = p.source.at(line, 1)
	}
	return d
}
func (p *yamlParser) issue(code string, n *yaml.Node, address string) {
	p.diagnostics = append(p.diagnostics, Diagnostic{Code: code, Message: strings.ReplaceAll(code, "_", " "), Address: address, Start: p.source.at(n.Line, n.Column)})
}
func (p *yamlParser) syntax(n *yaml.Node, address string) bool {
	bad := false
	if n.Style&yaml.TaggedStyle != 0 {
		p.issue("yaml_tag_unsupported", n, address)
		bad = true
	}
	if n.Anchor != "" {
		p.issue("yaml_anchor_unsupported", n, address)
		bad = true
	}
	if n.Kind == yaml.AliasNode {
		p.issue("yaml_alias_unsupported", n, address)
		bad = true
	}
	return bad
}
func (p *yamlParser) value(n *yaml.Node, address string, depth int) *Value {
	if depth > 1000 {
		p.issue("yaml_parse_error", n, address)
		return nil
	}
	badSyntax := p.syntax(n, address)
	if badSyntax && n.Kind == yaml.AliasNode {
		return nil
	}
	v := &Value{address: address, start: p.source.at(n.Line, n.Column), lexeme: n.Value}
	switch n.Kind {
	case yaml.ScalarNode:
		var err error
		v.kind, v.text, err = scalar(n)
		if err != nil {
			p.issue("yaml_scalar_unsupported", n, address)
			return nil
		}
	case yaml.SequenceNode:
		v.kind = Array
		for i, c := range n.Content {
			v.items = append(v.items, p.value(c, childAddress(address, strconv.Itoa(i)), depth+1))
		}
	case yaml.MappingNode:
		v.kind = Object
		seen := map[string]Position{}
		for i := 0; i < len(n.Content); i += 2 {
			key := n.Content[i]
			child := n.Content[i+1]
			if p.syntax(key, address) {
				continue
			}
			if key.Kind != yaml.ScalarNode {
				p.issue("yaml_complex_key_unsupported", key, address)
				continue
			}
			if key.Value == "<<" && key.Style == 0 {
				p.issue("yaml_merge_key_unsupported", key, address)
				continue
			}
			kind, text, err := scalar(key)
			if err != nil {
				p.issue("yaml_scalar_unsupported", key, address)
				continue
			}
			if kind != String && !(kind == Number && integerText.MatchString(text)) {
				p.issue("yaml_key_type_unsupported", key, address)
				continue
			}
			path := childAddress(address, text)
			pos := p.source.at(key.Line, key.Column)
			if first, ok := seen[text]; ok {
				p.diagnostics = append(p.diagnostics, Diagnostic{Code: "yaml_duplicate_key", Message: fmt.Sprintf("duplicate key %q", text), Address: path, Start: pos, Related: []Position{first}})
			} else {
				seen[text] = pos
			}
			v.entries = append(v.entries, Entry{text, kind, pos, p.value(child, path, depth+1)})
		}
	default:
		p.issue("yaml_parse_error", n, address)
		return nil
	}
	return v
}
