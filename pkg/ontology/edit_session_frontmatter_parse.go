package ontology

import (
	"strings"

	"gopkg.in/yaml.v3"
)

type frontmatterKeyLine struct {
	line       frontmatterSourceLine
	key        string
	indent     string
	valueStart int
	valueEnd   int
}

func parseFrontmatterKeyLine(line frontmatterSourceLine, rootIndent string) (frontmatterKeyLine, bool) {
	if line.text == "" || strings.TrimSpace(line.text) == "---" || !strings.HasPrefix(line.text, rootIndent) {
		return frontmatterKeyLine{}, false
	}
	indent := line.text[:len(line.text)-len(strings.TrimLeft(line.text, " \t"))]
	if indent != rootIndent || strings.ContainsRune(indent, '\t') {
		return frontmatterKeyLine{}, false
	}
	body := line.text[len(rootIndent):]
	colon := frontmatterKeyColon(body)
	if colon <= 0 || strings.TrimSpace(body[:colon]) == "" {
		return frontmatterKeyLine{}, false
	}
	key, ok := decodeFrontmatterKey(body[:colon])
	if !ok {
		return frontmatterKeyLine{}, false
	}
	valueStart := len(rootIndent) + colon + 1
	for valueStart < len(line.text) && (line.text[valueStart] == ' ' || line.text[valueStart] == '\t') {
		valueStart++
	}
	valueEnd := len(line.text)
	for valueEnd > valueStart && (line.text[valueEnd-1] == ' ' || line.text[valueEnd-1] == '\t') {
		valueEnd--
	}
	return frontmatterKeyLine{
		line:       line,
		key:        key,
		indent:     rootIndent,
		valueStart: line.start + valueStart,
		valueEnd:   line.start + frontmatterInlineValueEnd(line.text, valueStart, valueEnd),
	}, true
}

func frontmatterRootIndent(lines []frontmatterSourceLine) (string, bool) {
	minimum := -1
	for _, line := range lines {
		trimmed := strings.TrimSpace(line.text)
		if trimmed == "" || trimmed == "---" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := line.text[:len(line.text)-len(strings.TrimLeft(line.text, " \t"))]
		if strings.ContainsRune(indent, '\t') {
			return "", false
		}
		if minimum < 0 || len(indent) < minimum {
			minimum = len(indent)
		}
	}
	if minimum < 0 {
		return "", true
	}
	return strings.Repeat(" ", minimum), true
}

func frontmatterLineIndex(lines []frontmatterSourceLine, start int) int {
	for index, line := range lines {
		if line.start == start {
			return index
		}
	}
	return -1
}

func frontmatterKeyColon(line string) int {
	var quote byte
	escaped := false
	for index := 0; index < len(line); index++ {
		char := line[index]
		switch quote {
		case '"':
			if escaped {
				escaped = false
				continue
			}
			if char == '\\' {
				escaped = true
			} else if char == '"' {
				quote = 0
			}
		case '\'':
			if char == '\'' {
				if index+1 < len(line) && line[index+1] == '\'' {
					index++
				} else {
					quote = 0
				}
			}
		default:
			switch char {
			case '"', '\'':
				quote = char
			case ':':
				return index
			}
		}
	}
	return -1
}

func decodeFrontmatterKey(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}
	if trimmed[0] != '\'' && trimmed[0] != '"' {
		return trimmed, !strings.ContainsAny(trimmed, "\r\n")
	}
	var key string
	if err := yaml.Unmarshal([]byte(trimmed), &key); err != nil {
		return "", false
	}
	return key, true
}

func frontmatterInlineValueEnd(line string, start, end int) int {
	quote := byte(0)
	escaped := false
	bracketDepth := 0
	for index := start; index < end; index++ {
		char := line[index]
		if quote == '"' {
			if escaped {
				escaped = false
				continue
			}
			if char == '\\' {
				escaped = true
			} else if char == '"' {
				quote = 0
			}
			continue
		}
		if quote == '\'' {
			if char == '\'' {
				if index+1 < end && line[index+1] == '\'' {
					index++
				} else {
					quote = 0
				}
			}
			continue
		}
		switch char {
		case '\'', '"':
			quote = char
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case '#':
			if bracketDepth == 0 && (index == start || line[index-1] == ' ' || line[index-1] == '\t') {
				for index > start && (line[index-1] == ' ' || line[index-1] == '\t') {
					index--
				}
				return index
			}
		}
	}
	return end
}

func equalFrontmatterKey(key string, candidates []string) bool {
	for _, candidate := range candidates {
		if strings.EqualFold(strings.TrimSpace(key), strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}
