package init

import (
	"fmt"
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"strings"
)

func expandAgentTemplateEmbeds(body string) (string, error) {
	if !strings.Contains(body, "![[") {
		return body, nil
	}

	// Template embeds retain unmatched and escaped backtick protection; the
	// broader link parser's paired-span and indented-code rules do not apply.
	var b strings.Builder
	var fence markdownsyntax.FenceState

	inCodeSpan := false
	var codeSpanLen int

	for i := 0; i < len(body); {
		atLineStart := i == 0 || body[i-1] == '\n'
		if atLineStart && !inCodeSpan && fence.ConsumeLine(body, i) {
			next := markdownsyntax.NextLineStart(body, i)
			b.WriteString(body[i:next])
			i = next
			continue
		}

		if body[i] == '`' {
			runLen := markdownsyntax.ByteRun(body, i, '`')
			b.WriteString(body[i : i+runLen])
			if inCodeSpan {
				if runLen == codeSpanLen {
					inCodeSpan = false
					codeSpanLen = 0
				}
			} else {
				inCodeSpan = true
				codeSpanLen = runLen
			}
			i += runLen
			continue
		}

		if !inCodeSpan && strings.HasPrefix(body[i:], "![[") {
			raw, consumed, ok := parseObsidianEmbed(body[i:])
			if !ok {
				b.WriteByte(body[i])
				i++
				continue
			}
			target, err := normalizeEmbedTarget(raw)
			if err != nil {
				return "", err
			}
			content, err := loadRhizomeMdTemplate(target)
			if err != nil {
				return "", fmt.Errorf("expand embed %q: %w", raw, err)
			}
			b.WriteString(content)
			i += consumed
			continue
		}

		b.WriteByte(body[i])
		i++
	}
	return b.String(), nil
}

// parseObsidianEmbed parses a string starting with "![[", returning the raw
// target (inside the brackets) and how many bytes were consumed.
func parseObsidianEmbed(s string) (raw string, consumed int, ok bool) {
	if !strings.HasPrefix(s, "![[") {
		return "", 0, false
	}
	close := strings.Index(s[3:], "]]")
	if close < 0 {
		return "", 0, false
	}
	raw = strings.TrimSpace(s[3 : 3+close])
	return raw, 3 + close + 2, true
}

func normalizeEmbedTarget(raw string) (string, error) {
	target := strings.TrimSpace(raw)
	if target == "" {
		return "", fmt.Errorf("embed target is empty")
	}
	if cut := strings.IndexAny(target, "|#"); cut >= 0 {
		target = strings.TrimSpace(target[:cut])
	}
	if target == "" {
		return "", fmt.Errorf("embed target %q is empty", raw)
	}
	if !strings.HasSuffix(strings.ToLower(target), ".md") {
		target += ".md"
	}
	return target, nil
}
