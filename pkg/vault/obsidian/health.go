package obsidian

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// imageExtensions contains common image file extensions to exclude from broken link detection.
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

// BrokenLinksOptions configures broken link detection behavior.
type BrokenLinksOptions struct {
	WikilinkOptions
	IncludeImages bool // If false (default), exclude links to image files
}

// ContextNoteReader is an optional extension for note readers that can stop a
// filesystem or remote read when the health check is cancelled.
type ContextNoteReader interface {
	NoteReader
	GetContentsContext(context.Context, VaultDefinition, string) (string, error)
}

// ContextNoteListReader is an optional extension for note readers that can
// stop note discovery when the health check is cancelled.
type ContextNoteListReader interface {
	NoteReader
	GetNotesListContext(context.Context, VaultDefinition) ([]string, error)
}

// NoteReadError preserves which note read failed while allowing callers to
// inspect the underlying error with errors.Is/errors.As.
type NoteReadError struct {
	Source string
	Target string
	Err    error
}

func (e *NoteReadError) Error() string {
	if e.Target != "" {
		return fmt.Sprintf("read note target %q linked from %q: %v", e.Target, e.Source, e.Err)
	}
	return fmt.Sprintf("read note source %q: %v", e.Source, e.Err)
}

func (e *NoteReadError) Unwrap() error { return e.Err }

// NoteReadErrors retains all note read failures from a scan. The scan can
// still return valid broken-link findings alongside this error.
type NoteReadErrors []*NoteReadError

func (e NoteReadErrors) Error() string {
	if len(e) == 0 {
		return ""
	}
	messages := make([]string, len(e))
	for i, readErr := range e {
		messages[i] = readErr.Error()
	}
	return strings.Join(messages, "; ")
}

func (e NoteReadErrors) Unwrap() []error {
	causes := make([]error, len(e))
	for i, readErr := range e {
		causes[i] = readErr
	}
	return causes
}

type fragmentIndex struct {
	blocks   map[string]struct{}
	headings map[string]struct{}
	// anchors holds GitHub-style heading slugs and HTML anchor names, which
	// Markdown links in repository docs use instead of heading text.
	anchors map[string]struct{}
}

// DefaultBrokenLinksOptions provides standard options for broken link detection.
var DefaultBrokenLinksOptions = BrokenLinksOptions{
	WikilinkOptions: DefaultWikilinkOptions,
	IncludeImages:   false, // Exclude images by default
}

// isImageLink checks if a link target appears to reference an image file.
func isImageLink(target string) bool {
	ext := strings.ToLower(filepath.Ext(target))
	return imageExtensions[ext]
}

// BrokenLinkReason classifies why a wikilink is broken.
type BrokenLinkReason string

const (
	// BrokenLinkReasonNoteMissing means the target note does not exist.
	BrokenLinkReasonNoteMissing BrokenLinkReason = "note_missing"
	// BrokenLinkReasonBlockMissing means the target note exists but the
	// `^block-id` fragment does not resolve to any anchor in that note.
	BrokenLinkReasonBlockMissing BrokenLinkReason = "block_missing"
	// BrokenLinkReasonHeadingMissing means the target note exists but the
	// heading-text fragment does not match any heading in that note.
	BrokenLinkReasonHeadingMissing BrokenLinkReason = "heading_missing"
)

// BrokenLink represents a wikilink whose target cannot be fully resolved.
// Reason discriminates between missing notes (the original case) and
// fragment-level mismatches (block ID or heading text typos against an
// otherwise-resolvable note).
//
// WHY: SPEC-0023's orphan-block-id check needs to distinguish "this anchor
// has no inbound references" from "this anchor has a typo'd inbound
// reference"; the latter must be retained as a soft hold. Surfacing
// fragment-level breakage here is the substrate the orphan check consumes.
// Coderefs: [[linkable-embedded-node-identifiers#^spec-0023-us4]]
type BrokenLink struct {
	Source   string           `json:"source"`
	Target   string           `json:"target"`
	LinkType BacklinkType     `json:"linkType"`
	Alias    string           `json:"alias,omitempty"`
	Fragment string           `json:"fragment,omitempty"`
	Reason   BrokenLinkReason `json:"reason,omitempty"`
	// Line and Raw locate the authored occurrence; Raw is the complete link
	// syntax so repairs can keep its display text.
	Line int    `json:"line,omitempty"`
	Raw  string `json:"raw,omitempty"`
	// Markdown marks a [text](path) link rather than a wikilink.
	Markdown bool `json:"markdown,omitempty"`
}

// DeadEndNote represents a note with inbound links but no outbound links.
type DeadEndNote struct {
	Path         string `json:"path"`
	InboundLinks int    `json:"inboundLinks"`
}

// StaleNote represents a note not modified within the threshold.
type StaleNote struct {
	Path                  string    `json:"path"`
	LastModified          time.Time `json:"lastModified"`
	DaysSinceModification int       `json:"daysSinceModification"`
}

// MergeSuggestion represents two notes that may be consolidation candidates.
type MergeSuggestion struct {
	Note1      string  `json:"note1"`
	Note2      string  `json:"note2"`
	Similarity float64 `json:"similarity"`
	Reason     string  `json:"reason"`
}

type OntologyIssue struct {
	Code       string `json:"code"`
	NotePath   string `json:"notePath,omitempty"`
	TypeName   string `json:"typeName,omitempty"`
	FieldName  string `json:"fieldName,omitempty"`
	Line       int    `json:"line,omitempty"`
	LinkKind   string `json:"linkKind,omitempty"`
	LinkTarget string `json:"linkTarget,omitempty"`
	Message    string `json:"message"`
}

// HealthStats provides summary counts for vault health analysis.
type HealthStats struct {
	TotalNotes                int `json:"totalNotes"`
	BrokenLinkCount           int `json:"brokenLinkCount"`
	StaleNoteCount            int `json:"staleNoteCount"`
	DeadEndCount              int `json:"deadEndCount"`
	MergeSuggestionCount      int `json:"mergeSuggestionCount"`
	OntologyIssueCount        int `json:"ontologyIssueCount,omitempty"`
	SurprisingConnectionCount int `json:"surprisingConnectionCount,omitempty"`
}

// SurprisingConnectionEntry is a single surprising graph edge in the health report.
type SurprisingConnectionEntry struct {
	SrcPath    string   `json:"srcPath"`
	DstPath    string   `json:"dstPath"`
	EdgeKind   string   `json:"edgeKind"`
	Score      float64  `json:"score"`
	Confidence string   `json:"confidence"`
	Reasons    []string `json:"reasons,omitempty"`
}

// HealthReport aggregates all vault health findings.
type HealthReport struct {
	Vault                 string                      `json:"vault"`
	VaultPath             string                      `json:"vaultPath"`
	AnalyzedAt            time.Time                   `json:"analyzedAt"`
	Stats                 HealthStats                 `json:"stats"`
	BrokenLinks           []BrokenLink                `json:"brokenLinks"`
	StaleNotes            []StaleNote                 `json:"staleNotes"`
	DeadEnds              []DeadEndNote               `json:"deadEnds"`
	SuggestedMerges       []MergeSuggestion           `json:"suggestedMerges"`
	OntologyIssues        []OntologyIssue             `json:"ontologyIssues,omitempty"`
	SurprisingConnections []SurprisingConnectionEntry `json:"surprisingConnections,omitempty"`
}

// punctuationRegex matches punctuation characters for removal during normalization.
var punctuationRegex = regexp.MustCompile(`[^\w\s]`)

// whitespaceRegex matches multiple whitespace characters for collapsing.
var whitespaceRegex = regexp.MustCompile(`\s+`)

// NormalizeName converts a note name to a normalized form for comparison:
// - Converts to lowercase
// - Removes punctuation
// - Collapses multiple whitespace to single space
// - Trims leading/trailing whitespace
func NormalizeName(name string) string {
	// Remove .md extension if present
	name = RemoveMdSuffix(name)

	// Convert to lowercase
	name = strings.ToLower(name)

	// Remove punctuation
	name = punctuationRegex.ReplaceAllString(name, "")

	// Collapse whitespace
	name = whitespaceRegex.ReplaceAllString(name, " ")

	// Trim
	name = strings.TrimSpace(name)

	return name
}

// HeadingInfo describes one Markdown heading exposed by a note.
type HeadingInfo struct {
	Text           string `json:"text"`
	Level          int    `json:"level"`
	NormalizedText string `json:"normalizedText"`
	Line           int    `json:"line"`
	ParentText     string `json:"parentText,omitempty"`
}

// EnumerateHeadings returns canonical Markdown headings with
// resolver-normalized text and parent heading context.
func EnumerateHeadings(content string) []HeadingInfo {
	var headings []HeadingInfo
	parentByLevel := map[int]string{}
	for _, target := range EnumerateMarkdownTargets(content) {
		if target.Kind != MarkdownTargetHeading {
			continue
		}
		parent := ""
		for l := target.Level - 1; l >= 1; l-- {
			if candidate := strings.TrimSpace(parentByLevel[l]); candidate != "" {
				parent = candidate
				break
			}
		}
		for l := target.Level; l <= 6; l++ {
			delete(parentByLevel, l)
		}
		parentByLevel[target.Level] = target.Text
		headings = append(headings, HeadingInfo{
			Text:           target.Text,
			Level:          target.Level,
			NormalizedText: target.NormalizedText,
			Line:           target.Line,
			ParentText:     parent,
		})
	}
	return headings
}

// extractBlockIDsAndHeadings returns the block IDs and heading keys present
// in note content. Headings compare the way Obsidian's resolveSubpath does;
// block IDs stay case-sensitive because they back Rhizome node identifiers.
//
// WHY: fragment-broken-link detection needs to know what the target note
// actually exposes. The canonical parser remains in pkg/vault/obsidian so
// link health and persisted raw-note targets share syntax without depending on
// ontology projection.
// Coderefs: [[linkable-embedded-node-identifiers#^spec-0023-us4]]
func extractBlockIDsAndHeadings(content string) (map[string]struct{}, map[string]struct{}) {
	blocks := map[string]struct{}{}
	headings := map[string]struct{}{}
	for _, target := range EnumerateMarkdownTargets(content) {
		switch target.Kind {
		case MarkdownTargetBlock:
			blocks[target.Text] = struct{}{}
		case MarkdownTargetHeading:
			headings[ObsidianHeadingKey(target.Text)] = struct{}{}
		}
	}
	return blocks, headings
}

func buildFragmentIndex(content string) fragmentIndex {
	blocks, headings := extractBlockIDsAndHeadings(content)
	anchors := map[string]struct{}{}
	slugCounts := map[string]int{}
	for _, target := range EnumerateMarkdownTargets(content) {
		if target.Kind == MarkdownTargetHeading {
			// Renderers suffix repeated slugs: #setup, #setup-1, #setup-2.
			slug := githubHeadingSlug(target.Text)
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

// githubHeadingSlug is the anchor GitHub-flavored renderers give a heading:
// lowercase, punctuation removed, spaces become hyphens.
func githubHeadingSlug(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(heading)) {
		switch {
		case r == ' ':
			b.WriteRune('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

var obsidianHeadingPunctuation = regexp.MustCompile("[!\"#$%&()*+,.:;<=>?@^`{|}~/\\[\\]\\\\\r\n]")

// ObsidianHeadingKey mirrors Obsidian's stripHeading plus lowercasing: the
// comparison its resolveSubpath uses between a link fragment and a heading.
// Emoji and typographic dashes are not stripped, matching Obsidian.
func ObsidianHeadingKey(heading string) string {
	stripped := obsidianHeadingPunctuation.ReplaceAllString(heading, " ")
	return strings.ToLower(strings.Join(strings.Fields(stripped), " "))
}

// NormalizeWikilinkFragment lowercases and trims a heading-text fragment so
// `[[note#Heading Text]]` matches a heading authored as `## Heading Text`.
// Obsidian itself is more forgiving (case-insensitive, whitespace-flexible);
// this match keeps the same posture without requiring full Obsidian parsing.
func NormalizeWikilinkFragment(s string) string {
	return NormalizeMarkdownHeadingFragment(s)
}

func normalizeWikilinkFragment(s string) string {
	return NormalizeWikilinkFragment(s)
}

// FindBrokenLinks scans the vault and returns all wikilinks whose target
// cannot be fully resolved. The check covers two classes:
//
//   - the target note does not exist (`note_missing`)
//   - the target note exists but the trailing `^block-id` or heading-text
//     fragment does not resolve in that note (`block_missing` or
//     `heading_missing`)
//
// By default, excludes links to image files unless IncludeImages is true.
//
// WHY: SPEC-0023's orphan-block-id check needs fragment-level reference
// accounting so a typo'd inbound `[[note#^typo]]` keeps the real `^typo`
// anchor as a soft hold rather than orphaning it. Coderefs:
// [[linkable-embedded-node-identifiers#^spec-0023-us4]]
func FindBrokenLinks(vaultDef VaultDefinition, note NoteReader, options BrokenLinksOptions) ([]BrokenLink, error) {
	return FindBrokenLinksContext(context.Background(), vaultDef, note, options)
}

// FindBrokenLinksContext is the context-aware form of FindBrokenLinks.
func FindBrokenLinksContext(ctx context.Context, vaultDef VaultDefinition, note NoteReader, options BrokenLinksOptions) ([]BrokenLink, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	allNotes, err := getNotesListContext(ctx, vaultDef, note)
	if err != nil {
		return nil, err
	}

	var aliasesByPath map[string][]string
	if provider, ok := note.(NoteEntriesProvider); ok {
		if entries, snapErr := provider.NoteEntriesSnapshot(ctx); snapErr == nil {
			aliasesByPath = AliasesFromNoteEntries(entries)
		} else if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	contentByPath, readErrByPath, loadErr := loadNoteContents(ctx, vaultDef, note, allNotes)
	if loadErr != nil {
		return nil, loadErr
	}
	getContent := func(p string) string { return contentByPath[p] }
	if aliasesByPath == nil {
		aliasesByPath = aliasesFromNotePaths(allNotes, getContent)
	}
	cache := BuildNotePathCacheWithAliases(allNotes, aliasesByPath)
	fragmentByPath := make(map[string]fragmentIndex, len(allNotes))
	for _, notePath := range allNotes {
		fragmentByPath[notePath] = buildFragmentIndex(getContent(notePath))
	}
	resolver := &linkResolver{
		cache:    cache,
		files:    LazyVaultFileIndex(vaultDef, allNotes),
		markdown: vaultDef.SupportsMarkdownLinks(),
		wikilink: vaultDef.SupportsWikilinks(),
		root:     vaultFileRoot(vaultDef),
	}

	brokenByNote := make([][]BrokenLink, len(allNotes))
	readErrorsByNote := make([]NoteReadErrors, len(allNotes))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < brokenLinkWorkerCount(len(allNotes)); worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				var idx int
				select {
				case <-ctx.Done():
					return
				case jobIdx, ok := <-jobs:
					if !ok {
						return
					}
					idx = jobIdx
				}
				notePath := allNotes[idx]
				content := getContent(notePath)
				if readErr := readErrByPath[notePath]; readErr != nil {
					readErrorsByNote[idx] = NoteReadErrors{&NoteReadError{Source: notePath, Err: readErr}}
					continue
				}
				broken, readErrors := scanBrokenLinksInNote(notePath, content, resolver, fragmentByPath, readErrByPath, options)
				brokenByNote[idx] = broken
				readErrorsByNote[idx] = readErrors
			}
		}()
	}
sendJobs:
	for idx := range allNotes {
		select {
		case <-ctx.Done():
			break sendJobs
		case jobs <- idx:
		}
	}
	close(jobs)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var brokenLinks []BrokenLink
	var readErrors NoteReadErrors
	for idx, links := range brokenByNote {
		brokenLinks = append(brokenLinks, links...)
		readErrors = append(readErrors, readErrorsByNote[idx]...)
	}
	if len(readErrors) > 0 {
		targetReadErrors := make(map[string]struct{})
		for _, readErr := range readErrors {
			if readErr.Target != "" {
				targetReadErrors[readErr.Target] = struct{}{}
			}
		}
		filteredReadErrors := make(NoteReadErrors, 0, len(readErrors))
		for _, readErr := range readErrors {
			if readErr.Target == "" {
				if _, linked := targetReadErrors[readErr.Source]; linked {
					continue
				}
			}
			filteredReadErrors = append(filteredReadErrors, readErr)
		}
		readErrors = filteredReadErrors
	}

	if len(readErrors) > 0 {
		return brokenLinks, readErrors
	}
	return brokenLinks, nil
}

func getNotesListContext(ctx context.Context, vaultDef VaultDefinition, note NoteReader) ([]string, error) {
	if reader, ok := note.(ContextNoteListReader); ok {
		return reader.GetNotesListContext(ctx, vaultDef)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	allNotes, err := note.GetNotesList(vaultDef)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return allNotes, nil
}

func loadNoteContents(ctx context.Context, vaultDef VaultDefinition, note NoteReader, allNotes []string) (map[string]string, map[string]error, error) {
	contents := make([]string, len(allNotes))
	readErrors := make([]error, len(allNotes))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < brokenLinkWorkerCount(len(allNotes)); worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				var idx int
				select {
				case <-ctx.Done():
					return
				case jobIdx, ok := <-jobs:
					if !ok {
						return
					}
					idx = jobIdx
				}
				body, err := getContentsContext(ctx, vaultDef, note, allNotes[idx])
				if err != nil {
					readErrors[idx] = err
					continue
				}
				contents[idx] = body
			}
		}()
	}
sendJobs:
	for idx := range allNotes {
		select {
		case <-ctx.Done():
			break sendJobs
		case jobs <- idx:
		}
	}
	close(jobs)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	contentByPath := make(map[string]string, len(allNotes))
	readErrByPath := make(map[string]error)
	for idx, notePath := range allNotes {
		contentByPath[notePath] = contents[idx]
		if readErrors[idx] != nil {
			readErrByPath[notePath] = readErrors[idx]
		}
	}
	return contentByPath, readErrByPath, nil
}

func getContentsContext(ctx context.Context, vaultDef VaultDefinition, note NoteReader, notePath string) (string, error) {
	if reader, ok := note.(ContextNoteReader); ok {
		return reader.GetContentsContext(ctx, vaultDef, notePath)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	content, err := note.GetContents(vaultDef, notePath)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", ctxErr
	}
	return content, err
}

func brokenLinkWorkerCount(total int) int {
	if total <= 1 {
		return 1
	}
	workers := runtime.GOMAXPROCS(0)
	if workers < 4 {
		workers = 4
	}
	if workers > 16 {
		workers = 16
	}
	if workers > total {
		workers = total
	}
	return workers
}

// linkResolver answers "does this link open a file in Obsidian". Rhizome's
// note cache (with its alias tier) goes first; Obsidian's own rules over every
// vault file are the fallback, so case-only differences, ambiguous names,
// attachments, and files under ignored paths all resolve.
type linkResolver struct {
	cache    *NotePathCache
	files    func() *VaultFileIndex
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
	target := DecodeMarkdownPath(rawTarget)
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

// LazyVaultFileIndex builds the Obsidian file index on first use. Indexed note
// paths are included so in-memory readers resolve the same way as disk.
func LazyVaultFileIndex(vaultDef VaultDefinition, notes []string) func() *VaultFileIndex {
	return sync.OnceValue(func() *VaultFileIndex {
		files := append([]string(nil), notes...)
		if index, err := BuildVaultFileIndex(vaultDef); err == nil {
			for _, names := range index.byName {
				files = append(files, names...)
			}
		}
		seen := make(map[string]struct{}, len(files))
		unique := files[:0]
		for _, file := range files {
			if _, dup := seen[file]; !dup {
				seen[file] = struct{}{}
				unique = append(unique, file)
			}
		}
		return NewVaultFileIndex(unique)
	})
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
		for _, link := range ScanWikilinks(content, options.WikilinkOptions) {
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
			if link.LinkType == BacklinkTypeAlias {
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
		for _, link := range ScanStructuredLinks(content) {
			if link.Kind != StructuredLinkMarkdown {
				continue
			}
			if options.SkipEmbeds && link.Embed || options.SkipAnchors && strings.Contains(link.Target, "#") {
				continue
			}
			rawTarget, fragment := link.Path, link.Fragment
			target := DecodeMarkdownPath(rawTarget)
			// Markdown links are validated only when they name a note: a `.md`
			// path or an extensionless path. Other files are left to Obsidian.
			if ext := strings.ToLower(path.Ext(target)); target != "" && ext != "" && ext != ".md" || strings.HasSuffix(target, "/") {
				continue
			}
			broken := BrokenLink{
				Source: notePath, Target: target, LinkType: BacklinkTypeBasic,
				Fragment: fragment, Line: 1 + strings.Count(content[:link.RawSpan.Start], "\n"),
				Raw: link.RawSpan.Text(content), Markdown: true, Alias: link.Display,
			}
			if link.Embed {
				broken.LinkType = BacklinkTypeEmbed
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
		if _, ok := fragments.headings[ObsidianHeadingKey(part)]; !ok {
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
		fm, err := ExtractFrontmatter(getContent(notePath))
		if err != nil {
			continue
		}
		if aliases := AliasListFromFrontmatter(fm); len(aliases) > 0 {
			aliasesByPath[notePath] = aliases
		}
	}
	if len(aliasesByPath) == 0 {
		return nil
	}
	return aliasesByPath
}

// FindDeadEnds identifies notes with inbound links but no outbound links.
// Self-links are excluded from the outbound count.
func FindDeadEnds(analysis *GraphAnalysis) []DeadEndNote {
	var deadEnds []DeadEndNote

	for path, node := range analysis.Nodes {
		if node.Outbound == 0 && node.Inbound > 0 {
			deadEnds = append(deadEnds, DeadEndNote{
				Path:         path,
				InboundLinks: node.Inbound,
			})
		}
	}

	return deadEnds
}

// FindStaleNotes returns notes that haven't been modified within the threshold.
func FindStaleNotes(vaultDef VaultDefinition, note NoteReader, thresholdDays int) ([]StaleNote, error) {
	allNotes, err := note.GetNotesList(vaultDef)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	threshold := now.AddDate(0, 0, -thresholdDays)
	var staleNotes []StaleNote

	for _, notePath := range allNotes {
		modTime, err := note.GetModTime(vaultDef, notePath)
		if err != nil {
			// Skip files we can't stat
			continue
		}

		if modTime.Before(threshold) {
			daysSince := int(now.Sub(modTime).Hours() / 24)
			staleNotes = append(staleNotes, StaleNote{
				Path:                  notePath,
				LastModified:          modTime,
				DaysSinceModification: daysSince,
			})
		}
	}

	return staleNotes, nil
}

// FindMergeSuggestions identifies notes with matching normalized names.
func FindMergeSuggestions(allNotes []string) []MergeSuggestion {
	// Build a map of normalized names to original paths
	normMap := make(map[string][]string)
	for _, notePath := range allNotes {
		// Get just the filename without directory
		baseName := strings.TrimSuffix(notePath, ".md")
		fileName := baseName
		if idx := strings.LastIndex(baseName, "/"); idx >= 0 {
			fileName = baseName[idx+1:]
		}
		normalized := NormalizeName(fileName)
		normMap[normalized] = append(normMap[normalized], notePath)
	}

	var suggestions []MergeSuggestion
	// Find all groups with more than one note
	for _, paths := range normMap {
		if len(paths) > 1 {
			// Generate pairs
			for i := 0; i < len(paths); i++ {
				for j := i + 1; j < len(paths); j++ {
					suggestions = append(suggestions, MergeSuggestion{
						Note1:      paths[i],
						Note2:      paths[j],
						Similarity: 1.0,
						Reason:     "normalized name match",
					})
				}
			}
		}
	}

	return suggestions
}
