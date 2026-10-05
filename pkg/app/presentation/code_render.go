package presentation

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

func renderCodeSpan(ctx context.Context, codeRoot string, anchor codeanchor.IntelAnchor, isModule bool) string {
	if strings.TrimSpace(codeRoot) == "" || strings.TrimSpace(anchor.Path) == "" {
		return ""
	}
	full := filepath.Join(codeRoot, filepath.FromSlash(anchor.Path))
	data, err := os.ReadFile(full)
	if err != nil {
		return ""
	}
	indexingperf.AddCount(ctx, indexingperf.SemanticQueryOpBodyReads, 1)
	indexingperf.AddCount(ctx, indexingperf.SemanticQueryOpBodyReadBytes, int64(len(data)))

	var text string
	if isModule || strings.EqualFold(anchor.Kind, "module") {
		text = strings.TrimSpace(string(data))
		// Keep module renders compact by default; full small files still fit.
		return truncateCode(text, 3200)
	}

	text = strings.TrimSpace(extractSpan(data, anchor))
	if text == "" {
		return ""
	}
	return truncateCode(text, 2000)
}

func extractSpan(content []byte, anchor codeanchor.IntelAnchor) string {
	if anchor.StartByte >= 0 && anchor.EndByte > anchor.StartByte && int(anchor.EndByte) <= len(content) {
		return string(content[anchor.StartByte:anchor.EndByte])
	}
	// fallback to line-based extraction if byte spans are missing
	if anchor.StartLine > 0 && anchor.EndLine >= anchor.StartLine {
		return extractLines(content, int(anchor.StartLine), int(anchor.EndLine))
	}
	return ""
}

func extractLines(content []byte, startLine, endLine int) string {
	if startLine <= 0 || endLine < startLine {
		return ""
	}
	lines := strings.SplitAfter(string(content), "\n")
	start := startLine - 1
	if start >= len(lines) {
		return ""
	}
	end := endLine
	if end > len(lines) {
		end = len(lines)
	}
	return strings.TrimSpace(strings.Join(lines[start:end], ""))
}

func truncateCode(s string, maxChars int) string {
	const headLines, tailLines = 120, 40
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if maxChars <= 0 {
		return s
	}
	if runeLen(s) <= maxChars {
		return s
	}
	// Prefer keeping structure: head + tail.
	lines := strings.Split(s, "\n")
	if len(lines) <= headLines+tailLines+2 {
		return trimToRunes(s, maxChars)
	}

	head := strings.Join(lines[:headLines], "\n")
	tail := strings.Join(lines[len(lines)-tailLines:], "\n")
	out := strings.TrimSpace(head + "\n\n...\n\n" + tail)
	if runeLen(out) <= maxChars {
		return out
	}
	return trimToRunes(out, maxChars)
}

func trimToRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return strings.TrimSpace(string(r[:maxRunes]))
}

func runeLen(s string) int { return len([]rune(s)) }

// renderChunkBody renders chunk body text from FTS (includes doc comments).
// Similar to renderCodeSpan but works with pre-indexed chunk text instead of reading files.
func renderChunkBody(chunkBody string, isModule bool) string {
	chunkBody = strings.TrimSpace(chunkBody)
	if chunkBody == "" {
		return ""
	}
	// Extract core body (strip chunk headers/metadata)
	coreBody := extractCoreChunkBody(chunkBody)
	if coreBody == "" {
		return ""
	}
	if isModule {
		return truncateCode(coreBody, 3200)
	}
	return truncateCode(coreBody, 2000)
}

// extractCoreChunkBody extracts the actual content from chunk text, removing
// metadata headers like "Kind: ...", "Lang: ...", etc.
func extractCoreChunkBody(chunkText string) string {
	// Look for the first double newline which typically separates header from body
	parts := strings.SplitN(chunkText, "\n\n", 2)
	if len(parts) == 2 {
		// Check if first part looks like a header (contains "Kind:", "Lang:", etc.)
		header := strings.ToLower(parts[0])
		if strings.Contains(header, "kind:") || strings.Contains(header, "lang:") || strings.Contains(header, "path:") {
			return strings.TrimSpace(parts[1])
		}
	}
	// If no clear header separator, return as-is (might be a simple chunk)
	return strings.TrimSpace(chunkText)
}
