package validate

import (
	"regexp"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

type fixedNoteContent struct {
	absPath string
	content string
}

func rewriteIdentifierFieldByBlockID(runCtx RunContext, notePath, blockID string) (fixedNoteContent, bool, error) {
	content, absPath, err := readNoteForFix(runCtx, notePath)
	if err != nil {
		return fixedNoteContent{}, false, err
	}
	identifierFields := inlineIdentifierFieldNames(runCtx)
	if len(identifierFields) == 0 {
		return fixedNoteContent{}, false, nil
	}
	inlineFields := inlineFieldNames(runCtx)
	blockID = ontology.BlockSafeIdentifier(blockID)
	if blockID == "" || blockID == "node" {
		return fixedNoteContent{}, false, nil
	}
	lines := strings.SplitAfter(content, "\n")
	offset := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimRight(line, "\n"))
		idx := strings.Index(trimmed, "::")
		if idx <= 0 || !identifierFields[strings.ToLower(strings.TrimSpace(trimmed[:idx]))] {
			offset += len(line)
			continue
		}
		rawValue := strings.TrimSpace(trimmed[idx+2:])
		canonical := ontology.BlockSafeIdentifier(rawValue)
		if canonical != blockID || rawValue == "^"+blockID {
			offset += len(line)
			continue
		}
		lineStart := offset
		valueStart := strings.Index(line, rawValue)
		if valueStart < 0 {
			return fixedNoteContent{}, false, nil
		}
		if updated, ok := rewritePackedInlinePropertyBlock(content, lineStart, blockID, inlineFields); ok {
			return fixedNoteContent{absPath: absPath, content: updated}, true, nil
		}
		start := lineStart + valueStart
		end := start + len(rawValue)
		updated := content[:start] + "^" + blockID + content[end:]
		return fixedNoteContent{absPath: absPath, content: updated}, true, nil
	}
	return fixedNoteContent{}, false, nil
}

func rewritePackedInlinePropertyBlock(content string, lineStart int, blockID string, knownFields map[string]bool) (string, bool) {
	if lineStart < 0 || lineStart >= len(content) {
		return content, false
	}
	blockEnd := len(content)
	scan := lineStart
	for scan < len(content) {
		next := strings.IndexByte(content[scan:], '\n')
		lineEnd := len(content)
		afterLine := len(content)
		if next >= 0 {
			lineEnd = scan + next
			afterLine = lineEnd + 1
		}
		line := strings.TrimSpace(content[scan:lineEnd])
		if scan > lineStart && (line == "" || strings.HasPrefix(line, "#")) {
			blockEnd = scan
			break
		}
		scan = afterLine
	}
	block := content[lineStart:blockEnd]
	markerPattern := regexp.MustCompile(`(^|[ \t]+)([A-Za-z][A-Za-z0-9_-]*)::[ \t]*`)
	rawMatches := markerPattern.FindAllStringSubmatchIndex(block, -1)
	type marker struct {
		field      string
		start      int
		valueStart int
	}
	markers := make([]marker, 0, len(rawMatches))
	for _, m := range rawMatches {
		if len(m) < 6 {
			continue
		}
		field := strings.ToLower(block[m[4]:m[5]])
		if !knownFields[field] {
			continue
		}
		start := m[0]
		if block[start] == ' ' || block[start] == '\t' {
			start++
		}
		markers = append(markers, marker{field: field, start: start, valueStart: m[1]})
	}
	if len(markers) < 2 {
		return content, false
	}
	lines := make([]string, 0, len(markers))
	for i, m := range markers {
		valueEnd := len(block)
		if i+1 < len(markers) {
			valueEnd = markers[i+1].start
		}
		value := strings.TrimSpace(block[m.valueStart:valueEnd])
		if i == 0 {
			value = "^" + strings.TrimPrefix(blockID, "^")
		}
		lines = append(lines, m.field+":: "+value)
	}
	replacement := strings.Join(lines, "\n")
	if strings.HasSuffix(block, "\n") {
		replacement += "\n"
	}
	return content[:lineStart] + replacement + content[blockEnd:], true
}

func inlineIdentifierFieldNames(runCtx RunContext) map[string]bool {
	fields := inlineFieldNamesMatching(runCtx, func(field *ontology.Field) bool {
		return field.IsIdentifier
	})
	return fields
}

func inlineFieldNames(runCtx RunContext) map[string]bool {
	return inlineFieldNamesMatching(runCtx, func(field *ontology.Field) bool {
		return true
	})
}

func inlineFieldNamesMatching(runCtx RunContext, include func(*ontology.Field) bool) map[string]bool {
	schema, err := ontology.LoadSchema(runCtx.VaultPath)
	if err != nil || schema == nil {
		return nil
	}
	out := map[string]bool{}
	for _, noteType := range schema.Types {
		if noteType == nil {
			continue
		}
		for _, field := range noteType.Fields {
			if field == nil || field.SourceKind != ontology.FieldSourceInline || !include(field) {
				continue
			}
			out[strings.ToLower(strings.TrimSpace(field.Name))] = true
		}
	}
	return out
}

func removeExactBlockIDLine(content, blockID string) (string, bool) {
	id := strings.TrimSpace(blockID)
	if id == "" {
		return content, false
	}
	target := "^" + strings.TrimPrefix(id, "^")
	start := 0
	for start <= len(content) {
		end := strings.IndexByte(content[start:], '\n')
		lineEnd := len(content)
		next := len(content) + 1
		if end >= 0 {
			lineEnd = start + end
			next = lineEnd + 1
		}
		line := content[start:lineEnd]
		if strings.TrimSpace(strings.TrimSuffix(line, "\r")) == target {
			removeStart := start
			removeEnd := next
			if removeEnd > len(content) {
				removeEnd = lineEnd
				if removeStart > 0 && content[removeStart-1] == '\n' {
					removeStart--
				}
			}
			prefix := content[:removeStart]
			suffix := content[removeEnd:]
			if strings.HasSuffix(prefix, "\n\n") && strings.HasPrefix(suffix, "\n") {
				suffix = suffix[1:]
			}
			return prefix + suffix, true
		}
		if end < 0 {
			break
		}
		start = next
	}
	return content, false
}
