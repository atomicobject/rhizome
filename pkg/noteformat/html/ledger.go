package html

import (
	"bytes"
	stdhtml "html"
	"io"
	"strings"

	xhtml "golang.org/x/net/html"
)

// htmlToken is one original tokenizer token. start/end are offsets into the
// sealed authored byte slice, and index is stable for cross-token correlation.
// The raw bytes are intentionally not stored: all source evidence is read from
// the caller-owned immutable input using the ledger's offsets.
type htmlToken struct {
	typ            xhtml.TokenType
	index          int
	start          int
	end            int
	tag            string
	data           string
	attrs          []attribute
	duplicateAttrs bool
}

func tokenizeSource(source []byte) ([]htmlToken, error) {
	tokenizer := xhtml.NewTokenizer(bytes.NewReader(source))
	tokens := make([]htmlToken, 0)
	offset := 0
	for {
		typ := tokenizer.Next()
		if typ == xhtml.ErrorToken {
			if err := tokenizer.Err(); err != nil && err != io.EOF {
				return tokens, err
			}
			break
		}
		raw := append([]byte(nil), tokenizer.Raw()...)
		if len(raw) == 0 {
			continue
		}
		token := htmlToken{typ: typ, index: len(tokens), start: offset, end: offset + len(raw)}
		switch typ {
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			name, _ := tokenizer.TagName()
			token.tag = string(name)
			parsed := parseStartTagAttributes(source, token.start, token.end)
			token.attrs = parsed.attrs
			token.duplicateAttrs = parsed.duplicateAttrs
		case xhtml.EndTagToken:
			name, _ := tokenizer.TagName()
			token.tag = string(name)
		case xhtml.TextToken, xhtml.CommentToken, xhtml.DoctypeToken:
			token.data = tokenizer.Token().Data
		}
		tokens = append(tokens, token)
		offset = token.end
	}
	return tokens, nil
}

type startTagAttributes struct {
	attrs          []attribute
	duplicateAttrs bool
}

func parseStartTagAttributes(source []byte, start, limit int) startTagAttributes {
	parsed := startTagAttributes{}
	if start < 0 || start >= len(source) || limit < start || limit > len(source) || source[start] != '<' {
		return parsed
	}
	index := start + 1
	for index < limit && isSpace(source[index]) {
		index++
	}
	nameStart := index
	for index < limit && isNameByte(source[index]) {
		index++
	}
	if nameStart == index {
		return parsed
	}
	seen := make(map[string]bool)
	for index < limit {
		for index < limit && isSpace(source[index]) {
			index++
		}
		if index >= limit {
			return parsed
		}
		if source[index] == '>' {
			return parsed
		}
		if source[index] == '/' {
			index++
			continue
		}
		attrStart := index
		for index < limit && isAttrByte(source[index]) {
			index++
		}
		if attrStart == index {
			index++
			continue
		}
		name := strings.ToLower(string(source[attrStart:index]))
		for index < limit && isSpace(source[index]) {
			index++
		}
		valueStart, valueEnd := index, index
		if index < limit && source[index] == '=' {
			index++
			for index < limit && isSpace(source[index]) {
				index++
			}
			valueStart = index
			if index < limit && (source[index] == '\'' || source[index] == '"') {
				quote := source[index]
				index++
				valueStart = index
				for index < limit && source[index] != quote {
					index++
				}
				valueEnd = index
				if index < limit {
					index++
				}
			} else {
				for index < limit && !isSpace(source[index]) && source[index] != '>' {
					index++
				}
				valueEnd = index
			}
		}
		if seen[name] {
			parsed.duplicateAttrs = true
			continue
		}
		seen[name] = true
		parsed.attrs = append(parsed.attrs, attribute{name: name, rawValue: string(source[valueStart:valueEnd]), value: stdhtml.UnescapeString(string(source[valueStart:valueEnd])), valueFrom: valueStart, valueTo: valueEnd})
	}
	return parsed
}

func isNameByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == ':' || value == '-' || value == '_'
}

func isAttrByte(value byte) bool {
	return isNameByte(value) || value == '.'
}

func isSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r' || value == '\f'
}
