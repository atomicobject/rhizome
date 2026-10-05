package obsidian

import (
	"net/url"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// IsCaseInsensitiveFS returns true if the current OS typically uses a case-insensitive filesystem.
// macOS and Windows are case-insensitive by default; Linux is case-sensitive.
func IsCaseInsensitiveFS() bool {
	return runtime.GOOS == "darwin" || runtime.GOOS == "windows"
}

// pathsEqual compares two paths, using case-insensitive comparison on macOS/Windows.
func pathsEqual(a, b string) bool {
	if IsCaseInsensitiveFS() {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// NormalizeForComparison returns a string suitable for use as a map key when
// detecting duplicates. On case-insensitive filesystems (macOS/Windows), this
// lowercases the string; on Linux it returns it unchanged.
func NormalizeForComparison(s string) string {
	if IsCaseInsensitiveFS() {
		return strings.ToLower(s)
	}
	return s
}

// protectedRegion represents a code block or inline code that should not be modified
type protectedRegion struct {
	placeholder string
	content     string
}

// extractProtectedRegions replaces fenced, indented, and inline code with
// placeholders so link rewriting touches exactly the links the scanners see.
func extractProtectedRegions(content string) (string, []protectedRegion) {
	spans := markdownProtectedSpans(content)
	if len(spans) == 0 {
		return content, nil
	}
	var b strings.Builder
	regions := make([]protectedRegion, 0, len(spans))
	cursor := 0
	for index, span := range spans {
		placeholder := "\x00CODE" + strconv.Itoa(index) + "\x00"
		b.WriteString(content[cursor:span.Start])
		b.WriteString(placeholder)
		regions = append(regions, protectedRegion{placeholder: placeholder, content: content[span.Start:span.End]})
		cursor = span.End
	}
	b.WriteString(content[cursor:])
	return b.String(), regions
}

// restoreProtectedRegions puts the original code blocks and inline code back
func restoreProtectedRegions(content string, regions []protectedRegion) string {
	result := content
	for _, region := range regions {
		result = strings.Replace(result, region.placeholder, region.content, 1)
	}
	return result
}

// RewriteLinksInContent updates internal wikilinks and markdown links targeting oldPath to point to newPath.
// It preserves alias text and fragments (headings/block refs) and returns the rewritten content plus count of replacements.
// If basenameUnique is true, links matching just the basename (without folder) will also be rewritten.
// Set basenameUnique to false if there are other notes with the same basename in different folders.
func RewriteLinksInContent(content, oldPath, newPath string) (string, int) {
	return RewriteLinksInContentWithOptions(content, oldPath, newPath, true)
}

// RewriteLinksInContentWithOptions is like RewriteLinksInContent but allows controlling basename matching.
// It skips links inside fenced code blocks and inline code spans to match Obsidian behavior.
// Old path alternatives belong to the same move and match against the same
// original content; they do not add another basename owner or rewrite pass.
func RewriteLinksInContentWithOptions(content, oldPath, newPath string, basenameUnique bool, oldPathAliases ...string) (string, int) {
	return rewriteLinksInContent(content, oldPath, newPath, basenameUnique, false, oldPathAliases...)
}

// RewriteLinksPreservingDisplay retargets exactly the links a broken-link
// repair reviewed: the fragment must match oldPath's fragment (an empty one
// matches only unfragmented links), and a wikilink without display text keeps
// what the reader saw by taking its original target as the alias
// (`[[Old]]` becomes `[[New|Old]]`).
func RewriteLinksPreservingDisplay(content, oldPath, newPath string, basenameUnique bool) (string, int) {
	return rewriteLinksInContent(content, oldPath, newPath, basenameUnique, true)
}

func rewriteLinksInContent(content, oldPath, newPath string, basenameUnique, preserveDisplay bool, oldPathAliases ...string) (string, int) {
	// Protect code blocks and inline code from modification
	protectedContent, regions := extractProtectedRegions(content)
	oldPath, oldFragment := splitRewriteFragment(oldPath)
	newPath, newFragment := splitRewriteFragment(newPath)

	oldExt := strings.ToLower(filepath.Ext(oldPath))
	if oldExt == "" {
		oldExt = ".md"
	}
	newExt := strings.ToLower(filepath.Ext(newPath))
	if newExt == "" {
		newExt = oldExt
	}

	oldNorm := NormalizeWithDefaultExt(oldPath, oldExt)
	oldBase := strings.TrimSuffix(oldNorm, oldExt)
	// Also match against just the filename (no folder), since Obsidian wikilinks often omit the path
	oldBasename := baseName(oldBase)
	newNorm := NormalizeWithDefaultExt(newPath, newExt)
	newBasename := baseName(strings.TrimSuffix(newNorm, newExt))
	oldNorms := []string{oldNorm}
	for _, alias := range oldPathAliases {
		oldNorms = append(oldNorms, NormalizeWithDefaultExt(alias, oldExt))
	}
	matchesFullPath := func(targetNorm, targetBase string) bool {
		for _, candidate := range oldNorms {
			if pathsEqual(targetNorm, candidate) || pathsEqual(targetBase, strings.TrimSuffix(candidate, oldExt)) {
				return true
			}
		}
		return false
	}

	// Wikilinks and embeds: ![[...]] or [[...]]
	wikiPattern := regexp.MustCompile(`(!)?\[\[(.+?)\]\]`)
	rewriteCount := 0

	protectedContent = wikiPattern.ReplaceAllStringFunc(protectedContent, func(match string) string {
		m := wikiPattern.FindStringSubmatch(match)
		if len(m) < 3 {
			return match
		}
		isEmbed := m[1] == "!"
		inner := m[2]

		targetPart := inner
		aliasPart := ""
		if pipeIdx := strings.Index(inner, "|"); pipeIdx != -1 {
			targetPart = inner[:pipeIdx]
			aliasPart = inner[pipeIdx+1:]
		}

		fragment := ""
		base := targetPart
		if hashIdx := strings.Index(base, "#"); hashIdx != -1 {
			fragment = base[hashIdx:]
			base = base[:hashIdx]
		}
		if (oldFragment != "" || preserveDisplay) && fragment != oldFragment {
			return match
		}

		ext := filepath.Ext(base)
		hadExt := ext != ""
		if ext == "" {
			ext = oldExt
		}
		targetNorm := NormalizeWithDefaultExt(base, ext)
		targetBase := strings.TrimSuffix(targetNorm, ext)
		// Match against full path OR just basename (Obsidian links often omit folder)
		// Only match by basename if basenameUnique is true (no other notes share this basename)
		// Use OS-appropriate case sensitivity (case-insensitive on macOS/Windows)
		matchedByBasename := basenameUnique && pathsEqual(targetBase, oldBasename) && !pathsEqual(targetBase, oldBase)
		matchedByFullPath := matchesFullPath(targetNorm, targetBase)
		if !matchedByFullPath && !matchedByBasename {
			return match
		}

		// If matched by basename only, use the new basename (preserve link style without folder)
		var newBase string
		if matchedByBasename {
			newBase = newBasename
			if hadExt {
				newBase += ext
			}
		} else {
			newBase = newNorm
			if !hadExt {
				newBase = strings.TrimSuffix(newBase, newExt)
			}
		}
		if newFragment != "" {
			fragment = newFragment
		}
		newTarget := newBase + fragment
		if preserveDisplay && aliasPart == "" && !isEmbed {
			aliasPart = targetPart
		}
		if aliasPart != "" {
			newTarget += "|" + aliasPart
		}
		rewriteCount++
		prefix := ""
		if isEmbed {
			prefix = "!"
		}
		return prefix + "[[" + newTarget + "]]"
	})

	// Markdown links and embeds: [text](...) or ![text](...)
	mdPattern := regexp.MustCompile(`(!)?\[([^\]]*)\]\(([^)]+)\)`)
	protectedContent = mdPattern.ReplaceAllStringFunc(protectedContent, func(match string) string {
		m := mdPattern.FindStringSubmatch(match)
		if len(m) < 4 {
			return match
		}
		isEmbed := m[1] == "!"
		text := m[2]
		href := strings.TrimSpace(m[3])

		// Ignore external links
		if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
			return match
		}

		fragment := ""
		base := href
		if hashIdx := strings.Index(base, "#"); hashIdx != -1 {
			fragment = base[hashIdx:]
			base = base[:hashIdx]
		}
		if (oldFragment != "" || preserveDisplay) && fragment != oldFragment {
			return match
		}

		// Check if the path is URL-encoded and decode for matching
		decodedBase := DecodeMarkdownPath(base)
		wasEncoded := decodedBase != base

		ext := filepath.Ext(decodedBase)
		hadExt := ext != ""
		if ext == "" {
			ext = oldExt
		}
		targetNorm := NormalizeWithDefaultExt(decodedBase, ext)
		targetBase := strings.TrimSuffix(targetNorm, ext)
		// Match against full path OR just basename (Obsidian links often omit folder)
		// Only match by basename if basenameUnique is true (no other notes share this basename)
		// Use OS-appropriate case sensitivity (case-insensitive on macOS/Windows)
		matchedByBasename := basenameUnique && pathsEqual(targetBase, oldBasename) && !pathsEqual(targetBase, oldBase)
		matchedByFullPath := matchesFullPath(targetNorm, targetBase)
		if !matchedByFullPath && !matchedByBasename {
			return match
		}

		// If matched by basename only, use the new basename (preserve link style without folder)
		var newBase string
		if matchedByBasename {
			newBase = newBasename
			if hadExt {
				newBase += ext
			}
		} else {
			newBase = newNorm
			if !hadExt {
				newBase = strings.TrimSuffix(newBase, newExt)
			}
		}

		// If the original was URL-encoded, encode the new path too
		if wasEncoded {
			newBase = url.PathEscape(newBase)
		}

		if newFragment != "" {
			fragment = newFragment
		}
		newHref := newBase + fragment

		rewriteCount++
		prefix := ""
		if isEmbed {
			prefix = "!"
		}
		return prefix + "[" + text + "](" + newHref + ")"
	})

	// Restore protected code blocks and inline code
	result := restoreProtectedRegions(protectedContent, regions)

	return result, rewriteCount
}

func splitRewriteFragment(target string) (string, string) {
	if hash := strings.Index(target, "#"); hash >= 0 {
		return target[:hash], target[hash:]
	}
	return target, ""
}

// baseName returns the last element of a path (the filename without directory).
func baseName(path string) string {
	// Handle both forward and back slashes
	path = strings.ReplaceAll(path, "\\", "/")
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}
