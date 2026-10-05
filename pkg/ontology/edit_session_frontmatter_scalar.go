package ontology

import (
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func frontmatterEmptyValueReplacement(content string, keyLine frontmatterKeyLine, value string) string {
	if keyLine.valueStart == 0 || (content[keyLine.valueStart-1] != ' ' && content[keyLine.valueStart-1] != '\t') {
		value = " " + value
	}
	if keyLine.valueEnd < keyLine.line.end && content[keyLine.valueEnd] == '#' {
		value += " "
	}
	return value
}

func frontmatterHasIndentedContinuation(lines []frontmatterSourceLine, lineIndex int, keyIndent string) bool {
	for index := lineIndex + 1; index < len(lines); index++ {
		line := lines[index]
		trimmed := strings.TrimSpace(line.text)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := line.text[:len(line.text)-len(strings.TrimLeft(line.text, " \t"))]
		return len(indent) > len(keyIndent)
	}
	return false
}

func frontmatterHasUnsupportedBlockScalarContinuation(lines []frontmatterSourceLine, lineIndex int, keyIndent string) bool {
	for index := lineIndex + 1; index < len(lines); index++ {
		line := lines[index]
		trimmed := strings.TrimSpace(line.text)
		if trimmed == "" {
			continue
		}
		indent := line.text[:len(line.text)-len(strings.TrimLeft(line.text, " \t"))]
		if len(indent) <= len(keyIndent) {
			return false
		}
		if strings.ContainsRune(indent, '\t') {
			return true
		}
	}
	return false
}

func frontmatterInlineNode(value string) (*yaml.Node, bool) {
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(value), &document); err != nil || document.Kind != yaml.DocumentNode || len(document.Content) != 1 || document.Content[0] == nil {
		return nil, false
	}
	return document.Content[0], true
}

func frontmatterInlineScalarSupported(value string) bool {
	if strings.TrimSpace(value) == "" {
		return true
	}
	node, ok := frontmatterInlineNode(value)
	return ok && node.Kind == yaml.ScalarNode && node.Alias == nil && node.Anchor == ""
}

func frontmatterInlineListSupported(value string) bool {
	node, ok := frontmatterInlineNode(value)
	if !ok || node.Kind != yaml.SequenceNode || node.Alias != nil || node.Anchor != "" {
		return false
	}
	for _, item := range node.Content {
		if item == nil || item.Kind != yaml.ScalarNode || item.Alias != nil || item.Anchor != "" {
			return false
		}
	}
	return true
}

func frontmatterPlainScalarSafe(value string, stringLike bool) bool {
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\t") {
		return false
	}
	if strings.ContainsAny(value, "[],{}") {
		return false
	}
	node, ok := frontmatterInlineNode(value)
	if !ok || node.Kind != yaml.ScalarNode || node.Value != value || node.Alias != nil || node.Anchor != "" {
		return false
	}
	return !stringLike || node.Tag == "!!str"
}

func frontmatterStringLikeField(field *Field, asLinks bool) bool {
	if asLinks || field == nil {
		return true
	}
	if field.Kind == FieldKindEnum || field.Kind == FieldKindLink {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(field.TypeName)) {
	case "string", "id", "date", "datetime", "url", "uri", "json":
		return true
	default:
		return false
	}
}

func isFrontmatterBlockScalar(value string) bool {
	trimmed := strings.TrimSpace(value)
	return strings.HasPrefix(trimmed, "|") || strings.HasPrefix(trimmed, ">")
}

func patchFrontmatterBlockScalar(content string, lines []frontmatterSourceLine, keyLine frontmatterKeyLine, field *Field, values []string, asLinks bool) string {
	lineIndex := frontmatterLineIndex(lines, keyLine.line.start)
	if lineIndex < 0 {
		return content
	}
	marker := literalBlockScalarMarker(strings.TrimSpace(content[keyLine.valueStart:keyLine.valueEnd]))
	indent := frontmatterBlockScalarIndent(marker, keyLine.indent, lines, lineIndex)
	endIndex := frontmatterBlockScalarEndIndex(lines, lineIndex, indent)
	end := frontmatterLineEndWithBreak(content, keyLine.line)
	if endIndex > lineIndex+1 {
		end = frontmatterLineEndWithBreak(content, lines[endIndex-1])
	}
	value := valuesFirst(values)
	if asLinks {
		value = strings.TrimSpace(value)
	}
	suffix := content[keyLine.valueEnd:keyLine.line.end]
	lineEnding := frontmatterLineEnding(content, frontmatterRange(content))
	var b strings.Builder
	b.WriteString(marker)
	b.WriteString(suffix)
	b.WriteString(lineEnding)
	for _, line := range strings.Split(value, "\n") {
		b.WriteString(indent)
		b.WriteString(line)
		b.WriteString(lineEnding)
	}
	_ = field
	return replaceRange(content, ByteRange{Start: keyLine.valueStart, End: end}, b.String())
}

func frontmatterBlockScalarEndIndex(lines []frontmatterSourceLine, lineIndex int, contentIndent string) int {
	endIndex := lineIndex + 1
	for endIndex < len(lines) {
		line := lines[endIndex]
		if strings.TrimSpace(line.text) != "" {
			lineIndent := line.text[:len(line.text)-len(strings.TrimLeft(line.text, " \t"))]
			if len(lineIndent) < len(contentIndent) {
				break
			}
		}
		endIndex++
	}
	return endIndex
}

func frontmatterBlockScalarIndent(marker, keyIndent string, lines []frontmatterSourceLine, lineIndex int) string {
	for _, character := range marker[1:] {
		if character >= '1' && character <= '9' {
			return keyIndent + strings.Repeat(" ", int(character-'0'))
		}
	}
	for index := lineIndex + 1; index < len(lines); index++ {
		line := lines[index]
		if strings.TrimSpace(line.text) == "" {
			continue
		}
		indent := line.text[:len(line.text)-len(strings.TrimLeft(line.text, " \t"))]
		if len(indent) > len(keyIndent) {
			return indent
		}
		break
	}
	return keyIndent + "  "
}

func literalBlockScalarMarker(marker string) string {
	if !strings.HasPrefix(marker, ">") {
		return marker
	}
	return "|" + strings.TrimPrefix(marker, ">")
}

func renderFrontmatterValue(existing string, field *Field, values []string, asLinks bool) string {
	if field.List {
		return renderFrontmatterFlowList(field, values, asLinks)
	}
	return renderFrontmatterScalar(valuesFirst(values), existing, asLinks, field)
}

func renderFrontmatterFlowList(field *Field, values []string, asLinks bool) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, renderFrontmatterScalar(value, "", asLinks, field))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func valuesFirst(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func renderFrontmatterScalar(value, existing string, asLinks bool, field *Field) string {
	if asLinks {
		value = strings.TrimSpace(value)
	}
	trimmed := strings.TrimSpace(existing)
	switch {
	case strings.HasPrefix(trimmed, "\"") && strings.HasSuffix(trimmed, "\"") && !strings.ContainsAny(value, "\r\n"):
		return strconv.Quote(value)
	case strings.HasPrefix(trimmed, "'") && strings.HasSuffix(trimmed, "'") && !strings.ContainsAny(value, "\r\n"):
		return "'" + strings.ReplaceAll(value, "'", "''") + "'"
	case frontmatterPlainScalarSafe(value, frontmatterStringLikeField(field, asLinks)):
		return value
	default:
		return strconv.Quote(value)
	}
}
