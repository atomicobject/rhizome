package health

import (
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"

	"strconv"
	"strings"
	"sync"
)

var imageExtensions = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".webp": true,
	".svg":  true,
	".bmp":  true,
	".ico":  true,
	".tiff": true,
	".tif":  true,
}

type fragmentIndex struct {
	blocks   map[string]struct{}
	headings map[string]struct{}
	// anchors holds GitHub-style heading slugs and HTML anchor names, which
	// Markdown links in repository docs use instead of heading text.
	anchors map[string]struct{}
}

// isImageLink checks if a link target appears to reference an image file.
func isImageLink(target string) bool {
	ext := strings.ToLower(filepath.Ext(target))
	return imageExtensions[ext]
}

func buildFragmentIndex(content string) fragmentIndex {
	blocks, headings := markdownsyntax.BlockIDsAndHeadings(content)
	anchors := map[string]struct{}{}
	slugCounts := map[string]int{}
	for _, target := range markdownsyntax.EnumerateMarkdownTargets(content) {
		if target.Kind == markdownsyntax.MarkdownTargetHeading {
			// Renderers suffix repeated slugs: #setup, #setup-1, #setup-2.
			slug := markdownsyntax.MarkdownHeadingSlug(target.Text)
			if n := slugCounts[slug]; n > 0 {
				anchors[slug+"-"+strconv.Itoa(n)] = struct{}{}
			} else {
				anchors[slug] = struct{}{}
			}
			slugCounts[slug]++
		}
	}
	for _, match := range htmlAnchorName.FindAllStringSubmatch(content, -1) {
		anchors[strings.ToLower(match[1])] = struct{}{}
	}
	return fragmentIndex{blocks: blocks, headings: headings, anchors: anchors}
}

var htmlAnchorName = regexp.MustCompile(`(?i)<a\s[^>]*\b(?:name|id)\s*=\s*["']([^"']+)["']`)

// linkResolver answers "does this link open a file in Obsidian". Rhizome's
// note cache (with its alias tier) goes first; Obsidian's own rules over every
// vault file are the fallback, so case-only differences, ambiguous names,
// attachments, and files under ignored paths all resolve.
type linkResolver struct {
	cache    *obsidian.NotePathCache
	files    func() *obsidian.VaultFileIndex
	markdown bool
	wikilink bool
	// root and unindexed read fragments of Markdown files that resolve but
	// are not indexed notes (for example under ignored paths), once each.
	root      string
	unindexed sync.Map
}

// unindexedFragments returns the fragment index of a resolved Markdown file
// outside the indexed note set. Other files (attachments) have no fragments.
func (r *linkResolver) unindexedFragments(resolvedPath string) (fragmentIndex, bool) {
	if r.root == "" || !strings.EqualFold(path.Ext(resolvedPath), ".md") {
		return fragmentIndex{}, false
	}
	if cached, ok := r.unindexed.Load(resolvedPath); ok {
		index, _ := cached.(*fragmentIndex)
		return derefFragments(index)
	}
	var index *fragmentIndex
	if content, err := os.ReadFile(filepath.Join(r.root, filepath.FromSlash(resolvedPath))); err == nil {
		built := buildFragmentIndex(string(content))
		index = &built
	}
	r.unindexed.Store(resolvedPath, index)
	return derefFragments(index)
}

func derefFragments(index *fragmentIndex) (fragmentIndex, bool) {
	if index == nil {
		return fragmentIndex{}, false
	}
	return *index, true
}

func (r *linkResolver) resolveWikilink(target, source string) (string, bool) {
	if resolved, ok := r.cache.ResolveNote(target); ok {
		return resolved, true
	}
	return r.files().Resolve(target, source)
}

func (r *linkResolver) resolveMarkdown(rawTarget, source string) (string, bool) {
	if resolved, ok := r.cache.ResolveMdLink(rawTarget, source); ok && strings.TrimSpace(resolved) != "" {
		if _, indexed := r.cache.NotePaths[resolved]; indexed {
			return resolved, true
		}
	}
	// The cache owns URL decoding; the filesystem index accepts literal paths.
	target := markdownsyntax.DecodeMarkdownPath(rawTarget)
	joined := target
	if !strings.HasPrefix(target, "/") {
		if dir := path.Dir(filepath.ToSlash(source)); dir != "." {
			joined = path.Join(dir, target)
		}
	}
	joined = strings.TrimPrefix(path.Clean(filepath.ToSlash(joined)), "/")
	if resolved, ok := r.files().Resolve("/"+joined, source); ok {
		return resolved, true
	}
	if path.Ext(joined) == "" {
		return r.files().Resolve("/"+joined+".md", source)
	}
	return "", false
}

func scanBrokenLinksInNote(notePath, content string, resolver *linkResolver, fragmentByPath map[string]fragmentIndex, readErrByPath map[string]error, options BrokenLinksOptions) ([]BrokenLink, NoteReadErrors) {
	var brokenLinks []BrokenLink
	var readErrors NoteReadErrors
	seenReadErrors := map[string]struct{}{}
	check := func(broken BrokenLink, resolvedPath string, exists bool) {
		if !exists {
			broken.Reason = BrokenLinkReasonNoteMissing
			brokenLinks = append(brokenLinks, broken)
			return
		}
		if readErr := readErrByPath[resolvedPath]; readErr != nil {
			key := notePath + "\x00" + resolvedPath
			if _, seen := seenReadErrors[key]; !seen {
				seenReadErrors[key] = struct{}{}
				readErrors = append(readErrors, &NoteReadError{Source: notePath, Target: resolvedPath, Err: readErr})
			}
			return
		}
		if broken.Fragment == "" {
			return
		}
		// Attachments resolve without fragment evidence; Markdown files outside
		// the indexed notes (ignored paths) are read so their fragments count.
		fragments, indexed := fragmentByPath[resolvedPath]
		if !indexed {
			if fragments, indexed = resolver.unindexedFragments(resolvedPath); !indexed {
				return
			}
		}
		if reason, ok := missingFragment(fragments, broken.Fragment, broken.Markdown); ok {
			broken.Reason = reason
			broken.Alias = ""
			brokenLinks = append(brokenLinks, broken)
		}
	}
	if resolver.wikilink {
		for _, link := range markdownsyntax.ScanWikilinks(content, options.WikilinkOptions) {
			if link.InsideCodeBlock {
				continue
			}
			target, fragment := link.Target, ""
			if idx := strings.Index(target, "#"); idx >= 0 {
				fragment = target[idx+1:]
				target = target[:idx]
			}
			if !options.IncludeImages && isImageLink(target) {
				continue
			}
			broken := BrokenLink{
				Source: notePath, Target: target, LinkType: link.LinkType,
				Fragment: fragment, Line: link.Line, Raw: link.Raw,
			}
			if link.LinkType == markdownsyntax.BacklinkTypeAlias {
				if pipe := strings.Index(link.Raw, "|"); pipe >= 0 {
					broken.Alias = strings.TrimSuffix(link.Raw[pipe+1:], "]]")
				}
			}
			resolvedPath, exists := notePath, true
			if target != "" {
				resolvedPath, exists = resolver.resolveWikilink(target, notePath)
			}
			check(broken, resolvedPath, exists)
		}
	}
	if resolver.markdown {
		for _, link := range markdownsyntax.ScanStructuredLinks(content) {
			if link.Kind != markdownsyntax.StructuredLinkMarkdown {
				continue
			}
			if options.SkipEmbeds && link.Embed || options.SkipAnchors && strings.Contains(link.Target, "#") {
				continue
			}
			rawTarget, fragment := link.Path, link.Fragment
			target := markdownsyntax.DecodeMarkdownPath(rawTarget)
			// Markdown links are validated only when they name a note: a `.md`
			// path or an extensionless path. Other files are left to Obsidian.
			if ext := strings.ToLower(path.Ext(target)); target != "" && ext != "" && ext != ".md" || strings.HasSuffix(target, "/") {
				continue
			}
			broken := BrokenLink{
				Source: notePath, Target: target, LinkType: markdownsyntax.BacklinkTypeBasic,
				Fragment: fragment, Line: 1 + strings.Count(content[:link.RawSpan.Start], "\n"),
				Raw: link.RawSpan.Text(content), Markdown: true, Alias: link.Display,
			}
			if link.Embed {
				broken.LinkType = markdownsyntax.BacklinkTypeEmbed
			}
			resolvedPath, exists := notePath, true
			if target != "" {
				resolvedPath, exists = resolver.resolveMarkdown(rawTarget, notePath)
			}
			check(broken, resolvedPath, exists)
		}
	}
	return brokenLinks, readErrors
}

// NoteHasFragment reports whether content exposes a heading or block fragment
// the way broken-link detection compares them.
func NoteHasFragment(content, fragment string) bool {
	_, missing := missingFragment(buildFragmentIndex(content), fragment, false)
	return !missing
}

// missingFragment reports whether a heading or block fragment is absent from
// an indexed note. A nested heading path (`H1#H2`) requires each heading.
// Markdown links may also name a GitHub slug or HTML anchor.
func missingFragment(fragments fragmentIndex, fragment string, markdown bool) (BrokenLinkReason, bool) {
	if markdown {
		if decoded, err := url.PathUnescape(fragment); err == nil {
			fragment = decoded
		}
		if _, ok := fragments.anchors[strings.ToLower(fragment)]; ok {
			return "", false
		}
	}
	if strings.HasPrefix(fragment, "^") {
		if _, ok := fragments.blocks[strings.TrimPrefix(fragment, "^")]; ok {
			return "", false
		}
		return BrokenLinkReasonBlockMissing, true
	}
	for _, part := range strings.Split(fragment, "#") {
		if part == "" {
			continue
		}
		if _, ok := fragments.headings[markdownsyntax.ObsidianHeadingKey(part)]; !ok {
			return BrokenLinkReasonHeadingMissing, true
		}
	}
	return "", false
}

func aliasesFromNotePaths(allNotes []string, getContent func(string) string) map[string][]string {
	if len(allNotes) == 0 || getContent == nil {
		return nil
	}
	aliasesByPath := make(map[string][]string)
	for _, notePath := range allNotes {
		fm, err := markdownsyntax.ExtractFrontmatter(getContent(notePath))
		if err != nil {
			continue
		}
		if aliases := obsidian.AliasListFromFrontmatter(fm); len(aliases) > 0 {
			aliasesByPath[notePath] = aliases
		}
	}
	if len(aliasesByPath) == 0 {
		return nil
	}
	return aliasesByPath
}
