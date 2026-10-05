package notemeta

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type EnsureResult struct {
	NotesHash string
	Dirty     bool
}

const noteMetadataIndexerVersion = "12"

type noteEntry struct {
	Path            string
	Title           string
	Content         string
	Source          noteformat.AuthoredSource
	Projection      noteformat.Projection
	Frontmatter     map[string]any
	InlineProps     map[string][]string
	Tags            []string
	Aliases         []string
	Links           []noteformat.UnresolvedAuthoredLinkFact
	FragmentTargets []noteformat.FragmentTargetFact
	Mtime           int64
	Size            int64
}

type cacheEntriesProvider interface {
	EntriesSnapshot(context.Context) ([]cache.Entry, error)
}

type metadataPathProvider interface {
	CurrentNoteMetadataPaths(context.Context) ([]string, error)
}

type metadataRowsProvider interface {
	CurrentNoteMetadataRowsByPaths(context.Context, []string) (map[string]semdb.NoteMetadataRow, error)
}

type MetadataState = semdb.NoteMetadataState
type MetadataSnapshot = semdb.NoteMetadataSnapshot
type MetadataDelta = semdb.NoteMetadataDelta
type MetadataRow = semdb.NoteMetadataRow

type Store interface {
	GetNoteMetadataState(context.Context) (MetadataState, error)
	ReplaceNoteMetadataSnapshot(context.Context, MetadataSnapshot) error
	ApplyNoteMetadataDelta(context.Context, MetadataDelta) error
	AllNoteMetadataPaths(context.Context) ([]string, error)
	CurrentNoteMetadataPaths(context.Context) ([]string, error)
	CurrentNoteMetadataRowsByPaths(context.Context, []string) (map[string]MetadataRow, error)
	ResolveStoredNoteLinks(context.Context, []string) (map[string]string, error)
	CurrentNoteAliases(context.Context) (map[string][]string, error)
}

func loadEntries(ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader) ([]noteEntry, error) {
	if provider, ok := noteMgr.(cacheEntriesProvider); ok {
		entries, err := provider.EntriesSnapshot(ctx)
		if err == nil {
			out := make([]noteEntry, 0, len(entries))
			for _, entry := range entries {
				title := titleFor(entry.Path, entry.Content, entry.Frontmatter)
				out = append(out, noteEntry{
					Path:        entry.Path,
					Title:       title,
					Content:     entry.Content,
					Frontmatter: cloneMap(entry.Frontmatter),
					InlineProps: cloneInline(entry.InlineProps),
					Tags:        dedupeStrings(entry.Tags),
					Mtime:       entry.ModTime.Unix(),
					Size:        entry.Size,
				})
			}
			sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
			return out, nil
		}
	}

	pathsList, err := noteMgr.GetNotesList(vaultDef)
	if err != nil {
		return nil, err
	}
	sort.Strings(pathsList)
	if len(pathsList) == 0 {
		return nil, nil
	}
	workerCount := runtime.GOMAXPROCS(0)
	if workerCount < 1 {
		workerCount = 1
	}
	if workerCount > len(pathsList) {
		workerCount = len(pathsList)
	}
	type job struct {
		idx  int
		path string
	}
	type result struct {
		idx   int
		entry noteEntry
		err   error
	}
	jobs := make(chan job, workerCount)
	results := make(chan result, len(pathsList))
	var wg sync.WaitGroup
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				entry, err := readNoteEntry(vaultDef, noteMgr, job.path)
				results <- result{idx: job.idx, entry: entry, err: err}
			}
		}()
	}
	for idx, notePath := range pathsList {
		jobs <- job{idx: idx, path: notePath}
	}
	close(jobs)
	go func() {
		wg.Wait()
		close(results)
	}()

	outByIdx := make([]noteEntry, len(pathsList))
	for res := range results {
		if res.err != nil {
			return nil, res.err
		}
		outByIdx[res.idx] = res.entry
	}
	out := make([]noteEntry, 0, len(outByIdx))
	for _, entry := range outByIdx {
		if strings.TrimSpace(entry.Path) == "" {
			continue
		}
		out = append(out, entry)
	}
	return out, nil
}

func readNoteEntry(vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, notePath string) (noteEntry, error) {
	content, err := noteMgr.GetContents(vaultDef, notePath)
	if err != nil {
		return noteEntry{}, fmt.Errorf("read note %s: %w", notePath, err)
	}
	fm, _ := obsidian.ExtractFrontmatter(content)
	inline := obsidian.ExtractInlineProperties(content)
	tags := extractAllTags(content, fm)
	modTime, err := noteMgr.GetModTime(vaultDef, notePath)
	if err != nil {
		return noteEntry{}, fmt.Errorf("stat note %s: %w", notePath, err)
	}
	return noteEntry{
		Path:        notePath,
		Title:       titleFor(notePath, content, fm),
		Content:     content,
		Frontmatter: cloneMap(fm),
		InlineProps: cloneInline(inline),
		Tags:        tags,
		Mtime:       modTime.Unix(),
		Size:        int64(len(content)),
	}, nil
}

// LoadAliasMap fetches the frontmatter aliases map from the store so
// callers can build an alias-aware NotePathCache without reaching into
// the SQLite layer. Returns nil (not an error) when store is nil or
// the read fails so it composes cleanly with callers that may not
// have an index yet — the resulting cache silently degrades to
// filename-only resolution in that case.
func LoadAliasMap(ctx context.Context, store *semdb.Store) map[string][]string {
	if store == nil {
		return nil
	}
	rows, err := store.CurrentNoteAliases(ctx)
	if err != nil {
		return nil
	}
	return rows
}

// extractAliasList accepts scalar and list alias values for legacy source facts.
func extractAliasList(raw any) []string {
	switch current := raw.(type) {
	case nil:
		return nil
	case []interface{}:
		out := make([]string, 0, len(current))
		for _, item := range current {
			if s := aliasString(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(current))
		for _, item := range current {
			if s := strings.TrimSpace(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		if s := aliasString(current); s != "" {
			return []string{s}
		}
		return nil
	}
}

func aliasString(raw any) string {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return ""
	}
}

func inlineRows(notePath, propertyName string, values []string) []semdb.NotePropertyValueRow {
	propertyName = strings.ToLower(strings.TrimSpace(propertyName))
	rows := make([]semdb.NotePropertyValueRow, 0, max(1, len(values)))
	if len(values) == 0 {
		return []semdb.NotePropertyValueRow{{
			NotePath:     notePath,
			PropertyName: strings.TrimSpace(propertyName),
			Source:       semdb.NotePropertySourceInline,
			ValueKind:    semdb.NotePropertyValueUnknown,
		}}
	}
	for idx, value := range values {
		text, norm, kind := scalarMetadata(value)
		rows = append(rows, semdb.NotePropertyValueRow{
			NotePath:     notePath,
			PropertyName: strings.TrimSpace(propertyName),
			Source:       semdb.NotePropertySourceInline,
			ValueText:    text,
			ValueNorm:    norm,
			ValueKind:    kind,
			IsList:       len(values) > 1,
			ListOrdinal:  idx,
		})
	}
	return rows
}

func scalarMetadata(raw any) (string, string, semdb.NotePropertyValueKind) {
	switch current := raw.(type) {
	case nil:
		return "", "", semdb.NotePropertyValueUnknown
	case bool:
		text := strconv.FormatBool(current)
		return text, normalizePropertyValue(text), semdb.NotePropertyValueBool
	case int:
		text := strconv.Itoa(current)
		return text, normalizePropertyValue(text), semdb.NotePropertyValueInt
	case int64:
		text := strconv.FormatInt(current, 10)
		return text, normalizePropertyValue(text), semdb.NotePropertyValueInt
	case float64:
		text := strconv.FormatFloat(current, 'f', -1, 64)
		return text, normalizePropertyValue(text), semdb.NotePropertyValueFloat
	case float32:
		text := strconv.FormatFloat(float64(current), 'f', -1, 64)
		return text, normalizePropertyValue(text), semdb.NotePropertyValueFloat
	case time.Time:
		if current.Hour() != 0 || current.Minute() != 0 || current.Second() != 0 || current.Nanosecond() != 0 {
			text := current.Format(time.RFC3339)
			return text, normalizePropertyValue(text), semdb.NotePropertyValueDateTime
		}
		text := current.Format("2006-01-02")
		return text, normalizePropertyValue(text), semdb.NotePropertyValueDate
	case string:
		info := obsidian.AnalyzePropertyValue(current)
		text := strings.TrimSpace(current)
		return text, normalizePropertyValue(text), mapValueKind(info.ValueType)
	default:
		return "", "", semdb.NotePropertyValueUnknown
	}
}

func mapValueKind(kind string) semdb.NotePropertyValueKind {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "bool":
		return semdb.NotePropertyValueBool
	case "int":
		return semdb.NotePropertyValueInt
	case "float":
		return semdb.NotePropertyValueFloat
	case "date":
		return semdb.NotePropertyValueDate
	case "datetime":
		return semdb.NotePropertyValueDateTime
	case "url":
		return semdb.NotePropertyValueURL
	case "wikilink":
		return semdb.NotePropertyValueWikilink
	case "string":
		return semdb.NotePropertyValueString
	default:
		return semdb.NotePropertyValueUnknown
	}
}

func computeNotesHash(ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader) (string, []string, error) {
	if provider, ok := noteMgr.(cacheEntriesProvider); ok {
		entries, err := provider.EntriesSnapshot(ctx)
		if err == nil {
			pathsList := make([]string, 0, len(entries))
			for _, entry := range entries {
				if path := strings.TrimSpace(entry.Path); path != "" {
					pathsList = append(pathsList, path)
				}
			}
			sort.Strings(pathsList)
			return notesHashFromEntries(entries), pathsList, nil
		}
	}
	pathsList, err := noteMgr.GetNotesList(vaultDef)
	if err != nil {
		return "", nil, err
	}
	sort.Strings(pathsList)
	var aggregate [sha256.Size]byte
	for _, notePath := range pathsList {
		noteContentHash := ""
		if content, err := noteMgr.GetContents(vaultDef, notePath); err == nil {
			noteContentHash = contentHash(content)
		}
		xorDigest(&aggregate, noteStateDigest(notePath, noteContentHash))
	}
	return hex.EncodeToString(aggregate[:]), pathsList, nil
}

func noteMetadataPathsMaterialized(ctx context.Context, provider metadataPathProvider, expectedPaths []string) (bool, error) {
	if provider == nil {
		return false, nil
	}
	currentPaths, err := provider.CurrentNoteMetadataPaths(ctx)
	if err != nil {
		return false, err
	}
	expectedPaths = dedupeStrings(expectedPaths)
	currentPaths = dedupeStrings(currentPaths)
	if len(expectedPaths) != len(currentPaths) {
		return false, nil
	}
	for i := range expectedPaths {
		if expectedPaths[i] != currentPaths[i] {
			return false, nil
		}
	}
	return true, nil
}

func notesHashFromEntries(entries []cache.Entry) string {
	var aggregate [sha256.Size]byte
	for _, entry := range entries {
		path := strings.TrimSpace(entry.Path)
		if path == "" {
			continue
		}
		xorDigest(&aggregate, noteStateDigest(path, contentHash(entry.Content)))
	}
	return hex.EncodeToString(aggregate[:])
}

func notesHashFromNoteEntries(entries []noteEntry) string {
	var aggregate [sha256.Size]byte
	for _, entry := range entries {
		path := strings.TrimSpace(entry.Path)
		if path == "" {
			continue
		}
		xorDigest(&aggregate, noteStateDigest(path, contentHash(entry.Content)))
	}
	return hex.EncodeToString(aggregate[:])
}

func incrementalNotesHash(ctx context.Context, provider metadataRowsProvider, currentNotesHash string, entries []noteEntry, deletedPaths []string) (string, error) {
	if provider == nil {
		return "", nil
	}
	aggregate, err := decodeNotesHash(currentNotesHash)
	if err != nil {
		return "", err
	}
	touchedPaths := make([]string, 0, len(entries)+len(deletedPaths))
	for _, entry := range entries {
		if path := strings.TrimSpace(entry.Path); path != "" {
			touchedPaths = append(touchedPaths, path)
		}
	}
	touchedPaths = append(touchedPaths, deletedPaths...)
	oldRows, err := provider.CurrentNoteMetadataRowsByPaths(ctx, dedupeStrings(touchedPaths))
	if err != nil {
		return "", err
	}
	for _, row := range oldRows {
		xorDigest(&aggregate, noteStateDigest(row.Path, strings.TrimSpace(row.ContentHash)))
	}
	for _, entry := range entries {
		path := strings.TrimSpace(entry.Path)
		if path == "" {
			continue
		}
		xorDigest(&aggregate, noteStateDigest(path, contentHash(entry.Content)))
	}
	return hex.EncodeToString(aggregate[:]), nil
}

func notesHashFromRows(rows []semdb.NoteMetadataRow) string {
	var aggregate [sha256.Size]byte
	for _, row := range rows {
		path := strings.TrimSpace(row.Path)
		if path == "" {
			continue
		}
		xorDigest(&aggregate, noteStateDigest(path, strings.TrimSpace(row.ContentHash)))
	}
	return hex.EncodeToString(aggregate[:])
}

func metadataStateHash(vaultDef obsidian.VaultDefinition, notesHash string) string {
	sum := sha256.New()
	_, _ = sum.Write([]byte(noteMetadataIndexerVersion))
	_, _ = sum.Write([]byte{0})
	_, _ = sum.Write([]byte(notesHash))
	_, _ = sum.Write([]byte{0})
	_, _ = sum.Write([]byte(vaultDef.Links))
	_, _ = sum.Write([]byte{0})
	_, _ = sum.Write([]byte(vaultDef.BasePath()))
	_, _ = sum.Write([]byte{0})
	includes := append([]string(nil), vaultDef.Includes...)
	excludes := append([]string(nil), vaultDef.Excludes...)
	sort.Strings(includes)
	sort.Strings(excludes)
	for _, pattern := range includes {
		_, _ = sum.Write([]byte(pattern))
		_, _ = sum.Write([]byte{0})
	}
	_, _ = sum.Write([]byte{1})
	for _, pattern := range excludes {
		_, _ = sum.Write([]byte(pattern))
		_, _ = sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func MetadataStateReady(ctx context.Context, store Store) (bool, error) {
	if store == nil {
		return false, nil
	}
	state, err := store.GetNoteMetadataState(ctx)
	if err != nil {
		return false, err
	}
	return state.Ready && state.LoadedAt > 0, nil
}

func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// noteStateDigest hashes content identity only: filesystem mtime and size are
// freshness evidence persisted on the row, never part of the aggregate hash.
func noteStateDigest(path string, contentHash string) [sha256.Size]byte {
	sum := sha256.New()
	_, _ = sum.Write([]byte(strings.TrimSpace(path)))
	_, _ = sum.Write([]byte{0})
	_, _ = sum.Write([]byte(strings.TrimSpace(contentHash)))
	var out [sha256.Size]byte
	copy(out[:], sum.Sum(nil))
	return out
}

func xorDigest(dst *[sha256.Size]byte, value [sha256.Size]byte) {
	for i := range dst {
		dst[i] ^= value[i]
	}
}

func decodeNotesHash(value string) ([sha256.Size]byte, error) {
	var out [sha256.Size]byte
	value = strings.TrimSpace(value)
	if value == "" {
		return out, nil
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return out, err
	}
	if len(decoded) != len(out) {
		return out, fmt.Errorf("invalid notes hash length")
	}
	copy(out[:], decoded)
	return out, nil
}

func titleFor(notePath string, content string, fm map[string]any) string {
	if title, ok := fm["title"].(string); ok && strings.TrimSpace(title) != "" {
		return strings.TrimSpace(title)
	}
	if name, ok := fm["name"].(string); ok && strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	if heading := singleMarkdownH1Title(content); heading != "" {
		return heading
	}
	base := filepath.Base(notePath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func singleMarkdownH1Title(content string) string {
	return obsidian.SingleMarkdownH1Title(content)
}

func extractAllTags(content string, fm map[string]any) []string {
	var out []string
	if tagsRaw, ok := fm["tags"]; ok {
		switch tags := tagsRaw.(type) {
		case []string:
			out = append(out, tags...)
		case []interface{}:
			for _, item := range tags {
				if text, ok := item.(string); ok {
					out = append(out, text)
				}
			}
		}
	}
	for _, tag := range obsidian.ExtractHashtags(content) {
		out = append(out, strings.TrimPrefix(tag, "#"))
	}
	return dedupeStrings(out)
}

func normalizePropertyValue(v string) string {
	val := strings.ToLower(strings.TrimSpace(v))
	if strings.HasPrefix(val, "[[") && strings.HasSuffix(val, "]]") {
		val = strings.TrimSuffix(strings.TrimPrefix(val, "[["), "]]")
	}
	if strings.Contains(val, "|") {
		parts := strings.SplitN(val, "|", 2)
		val = strings.TrimSpace(parts[0])
	}
	return val
}

func normalizeTag(tag string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(tag, "#")))
}

func dedupeStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func dedupeNormalizedTags(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		tag := normalizeTag(item)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneInline(in map[string][]string) map[string][]string {
	if len(in) == 0 {
		return map[string][]string{}
	}
	out := make(map[string][]string, len(in))
	for key, values := range in {
		cp := make([]string, len(values))
		copy(cp, values)
		out[key] = cp
	}
	return out
}

func buildIncrementalPathCache(currentPaths, changedPaths, deletedPaths []string) *obsidian.NotePathCache {
	keepDeleted := make(map[string]struct{}, len(deletedPaths))
	for _, path := range deletedPaths {
		path = strings.TrimSpace(path)
		if path != "" {
			keepDeleted[path] = struct{}{}
		}
	}
	pathsList := make([]string, 0, len(currentPaths)+len(changedPaths))
	seen := make(map[string]struct{}, len(currentPaths)+len(changedPaths))
	for _, path := range currentPaths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, deleted := keepDeleted[path]; deleted {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		pathsList = append(pathsList, path)
	}
	for _, path := range changedPaths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, deleted := keepDeleted[path]; deleted {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		pathsList = append(pathsList, path)
	}
	sort.Strings(pathsList)
	return obsidian.BuildNotePathCache(pathsList)
}

func nextLoadedAt(current int64) int64 {
	next := time.Now().UnixNano()
	if next <= current {
		return current + 1
	}
	return next
}

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
