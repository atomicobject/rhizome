package html

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type jsonMember struct {
	key                  string
	value                any
	keyStart, keyEnd     int
	valueStart, valueEnd int
}

type jsonObject struct{ members []jsonMember }

type jsonMetadataParser struct {
	data []byte
	pos  int
}

func parseJSONMetadata(data []byte) (jsonObject, error) {
	parser := jsonMetadataParser{data: data}
	value, _, err := parser.value()
	if err != nil {
		return jsonObject{}, err
	}
	parser.space()
	if parser.pos != len(data) {
		return jsonObject{}, fmt.Errorf("canonical metadata has trailing JSON")
	}
	object, ok := value.(jsonObject)
	if !ok {
		return jsonObject{}, fmt.Errorf("canonical metadata must be a JSON object")
	}
	return object, nil
}

func (p *jsonMetadataParser) value() (any, int, error) {
	p.space()
	start := p.pos
	if p.pos >= len(p.data) {
		return nil, start, fmt.Errorf("canonical metadata has an incomplete JSON value")
	}
	switch p.data[p.pos] {
	case '{':
		return p.object(start)
	case '[':
		return p.array(start)
	case '"':
		value, err := p.string()
		return value, start, err
	case 't':
		return p.literal("true", true, start)
	case 'f':
		return p.literal("false", false, start)
	case 'n':
		return p.literal("null", nil, start)
	default:
		return p.number(start)
	}
}

func (p *jsonMetadataParser) object(start int) (any, int, error) {
	p.pos++
	object := jsonObject{}
	seen := make(map[string]bool)
	p.space()
	if p.take('}') {
		return object, start, nil
	}
	for {
		p.space()
		keyStart := p.pos
		key, err := p.string()
		if err != nil {
			return nil, start, err
		}
		keyEnd := p.pos
		if seen[key] {
			return nil, start, fmt.Errorf("canonical metadata has duplicate key %q", key)
		}
		seen[key] = true
		p.space()
		if !p.take(':') {
			return nil, start, fmt.Errorf("canonical metadata object is missing a colon")
		}
		p.space()
		valueStart := p.pos
		value, _, err := p.value()
		if err != nil {
			return nil, start, err
		}
		valueEnd := p.pos
		object.members = append(object.members, jsonMember{key: key, value: value, keyStart: keyStart, keyEnd: keyEnd, valueStart: valueStart, valueEnd: valueEnd})
		p.space()
		if p.take('}') {
			return object, start, nil
		}
		if !p.take(',') {
			return nil, start, fmt.Errorf("canonical metadata object is missing a comma")
		}
	}
}

func (p *jsonMetadataParser) array(start int) (any, int, error) {
	p.pos++
	values := make([]any, 0)
	p.space()
	if p.take(']') {
		return values, start, nil
	}
	for {
		value, _, err := p.value()
		if err != nil {
			return nil, start, err
		}
		values = append(values, value)
		p.space()
		if p.take(']') {
			return values, start, nil
		}
		if !p.take(',') {
			return nil, start, fmt.Errorf("canonical metadata array is missing a comma")
		}
	}
}

func (p *jsonMetadataParser) string() (string, error) {
	start := p.pos
	if !p.take('"') {
		return "", fmt.Errorf("canonical metadata expected a JSON string")
	}
	escaped := false
	for p.pos < len(p.data) {
		value := p.data[p.pos]
		p.pos++
		if escaped {
			escaped = false
			continue
		}
		if value == '\\' {
			escaped = true
		} else if value == '"' {
			var result string
			if err := json.Unmarshal(p.data[start:p.pos], &result); err != nil {
				return "", fmt.Errorf("canonical metadata has invalid JSON string")
			}
			return result, nil
		}
	}
	return "", fmt.Errorf("canonical metadata has an unterminated JSON string")
}

func (p *jsonMetadataParser) literal(literal string, value any, start int) (any, int, error) {
	if !bytes.HasPrefix(p.data[p.pos:], []byte(literal)) {
		return nil, start, fmt.Errorf("canonical metadata has invalid JSON literal")
	}
	p.pos += len(literal)
	return value, start, nil
}

func (p *jsonMetadataParser) number(start int) (any, int, error) {
	for p.pos < len(p.data) && !bytes.ContainsRune([]byte(" \t\r\n,]}"), rune(p.data[p.pos])) {
		p.pos++
	}
	if p.pos == start {
		return nil, start, fmt.Errorf("canonical metadata has an invalid JSON value")
	}
	raw := string(p.data[start:p.pos])
	var number json.Number
	if err := json.Unmarshal([]byte(raw), &number); err != nil {
		return nil, start, fmt.Errorf("canonical metadata has invalid JSON number")
	}
	return number, start, nil
}

func (p *jsonMetadataParser) space() {
	for p.pos < len(p.data) && (p.data[p.pos] == ' ' || p.data[p.pos] == '\t' || p.data[p.pos] == '\r' || p.data[p.pos] == '\n') {
		p.pos++
	}
}

func (p *jsonMetadataParser) take(value byte) bool {
	if p.pos >= len(p.data) || p.data[p.pos] != value {
		return false
	}
	p.pos++
	return true
}
