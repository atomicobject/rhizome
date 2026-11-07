package embeddings

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax/sectionparse"
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
	chunks := noteSectionChunks(relPath, title, string(b))
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
	chunks := noteSectionChunks(relPath, title, string(content))
	for _, chunk := range chunks {
		if chunk.Index == idx {
			return chunk.Text
		}
	}
	return ""
}

// noteSectionChunks produces one display chunk per Markdown section, in the
// order intel_doc_sections persists: sectionparse.Parse flattened pre-order,
// or the whole file when there are no headings. Chunk index i therefore names
// the section a search hit found at position i. A body is the section's own
// text, from after its heading line to the next heading.
func noteSectionChunks(path, title, content string) []ChunkInput {
	type sec struct {
		title string
		level int
		body  string
	}
	nodes := sectionparse.Flatten(sectionparse.Parse(path, content))
	sections := make([]sec, 0, len(nodes))
	for i, node := range nodes {
		start, end := node.StartByte, node.EndByte
		if newline := strings.IndexByte(content[start:end], '\n'); newline >= 0 {
			start += newline + 1
		} else {
			start = end
		}
		if i+1 < len(nodes) && nodes[i+1].StartByte < end {
			end = nodes[i+1].StartByte
		}
		sections = append(sections, sec{title: node.Title, level: node.Level, body: content[start:end]})
	}
	if len(sections) == 0 {
		sections = append(sections, sec{title: title, level: 1, body: content})
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

		text := buildChunkText(path, title, breadcrumb, nil, strings.TrimSpace(sec.body), 1, 1)
		out = append(out, NewChunkInput(idx, text, breadcrumb, sec.title))
	}
	return out
}
