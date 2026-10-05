package ontology

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

func appendFrontmatterFieldSource(content string, fmRange ByteRange, rootIndent string, field *Field, values []string, asLinks bool) string {
	key := field.Source
	if strings.TrimSpace(key) == "" {
		key = field.Name
	}
	value := renderFrontmatterValue("", field, values, asLinks)
	lines := frontmatterSourceLines(content, fmRange)
	if len(lines) == 0 {
		return content
	}
	// The frontmatter range also swallows blank lines after the closing
	// delimiter, so find the delimiter itself.
	closingIndex := len(lines) - 1
	for closingIndex > 0 && strings.TrimSpace(lines[closingIndex].text) != "---" {
		closingIndex--
	}
	closing := lines[closingIndex].start
	return content[:closing] + rootIndent + key + ": " + value + frontmatterLineEnding(content, fmRange) + content[closing:]
}

func patchFrontmatterBlockList(content string, lines []frontmatterSourceLine, keyLine frontmatterKeyLine, field *Field, values []string, asLinks bool) (string, error) {
	lineIndex := frontmatterLineIndex(lines, keyLine.line.start)
	if lineIndex < 0 {
		return content, fmt.Errorf("unsupported target: invalid frontmatter list span for %s", field.Name)
	}
	info, err := frontmatterBlockListInfo(lines, lineIndex)
	if err != nil {
		return content, err
	}
	updated := content
	if len(values) == 0 {
		for index := len(info.itemLines) - 1; index >= 0; index-- {
			line := info.itemLines[index]
			updated = replaceRange(updated, frontmatterLineRange(content, line), "")
		}
		return replaceRange(updated, ByteRange{Start: keyLine.valueStart, End: keyLine.valueEnd}, frontmatterEmptyValueReplacement(updated, keyLine, "[]")), nil
	}

	indent := info.indent
	if len(info.itemLines) > 0 {
		first := info.itemLines[0]
		for index := len(info.itemLines) - 1; index >= 1; index-- {
			line := info.itemLines[index]
			updated = replaceRange(updated, frontmatterLineRange(content, line), "")
		}
		end := frontmatterLineEndWithBreak(updated, first)
		updated = replaceRange(updated, ByteRange{Start: first.start, End: end}, renderFrontmatterBlockItems(indent, field, values, asLinks, frontmatterLineEnding(content, frontmatterRange(content))))
		return updated, nil
	}

	start := frontmatterLineEndWithBreak(content, keyLine.line)
	return replaceRange(updated, ByteRange{Start: start, End: start}, renderFrontmatterBlockItems(indent, field, values, asLinks, frontmatterLineEnding(content, frontmatterRange(content)))), nil
}

type frontmatterBlockList struct {
	itemLines []frontmatterSourceLine
	indent    string
}

func frontmatterBlockListInfo(lines []frontmatterSourceLine, lineIndex int) (frontmatterBlockList, error) {
	keyIndent := lines[lineIndex].text[:len(lines[lineIndex].text)-len(strings.TrimLeft(lines[lineIndex].text, " \t"))]
	info := frontmatterBlockList{indent: keyIndent + "  "}
	for index := lineIndex + 1; index < len(lines); index++ {
		line := lines[index]
		trimmed := strings.TrimSpace(line.text)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := line.text[:len(line.text)-len(strings.TrimLeft(line.text, " \t"))]
		if len(indent) <= len(keyIndent) {
			break
		}
		if strings.ContainsRune(indent, '\t') {
			return info, fmt.Errorf("unsupported target: tab-indented frontmatter list")
		}
		if !frontmatterListItem(trimmed) {
			return info, fmt.Errorf("unsupported target: unsupported frontmatter list span")
		}
		itemValue := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
		if itemValue != "" {
			node, ok := frontmatterInlineNode(itemValue)
			if !ok || node.Kind != yaml.ScalarNode || node.Alias != nil || node.Anchor != "" {
				return info, fmt.Errorf("unsupported target: unsupported frontmatter list item")
			}
		}
		if len(info.itemLines) == 0 {
			info.indent = indent
		} else if indent != info.indent {
			return info, fmt.Errorf("unsupported target: inconsistent frontmatter list indentation")
		}
		info.itemLines = append(info.itemLines, line)
	}
	return info, nil
}

func frontmatterListItem(value string) bool {
	if !strings.HasPrefix(value, "-") {
		return false
	}
	return len(value) == 1 || value[1] == ' ' || value[1] == '\t'
}

func renderFrontmatterBlockItems(indent string, field *Field, values []string, asLinks bool, lineEnding string) string {
	var b strings.Builder
	for _, value := range values {
		b.WriteString(indent)
		b.WriteString("- ")
		b.WriteString(renderFrontmatterScalar(value, "", asLinks, field))
		b.WriteString(lineEnding)
	}
	return b.String()
}

func frontmatterLineRange(content string, line frontmatterSourceLine) ByteRange {
	return ByteRange{Start: line.start, End: frontmatterLineEndWithBreak(content, line)}
}

func frontmatterLineEndWithBreak(content string, line frontmatterSourceLine) int {
	end := line.end
	if end < len(content) && content[end] == '\r' {
		end++
	}
	if end < len(content) && content[end] == '\n' {
		end++
	}
	return end
}

func frontmatterLineEnding(content string, sourceRange ByteRange) string {
	if sourceRange.Valid(len(content)) && strings.Contains(content[sourceRange.Start:sourceRange.End], "\r\n") {
		return "\r\n"
	}
	return "\n"
}
