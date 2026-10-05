package obsidian

import (
	"regexp"
	"strings"
)

// SingleMarkdownH1Title returns the title text only when source contains
// exactly one legacy metadata H1. It deliberately preserves the historic
// strict ATX and Setext rules used by notemeta rather than broadening title
// detection to every fragment target.
func SingleMarkdownH1Title(content string) string {
	headings := markdownH1Titles(content)
	if len(headings) != 1 {
		return ""
	}
	return headings[0]
}

// FirstMarkdownH1Title returns the first legacy metadata H1, if any. It is
// shared by title analysis callers that intentionally accept multiple H1s.
func FirstMarkdownH1Title(content string) string {
	headings := markdownH1Titles(content)
	if len(headings) == 0 {
		return ""
	}
	return headings[0]
}

func markdownH1Titles(content string) []string {
	body := stripMarkdownTitleFrontmatter(content)
	if body == "" {
		return nil
	}
	lines := strings.Split(body, "\n")
	inFence := false
	var fenceMarker string
	var previous string
	headings := []string{}
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if inFence {
			if fenceMarker != "" && strings.HasPrefix(trimmed, fenceMarker) {
				inFence = false
				fenceMarker = ""
			}
			previous = ""
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = true
			if strings.HasPrefix(trimmed, "```") {
				fenceMarker = "```"
			} else {
				fenceMarker = "~~~"
			}
			previous = ""
			continue
		}
		if previous != "" && len(trimmed) >= 3 && strings.Trim(trimmed, "=") == "" {
			headings = append(headings, cleanMarkdownTitleText(previous))
			previous = ""
			continue
		}
		if strings.HasPrefix(line, "# ") {
			headings = append(headings, cleanMarkdownTitleText(strings.TrimPrefix(line, "# ")))
			previous = ""
			continue
		}
		if line == "#" {
			previous = trimmed
			continue
		}
		if trimmed != "" {
			previous = trimmed
		} else {
			previous = ""
		}
	}
	return headings
}

func stripMarkdownTitleFrontmatter(content string) string {
	if !strings.HasPrefix(content, "---") {
		return content
	}
	const closingFence = "\n---"
	relativeEnd := strings.Index(content[3:], closingFence)
	if relativeEnd == -1 {
		return content
	}
	end := 3 + relativeEnd + len(closingFence)
	if end < len(content) && content[end] == '\n' {
		end++
	}
	return content[end:]
}

func cleanMarkdownTitleText(text string) string {
	return PlainMarkdownTitle(trimMarkdownTitleClosingHashes(text))
}

var (
	titleLiteralPattern  = regexp.MustCompile("`([^`]+)`|\\\\([!-/:-@\\[-`{-~])")
	titleWikilinkPattern = regexp.MustCompile(`!?\[\[([^\]]+)\]\]`)
	titleLinkPattern     = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	// Asterisk, tilde, and equals runs close anywhere; underscores only at
	// word boundaries, so snake_case names survive.
	titleDelimiterPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\*\*(\S(?:.*?\S)?)\*\*`),
		regexp.MustCompile(`~~(\S(?:.*?\S)?)~~`),
		regexp.MustCompile(`==(\S(?:.*?\S)?)==`),
		regexp.MustCompile(`\*(\S(?:[^*]*?\S)?)\*`),
	}
	titleUnderscorePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(^|[^\p{L}\p{N}_])__(\S(?:.*?\S)?)__($|[^\p{L}\p{N}_])`),
		regexp.MustCompile(`(^|[^\p{L}\p{N}_])_(\S(?:[^_]*?\S)?)_($|[^\p{L}\p{N}_])`),
	}
)

// PlainMarkdownTitle renders a heading's inline Markdown as the plain text a
// reader sees: wikilinks become their alias (or target note), links their
// label, and emphasis, strikethrough, highlight, and code markers drop.
// Code spans and backslash escapes stay literal.
func PlainMarkdownTitle(text string) string {
	// Park literals in private-use runes so emphasis rules cannot touch them.
	var literals []string
	out := titleLiteralPattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := titleLiteralPattern.FindStringSubmatch(match)
		literals = append(literals, parts[1]+parts[2])
		return string(rune(0xE000 + len(literals) - 1))
	})
	out = titleWikilinkPattern.ReplaceAllStringFunc(out, func(match string) string {
		inner := titleWikilinkPattern.FindStringSubmatch(match)[1]
		if _, alias, ok := strings.Cut(inner, "|"); ok {
			return strings.TrimSpace(alias)
		}
		target, heading, _ := strings.Cut(inner, "#")
		return strings.TrimSpace(firstNonEmptyString(target, heading))
	})
	out = titleLinkPattern.ReplaceAllString(out, "$1")
	for _, pattern := range titleDelimiterPatterns {
		out = pattern.ReplaceAllString(out, "$1")
	}
	for _, pattern := range titleUnderscorePatterns {
		// Each match consumes its boundaries, so adjacent spans need another pass.
		for next := pattern.ReplaceAllString(out, "$1$2$3"); next != out; next = pattern.ReplaceAllString(out, "$1$2$3") {
			out = next
		}
	}
	return strings.Join(strings.Fields(restoreTitleLiterals(out, literals)), " ")
}

func restoreTitleLiterals(text string, literals []string) string {
	if len(literals) == 0 {
		return text
	}
	var out strings.Builder
	for _, r := range text {
		if index := int(r - 0xE000); index >= 0 && index < len(literals) {
			out.WriteString(literals[index])
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func trimMarkdownTitleClosingHashes(text string) string {
	out := strings.TrimSpace(text)
	if out == "" {
		return ""
	}
	hashStart := len(out)
	for hashStart > 0 && out[hashStart-1] == '#' {
		hashStart--
	}
	if hashStart == len(out) {
		return out
	}
	if hashStart == 0 {
		return ""
	}
	if out[hashStart-1] != ' ' && out[hashStart-1] != '\t' {
		return out
	}
	return strings.TrimRight(out[:hashStart], " \t")
}
