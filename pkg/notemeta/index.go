package notemeta

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type EnsureResult struct {
	NotesHash string
	Dirty     bool
}

const noteMetadataIndexerVersion = "14"

type noteEntry struct {
	Title       string
	Frontmatter map[string]any
	InlineProps map[string][]string
	Tags        []string
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
	CurrentNoteAliases(context.Context) (map[string][]string, error)
	DurableNoteMetadataRowsByPaths(context.Context, []string) (map[string]MetadataRow, error)
	DurableNoteFactsByPaths(context.Context, []string) (semdb.DurableNoteFacts, error)
	GraphDocEdgesWithConfidenceForPaths(context.Context, []string) ([]semdb.GraphDocEdge, error)
	NoteLinkInputSources(context.Context, []string) ([]string, error)
}

// LoadAliasMap loads projected aliases. A read failure must stop publication
// rather than durably resolving relations against an alias-free cache.
func LoadAliasMap(ctx context.Context, store *semdb.Store) (map[string][]string, error) {
	if store == nil {
		return nil, nil
	}
	return store.CurrentNoteAliases(ctx)
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
		return text, NormalizePropertyValue(text), semdb.NotePropertyValueBool
	case int:
		text := strconv.Itoa(current)
		return text, NormalizePropertyValue(text), semdb.NotePropertyValueInt
	case int64:
		text := strconv.FormatInt(current, 10)
		return text, NormalizePropertyValue(text), semdb.NotePropertyValueInt
	case float64:
		text := strconv.FormatFloat(current, 'f', -1, 64)
		return text, NormalizePropertyValue(text), semdb.NotePropertyValueFloat
	case float32:
		text := strconv.FormatFloat(float64(current), 'f', -1, 64)
		return text, NormalizePropertyValue(text), semdb.NotePropertyValueFloat
	case time.Time:
		if current.Hour() != 0 || current.Minute() != 0 || current.Second() != 0 || current.Nanosecond() != 0 {
			text := current.Format(time.RFC3339)
			return text, NormalizePropertyValue(text), semdb.NotePropertyValueDateTime
		}
		text := current.Format("2006-01-02")
		return text, NormalizePropertyValue(text), semdb.NotePropertyValueDate
	case string:
		info := obsidian.AnalyzePropertyValue(current)
		text := strings.TrimSpace(current)
		return text, NormalizePropertyValue(text), mapValueKind(info.ValueType)
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
	contents, _, err := captureNoteContents(ctx, vaultDef, noteMgr)
	if err != nil {
		return "", nil, err
	}
	hash, paths := capturedNotesHash(contents)
	return hash, paths, nil
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

func notesHashFromRows(rows []semdb.NoteMetadataRow) string {
	var aggregate noteSetDigest
	for _, row := range rows {
		path := strings.TrimSpace(row.Path)
		if path == "" {
			continue
		}
		aggregate.Add(path, strings.TrimSpace(row.ContentHash))
	}
	return aggregate.String()
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

func NormalizePropertyValue(v string) string {
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
