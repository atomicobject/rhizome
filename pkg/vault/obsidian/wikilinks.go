package obsidian

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/paths"
	"golang.org/x/text/unicode/norm"
)

// NotePathCache maps note names to their full paths for efficient wikilink resolution
type NotePathCache struct {
	// NotePaths is the authoritative set used for incremental cache updates.
	NotePaths map[string]struct{}
	// Paths contains only unambiguous note-name keys. pathCandidates retains
	// every claimant so basename collisions never degrade to first-wins.
	// e.g. "my note" -> "Notes/my note.md"
	// and  "Notes/my note" -> "Notes/my note.md"
	Paths                  map[string]string
	pathCandidates         map[string][]string
	explicitPathCandidates map[string][]string
	noteExtensions         map[string]int
	// Aliases maps frontmatter-declared alias strings to every claimant in
	// deterministic path order.
	// Real file path/title matches win over aliases during wikilink resolution.
	Aliases    map[string][]string
	aliasExact map[string][]string
}

// BacklinkType represents the type of wikilink variant used.
// Backlink captures a referrer and the link variant used.
type Backlink struct {
	Referrer string                      `json:"referrer"`
	LinkType markdownsyntax.BacklinkType `json:"linkType"`
	Fragment string                      `json:"fragment,omitempty"`
}

type ResolvedNoteTarget struct {
	Path     string
	Fragment string
}

// BuildNotePathCache creates a cache of note paths for efficient wikilink resolution
func BuildNotePathCache(allNotes []string) *NotePathCache {
	return BuildNotePathCacheWithAliases(allNotes, nil)
}

// AliasesFromNoteEntries extracts the frontmatter `aliases:` list
// from each NoteEntry and returns a map keyed by note path suitable
// for BuildNotePathCacheWithAliases. Accepts scalar and list values
// like the metadata index's alias projection.
func AliasesFromNoteEntries(entries []NoteEntry) map[string][]string {
	if len(entries) == 0 {
		return nil
	}
	out := make(map[string][]string, len(entries))
	for _, entry := range entries {
		if list := AliasListFromFrontmatter(entry.Frontmatter); len(list) > 0 {
			out[entry.Path] = list
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// AliasListFromFrontmatter pulls `aliases:` values out of a parsed
// YAML frontmatter map. It accepts scalar/list strings and fmt.Stringer
// values, matching metadata alias extraction; empty values are dropped.
func AliasListFromFrontmatter(fm map[string]interface{}) []string {
	if len(fm) == 0 {
		return nil
	}
	raw, ok := fm["aliases"]
	if !ok {
		return nil
	}
	switch current := raw.(type) {
	case []interface{}:
		out := make([]string, 0, len(current))
		for _, item := range current {
			if alias := aliasStringFromFrontmatter(item); alias != "" {
				out = append(out, alias)
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(current))
		for _, item := range current {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	default:
		if alias := aliasStringFromFrontmatter(current); alias != "" {
			return []string{alias}
		}
	}
	return nil
}

func aliasStringFromFrontmatter(raw any) string {
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case fmt.Stringer:
		return strings.TrimSpace(value.String())
	default:
		return ""
	}
}

// BuildNotePathCacheWithAliases creates a cache of note paths plus a
// frontmatter-alias index. aliasesByPath maps a note path to the list of
// alias strings declared by the note's `aliases:` frontmatter list.
func BuildNotePathCacheWithAliases(allNotes []string, aliasesByPath map[string][]string) *NotePathCache {
	cache := &NotePathCache{
		NotePaths:  make(map[string]struct{}, len(allNotes)),
		Paths:      make(map[string]string),
		Aliases:    make(map[string][]string),
		aliasExact: make(map[string][]string),
	}

	for _, notePath := range allNotes {
		cache.NotePaths[notePath] = struct{}{}
	}
	cache.rebuildPathIndex()

	for notePath, aliases := range aliasesByPath {
		for _, alias := range aliases {
			exact := norm.NFC.String(strings.TrimSpace(alias))
			alias = paths.LinkKey(exact)
			if alias == "" {
				continue
			}
			cache.Aliases[alias] = addSortedCandidate(cache.Aliases[alias], notePath)
			cache.aliasExact[exact] = addSortedCandidate(cache.aliasExact[exact], notePath)
		}
	}

	return cache
}

func (c *NotePathCache) AddOrUpdate(notePath string, aliases []string) {
	if c == nil {
		return
	}
	notePath = strings.TrimSpace(filepath.ToSlash(notePath))
	if notePath == "" {
		return
	}
	c.ensureMutable()
	_, existed := c.NotePaths[notePath]
	c.NotePaths[notePath] = struct{}{}
	if !existed {
		c.addPathKeys(notePath)
	}
	c.removeAliasesForPath(notePath)
	for _, alias := range aliases {
		exact := norm.NFC.String(strings.TrimSpace(alias))
		alias = paths.LinkKey(exact)
		if alias == "" {
			continue
		}
		c.Aliases[alias] = addSortedCandidate(c.Aliases[alias], notePath)
		c.aliasExact[exact] = addSortedCandidate(c.aliasExact[exact], notePath)
	}
}

func (c *NotePathCache) Remove(notePath string) {
	if c == nil {
		return
	}
	notePath = strings.TrimSpace(filepath.ToSlash(notePath))
	if notePath == "" {
		return
	}
	c.ensureMutable()
	_, existed := c.NotePaths[notePath]
	delete(c.NotePaths, notePath)
	c.removeAliasesForPath(notePath)
	if existed {
		c.removePathKeys(notePath)
	}
}

func (c *NotePathCache) ensureMutable() {
	if c.Paths == nil {
		c.Paths = map[string]string{}
	}
	if c.Aliases == nil {
		c.Aliases = map[string][]string{}
	}
	if c.aliasExact == nil {
		c.aliasExact = map[string][]string{}
	}
	if c.NotePaths == nil {
		c.NotePaths = map[string]struct{}{}
	}
	if c.pathCandidates == nil || c.explicitPathCandidates == nil || c.noteExtensions == nil {
		c.rebuildPathIndex()
	}
}

func (c *NotePathCache) removeAliasesForPath(notePath string) {
	for alias, candidates := range c.aliasExact {
		candidates = removeCandidate(candidates, notePath)
		if len(candidates) == 0 {
			delete(c.aliasExact, alias)
		} else {
			c.aliasExact[alias] = candidates
		}
	}

	for alias, candidates := range c.Aliases {
		candidates = removeCandidate(candidates, notePath)
		if len(candidates) == 0 {
			delete(c.Aliases, alias)
			continue
		}
		c.Aliases[alias] = candidates
	}
}

func (c *NotePathCache) rebuildPathIndex() {
	c.Paths = make(map[string]string, len(c.NotePaths)*2)
	c.pathCandidates = make(map[string][]string, len(c.NotePaths)*2)
	c.explicitPathCandidates = make(map[string][]string, len(c.NotePaths))
	c.noteExtensions = make(map[string]int)
	notes := make([]string, 0, len(c.NotePaths))
	for notePath := range c.NotePaths {
		notes = append(notes, notePath)
	}
	sort.Strings(notes)
	for _, notePath := range notes {
		c.addPathKeys(notePath)
	}
}

func (c *NotePathCache) addPathKeys(notePath string) {
	key := paths.LinkKey(notePath)
	c.explicitPathCandidates[key] = addSortedCandidate(c.explicitPathCandidates[key], notePath)
	c.noteExtensions[strings.ToLower(filepath.Ext(notePath))]++
	baseName := strings.TrimSuffix(notePath, filepath.Ext(notePath))
	c.addPathCandidate(baseName, notePath)
	fileName := filepath.Base(baseName)
	c.addPathCandidate(fileName, notePath)
}

func (c *NotePathCache) removePathKeys(notePath string) {
	key := paths.LinkKey(notePath)
	candidates := removeCandidate(c.explicitPathCandidates[key], notePath)
	if len(candidates) == 0 {
		delete(c.explicitPathCandidates, key)
	} else {
		c.explicitPathCandidates[key] = candidates
	}
	extension := strings.ToLower(filepath.Ext(notePath))
	if c.noteExtensions[extension] <= 1 {
		delete(c.noteExtensions, extension)
	} else {
		c.noteExtensions[extension]--
	}
	baseName := strings.TrimSuffix(notePath, filepath.Ext(notePath))
	fileName := filepath.Base(baseName)
	c.removePathCandidate(baseName, notePath)
	c.removePathCandidate(fileName, notePath)
}

// ResolveExplicitPath resolves an authored note path using platform-independent NFC and case
// folding. Case-fold collisions remain ambiguous.
func (c *NotePathCache) ResolveExplicitPath(notePath string) (string, bool) {
	if c == nil {
		return "", false
	}
	c.ensureMutable()
	candidates := c.explicitPathCandidates[paths.LinkKey(filepath.ToSlash(notePath))]
	candidates = exactPathCandidates(candidates, filepath.ToSlash(notePath), true)
	if len(candidates) != 1 {
		return "", false
	}
	return candidates[0], true
}

// HasNoteExtension reports whether any authored note uses extension.
func (c *NotePathCache) HasNoteExtension(extension string) bool {
	if c == nil {
		return false
	}
	c.ensureMutable()
	return c.noteExtensions[strings.ToLower(extension)] > 0
}

func (c *NotePathCache) addPathCandidate(key string, notePath string) {
	key = paths.LinkKey(key)
	candidates := addSortedCandidate(c.pathCandidates[key], notePath)
	c.pathCandidates[key] = candidates
	c.updateUniquePath(key, candidates)
}

func (c *NotePathCache) removePathCandidate(key string, notePath string) {
	key = paths.LinkKey(key)
	candidates := removeCandidate(c.pathCandidates[key], notePath)
	if len(candidates) == 0 {
		delete(c.pathCandidates, key)
	} else {
		c.pathCandidates[key] = candidates
	}
	c.updateUniquePath(key, candidates)
}

func (c *NotePathCache) updateUniquePath(key string, candidates []string) {
	if len(candidates) == 1 {
		c.Paths[key] = candidates[0]
		return
	}
	delete(c.Paths, key)
}

func addSortedCandidate(candidates []string, notePath string) []string {
	idx := sort.SearchStrings(candidates, notePath)
	if idx < len(candidates) && candidates[idx] == notePath {
		return candidates
	}
	candidates = append(candidates, "")
	copy(candidates[idx+1:], candidates[idx:])
	candidates[idx] = notePath
	return candidates
}

func removeCandidate(candidates []string, notePath string) []string {
	idx := sort.SearchStrings(candidates, notePath)
	if idx >= len(candidates) || candidates[idx] != notePath {
		return candidates
	}
	return append(candidates[:idx], candidates[idx+1:]...)
}

// Title returns a best-effort display title for a note path.
func (c *NotePathCache) Title(path string) (string, bool) {
	if c == nil {
		return "", false
	}
	normalized := paths.LinkKey(paths.Normalize(path).String())
	if resolved, ok := c.ResolveNoteTarget(normalized); ok {
		return paths.NoteTitle(resolved.Path), true
	}
	var matches []string
	for key := range c.pathCandidates {
		if paths.LinkKey(paths.Normalize(key).String()) == normalized {
			for _, candidate := range c.pathCandidates[key] {
				matches = addSortedCandidate(matches, candidate)
			}
		}
	}
	if len(matches) == 1 {
		return paths.NoteTitle(matches[0]), true
	}
	return "", false
}

// ResolveNote finds the actual note path for a wikilink.
//
// Resolution order:
//  1. exact path or base-name match in Paths (a real file named `Foo.md`
//     always wins over anything else declared as `Foo`)
//  2. filename-only fallback when the link contains path separators
//  3. frontmatter alias match
func (c *NotePathCache) ResolveNote(link string) (string, bool) {
	resolved, ok := c.ResolveNoteTarget(link)
	if !ok {
		return "", false
	}
	return resolved.Path, true
}

func (c *NotePathCache) ResolveNoteTarget(link string) (ResolvedNoteTarget, bool) {
	candidates := c.ResolveNoteCandidates(link)
	if len(candidates) != 1 {
		return ResolvedNoteTarget{}, false
	}
	return candidates[0], true
}

// ResolveNoteCandidates returns every claimant at the highest-precedence
// resolution tier. Results are sorted by vault-relative path. Callers that
// require one target must reject results whose length is not exactly one.
func (c *NotePathCache) ResolveNoteCandidates(link string) []ResolvedNoteTarget {
	// Remove anchors (anything after #) if present
	fragment := ""
	if idx := strings.Index(link, "#"); idx >= 0 {
		fragment = link[idx+1:]
		link = link[:idx]
	}
	return c.resolveNotePathCandidates(link, fragment)
}

// resolveNotePathCandidates accepts a path whose syntax has already been
// decoded. Literal hashes in Markdown filenames must not be split again.
func (c *NotePathCache) resolveNotePathCandidates(link, fragment string) []ResolvedNoteTarget {
	if c == nil {
		return nil
	}
	c.ensureMutable()

	// Preserve exact authored paths before matching extensionless names.
	// Bare Markdown filenames retain basename ambiguity across directories.
	ext := filepath.Ext(link)
	markdownLink := strings.EqualFold(ext, ".md")
	if ext != "" && (!strings.EqualFold(ext, ".md") || strings.Contains(link, "/")) {
		if candidates := c.explicitPathCandidates[paths.LinkKey(link)]; len(candidates) > 0 {
			return c.targets(candidates, fragment)
		}
		// An explicit HTML suffix must not fall back to a Markdown source.
		if strings.EqualFold(ext, ".html") || strings.EqualFold(ext, ".htm") {
			return nil
		}
	}

	// Remove extension if present
	baseName := trimMarkdownExtension(link)

	// Try exact match first
	if candidates := c.pathCandidates[paths.LinkKey(baseName)]; len(candidates) > 0 {
		candidates = exactPathCandidates(candidates, baseName, false)
		if !markdownLink {
			return c.targets(candidates, fragment)
		}
		if candidates = notePathsWithExtension(candidates, ".md"); len(candidates) > 0 {
			return c.targets(candidates, fragment)
		}
	}

	// Dotted Markdown paths can match exactly, but missing explicit paths
	// must not fall back to a different basename or alias.
	if strings.Contains(link, "/") && ext != "" && !strings.EqualFold(ext, ".md") {
		return nil
	}

	// If link contains path separators, try without them
	if strings.Contains(baseName, "/") {
		fileName := filepath.Base(baseName)
		if candidates := c.pathCandidates[paths.LinkKey(fileName)]; len(candidates) > 0 {
			candidates = exactPathCandidates(candidates, fileName, false)
			if !markdownLink {
				return c.targets(candidates, fragment)
			}
			if candidates = notePathsWithExtension(candidates, ".md"); len(candidates) > 0 {
				return c.targets(candidates, fragment)
			}
		}
	}

	if candidates := c.Aliases[paths.LinkKey(baseName)]; len(candidates) > 0 {
		if exact := c.aliasExact[norm.NFC.String(baseName)]; len(exact) == 1 {
			candidates = exact
		}
		if !markdownLink {
			return c.targets(candidates, fragment)
		}
		if candidates = notePathsWithExtension(candidates, ".md"); len(candidates) > 0 {
			return c.targets(candidates, fragment)
		}
	}

	return nil
}

func notePathsWithExtension(candidates []string, extension string) []string {
	filtered := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if strings.EqualFold(filepath.Ext(candidate), extension) {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

func trimMarkdownExtension(path string) string {
	ext := filepath.Ext(path)
	if !strings.EqualFold(ext, ".md") {
		return path
	}
	return strings.TrimSuffix(path, ext)
}

func (c *NotePathCache) targets(paths []string, fragment string) []ResolvedNoteTarget {
	targets := make([]ResolvedNoteTarget, 0, len(paths))
	for _, path := range paths {
		targets = append(targets, ResolvedNoteTarget{Path: path, Fragment: fragment})
	}
	return targets
}

// WikilinkOptions defines options for extracting wikilinks
// FollowWikilinksOptions contains options for following wikilinks
type FollowWikilinksOptions struct {
	markdownsyntax.WikilinkOptions     // Embed the WikilinkOptions struct
	MaxDepth                       int // Maximum depth to follow links
}

// DefaultFollowWikilinksOptions provides standard options for following wikilinks
var DefaultFollowWikilinksOptions = FollowWikilinksOptions{
	WikilinkOptions: markdownsyntax.DefaultWikilinkOptions,
	MaxDepth:        -1, // -1 indicates no limit
}

// CreateWikilinksOptions is a helper function to create a FollowWikilinksOptions struct
// from individual parameters for easier migration from legacy code
func CreateWikilinksOptions(maxDepth int, skipAnchors bool, skipEmbeds bool) FollowWikilinksOptions {
	return FollowWikilinksOptions{
		WikilinkOptions: markdownsyntax.WikilinkOptions{
			SkipAnchors: skipAnchors,
			SkipEmbeds:  skipEmbeds,
		},
		MaxDepth: maxDepth,
	}
}

// FollowWikilinks recursively follows wikilinks (and markdown links when supported)
// from startFile, returning all reachable notes.
//
// Parameters:
//   - visited: shared map to track already-visited paths (prevents cycles)
//   - cache: pre-built NotePathCache for efficient link resolution
//   - options: controls depth limit and which link types to skip
//
// Returns the list of visited note paths (including startFile).
// Depth is decremented per hop; -1 means unlimited.
func FollowWikilinks(vaultDef VaultDefinition, note NoteReader, startFile string, visited map[string]bool, cache *NotePathCache, options FollowWikilinksOptions) ([]string, error) {
	if visited[startFile] {
		return nil, nil
	}
	visited[startFile] = true

	content, err := note.GetContents(vaultDef, startFile)
	if err != nil {
		return nil, err
	}

	result := []string{startFile}

	// Only follow links if maxDepth != 0
	if options.MaxDepth != 0 {
		mdOpts := markdownsyntax.MdLinkOptions{
			SkipAnchors: options.SkipAnchors,
			SkipEmbeds:  options.SkipEmbeds,
		}
		links := markdownsyntax.ScanAllLinks(content, options.WikilinkOptions, mdOpts)

		for _, link := range links {
			var resolvedTarget ResolvedNoteTarget
			var exists bool
			switch link.LinkType {
			case "wikilink":
				if vaultDef.SupportsWikilinks() {
					resolvedTarget, exists = cache.ResolveNoteTarget(link.Target)
				}
			case "mdlink":
				if vaultDef.SupportsMarkdownLinks() {
					resolvedTarget, exists = cache.ResolveMdLinkTarget(link.Target, startFile)
				}
			}

			if exists {
				// Create a new options with decremented MaxDepth for recursion
				nextOptions := options
				if nextOptions.MaxDepth > 0 {
					nextOptions.MaxDepth--
				}

				if followed, err := FollowWikilinks(vaultDef, note, resolvedTarget.Path, visited, cache, nextOptions); err == nil {
					result = append(result, followed...)
				}
			}
		}
	}

	return result, nil
}

func exactPathCandidates(candidates []string, input string, explicit bool) []string {
	input = norm.NFC.String(input)
	var exact []string
	for _, candidate := range candidates {
		name := norm.NFC.String(candidate)
		if !explicit {
			name = strings.TrimSuffix(name, filepath.Ext(name))
		}
		if name == input || !explicit && !strings.Contains(input, "/") && filepath.Base(name) == input {
			exact = append(exact, candidate)
		}
	}
	if len(exact) == 1 {
		return exact
	}
	return candidates
}
