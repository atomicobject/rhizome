package embeddings

import (
	"strconv"
	"strings"
)

// CoreChunkBody extracts the "core" body text from a chunk, trimming overlap
// regions used when large sections are split into multiple chunks.
//
// Use this when you need the deduplicated body text for display (e.g., in
// search result rendering) rather than the full chunk text used for embedding.
//
// Chunk format: expects text in ChunkNote/ChunkTextForPath format:
//   - Header (Path:, Title:, Breadcrumb:, Chunk:) + blank line + body
//   - If chunk has "Chunk: i/n" header, trims overlapChars from start (i>1)
//     and end (i<n) to remove the overlap region.
//
// Returns trimmed body text, or the full input if parsing fails.
func CoreChunkBody(chunkText string) string {
	header, body, ok := strings.Cut(chunkText, "\n\n")
	if !ok {
		return strings.TrimSpace(chunkText)
	}

	part, total := parseChunkPart(header)
	body = strings.TrimSpace(body)
	if body == "" || total <= 1 || part <= 0 {
		return body
	}

	startTrim := 0
	endTrim := 0
	if part > 1 {
		startTrim = overlapChars
	}
	if part < total {
		endTrim = overlapChars
	}
	return strings.TrimSpace(trimRunes(body, startTrim, endTrim))
}

func parseChunkPart(header string) (part int, total int) {
	for _, line := range strings.Split(header, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Chunk:") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, "Chunk:"))
		a, b, ok := strings.Cut(rest, "/")
		if !ok {
			return 0, 0
		}
		p, err1 := strconv.Atoi(strings.TrimSpace(a))
		t, err2 := strconv.Atoi(strings.TrimSpace(b))
		if err1 != nil || err2 != nil || p <= 0 || t <= 0 {
			return 0, 0
		}
		return p, t
	}
	return 0, 0
}

func trimRunes(s string, start, end int) string {
	if start <= 0 && end <= 0 {
		return s
	}
	r := []rune(s)
	if start < 0 {
		start = 0
	}
	if end < 0 {
		end = 0
	}
	if start > len(r) {
		start = len(r)
	}
	if end > len(r)-start {
		end = len(r) - start
	}
	return string(r[start : len(r)-end])
}
