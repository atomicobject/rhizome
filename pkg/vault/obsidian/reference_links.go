package obsidian

import "strings"

// ExtractMarkdownReferenceDefinitions returns internal destinations authored
// by Markdown reference definitions such as [workflow]: docs/workflow.md. It
// skips definitions inside protected code and leaves path resolution to the
// caller because definitions do not carry source-note semantics themselves.
func ExtractMarkdownReferenceDefinitions(content string) []string {
	protected := markdownProtectedSpans(content)
	var targets []string
	for lineStart := 0; lineStart < len(content); {
		lineEnd := lineStart
		for lineEnd < len(content) && content[lineEnd] != '\n' {
			lineEnd++
		}

		cursor := lineStart
		for cursor < lineEnd && cursor-lineStart < 3 && content[cursor] == ' ' {
			cursor++
		}
		if cursor < lineEnd && content[cursor] == '[' && !spanContains(protected, cursor) {
			if closeLabel, ok := markdownLabelClose(content, cursor); ok && closeLabel < lineEnd && closeLabel > cursor+1 && closeLabel+1 < lineEnd && content[closeLabel+1] == ':' {
				start := closeLabel + 2
				for start < lineEnd && (content[start] == ' ' || content[start] == '\t') {
					start++
				}
				destinationEnd := lineEnd
				if referenceDestinationMissingOnLine(content, start, lineEnd) && lineEnd < len(content) {
					start = lineEnd + 1
					destinationEnd = start
					for destinationEnd < len(content) && content[destinationEnd] != '\n' {
						destinationEnd++
					}
					for start < destinationEnd && start-(lineEnd+1) < 3 && content[start] == ' ' {
						start++
					}
					if spanContains(protected, start) {
						start = destinationEnd
					}
				}
				if target, ok := markdownReferenceDestination(content, start, destinationEnd); ok && !isExternalStructuredTarget(target) {
					targets = append(targets, target)
				}
			}
		}

		if lineEnd == len(content) {
			break
		}
		lineStart = lineEnd + 1
	}
	return targets
}

func referenceDestinationMissingOnLine(content string, start, lineEnd int) bool {
	return start >= lineEnd || start+1 == lineEnd && content[start] == '\r'
}

func markdownReferenceDestination(content string, start, lineEnd int) (string, bool) {
	if start >= lineEnd {
		return "", false
	}
	if content[start] == '<' {
		for cursor := start + 1; cursor < lineEnd; cursor++ {
			if content[cursor] == '\\' && cursor+1 < lineEnd {
				cursor++
				continue
			}
			if content[cursor] == '>' {
				target := content[start+1 : cursor]
				return target, target != ""
			}
		}
		return "", false
	}
	end := start
	for end < lineEnd && content[end] != ' ' && content[end] != '\t' && content[end] != '\r' {
		end++
	}
	target := strings.TrimSpace(content[start:end])
	return target, target != ""
}
