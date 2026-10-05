package codeanchor

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// RationaleKind represents a detected rationale comment kind.
type RationaleKind string

const (
	RationaleNote      RationaleKind = "note"
	RationaleImportant RationaleKind = "important"
	RationaleHack      RationaleKind = "hack"
	RationaleWhy       RationaleKind = "why"
	RationaleRationale RationaleKind = "rationale"
	RationaleTodo      RationaleKind = "todo"
	RationaleFixme     RationaleKind = "fixme"
)

// rationaleKinds maps lowercase prefix (without colon) to RationaleKind.
var rationaleKinds = map[string]RationaleKind{
	"note":      RationaleNote,
	"important": RationaleImportant,
	"hack":      RationaleHack,
	"why":       RationaleWhy,
	"rationale": RationaleRationale,
	"todo":      RationaleTodo,
	"fixme":     RationaleFixme,
}

// Rationale represents a rationale-style comment extracted from source code.
type Rationale struct {
	// ID is the first 16 hex chars of SHA256(path + ":" + startLine).
	ID string
	// Path is the vault-relative file path.
	Path string
	// SymbolFQN is the nearest enclosing symbol FQN, or "" if file-level.
	SymbolFQN string
	Kind      RationaleKind
	// Content is the full comment text, multi-line collapsed with space.
	Content string
	// StartLine is 1-indexed.
	StartLine int64
	// EndLine is 1-indexed, inclusive.
	EndLine int64
	// Fingerprint is SHA256 of Content.
	Fingerprint string
}

// ExtractRationale scans source bytes for rationale comments.
// symbols must be sorted by StartLine ascending.
// lang determines comment prefix detection.
func ExtractRationale(sourceBytes []byte, path string, lang Lang, symbols []Symbol) []Rationale {
	lines := strings.Split(string(sourceBytes), "\n")
	linePrefix := commentLinePrefix(lang)

	// Sort symbols by start line for binary search.
	sorted := make([]Symbol, len(symbols))
	copy(sorted, symbols)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].StartLine < sorted[j].StartLine
	})

	var result []Rationale
	i := 0
	for i < len(lines) {
		stripped := stripCommentPrefix(lines[i], linePrefix)
		kind, matched := matchRationalePrefix(stripped)
		if !matched {
			i++
			continue
		}

		// Collect the content: current line + continuation lines.
		startLine := int64(i + 1) // 1-indexed
		contentParts := []string{stripped}
		j := i + 1
		for j < len(lines) {
			next := stripCommentPrefix(lines[j], linePrefix)
			if next == "" {
				// Empty comment line could be a blank continuation — stop.
				break
			}
			// Stop if next line starts a new rationale prefix.
			if _, isNew := matchRationalePrefix(next); isNew {
				break
			}
			contentParts = append(contentParts, next)
			j++
		}
		endLine := startLine + int64(len(contentParts)) - 1

		content := strings.Join(contentParts, " ")
		symbolFQN := findEnclosingSymbol(sorted, startLine)

		id := rationaleID(path, startLine)
		fp := rationaleFingerprint(content)

		result = append(result, Rationale{
			ID:          id,
			Path:        path,
			SymbolFQN:   symbolFQN,
			Kind:        kind,
			Content:     content,
			StartLine:   startLine,
			EndLine:     endLine,
			Fingerprint: fp,
		})
		i = int(endLine) // advance past the consumed block (endLine is 1-indexed, so index = endLine-1+1)
	}
	return result
}

// commentLinePrefix returns the single-line comment prefix for a language.
func commentLinePrefix(lang Lang) string {
	switch lang {
	case LangPy:
		return "#"
	default:
		return "//"
	}
}

// stripCommentPrefix strips the comment marker and surrounding whitespace from a line.
// Handles "//" and "#" line prefixes, and "*" for block comment continuations.
func stripCommentPrefix(line string, prefix string) string {
	s := strings.TrimSpace(line)
	if strings.HasPrefix(s, prefix) {
		s = strings.TrimSpace(s[len(prefix):])
		return s
	}
	// Block comment continuation: lines starting with "*" (inside /* */)
	if strings.HasPrefix(s, "*") && !strings.HasPrefix(s, "*/") {
		s = strings.TrimSpace(s[1:])
		return s
	}
	// Inline block comment: /* ... */
	if strings.HasPrefix(s, "/*") {
		s = strings.TrimPrefix(s, "/*")
		s = strings.TrimSuffix(s, "*/")
		s = strings.TrimSpace(s)
		return s
	}
	return ""
}

// matchRationalePrefix checks if s starts with a known rationale prefix (case-insensitive).
// Returns the kind and true if matched.
func matchRationalePrefix(s string) (RationaleKind, bool) {
	upper := strings.ToUpper(s)
	for prefix, kind := range rationaleKinds {
		marker := strings.ToUpper(prefix) + ":"
		if strings.HasPrefix(upper, marker) {
			return kind, true
		}
	}
	return "", false
}

// findEnclosingSymbol returns the FQN of the innermost symbol containing line (1-indexed),
// or "" if no enclosing symbol is found.
func findEnclosingSymbol(symbols []Symbol, line int64) string {
	// Find all symbols whose range contains line, pick the one with the largest StartLine
	// (innermost / most specific).
	best := ""
	bestStart := int64(-1)
	for _, sym := range symbols {
		if sym.StartLine <= line && sym.EndLine >= line {
			if sym.StartLine > bestStart {
				bestStart = sym.StartLine
				best = sym.FQN
			}
		}
	}
	return best
}

// rationaleID builds a stable short ID for a rationale record.
func rationaleID(path string, startLine int64) string {
	h := sha256.Sum256([]byte(path + ":" + strconv.FormatInt(startLine, 10)))
	return fmt.Sprintf("%x", h[:8]) // 16 hex chars
}

// rationaleFingerprint returns a SHA256 hex fingerprint of the content.
func rationaleFingerprint(content string) string {
	h := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", h[:])
}
