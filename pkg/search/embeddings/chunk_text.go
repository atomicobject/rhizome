package embeddings

import (
	"os"
	"path/filepath"
	"strings"
)

// ChunkTextForPath returns the chunk text for a note's chunk index, caching
// results to avoid re-chunking the same file repeatedly.
func ChunkTextForPath(vaultPath, relPath string, idx int, cache map[string][]ChunkInput) string {
	text, _, _ := ChunkTextForPathObserved(vaultPath, relPath, idx, cache)
	return text
}

// ChunkTextForPathObserved returns the chunk text and reports the bytes read
// when this call physically loads and caches the note.
func ChunkTextForPathObserved(vaultPath, relPath string, idx int, cache map[string][]ChunkInput) (string, int64, bool) {
	if relPath == "" {
		return "", 0, false
	}
	if cache == nil {
		cache = make(map[string][]ChunkInput)
	}
	if chunks, ok := cache[relPath]; ok {
		for _, ch := range chunks {
			if ch.Index == idx {
				return ch.Text, 0, false
			}
		}
		return "", 0, false
	}
	abs := filepath.Join(vaultPath, relPath)
	b, err := os.ReadFile(abs)
	if err != nil {
		return "", 0, false
	}
	title := strings.TrimSuffix(filepath.Base(relPath), filepath.Ext(relPath))
	chunks, err := chunkNoteSections(relPath, title, string(b))
	if err != nil {
		return "", int64(len(b)), true
	}
	cache[relPath] = chunks
	for _, ch := range chunks {
		if ch.Index == idx {
			return ch.Text, int64(len(b)), true
		}
	}
	return "", int64(len(b)), true
}

// ChunkTextForContent returns the indexed section text without performing a
// filesystem read. Request-scoped materializers use it after their shared
// physical-read cache has loaded the note.
func ChunkTextForContent(relPath string, idx int, content []byte) string {
	if relPath == "" {
		return ""
	}
	title := strings.TrimSuffix(filepath.Base(relPath), filepath.Ext(relPath))
	chunks, err := chunkNoteSections(relPath, title, string(content))
	if err != nil {
		return ""
	}
	for _, chunk := range chunks {
		if chunk.Index == idx {
			return chunk.Text
		}
	}
	return ""
}

// chunkNoteSections produces one chunk per Markdown heading section (no overlap splitting).
// This matches the chunk indexing used by semantic.NoteSyncer (intel_doc_sections order).
func chunkNoteSections(path, title, content string) ([]ChunkInput, error) {
	lines := strings.Split(content, "\n")

	type sec struct {
		title string
		level int
		lines []string
	}

	var sections []sec
	var current *sec
	inCode := false

	flush := func() {
		if current == nil {
			return
		}
		sections = append(sections, *current)
		current = nil
	}

	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			inCode = !inCode
		}
		if !inCode {
			trimLeft := strings.TrimLeft(line, " ")
			if strings.HasPrefix(trimLeft, "#") {
				hashCount := 0
				for hashCount < len(trimLeft) && trimLeft[hashCount] == '#' {
					hashCount++
				}
				rest := strings.TrimSpace(trimLeft[hashCount:])
				if rest != "" {
					flush()
					current = &sec{title: rest, level: hashCount}
					continue
				}
			}
		}
		if current != nil {
			current.lines = append(current.lines, line)
		}
	}
	flush()

	// Fallback: whole-file single chunk when no headings.
	if len(sections) == 0 {
		sections = append(sections, sec{
			title: title,
			level: 1,
			lines: lines,
		})
	}

	type levelTitle struct {
		level int
		title string
	}
	var stack []levelTitle

	out := make([]ChunkInput, 0, len(sections))
	for idx, sec := range sections {
		level := sec.level
		for len(stack) > 0 && stack[len(stack)-1].level >= level {
			stack = stack[:len(stack)-1]
		}
		if strings.TrimSpace(sec.title) != "" {
			stack = append(stack, levelTitle{level: level, title: strings.TrimSpace(sec.title)})
		}

		breadcrumbParts := make([]string, 0, 1+len(stack))
		if strings.TrimSpace(title) != "" {
			breadcrumbParts = append(breadcrumbParts, strings.TrimSpace(title))
		}
		for _, lt := range stack {
			if lt.title != "" {
				breadcrumbParts = append(breadcrumbParts, lt.title)
			}
		}
		breadcrumb := strings.Join(breadcrumbParts, " > ")

		body := strings.TrimSpace(strings.Join(sec.lines, "\n"))
		text := buildChunkText(path, title, breadcrumb, nil, body, 1, 1)
		out = append(out, NewChunkInput(idx, text, breadcrumb, sec.title))
	}

	return out, nil
}
