package obsidian

import (
	"strings"
	"unicode"
)

// InlineProperty is one Dataview-style inline property in authored source
// order. It intentionally carries only the parser's semantic text; consumers
// that need editable spans must use a parser that can attest those ranges.
type InlineProperty struct {
	Key   string
	Value string
}

// ExtractInlineProperties collects Dataview-style inline properties (Key:: Value) from markdown content outside frontmatter.
// Returns a map of property name to list of raw string values.
func ExtractInlineProperties(content string) map[string][]string {
	result := make(map[string][]string)
	for _, property := range ExtractInlinePropertyOccurrences(content) {
		result[property.Key] = append(result[property.Key], property.Value)
	}
	return result
}

// ExtractInlinePropertyOccurrences collects the same properties as
// ExtractInlineProperties while preserving their authored order. The map API
// remains for existing grouped consumers.
func ExtractInlinePropertyOccurrences(content string) []InlineProperty {
	var result []InlineProperty
	lines := strings.Split(content, "\n")
	paragraph := make([]string, 0, 4)

	inFrontmatter := false
	inCodeFence := false
	flushParagraph := func() {
		if len(paragraph) == 0 {
			return
		}
		extractInlinePropertiesFromParagraph(strings.Join(paragraph, "\n"), &result)
		paragraph = paragraph[:0]
	}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if i == 0 && strings.HasPrefix(trimmed, "---") {
			flushParagraph()
			inFrontmatter = true
			continue
		}
		if inFrontmatter {
			if strings.TrimSpace(trimmed) == "---" {
				inFrontmatter = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			flushParagraph()
			inCodeFence = !inCodeFence
			continue
		}
		if inCodeFence {
			continue
		}
		if trimmed == "" {
			flushParagraph()
			continue
		}
		if shouldSkipInlinePropertyLine(trimmed) {
			flushParagraph()
			continue
		}
		paragraph = append(paragraph, trimmed)
	}
	flushParagraph()

	return result
}

func extractInlinePropertiesFromParagraph(paragraph string, result *[]InlineProperty) {
	text := strings.TrimSpace(paragraph)
	if text == "" {
		return
	}
	_, key, valueStart, ok := nextInlinePropertyToken(text, 0)
	for ok {
		valueEnd := len(text)
		nextStart, nextKey, nextValueStart, nextOK := nextInlinePropertyToken(text, valueStart)
		if nextOK {
			valueEnd = nextStart
		}
		val := normalizeInlinePropertyValue(text[valueStart:valueEnd])
		if val != "" {
			*result = append(*result, InlineProperty{Key: key, Value: val})
		}
		key, valueStart, ok = nextKey, nextValueStart, nextOK
	}
}

func shouldSkipInlinePropertyLine(trimmed string) bool {
	if strings.HasPrefix(trimmed, "#") {
		return true
	}
	if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "+ ") {
		return true
	}
	if len(trimmed) > 1 && trimmed[0] >= '0' && trimmed[0] <= '9' && strings.Contains(trimmed, ". ") {
		return true
	}
	if strings.HasPrefix(trimmed, "|") || strings.HasPrefix(trimmed, ">") {
		return true
	}
	return false
}

func normalizeInlinePropertyValue(raw string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
}

func nextInlinePropertyToken(text string, from int) (int, string, int, bool) {
	search := from
	for search < len(text) {
		for search < len(text) && isInlineWhitespace(text[search]) {
			search++
		}
		if search >= len(text) {
			return 0, "", 0, false
		}
		sepRel := strings.Index(text[search:], "::")
		if sepRel < 0 {
			return 0, "", 0, false
		}
		sep := search + sepRel
		for start := sep - 1; start >= search; start-- {
			if start > search && !isInlineWhitespace(text[start-1]) {
				continue
			}
			if from > 0 && sep+2 < len(text) && !isInlineWhitespace(text[sep+2]) && !isInlineLineStart(text, start) {
				continue
			}
			key := text[start:sep]
			if isValidInlineKey(key) {
				return start, key, sep + 2, true
			}
		}
		search = sep + 2
	}
	return 0, "", 0, false
}

func isInlineWhitespace(ch byte) bool {
	switch ch {
	case ' ', '\n', '\r', '\t':
		return true
	default:
		return false
	}
}

func isInlineLineStart(text string, idx int) bool {
	if idx <= 0 {
		return true
	}
	switch text[idx-1] {
	case '\n', '\r':
		return true
	default:
		return false
	}
}

func isValidInlineKey(key string) bool {
	runes := []rune(key)
	if len(runes) == 0 {
		return false
	}
	first := runes[0]
	if !unicode.IsLetter(first) && !unicode.IsDigit(first) {
		return false
	}
	for _, r := range runes {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}
