package coderefs

// Docs:
// - [Coderefs (Hub)](docs/hubs/Coderefs (Hub).md)
// - [Coderefs - scanning + indexing](docs/reference/guides/Coderefs - scanning + indexing.md)
// See also: parser.go, index.go, rewrite.go

import (
	"regexp"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// MaxFileSizeBytes is the maximum file size to scan (2MB).
// Larger files are skipped to avoid memory pressure.
const MaxFileSizeBytes = 2 * 1024 * 1024

// mentionRegex matches @NoteName patterns.
// Matches @ followed by valid path chars, ensuring it's not part of an email (preceded by non-word char).
//
// Note: This regex only constrains the prefix (to reject emails like user@domain).
// It does NOT enforce a trailing boundary—the captured token is then looked up via ResolveNote,
// which handles the full match. This differs from rewrite.go's MentionFull regex, which adds
// a trailing boundary to prevent partial replacements like @MyNote matching @MyNoteExtra.
//
// Supported characters in @mentions: alphanumeric, _, /, ., -
// Spaces and other characters are intentionally excluded to avoid ambiguous parsing.
var mentionRegex = regexp.MustCompile(`(?:^|[^\w@])@([A-Za-z0-9_/\.\-]+)`)

// ScanFile scans a source file for references to vault notes.
// It extracts comments/docstrings and looks for wikilinks, markdown links, and @mentions.
// Returns nil (no refs) for files that are too large or appear to be binary.
//
// Only resolved note targets become CodeRefs. This keeps retrieval-visible doc
// links tied to real vault notes and lets ordinary code comments mention unknown
// terms without polluting the code-doc graph.
func ScanFile(path string, content []byte, noteCache *obsidian.NotePathCache) ([]CodeRef, error) {
	// Skip files that are too large
	if len(content) > MaxFileSizeBytes {
		return nil, nil
	}

	// Skip binary files (check for null bytes in first 8KB)
	checkLen := len(content)
	if checkLen > 8192 {
		checkLen = 8192
	}
	for i := 0; i < checkLen; i++ {
		if content[i] == 0 {
			return nil, nil // Likely binary
		}
	}

	src := string(content)
	lang := DetectLanguage(path)
	blocks := ExtractCommentBlocks(lang, src)

	var refs []CodeRef

	for _, block := range blocks {
		// The caller has selected comments, so explicit links inside code examples
		// are included. Markdown destinations exclude title and angle wrappers.
		links := obsidian.ScanCommentLinks(block.Text)
		linkSpans := mergedStructuredLinkSpans(links)
		// Keep the established wiki, Markdown, mention grouping in each block.
		for _, linkKind := range []obsidian.StructuredLinkKind{obsidian.StructuredLinkWikilink, obsidian.StructuredLinkMarkdown} {
			for _, link := range links {
				if link.Kind != linkKind {
					continue
				}
				var resolved obsidian.ResolvedNoteTarget
				var ok bool
				kind := RefKindWikilink
				if link.Kind == obsidian.StructuredLinkMarkdown {
					kind = RefKindMdLink
					resolved, ok = noteCache.ResolveMdLinkTarget(link.Target, "")
				} else {
					resolved, ok = noteCache.ResolveNoteTarget(link.Target)
				}
				if ok {
					refs = append(refs, CodeRef{
						SourceFile: path,
						Language:   lang,
						Target:     string(paths.NormalizeNotePath(resolved.Path)),
						Fragment:   strings.TrimSpace(resolved.Fragment),
						RawTarget:  strings.TrimSpace(link.Target),
						Kind:       kind,
						Line:       block.Line + strings.Count(block.Text[:link.RawSpan.Start], "\n"),
						Snippet:    extractSnippetAt(block.Text, link.RawSpan.Start),
					})
				}
			}
		}

		// 3. Scan for @mentions. Resolution through NotePathCache is the ambiguity
		// boundary; unresolved tokens are ignored rather than guessed.
		matches := mentionRegex.FindAllStringSubmatchIndex(block.Text, -1)
		for _, match := range matches {
			if overlapsStructuredLink(linkSpans, match[2]-1, match[3]) {
				continue
			}
			// match[2], match[3] are start/end of the capturing group (the note name)
			name := block.Text[match[2]:match[3]]

			// Try to resolve the mentioned name
			if target, ok := noteCache.ResolveNote(name); ok {
				refs = append(refs, CodeRef{
					SourceFile: path,
					Language:   lang,
					Target:     string(paths.NormalizeNotePath(target)),
					RawTarget:  name,
					Kind:       RefKindMention,
					Line:       block.Line + strings.Count(block.Text[:match[2]], "\n"),
					Snippet:    extractSnippetAt(block.Text, match[2]),
				})
			}
		}
	}

	return refs, nil
}

// Parsed links own their labels and destinations; embedded @ text is not a mention.
// Links arrive in start order but can nest, so merge their raw spans before lookup.
func mergedStructuredLinkSpans(links []obsidian.StructuredLink) []obsidian.StructuredLinkSpan {
	var spans []obsidian.StructuredLinkSpan
	for _, link := range links {
		span := link.RawSpan
		if len(spans) > 0 && span.Start <= spans[len(spans)-1].End {
			spans[len(spans)-1].End = max(spans[len(spans)-1].End, span.End)
		} else {
			spans = append(spans, span)
		}
	}
	return spans
}

func overlapsStructuredLink(spans []obsidian.StructuredLinkSpan, start, end int) bool {
	i := sort.Search(len(spans), func(i int) bool { return spans[i].End > start })
	return i < len(spans) && spans[i].Start < end
}

// extractSnippetAt extracts the line around a parsed byte offset.
func extractSnippetAt(text string, idx int) string {
	// Find line start
	start := strings.LastIndex(text[:idx], "\n")
	if start == -1 {
		start = 0
	} else {
		start++ // Skip the newline
	}

	// Find line end
	end := strings.Index(text[idx:], "\n")
	if end == -1 {
		end = len(text)
	} else {
		end += idx
	}

	return truncateSnippet(text[start:end])
}

// truncateSnippet trims whitespace and truncates to ~100 chars with ellipsis.
func truncateSnippet(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 100 {
		return s[:100] + "..."
	}
	return s
}
