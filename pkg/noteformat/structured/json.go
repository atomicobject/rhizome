package structured

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"
)

type jsonParser struct {
	decoder     *json.Decoder
	source      sourceIndex
	diagnostics []Diagnostic
}

// ParseJSON rejects duplicate keys, invalid UTF-8, and trailing values. Failure
// always returns a nil tree, even when some earlier values were valid.
func ParseJSON(data []byte) (*Value, []Diagnostic) {
	p := jsonParser{decoder: json.NewDecoder(bytes.NewReader(data)), source: indexSource(data, false)}
	p.decoder.UseNumber()
	if !utf8.Valid(data) {
		return nil, []Diagnostic{{Code: "json_parse_error", Message: "invalid UTF-8", Start: p.source.position(0)}}
	}
	root, err := p.value("", 0)
	if err == nil {
		_, err = p.decoder.Token()
		if err == io.EOF {
			err = nil
		} else if err == nil {
			err = fmt.Errorf("trailing JSON value")
		}
	}
	if err != nil {
		offset := p.decoder.InputOffset()
		if e, ok := err.(*json.SyntaxError); ok {
			offset = e.Offset - 1
		}
		p.diagnostics = append(p.diagnostics, Diagnostic{Code: "json_parse_error", Message: err.Error(), Start: p.source.position(int(offset))})
	}
	if len(p.diagnostics) > 0 {
		return nil, p.diagnostics
	}
	return root, nil
}
func (p *jsonParser) token() (json.Token, Position, error) {
	offset := int(p.decoder.InputOffset())
	for offset < len(p.source.data) {
		switch p.source.data[offset] {
		case ' ', '\t', '\r', '\n', ',', ':':
			offset++
		default:
			goto ready
		}
	}
ready:
	t, err := p.decoder.Token()
	if _, ok := t.(string); ok && err == nil {
		err = checkJSONSurrogates(p.source.data[offset:int(p.decoder.InputOffset())])
	}
	return t, p.source.position(offset), err
}
func (p *jsonParser) value(address string, depth int) (*Value, error) {
	if depth > 1000 {
		return nil, fmt.Errorf("nesting exceeds 1000")
	}
	token, start, err := p.token()
	if err != nil {
		return nil, err
	}
	v := &Value{address: address, start: start}
	switch t := token.(type) {
	case nil:
		v.kind = Null
	case bool:
		v.kind = Bool
		v.text = strconv.FormatBool(t)
	case string:
		v.kind = String
		v.text = t
	case json.Number:
		v.kind = Number
		v.text = t.String()
	case json.Delim:
		if t == '[' {
			v.kind = Array
			for p.decoder.More() {
				child, e := p.value(childAddress(address, strconv.Itoa(len(v.items))), depth+1)
				if e != nil {
					return nil, e
				}
				v.items = append(v.items, child)
			}
		} else if t == '{' {
			v.kind = Object
			seen := map[string]Position{}
			for p.decoder.More() {
				key, pos, e := p.token()
				if e != nil {
					return nil, e
				}
				name, ok := key.(string)
				if !ok {
					return nil, fmt.Errorf("expected object key")
				}
				childPath := childAddress(address, name)
				if first, exists := seen[name]; exists {
					p.diagnostics = append(p.diagnostics, Diagnostic{Code: "json_duplicate_key", Message: "duplicate key " + name, Address: childPath, Start: pos, Related: []Position{first}})
				} else {
					seen[name] = pos
				}
				child, e := p.value(childPath, depth+1)
				if e != nil {
					return nil, e
				}
				v.entries = append(v.entries, Entry{name, String, pos, child})
			}
		} else {
			return nil, fmt.Errorf("unexpected delimiter %q", t)
		}
		if _, _, err = p.token(); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unexpected JSON token")
	}
	v.lexeme = v.text
	return v, nil
}
