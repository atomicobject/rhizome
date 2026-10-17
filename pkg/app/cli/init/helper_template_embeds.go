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

	// We only expand Obsidian-style embeds (e.g., ![[Title]]) that appear in normal
	// prose. Ignore anything inside fenced code blocks or inline code spans so
	// templates can mention embed syntax safely.
	//
	// This is intentionally a small, purpose-built markdown scanner rather than
	// a full parser.
	var b strings.Builder

	inFence := false
	var fenceChar byte
	var fenceLen int

	inCodeSpan := false
	var codeSpanLen int

	for i := 0; i < len(body); {
		atLineStart := i == 0 || body[i-1] == '\n'
		if atLineStart && !inCodeSpan {
			if ok, fc, fl, next := markdownsyntax.FenceLine(body, i); ok {
				// Fence delimiter line; copy as-is and toggle fence state.
				b.WriteString(body[i:next])
				if !inFence {
					inFence = true
					fenceChar = fc
					fenceLen = fl
				} else if fc == fenceChar && fl >= fenceLen {
					inFence = false
					fenceChar = 0
					fenceLen = 0
				}
				i = next
				continue
			}
			if inFence {
				// Inside fenced code block; copy the whole line and keep going.
				next := markdownsyntax.NextLineStart(body, i)
				b.WriteString(body[i:next])
				i = next
				continue
			}
		}

		// Not in a fenced code block.
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
