package ontology

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func validateEditFieldValues(field *Field, values []string, schema *Schema) error {
	if field == nil {
		return nil
	}
	if !field.List && len(values) > 1 {
		return unsupportedEditValueError(field, "a single value is required")
	}
	if field.SourceKind == FieldSourceCheckbox {
		// Checkbox fields accept the user-facing aliases handled by
		// parseCheckboxEditValue (for example "done" and "open").
		return nil
	}
	if field.Kind != FieldKindScalar && field.Kind != FieldKindEnum {
		return nil
	}
	for _, value := range values {
		if !validateScalarValue(value, field, schema) {
			return unsupportedEditValueError(field, "value "+strconv.Quote(value)+" does not match "+field.TypeName)
		}
	}
	return nil
}

func unsupportedEditValueError(field *Field, reason string) error {
	return &editValueError{field: field.Name, reason: reason}
}

type editValueError struct {
	field  string
	reason string
}

func (e *editValueError) Error() string {
	return "unsupported target: field " + e.field + ": " + e.reason
}

func patchFrontmatterFieldSource(content string, field *Field, values []string, asLinks bool) (string, bool, error) {
	if field == nil {
		return content, false, nil
	}
	fmRange := frontmatterRange(content)
	if !fmRange.Valid(len(content)) || fmRange.Len() == 0 {
		return content, false, nil
	}
	lines := frontmatterSourceLines(content, fmRange)
	rootIndent, ok := frontmatterRootIndent(lines)
	if !ok {
		return content, false, fmt.Errorf("unsupported target: tab-indented frontmatter")
	}
	keys := FieldSourceNames(field)
	if len(keys) == 0 {
		keys = []string{field.Name}
	}
	var match *frontmatterKeyLine
	for _, line := range lines {
		candidate, ok := parseFrontmatterKeyLine(line, rootIndent)
		if !ok || !equalFrontmatterKey(candidate.key, keys) {
			continue
		}
		if match != nil {
			return content, false, fmt.Errorf("unsupported target: duplicate frontmatter field %s", field.Name)
		}
		match = &candidate
	}
	if match == nil {
		if err := validateFrontmatterFieldAppend(content, keys); err != nil {
			return content, false, err
		}
		return appendFrontmatterFieldSource(content, fmRange, rootIndent, field, values, asLinks), true, nil
	}

	lineIndex := frontmatterLineIndex(lines, match.line.start)
	if lineIndex < 0 {
		return content, false, fmt.Errorf("unsupported target: invalid frontmatter span for %s", field.Name)
	}
	if match.valueStart == match.valueEnd {
		if !field.List {
			if frontmatterHasIndentedContinuation(lines, lineIndex, match.indent) {
				return content, false, fmt.Errorf("unsupported target: multiline frontmatter value %s", field.Name)
			}
			replacement := frontmatterEmptyValueReplacement(content, *match, renderFrontmatterScalar(valuesFirst(values), "", asLinks, field))
			return replaceRange(content, ByteRange{Start: match.valueStart, End: match.valueEnd}, replacement), true, nil
		}
		updated, err := patchFrontmatterBlockList(content, lines, *match, field, values, asLinks)
		if err != nil {
			return content, false, err
		}
		return updated, true, nil
	}
	existing := content[match.valueStart:match.valueEnd]
	if isFrontmatterBlockScalar(existing) {
		if field.List {
			return content, false, fmt.Errorf("unsupported target: block scalar cannot represent list field %s", field.Name)
		}
		if frontmatterHasUnsupportedBlockScalarContinuation(lines, lineIndex, match.indent) {
			return content, false, fmt.Errorf("unsupported target: invalid block scalar span for %s", field.Name)
		}
		return patchFrontmatterBlockScalar(content, lines, *match, field, values, asLinks), true, nil
	}
	if field.List {
		if !frontmatterInlineListSupported(existing) {
			return content, false, fmt.Errorf("unsupported target: unsupported frontmatter list span for %s", field.Name)
		}
	} else {
		if frontmatterHasIndentedContinuation(lines, lineIndex, match.indent) || !frontmatterInlineScalarSupported(existing) {
			return content, false, fmt.Errorf("unsupported target: unsupported frontmatter value span for %s", field.Name)
		}
	}
	replacement := renderFrontmatterValue(existing, field, values, asLinks)
	return replaceRange(content, ByteRange{Start: match.valueStart, End: match.valueEnd}, replacement), true, nil
}

func validateFrontmatterFieldAppend(content string, keys []string) error {
	_, mapping, err := parseFrontmatterNode(content)
	if err != nil {
		return fmt.Errorf("unsupported target: cannot verify frontmatter field absence: %w", err)
	}
	if mapping.Style&yaml.FlowStyle != 0 {
		return fmt.Errorf("unsupported target: flow-style frontmatter mappings cannot be patched safely")
	}
	values := map[string]interface{}{}
	if err := mapping.Decode(&values); err != nil {
		return fmt.Errorf("unsupported target: cannot verify frontmatter field absence: %w", err)
	}
	for key := range values {
		if equalFrontmatterKey(key, keys) {
			return fmt.Errorf("unsupported target: existing frontmatter field has an unsupported source shape")
		}
	}
	return nil
}

func frontmatterContainsCanonicalField(content string, keys []string) (bool, error) {
	_, mapping, err := parseFrontmatterNode(content)
	if err != nil {
		return false, fmt.Errorf("cannot verify frontmatter field presence: %w", err)
	}
	values := map[string]interface{}{}
	if err := mapping.Decode(&values); err != nil {
		return false, fmt.Errorf("cannot verify frontmatter field presence: %w", err)
	}
	for key := range values {
		if equalFrontmatterKey(key, keys) {
			return true, nil
		}
	}
	return false, nil
}

func unsetFrontmatterField(content string, field *Field) (string, error) {
	if field == nil {
		return content, nil
	}
	fmRange := frontmatterRange(content)
	if !fmRange.Valid(len(content)) || fmRange.Len() == 0 {
		return content, nil
	}
	lines := frontmatterSourceLines(content, fmRange)
	rootIndent, ok := frontmatterRootIndent(lines)
	if !ok {
		return content, fmt.Errorf("unsupported target: tab-indented frontmatter")
	}
	keys := FieldSourceNames(field)
	if len(keys) == 0 {
		keys = []string{field.Name}
	}
	lineIndex := -1
	var match *frontmatterKeyLine
	for index, line := range lines {
		candidate, ok := parseFrontmatterKeyLine(line, rootIndent)
		if !ok || !equalFrontmatterKey(candidate.key, keys) {
			continue
		}
		if lineIndex >= 0 {
			return content, fmt.Errorf("unsupported target: duplicate frontmatter field %s", field.Name)
		}
		lineIndex = index
		match = &candidate
	}
	if lineIndex < 0 {
		present, err := frontmatterContainsCanonicalField(content, keys)
		if err != nil {
			return content, fmt.Errorf("unsupported target: %w", err)
		}
		if present {
			return content, fmt.Errorf("unsupported target: existing frontmatter field has an unsupported source shape")
		}
		return content, nil
	}
	if match != nil && field.List && match.valueStart == match.valueEnd {
		info, err := frontmatterBlockListInfo(lines, lineIndex)
		if err != nil {
			return content, err
		}
		updated := content
		for index := len(info.itemLines) - 1; index >= 0; index-- {
			updated = replaceRange(updated, frontmatterLineRange(content, info.itemLines[index]), "")
		}
		return replaceRange(updated, frontmatterLineRange(content, lines[lineIndex]), ""), nil
	}
	endIndex := lineIndex + 1
	if match != nil && match.valueStart < match.valueEnd && isFrontmatterBlockScalar(content[match.valueStart:match.valueEnd]) {
		marker := strings.TrimSpace(content[match.valueStart:match.valueEnd])
		indent := frontmatterBlockScalarIndent(marker, match.indent, lines, lineIndex)
		endIndex = frontmatterBlockScalarEndIndex(lines, lineIndex, indent)
	} else if match == nil || match.valueStart == match.valueEnd {
		for endIndex < len(lines) {
			line := lines[endIndex]
			if strings.TrimSpace(line.text) != "" && len(line.text)-len(strings.TrimLeft(line.text, " \t")) <= len(rootIndent) {
				break
			}
			endIndex++
		}
	}
	end := fmRange.End
	if endIndex < len(lines) {
		end = lines[endIndex].start
	}
	return replaceRange(content, ByteRange{Start: lines[lineIndex].start, End: end}, ""), nil
}
