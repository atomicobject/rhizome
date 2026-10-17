package ontology

import (
	"github.com/atomicobject/rhizome/pkg/ontology/sectionparse"
	"sort"
	"strings"
	"unicode"
)

func markerSpans(content string, owner ByteRange) []MarkerSpan {
	if !owner.Valid(len(content)) || owner.Len() == 0 {
		return nil
	}
	segment := content[owner.Start:owner.End]
	out := make([]MarkerSpan, 0)
	for offset := 0; offset < len(segment); offset++ {
		if segment[offset] != '#' {
			continue
		}
		if offset > 0 && !isMarkerBoundary(rune(segment[offset-1])) {
			continue
		}
		end := offset + 1
		for end < len(segment) && isMarkerChar(rune(segment[end])) {
			end++
		}
		if end == offset+1 {
			continue
		}
		value := segment[offset:end]
		out = append(out, MarkerSpan{
			Value: value,
			Range: ByteRange{
				Start: owner.Start + offset,
				End:   owner.Start + end,
			},
		})
	}
	return out
}

func isMarkerBoundary(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune("([{", r)
}

func isMarkerChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '/'
}

func subtractByteRanges(owner ByteRange, children []ByteRange) []ByteRange {
	if owner.Len() == 0 {
		return nil
	}
	clean := make([]ByteRange, 0, len(children))
	for _, child := range children {
		start := maxInt(child.Start, owner.Start)
		end := minInt(child.End, owner.End)
		if start < end {
			clean = append(clean, ByteRange{Start: start, End: end})
		}
	}
	sort.Slice(clean, func(i, j int) bool {
		if clean[i].Start == clean[j].Start {
			return clean[i].End < clean[j].End
		}
		return clean[i].Start < clean[j].Start
	})
	out := make([]ByteRange, 0, len(clean)+1)
	cursor := owner.Start
	for _, child := range clean {
		if cursor < child.Start {
			out = append(out, ByteRange{Start: cursor, End: child.Start})
		}
		if cursor < child.End {
			cursor = child.End
		}
	}
	if cursor < owner.End {
		out = append(out, ByteRange{Start: cursor, End: owner.End})
	}
	return out
}

func firstLineTitle(s string) string {
	line := strings.TrimSpace(s)
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	if line == "" {
		return ""
	}
	if len(line) > 80 {
		return strings.TrimSpace(line[:80])
	}
	return line
}

func isTopLevelATXHeading(line string) bool {
	leadingSpaces := sectionparse.CountLeadingSpaces(line)
	if leadingSpaces > 3 {
		return false
	}
	left := strings.TrimLeft(line, " ")
	return strings.HasPrefix(left, "#") && strings.TrimSpace(left) != ""
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
