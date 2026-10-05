package sectionparse

import (
	"fmt"
	"strings"
	"unicode"
)

// Node is a source-span section parsed from markdown headings.
type Node struct {
	ID               string
	NotePath         string
	Title            string
	Level            int
	BlockID          string
	MalformedBlockID string
	StartByte        int
	EndByte          int
	Content          string
	Children         []*Node
}

// Parse returns source-spanned markdown heading sections, excluding headings
// inside fenced code blocks.
func Parse(notePath, content string) []*Node {
	lines := strings.Split(content, "\n")
	lineOffsets := make([]int, len(lines)+1)
	for i := 0; i < len(lines); i++ {
		lineOffsets[i+1] = lineOffsets[i] + len(lines[i]) + 1
	}

	type rawSection struct {
		title            string
		level            int
		startByte        int
		endByte          int
		blockID          string
		malformedBlockID string
	}

	raw := make([]rawSection, 0)
	active := make([]int, 0)
	inCode := false
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			inCode = !inCode
			continue
		}
		if inCode {
			continue
		}
		leadingSpaces := countLeadingSpaces(line)
		if leadingSpaces > 3 {
			continue
		}
		left := strings.TrimLeft(line, " ")
		if len(active) > 0 {
			identifierLine := trim
			listItem := false
			if content, ok := sectionListItemContent(trim); ok {
				identifierLine = content
				listItem = true
			}
			if candidate, malformed, ok := proseBlockIDCandidate(trim); ok && !listItem {
				current := active[len(active)-1]
				if malformed {
					if raw[current].malformedBlockID == "" {
						raw[current].malformedBlockID = candidate
					}
					continue
				}
				if raw[current].blockID == "" {
					raw[current].blockID = candidate
				}
			}
			if candidate, ok := inlineIdentifierBlockID(identifierLine); ok {
				current := active[len(active)-1]
				if !validBlockID(candidate) {
					if raw[current].malformedBlockID == "" {
						raw[current].malformedBlockID = candidate
					}
					continue
				}
				if raw[current].blockID == "" {
					raw[current].blockID = candidate
				}
			}
		}
		if !strings.HasPrefix(left, "#") {
			continue
		}
		hashes := 0
		for hashes < len(left) && left[hashes] == '#' {
			hashes++
		}
		title := normalizeATXHeadingText(left[hashes:])
		if title == "" || hashes < 1 || hashes > 6 {
			continue
		}
		headingBlockID := ""
		if candidate, _, ok := proseBlockIDCandidate(title); ok {
			headingBlockID = candidate
		}
		startByte := lineOffsets[i]
		if len(raw) > 0 {
			for idx := len(raw) - 1; idx >= 0; idx-- {
				if raw[idx].endByte != 0 {
					continue
				}
				if raw[idx].level >= hashes {
					raw[idx].endByte = startByte
					continue
				}
				break
			}
		}
		for len(active) > 0 && raw[active[len(active)-1]].level >= hashes {
			active = active[:len(active)-1]
		}
		raw = append(raw, rawSection{
			title:            title,
			level:            hashes,
			startByte:        startByte,
			malformedBlockID: headingBlockID,
		})
		active = append(active, len(raw)-1)
	}
	for i := range raw {
		if raw[i].endByte == 0 {
			raw[i].endByte = len(content)
		}
	}
	if len(raw) == 0 {
		return nil
	}

	root := make([]*Node, 0)
	stack := make([]*Node, 0)
	for _, item := range raw {
		node := &Node{
			ID:               fmt.Sprintf("%s#%s-%d", notePath, slugifyHeading(item.title), item.startByte),
			NotePath:         notePath,
			Title:            item.title,
			Level:            item.level,
			BlockID:          item.blockID,
			MalformedBlockID: item.malformedBlockID,
			StartByte:        item.startByte,
			EndByte:          item.endByte,
			Content:          substringBytes(content, item.startByte, item.endByte),
		}
		if item.blockID != "" {
			node.ID = fmt.Sprintf("%s#^%s", notePath, item.blockID)
		}
		for len(stack) > 0 && stack[len(stack)-1].Level >= node.Level {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			root = append(root, node)
		} else {
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, node)
		}
		stack = append(stack, node)
	}
	return root
}

func sectionListItemContent(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if len(line) >= 2 && (line[0] == '-' || line[0] == '*' || line[0] == '+') && (line[1] == ' ' || line[1] == '\t') {
		return strings.TrimSpace(line[2:]), true
	}
	index := 0
	for index < len(line) && line[index] >= '0' && line[index] <= '9' {
		index++
	}
	if index == 0 || index+1 >= len(line) || (line[index] != '.' && line[index] != ')') || (line[index+1] != ' ' && line[index+1] != '\t') {
		return "", false
	}
	return strings.TrimSpace(line[index+2:]), true
}

// UnwrapSingleH1Root returns children of a lone H1 wrapper when present.
func UnwrapSingleH1Root(nodes []*Node) []*Node {
	if len(nodes) != 1 {
		return nodes
	}
	only := nodes[0]
	if only == nil || only.Level != 1 || len(only.Children) == 0 {
		return nodes
	}
	return only.Children
}

func countLeadingSpaces(s string) int {
	count := 0
	for _, r := range s {
		if r != ' ' {
			return count
		}
		count++
	}
	return count
}

func normalizeATXHeadingText(raw string) string {
	title := strings.TrimSpace(raw)
	if title == "" {
		return ""
	}
	end := len(title)
	for end > 0 && title[end-1] == '#' {
		end--
	}
	if end < len(title) {
		trimmed := strings.TrimRight(title[:end], " \t")
		if trimmed == "" || len(trimmed) != len(title[:end]) {
			title = trimmed
		}
	}
	return strings.TrimSpace(title)
}

func proseBlockIDCandidate(trimmed string) (string, bool, bool) {
	clean := stripInlineCodeSpans(trimmed)
	trim := strings.TrimSpace(clean)
	if strings.HasPrefix(trim, "^") {
		rest := strings.TrimSpace(strings.TrimPrefix(trim, "^"))
		if rest == "" {
			return "", false, false
		}
		if validBlockID(rest) {
			return rest, false, true
		}
		return rest, true, true
	}
	for i := 0; i < len(clean); i++ {
		if clean[i] != '^' {
			continue
		}
		if i > 0 && !isBlockIDBoundary(rune(clean[i-1])) {
			continue
		}
		token := firstBlockIDToken(clean[i+1:])
		if token == "" || !validBlockID(token) {
			continue
		}
		return token, false, true
	}
	return "", false, false
}

func inlineIdentifierBlockID(line string) (string, bool) {
	line = stripInlineCodeSpans(line)
	idx := strings.Index(line, "::")
	if idx <= 0 {
		return "", false
	}
	key := strings.TrimSpace(line[:idx])
	value := strings.TrimSpace(line[idx+2:])
	if key == "" || !strings.HasPrefix(value, "^") {
		return "", false
	}
	candidate := strings.TrimSpace(strings.TrimPrefix(value, "^"))
	if candidate == "" {
		return "", false
	}
	return candidate, true
}

func validBlockID(id string) bool {
	for _, r := range id {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return id != ""
}

func stripInlineCodeSpans(line string) string {
	var b strings.Builder
	inCode := false
	for _, r := range line {
		if r == '`' {
			inCode = !inCode
			b.WriteRune(' ')
			continue
		}
		if inCode {
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func firstBlockIDToken(s string) string {
	s = strings.TrimLeft(s, " \t")
	if s == "" {
		return ""
	}
	end := 0
	for end < len(s) {
		r := rune(s[end])
		if isBlockIDBoundary(r) {
			break
		}
		end++
	}
	return strings.TrimRight(s[:end], ".,;:)")
}

func isBlockIDBoundary(r rune) bool {
	return unicode.IsSpace(r) || r == '(' || r == '[' || r == '{'
}

func slugifyHeading(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func substringBytes(s string, start, end int) string {
	if start < 0 {
		start = 0
	}
	if end > len(s) {
		end = len(s)
	}
	if start >= end {
		return ""
	}
	return s[start:end]
}
