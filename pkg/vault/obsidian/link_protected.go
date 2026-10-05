package obsidian

import (
	"regexp"
	"sort"
	"strings"
)

// MarkdownCodeMask reports whether a byte offset of content lies in fenced,
// indented, or inline code, where Obsidian creates no links.
func MarkdownCodeMask(content string) func(int) bool {
	spans := markdownProtectedSpans(content)
	return func(pos int) bool { return spanContains(spans, pos) }
}

func markdownProtectedSpans(content string) []StructuredLinkSpan {
	fences := fencedCodeSpans(content)
	blocks := append([]StructuredLinkSpan(nil), fences...)
	blocks = append(blocks, indentedCodeSpans(content)...)
	blocks = mergeProtectedSpans(blocks)
	spans := append([]StructuredLinkSpan(nil), blocks...)
	runs := collectUnprotectedBacktickRuns(content, blocks)
	nextSameLength := make([]int, len(runs))
	nextByLength := make(map[int]int)
	for index := len(runs) - 1; index >= 0; index-- {
		nextSameLength[index] = -1
		if next, found := nextByLength[runs[index].length]; found {
			nextSameLength[index] = next
		}
		nextByLength[runs[index].length] = index
	}
	for index := 0; index < len(runs); {
		closeIndex := nextSameLength[index]
		if closeIndex < 0 {
			index++
			continue
		}
		spans = append(spans, linkSpan(runs[index].start, runs[closeIndex].start+runs[closeIndex].length))
		index = closeIndex + 1
	}
	return mergeProtectedSpans(spans)
}

type backtickRun struct {
	start  int
	length int
}

func collectUnprotectedBacktickRuns(content string, blocks []StructuredLinkSpan) []backtickRun {
	var runs []backtickRun
	for i := 0; i < len(content); {
		if protected, ok := containingSpan(blocks, i); ok {
			i = protected.End
			continue
		}
		if content[i] != '`' || isBackslashEscaped(content, i) {
			i++
			continue
		}
		run := byteRun(content, i, '`')
		runs = append(runs, backtickRun{start: i, length: run})
		i += run
	}
	return runs
}

// indentedCodeSpans follows CommonMark closely enough for link scanning: an
// indented line cannot interrupt a paragraph or continue a list item, so
// nested list items indented by tabs or four spaces keep their links.
func indentedCodeSpans(content string) []StructuredLinkSpan {
	var spans []StructuredLinkSpan
	prevParagraph, inCode, inList := false, false, false
	for lineStart := 0; lineStart < len(content); {
		lineEnd := strings.IndexByte(content[lineStart:], '\n')
		if lineEnd < 0 {
			lineEnd = len(content)
		} else {
			lineEnd += lineStart + 1
		}
		line := content[lineStart:lineEnd]
		trimmed := strings.TrimSpace(line)
		indented := strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t")
		switch {
		case trimmed == "":
			prevParagraph = false
		case indented && (inCode || !prevParagraph && !inList):
			inCode = true
			spans = append(spans, linkSpan(lineStart, lineEnd))
		case indented:
			prevParagraph = true
		default:
			inCode = false
			blockStart := strings.HasPrefix(line, "#") || strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
			if listItemStart.MatchString(trimmed) {
				inList = true
			} else if !prevParagraph || blockStart {
				inList = false
			}
			prevParagraph = !strings.HasPrefix(trimmed, "#") &&
				!strings.HasPrefix(trimmed, "```") && !strings.HasPrefix(trimmed, "~~~")
		}
		lineStart = lineEnd
	}
	return spans
}

var listItemStart = regexp.MustCompile(`^([-*+]|\d{1,9}[.)])(\s|$)`)

func mergeProtectedSpans(spans []StructuredLinkSpan) []StructuredLinkSpan {
	if len(spans) == 0 {
		return nil
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].Start != spans[j].Start {
			return spans[i].Start < spans[j].Start
		}
		return spans[i].End < spans[j].End
	})
	out := spans[:1]
	for _, span := range spans[1:] {
		last := &out[len(out)-1]
		if span.Start <= last.End {
			if span.End > last.End {
				last.End = span.End
			}
			continue
		}
		out = append(out, span)
	}
	return out
}

func fencedCodeSpans(content string) []StructuredLinkSpan {
	var spans []StructuredLinkSpan
	openStart := -1
	var fenceByte byte
	fenceLen := 0
	for lineStart := 0; lineStart <= len(content); {
		lineEnd := strings.IndexByte(content[lineStart:], '\n')
		if lineEnd < 0 {
			lineEnd = len(content)
		} else {
			lineEnd += lineStart + 1
		}
		contentEnd := lineEnd
		for contentEnd > lineStart && (content[contentEnd-1] == '\n' || content[contentEnd-1] == '\r') {
			contentEnd--
		}
		marker, validIndent := fenceMarkerStart(content, lineStart, contentEnd)
		if validIndent && marker < contentEnd && (content[marker] == '`' || content[marker] == '~') {
			run := byteRun(content, marker, content[marker])
			if run >= 3 {
				if openStart < 0 {
					openStart, fenceByte, fenceLen = lineStart, content[marker], run
				} else if content[marker] == fenceByte && run >= fenceLen && onlyMarkdownSpace(content[marker+run:contentEnd]) {
					spans = append(spans, linkSpan(openStart, lineEnd))
					openStart = -1
				}
			}
		}
		if lineEnd == len(content) {
			break
		}
		lineStart = lineEnd
	}
	if openStart >= 0 {
		spans = append(spans, linkSpan(openStart, len(content)))
	}
	return spans
}

// fenceMarkerStart accepts any leading indentation: a fence inside a nested
// list item is still code, and an over-indented fence line is code either way.
func fenceMarkerStart(content string, lineStart, lineEnd int) (int, bool) {
	marker := lineStart
	for marker < lineEnd && (content[marker] == ' ' || content[marker] == '\t') {
		marker++
	}
	return marker, true
}

func onlyMarkdownSpace(content string) bool {
	for i := range content {
		if content[i] != ' ' && content[i] != '\t' {
			return false
		}
	}
	return true
}

func byteRun(content string, start int, value byte) int {
	end := start
	for end < len(content) && content[end] == value {
		end++
	}
	return end - start
}

func containingSpan(spans []StructuredLinkSpan, pos int) (StructuredLinkSpan, bool) {
	span, found, _ := containingSpanLookup(spans, pos)
	return span, found
}

func containingSpanLookup(spans []StructuredLinkSpan, pos int) (StructuredLinkSpan, bool, int) {
	low, high, steps := 0, len(spans), 0
	for low < high {
		steps++
		middle := low + (high-low)/2
		if spans[middle].Start <= pos {
			low = middle + 1
		} else {
			high = middle
		}
	}
	index := low - 1
	if index >= 0 && pos < spans[index].End {
		return spans[index], true, steps
	}
	return StructuredLinkSpan{}, false, steps
}
