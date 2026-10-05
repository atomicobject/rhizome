// Package diff provides unified diff generation and hunk parsing, used to show
// what an rzm init update would change in a locally edited file.
package diff

import (
	"fmt"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// GenerateUnifiedDiff generates a unified diff between old and new content.
// Returns the diff as a string and the parsed hunks.
func GenerateUnifiedDiff(oldContent, newContent string, filename string) (string, []Hunk, error) {
	dmp := diffmatchpatch.New()

	// Use line-mode diffing for cleaner, line-based hunks
	chars1, chars2, lineArray := dmp.DiffLinesToChars(oldContent, newContent)
	lineDiffs := dmp.DiffCharsToLines(dmp.DiffMain(chars1, chars2, false), lineArray)
	if !HasChanges(lineDiffs) {
		return "", nil, nil
	}

	// Semantic cleanup can shift edit boundaries off line breaks, so the
	// unified text is built from the raw line diffs.
	unifiedDiff := formatUnifiedDiff(lineDiffs, filename)
	hunks := parseHunks(dmp.DiffCleanupSemantic(lineDiffs), oldContent, newContent)

	return unifiedDiff, hunks, nil
}

// HasChanges returns true if the diffs contain actual changes.
func HasChanges(diffs []diffmatchpatch.Diff) bool {
	for _, d := range diffs {
		if d.Type != diffmatchpatch.DiffEqual {
			return true
		}
	}
	return false
}

// unifiedContextLines matches the default context of diff -u and git diff.
const unifiedContextLines = 3

type diffLine struct {
	op   byte // ' ', '-', or '+'
	text string
}

// formatUnifiedDiff renders line-mode diffs as a standard unified diff with
// line-numbered hunks and literal (unescaped) content.
func formatUnifiedDiff(diffs []diffmatchpatch.Diff, filename string) string {
	var lines []diffLine
	for _, d := range diffs {
		op := byte(' ')
		switch d.Type {
		case diffmatchpatch.DiffDelete:
			op = '-'
		case diffmatchpatch.DiffInsert:
			op = '+'
		}
		for _, text := range strings.SplitAfter(d.Text, "\n") {
			if text != "" {
				lines = append(lines, diffLine{op: op, text: text})
			}
		}
	}

	var b strings.Builder
	b.WriteString("--- a/" + filename + "\n+++ b/" + filename + "\n")
	oldLine, newLine := 1, 1 // 1-based numbers of lines[i] in each file
	for i := 0; i < len(lines); {
		if lines[i].op == ' ' {
			oldLine++
			newLine++
			i++
			continue
		}
		// Extend the hunk while the next change is close enough that the
		// contexts would touch.
		end := i
		for j := i; j < len(lines); j++ {
			if lines[j].op != ' ' {
				end = j
			} else if j-end > 2*unifiedContextLines {
				break
			}
		}
		// Hunks are separated by more than 2*context equal lines, so leading
		// context never overlaps the previous hunk.
		start := max(i-unifiedContextLines, 0)
		stop := min(end+unifiedContextLines+1, len(lines))
		oldStart, newStart := oldLine-(i-start), newLine-(i-start)
		var oldCount, newCount int
		var body strings.Builder
		for _, l := range lines[start:stop] {
			if l.op != '+' {
				oldCount++
			}
			if l.op != '-' {
				newCount++
			}
			body.WriteByte(l.op)
			body.WriteString(l.text)
			if !strings.HasSuffix(l.text, "\n") {
				body.WriteString("\n\\ No newline at end of file\n")
			}
		}
		fmt.Fprintf(&b, "@@ -%s +%s @@\n", hunkRange(oldStart, oldCount), hunkRange(newStart, newCount))
		b.WriteString(body.String())
		for _, l := range lines[i:stop] {
			if l.op != '+' {
				oldLine++
			}
			if l.op != '-' {
				newLine++
			}
		}
		i = stop
	}
	return b.String()
}

// hunkRange formats a unified hunk range the way diff -u does: an empty range
// names the line before it, and a one-line range omits its count.
func hunkRange(start, count int) string {
	switch count {
	case 0:
		return fmt.Sprintf("%d,0", start-1)
	case 1:
		return fmt.Sprintf("%d", start)
	}
	return fmt.Sprintf("%d,%d", start, count)
}

// parseHunks converts diffs into individual hunks with context.
func parseHunks(diffs []diffmatchpatch.Diff, oldContent, newContent string) []Hunk {
	if len(diffs) == 0 {
		return nil
	}

	// Split content into lines for line-based hunk representation
	oldLines := splitLines(oldContent)
	newLines := splitLines(newContent)

	// Track positions in old and new content
	var hunks []Hunk
	var currentHunk *Hunk

	oldLineNum := 1
	newLineNum := 1

	// Context lines to include (before and after)
	const contextLines = 3

	for _, d := range diffs {
		lines := splitLines(d.Text)
		if len(lines) == 0 {
			continue
		}

		switch d.Type {
		case diffmatchpatch.DiffEqual:
			// Equal content - just advance line counters
			if currentHunk != nil {
				// Add trailing context (up to contextLines)
				for i := 0; i < len(lines) && i < contextLines; i++ {
					currentHunk.ContextAfter = append(currentHunk.ContextAfter, lines[i])
				}
				// If we have enough equal lines after changes, close the hunk
				if len(lines) > contextLines*2 {
					hunks = append(hunks, *currentHunk)
					currentHunk = nil
				}
			}
			oldLineNum += len(lines)
			newLineNum += len(lines)

		case diffmatchpatch.DiffDelete:
			if currentHunk == nil {
				currentHunk = &Hunk{
					OldStart: oldLineNum,
					NewStart: newLineNum,
				}
				// Add leading context
				startCtx := oldLineNum - contextLines - 1
				if startCtx < 0 {
					startCtx = 0
				}
				for i := startCtx; i < oldLineNum-1 && i < len(oldLines); i++ {
					currentHunk.ContextBefore = append(currentHunk.ContextBefore, oldLines[i])
				}
			}
			currentHunk.Removed = append(currentHunk.Removed, lines...)
			oldLineNum += len(lines)

		case diffmatchpatch.DiffInsert:
			if currentHunk == nil {
				currentHunk = &Hunk{
					OldStart: oldLineNum,
					NewStart: newLineNum,
				}
				// Add leading context
				startCtx := newLineNum - contextLines - 1
				if startCtx < 0 {
					startCtx = 0
				}
				for i := startCtx; i < newLineNum-1 && i < len(newLines); i++ {
					currentHunk.ContextBefore = append(currentHunk.ContextBefore, newLines[i])
				}
			}
			currentHunk.Added = append(currentHunk.Added, lines...)
			newLineNum += len(lines)
		}
	}

	// Close any remaining hunk
	if currentHunk != nil {
		hunks = append(hunks, *currentHunk)
	}

	return hunks
}

// splitLines splits content into lines, preserving empty lines.
func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	// Remove trailing empty string if content ended with newline
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
