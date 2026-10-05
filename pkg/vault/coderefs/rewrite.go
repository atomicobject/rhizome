package coderefs

// Docs:
// - [Coderefs (Hub)](docs/hubs/Coderefs (Hub).md)
// - [Coderefs - rewrite on rename + move](docs/reference/guides/Coderefs - rewrite on rename + move.md)
// Related: [Dirty tracking + Refresh semantics](docs/reference/analysis/Dirty tracking + Refresh semantics.md) (rename/recreate edge cases)

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/bmatcuk/doublestar/v4"
)

// RewriteResult contains the outcome of rewriting code references.
type RewriteResult struct {
	FilesUpdated int
	RefsUpdated  int
}

// RefMapping defines a single rename operation.
type RefMapping struct {
	OldPath        string
	NewPath        string
	OldPathAliases []string // Alternative authored spellings of this same source.
}

// compiledMapping prepares path spellings and the precise mention boundaries.
type compiledMapping struct {
	RefMapping
	OldPathNoExt    string
	NewPathNoExt    string
	OldBasename     string
	NewBasename     string
	MentionFull     *regexp.Regexp
	MentionBasename *regexp.Regexp
	aliases         []compiledMapping
}

// RewriteBatch updates wikilinks, markdown links, and @mentions in code files for multiple rename operations.
// It scans code files matching the config patterns once and applies all mappings.
//
// Rewrite only touches extracted comment/doc blocks for known languages. Unknown
// extensions fall back to text-wide rewrite for compatibility, so keep Config
// includes pointed at code/comment formats rather than arbitrary generated data.
func RewriteBatch(vaultPath string, config *Config, mappings []RefMapping) (RewriteResult, error) {
	var result RewriteResult

	if config == nil || !config.Enabled || len(mappings) == 0 {
		return result, nil
	}

	// Prepare paths and mention regexes once; every file uses the same mappings.
	compiled := make([]compiledMapping, 0, len(mappings))
	for _, m := range mappings {
		cm := compileMapping(m)
		compiled = append(compiled, cm)
	}

	// Discover code files
	codeFiles, err := discoverCodeFilesForRewrite(vaultPath, config)
	if err != nil {
		return result, err
	}

	for _, relPath := range codeFiles {
		absPath := filepath.Join(vaultPath, relPath)
		content, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}

		// Retain the size guard in case a file grew after discovery.
		if len(content) > MaxFileSizeBytes {
			continue
		}
		if isBinary(content) {
			continue
		}

		updatedContent, totalUpdates := rewriteRefsInPath(relPath, string(content), compiled)

		if totalUpdates > 0 {
			info, err := os.Stat(absPath)
			if err != nil {
				continue
			}
			perm := info.Mode().Perm()
			if err := os.WriteFile(absPath, []byte(updatedContent), perm); err != nil {
				continue
			}
			// Preserve original permissions even on platforms where WriteFile may
			// not honor them for existing files.
			if err := os.Chmod(absPath, perm); err != nil {
				continue
			}
			result.FilesUpdated++
			result.RefsUpdated += totalUpdates
		}
	}

	return result, nil
}

// RewriteCodeRefs is a convenience wrapper for RewriteBatch for a single move.
func RewriteCodeRefs(vaultPath string, config *Config, oldPath, newPath string) (RewriteResult, error) {
	return RewriteBatch(vaultPath, config, []RefMapping{{OldPath: oldPath, NewPath: newPath}})
}

func compileMapping(m RefMapping) compiledMapping {
	oldPath := string(paths.Normalize(m.OldPath))
	newPath := string(paths.Normalize(m.NewPath))

	cm := compiledMapping{
		RefMapping:   RefMapping{OldPath: oldPath, NewPath: newPath},
		OldPathNoExt: strings.TrimSuffix(oldPath, ".md"),
		NewPathNoExt: strings.TrimSuffix(newPath, ".md"),
		OldBasename:  strings.TrimSuffix(filepath.Base(oldPath), ".md"),
		NewBasename:  strings.TrimSuffix(filepath.Base(newPath), ".md"),
	}

	// Keep email and trailing-boundary protection. Scanning only constrains the
	// prefix; rewriting also prevents @MyNote matching inside @MyNoteExtra.

	// @old/path/Note followed by non-path-char or end
	cm.MentionFull = regexp.MustCompile(`(^|[^\w@])@` + regexp.QuoteMeta(cm.OldPathNoExt) + `([^A-Za-z0-9_/.\-]|$)`)

	if cm.OldBasename != "" && cm.OldBasename != cm.OldPathNoExt {
		// @Note followed by non-path-char or end
		cm.MentionBasename = regexp.MustCompile(`(^|[^\w@])@` + regexp.QuoteMeta(cm.OldBasename) + `([^A-Za-z0-9_/.\-]|$)`)
	}
	for _, alias := range m.OldPathAliases {
		cm.aliases = append(cm.aliases, compileMapping(RefMapping{OldPath: alias, NewPath: newPath}))
	}

	return cm
}

// Each mapping collects edits against one immutable comment scan.
func mappingEdits(content string, links []obsidian.StructuredLink, linkSpans []obsidian.StructuredLinkSpan, cm compiledMapping) []linkEdit {
	var edits []linkEdit
	for _, link := range links {
		if link.Kind == obsidian.StructuredLinkMarkdown && link.Embed {
			continue // Image destinations are scanned but are not rename targets.
		}
		path := link.Path
		if link.Kind == obsidian.StructuredLinkMarkdown {
			path = obsidian.DecodeMarkdownPath(path)
		}
		target, matched := cm.linkDestination(path, link.Kind)
		if !matched {
			continue
		}
		edit := linkEdit{span: link.PathSpan, text: target}
		if link.Kind == obsidian.StructuredLinkMarkdown {
			edit.text = obsidian.EncodeMarkdownPath(target)
		} else if !wikiPathRoundTrips(target) {
			// An unrepresentable wiki embed also becomes a normal link. Turning it
			// into a Markdown image would exclude it from subsequent renames.
			edit.span = link.RawSpan
			label := cm.NewBasename
			if link.DisplaySpan.Valid() {
				label = link.Display
			}
			edit.text = markdownReference(cm.NewPath, label, link.Fragment, link.FragmentSpan.Valid())
		}
		edits = append(edits, edit)
	}
	edits = append(edits, mentionEdits(content, linkSpans, cm.MentionFull, cm.OldPathNoExt, cm.NewPathNoExt, cm.NewPath)...)
	if cm.MentionBasename != nil {
		edits = append(edits, mentionEdits(content, linkSpans, cm.MentionBasename, cm.OldBasename, cm.NewBasename, cm.NewPath)...)
	}
	if len(cm.aliases) > 0 {
		seen := make(map[obsidian.StructuredLinkSpan]bool, len(edits))
		for _, edit := range edits {
			seen[edit.span] = true
		}
		for _, alias := range cm.aliases {
			candidates := mentionEdits(content, linkSpans, alias.MentionFull, alias.OldPathNoExt, cm.NewPathNoExt, cm.NewPath)
			if alias.MentionBasename != nil {
				candidates = append(candidates, mentionEdits(content, linkSpans, alias.MentionBasename, alias.OldBasename, cm.NewBasename, cm.NewPath)...)
			}
			for _, edit := range candidates {
				if !seen[edit.span] {
					seen[edit.span] = true
					edits = append(edits, edit)
				}
			}
		}
	}
	return edits
}

func (cm compiledMapping) linkDestination(path string, kind obsidian.StructuredLinkKind) (string, bool) {
	switch {
	case path == cm.OldPath:
		return cm.NewPath, true
	case path == cm.OldPathNoExt:
		return cm.NewPathNoExt, true
	case kind == obsidian.StructuredLinkWikilink && cm.OldBasename != cm.OldPathNoExt && strings.EqualFold(path, cm.OldBasename):
		return cm.NewBasename, true
	default:
		for _, alias := range cm.aliases {
			if destination, matched := alias.linkDestination(path, kind); matched {
				return destination, true
			}
		}
		return "", false
	}
}

// Check the parser's actual grammar rather than a filename character allowlist.
func wikiPathRoundTrips(path string) bool {
	text := "[[" + path + "]]"
	links := obsidian.ScanCommentLinks(text)
	return len(links) == 1 && links[0].Kind == obsidian.StructuredLinkWikilink &&
		links[0].RawSpan.Text(text) == text && links[0].Path == path &&
		!links[0].FragmentSpan.Valid() && !links[0].DisplaySpan.Valid()
}

func markdownReference(path, label, fragment string, hasFragment bool) string {
	label = strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]").Replace(label)
	target := obsidian.EncodeMarkdownPath(path)
	if hasFragment {
		target += "#" + obsidian.EncodeMarkdownPath(fragment)
	}
	return "[" + label + "](" + target + ")"
}

type linkEdit struct {
	span obsidian.StructuredLinkSpan
	text string
}

func applyLinkEdits(content string, edits []linkEdit) string {
	// Link destinations are disjoint; nested wiki links occupy a Markdown
	// label/title or a distinct literal destination. Mentions exclude raw links.
	sort.Slice(edits, func(i, j int) bool { return edits[i].span.Start < edits[j].span.Start })
	size := len(content)
	for _, edit := range edits {
		size += len(edit.text) - (edit.span.End - edit.span.Start)
	}
	var b strings.Builder
	b.Grow(size)
	last := 0
	for _, edit := range edits {
		b.WriteString(content[last:edit.span.Start])
		b.WriteString(edit.text)
		last = edit.span.End
	}
	b.WriteString(content[last:])
	return b.String()
}

func rewriteRefsInPath(path, content string, compiled []compiledMapping) (string, int) {
	spec, ok := languageSpecForPath(path)
	if !ok {
		// Compatibility fallback for older configs that included extra extensions.
		// Known code extensions should use extractors so strings stay untouched.
		return rewriteRefsInText(content, compiled)
	}
	extractor := spec.extractor()
	if extractor == nil {
		return rewriteRefsInText(content, compiled)
	}

	blocks := extractor(content)
	if len(blocks) == 0 {
		return content, 0
	}

	var b strings.Builder
	b.Grow(len(content))
	last := 0
	total := 0

	for _, block := range blocks {
		if block.Offset < last || block.Offset > len(content) {
			continue
		}
		end := block.Offset + len(block.Text)
		if end > len(content) {
			continue
		}
		b.WriteString(content[last:block.Offset])
		updated, count := rewriteRefsInText(block.Text, compiled)
		b.WriteString(updated)
		last = end
		total += count
	}
	b.WriteString(content[last:])

	if total == 0 {
		return content, 0
	}
	return b.String(), total
}

func rewriteRefsInText(content string, compiled []compiledMapping) (string, int) {
	totalUpdates := 0
	var links []obsidian.StructuredLink
	var linkSpans []obsidian.StructuredLinkSpan
	parsed := false
	for _, cm := range compiled {
		if !parsed {
			links = obsidian.ScanCommentLinks(content)
			linkSpans = mergedStructuredLinkSpans(links)
			parsed = true
		}
		edits := mappingEdits(content, links, linkSpans, cm)
		if len(edits) > 0 {
			content = applyLinkEdits(content, edits)
			totalUpdates += len(edits)
			// Later mappings must see changed destinations and syntax.
			parsed = false
		}
	}
	return content, totalUpdates
}

func mentionEdits(s string, linkSpans []obsidian.StructuredLinkSpan, re *regexp.Regexp, oldSpelling, target, canonicalPath string) []linkEdit {
	// The literal is necessary for a match, except that regexp interprets invalid
	// UTF-8 source bytes as RuneError while Contains compares their original bytes.
	if !strings.ContainsRune(oldSpelling, utf8.RuneError) && !strings.Contains(s, oldSpelling) {
		return nil
	}
	matches := re.FindAllStringSubmatchIndex(s, -1)
	var edits []linkEdit
	var replacement string
	for _, match := range matches {
		if overlapsStructuredLink(linkSpans, match[3], match[4]) {
			continue
		}
		if replacement == "" {
			replacement = "@" + target
			if mentionRegex.FindString(replacement) != replacement {
				if wikiPathRoundTrips(canonicalPath) {
					replacement = "[[" + canonicalPath + "]]"
				} else {
					replacement = markdownReference(canonicalPath, strings.TrimSuffix(filepath.Base(canonicalPath), ".md"), "", false)
				}
			}
		}
		// Exclude captured boundary bytes so punctuation remains unchanged.
		edits = append(edits, linkEdit{
			span: obsidian.StructuredLinkSpan{Start: match[3], End: match[4], Present: true},
			text: replacement,
		})
	}
	return edits
}

func isBinary(content []byte) bool {
	checkLen := len(content)
	if checkLen > 8192 {
		checkLen = 8192
	}
	for i := 0; i < checkLen; i++ {
		if content[i] == 0 {
			return true
		}
	}
	return false
}

func discoverCodeFilesForRewrite(vaultPath string, config *Config) ([]string, error) {
	if config == nil {
		return nil, nil
	}

	var codeFiles []string
	seen := make(map[string]struct{})

	for _, pattern := range config.Includes {
		matches, err := doublestar.Glob(os.DirFS(vaultPath), pattern)
		if err != nil {
			continue
		}

		for _, match := range matches {
			// Check excludes
			excluded := false
			for _, excl := range config.Excludes {
				if ok, _ := doublestar.Match(excl, match); ok {
					excluded = true
					break
				}
			}
			if excluded {
				continue
			}

			if _, ok := seen[match]; ok {
				continue
			}
			seen[match] = struct{}{}

			info, err := os.Stat(filepath.Join(vaultPath, match))
			if err != nil || info.IsDir() {
				continue
			}
			// Reject oversized files before ReadFile allocates their contents.
			if info.Size() > MaxFileSizeBytes {
				continue
			}

			codeFiles = append(codeFiles, match)
		}
	}

	return codeFiles, nil
}
