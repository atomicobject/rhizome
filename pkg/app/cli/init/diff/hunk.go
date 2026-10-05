package diff

import (
	"fmt"
	"strings"
)

// Hunk represents a single change region in a diff.
type Hunk struct {
	OldStart      int      // Starting line number in old file (1-based)
	NewStart      int      // Starting line number in new file (1-based)
	Removed       []string // Lines removed from old file
	Added         []string // Lines added in new file
	ContextBefore []string // Context lines before the change
	ContextAfter  []string // Context lines after the change
}

// String returns a unified diff representation of the hunk.
func (h *Hunk) String() string {
	var b strings.Builder

	oldCount := len(h.ContextBefore) + len(h.Removed) + len(h.ContextAfter)
	newCount := len(h.ContextBefore) + len(h.Added) + len(h.ContextAfter)

	// Adjust start positions to account for context
	oldStart := h.OldStart - len(h.ContextBefore)
	if oldStart < 1 {
		oldStart = 1
	}
	newStart := h.NewStart - len(h.ContextBefore)
	if newStart < 1 {
		newStart = 1
	}

	b.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount))

	for _, line := range h.ContextBefore {
		b.WriteString(" ")
		b.WriteString(line)
		b.WriteString("\n")
	}

	for _, line := range h.Removed {
		b.WriteString("-")
		b.WriteString(line)
		b.WriteString("\n")
	}

	for _, line := range h.Added {
		b.WriteString("+")
		b.WriteString(line)
		b.WriteString("\n")
	}

	for _, line := range h.ContextAfter {
		b.WriteString(" ")
		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

// ColorStyler provides methods for styling diff output with colors.
type ColorStyler interface {
	HunkHeader(s string) string
	Removed(s string) string
	Added(s string) string
	Context(s string) string
}

// ColoredString returns a unified diff representation with ANSI colors.
func (h *Hunk) ColoredString(colors ColorStyler) string {
	var b strings.Builder

	oldCount := len(h.ContextBefore) + len(h.Removed) + len(h.ContextAfter)
	newCount := len(h.ContextBefore) + len(h.Added) + len(h.ContextAfter)

	// Adjust start positions to account for context
	oldStart := h.OldStart - len(h.ContextBefore)
	if oldStart < 1 {
		oldStart = 1
	}
	newStart := h.NewStart - len(h.ContextBefore)
	if newStart < 1 {
		newStart = 1
	}

	header := fmt.Sprintf("@@ -%d,%d +%d,%d @@", oldStart, oldCount, newStart, newCount)
	b.WriteString(colors.HunkHeader(header))
	b.WriteString("\n")

	for _, line := range h.ContextBefore {
		b.WriteString(colors.Context(" " + line))
		b.WriteString("\n")
	}

	for _, line := range h.Removed {
		b.WriteString(colors.Removed("-" + line))
		b.WriteString("\n")
	}

	for _, line := range h.Added {
		b.WriteString(colors.Added("+" + line))
		b.WriteString("\n")
	}

	for _, line := range h.ContextAfter {
		b.WriteString(colors.Context(" " + line))
		b.WriteString("\n")
	}

	return b.String()
}
