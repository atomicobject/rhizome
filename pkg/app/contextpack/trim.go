package contextpack

import (
	"strings"
	"unicode/utf8"
)

// TrimToBudget truncates text to maxChars bytes, best-effort on a line boundary.
// Valid UTF-8 input is truncated only between complete runes.
func TrimToBudget(text string, maxChars int) string {
	trimmed, _ := TrimToBudgetDetailed(text, maxChars)
	return trimmed
}

// TrimToBudgetDetailed also reports retained bytes from the trimmed input,
// excluding any truncation indicator appended to the output.
func TrimToBudgetDetailed(text string, maxChars int) (string, int) {
	if maxChars <= 0 {
		return "", 0
	}
	text = strings.TrimSpace(text)
	if len(text) <= maxChars {
		return text, len(text)
	}
	cut := maxChars
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	prefix := text[:cut]
	if idx := strings.LastIndex(prefix, "\n"); idx > 0 && maxChars-idx < 200 {
		cut = idx
	}
	out := strings.TrimRight(text[:cut], " \t\r\n")
	if len(out) == 0 {
		out = strings.TrimSpace(prefix)
	}
	// Keep the indicator short; callers can fetch full content via tools.
	if len(out) < maxChars-20 {
		return out + "\n…", len(out)
	}
	return out, len(out)
}

// TrimMarkdown keeps the beginning of a markdown document and, when possible,
// appends a small "selected headings" digest for orientation.
func TrimMarkdown(md string, maxChars int) (string, bool) {
	md = strings.TrimSpace(md)
	if maxChars <= 0 || md == "" {
		return "", md != ""
	}
	if len(md) <= maxChars {
		return md, false
	}

	// Always keep a prefix; it's usually the synopsis.
	prefixLimit := min(maxChars, 2000)
	prefix := TrimToBudget(md, prefixLimit)

	remaining := maxChars - len(prefix)
	if remaining < 400 {
		return prefix, true
	}

	lines := strings.Split(md, "\n")
	type hunk struct {
		text string
	}
	var hunks []hunk
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "#") {
			continue
		}
		// Require "# " style headings.
		hashes := 0
		for hashes < len(line) && line[hashes] == '#' {
			hashes++
		}
		if hashes == 0 || hashes >= len(line) || line[hashes] != ' ' {
			continue
		}
		var b strings.Builder
		b.WriteString(strings.TrimRight(lines[i], " \t\r"))
		b.WriteString("\n")
		// Include a short snippet under the heading.
		added := 0
		for j := i + 1; j < len(lines) && added < 6; j++ {
			l := strings.TrimRight(lines[j], " \t\r")
			if strings.TrimSpace(l) == "" {
				if added > 0 {
					break
				}
				continue
			}
			b.WriteString(l)
			b.WriteString("\n")
			added++
		}
		hunks = append(hunks, hunk{text: strings.TrimSpace(b.String())})
	}

	if len(hunks) == 0 {
		return prefix, true
	}

	var digest strings.Builder
	digest.WriteString(prefix)
	digest.WriteString("\n\n---\n\n## Selected headings\n")

	for _, h := range hunks {
		if h.text == "" {
			continue
		}
		add := "\n\n" + h.text
		if len(add) > remaining {
			break
		}
		digest.WriteString(add)
		remaining -= len(add)
		if remaining < 200 {
			break
		}
	}

	out := TrimToBudget(digest.String(), maxChars)
	return out, true
}
