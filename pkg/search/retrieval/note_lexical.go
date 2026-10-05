package retrieval

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// NoteLexicalRetriever performs a cheap lexical match over note paths/titles.
// This provides a strong fallback when embeddings or FTS aren't available.
type NoteLexicalRetriever struct {
	VaultDef   obsidian.VaultDefinition
	NoteReader obsidian.NoteReader
	PathSource NotePathSource
	// CatalogSource supplies authoritative indexed titles. Filename-derived
	// titles remain the fallback for stores that only expose paths.
	CatalogSource NoteCatalogSource
	// TypePathSource supplies note paths whose resolved ontology type matches
	// the query. It is required when a note-type filter is present; falling
	// back to all paths would apply the filter after the local limit.
	TypePathSource NoteTypePathSource
}

func (r *NoteLexicalRetriever) Name() string { return "note_lexical" }

// NotePathSource provides note paths without requiring filesystem discovery.
// Implementations can pull from cached or indexed metadata.
type NotePathSource interface {
	NotePaths(ctx context.Context) ([]string, error)
}

// NotePathSourceFunc adapts a function to a NotePathSource.
type NotePathSourceFunc func(context.Context) ([]string, error)

func (f NotePathSourceFunc) NotePaths(ctx context.Context) ([]string, error) {
	return f(ctx)
}

type NoteCatalogEntry struct {
	Path  string
	Title string
}

type NoteCatalogSource interface {
	NoteCatalog(ctx context.Context) ([]NoteCatalogEntry, error)
}

type NoteCatalogSourceFunc func(context.Context) ([]NoteCatalogEntry, error)

func (f NoteCatalogSourceFunc) NoteCatalog(ctx context.Context) ([]NoteCatalogEntry, error) {
	return f(ctx)
}

// NoteTypePathSource provides note paths selected by their owning note type.
type NoteTypePathSource interface {
	NotePathsByType(ctx context.Context, typeNames []string) ([]string, error)
}

// NoteTypePathSourceFunc adapts a typed path lookup to NoteTypePathSource.
type NoteTypePathSourceFunc func(context.Context, []string) ([]string, error)

func (f NoteTypePathSourceFunc) NotePathsByType(ctx context.Context, typeNames []string) ([]string, error) {
	return f(ctx, typeNames)
}

func (r *NoteLexicalRetriever) Retrieve(ctx context.Context, spec search.QuerySpec) ([]search.Candidate, error) {
	q := strings.TrimSpace(spec.Text)
	if q == "" {
		return nil, nil
	}
	notes, err := r.notePaths(ctx, spec.Filters.NoteTypes)
	if err != nil {
		return nil, err
	}
	if len(notes) == 0 {
		return nil, nil
	}
	titles := map[string]string{}
	if r.CatalogSource != nil {
		if catalog, catalogErr := r.CatalogSource.NoteCatalog(ctx); catalogErr == nil {
			for _, entry := range catalog {
				if path := filepath.ToSlash(strings.TrimSpace(entry.Path)); path != "" {
					titles[path] = strings.TrimSpace(entry.Title)
				}
			}
		} else if r.PathSource == nil && r.NoteReader == nil {
			return nil, catalogErr
		}
	}

	tokens := tokenizeQuery(q)
	if filtered := filterDocTokens(tokens); len(filtered) > 0 {
		tokens = filtered
	}
	if len(tokens) == 0 {
		return nil, nil
	}

	type scored struct {
		path       string
		title      string
		score      float64
		exactTitle bool
		exactPath  bool
		typo       bool
	}
	var scoredNotes []scored
	for _, p := range notes {
		if ctx.Err() != nil {
			break
		}
		np, ok := cleanTypedNotePath(p)
		if !ok {
			continue
		}
		if !pathMatchesPrefix(np, spec.Filters.PathPrefixes) {
			continue
		}
		if !spec.Filters.AllowsTestPath(np) {
			continue
		}
		title := titles[np]
		if title == "" {
			title = titleFromPath(np)
		}
		combined := strings.ToLower(title + " " + filepath.ToSlash(np))

		score := tokenMatchScore(tokens, combined)
		if score == 0 {
			continue
		}
		scoredNotes = append(scoredNotes, scored{path: np, title: title, score: score, exactTitle: strings.EqualFold(title, q), exactPath: np == filepath.ToSlash(q) || filepath.Base(np) == q})
	}
	if len(scoredNotes) == 0 && !search.IsPrecisionIntent(spec.Intent) {
		for _, p := range notes[:min(len(notes), 2000)] {
			np, ok := cleanTypedNotePath(p)
			if !ok || !pathMatchesPrefix(np, spec.Filters.PathPrefixes) || !spec.Filters.AllowsTestPath(np) {
				continue
			}
			title := titles[np]
			if title == "" {
				title = titleFromPath(np)
			}
			if corrected, ok := boundedTypoMatch(tokens, tokenizeQuery(title)); ok {
				scoredNotes = append(scoredNotes, scored{path: np, title: title, score: 0.55, typo: true})
				search.AddRuntimeWarning(ctx, search.Warning{Code: "typo_fallback", Kind: "query_interpretation", Source: "note_lexical", Message: "Included close title matches for " + corrected + "."})
			}
		}
	}

	sort.SliceStable(scoredNotes, func(i, j int) bool {
		if scoredNotes[i].score != scoredNotes[j].score {
			return scoredNotes[i].score > scoredNotes[j].score
		}
		return scoredNotes[i].path < scoredNotes[j].path
	})

	limit := spec.Limits.Total
	if limit <= 0 {
		limit = 25
	}
	if len(scoredNotes) > limit {
		scoredNotes = scoredNotes[:limit]
	}

	out := make([]search.Candidate, 0, len(scoredNotes))
	for _, s := range scoredNotes {
		h := knowledge.NoteHandle(s.path)
		evidenceType := "note_title_match"
		if s.typo {
			evidenceType = "typo_title_match"
		}
		evidence := []search.Evidence{{
			Type: evidenceType, RawScore: s.score, Source: "note_lexical", Details: map[string]string{"query": q},
		}}
		if s.exactTitle {
			evidence = append(evidence, search.Evidence{Type: "note_title_exact", RawScore: 1, Source: "note_lexical", Details: map[string]string{"title": s.title}})
		}
		if s.exactPath {
			evidence = append(evidence, search.Evidence{Type: "path_exact", RawScore: 1, Source: "note_lexical", Details: map[string]string{"path": s.path}})
		}
		out = append(out, search.Candidate{
			Handle:     h,
			Owner:      h,
			Evidence:   evidence,
			Type:       "note",
			NoteID:     s.path,
			Path:       s.path,
			Title:      s.title,
			ChunkIndex: -1,
		})
	}
	return out, nil
}

// Multiword queries need their full meaning preserved; a close match to one
// token is not evidence that the user misspelled the query.
func boundedTypoMatch(queryTokens, titleTokens []string) (string, bool) {
	if len(queryTokens) != 1 || len([]rune(queryTokens[0])) < 5 {
		return "", false
	}
	query := queryTokens[0]
	for _, title := range titleTokens {
		if query != title && distanceWithinOne(query, title) {
			return title, true
		}
	}
	return "", false
}

func distanceWithinOne(left, right string) bool {
	a, b := []rune(strings.ToLower(left)), []rune(strings.ToLower(right))
	if len(a)-len(b) > 1 || len(b)-len(a) > 1 {
		return false
	}
	rows := make([][]int, len(a)+1)
	for i := range rows {
		rows[i] = make([]int, len(b)+1)
		rows[i][0] = i
	}
	for j := range rows[0] {
		rows[0][j] = j
	}
	for i := 1; i <= len(a); i++ {
		rowMin := 2
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			rows[i][j] = min(rows[i-1][j]+1, rows[i][j-1]+1, rows[i-1][j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				rows[i][j] = min(rows[i][j], rows[i-2][j-2]+1)
			}
			rowMin = min(rowMin, rows[i][j])
		}
		if rowMin > 1 {
			return false
		}
	}
	return rows[len(a)][len(b)] <= 1
}

func (r *NoteLexicalRetriever) notePaths(ctx context.Context, noteTypes []string) ([]string, error) {
	if len(noteTypes) > 0 {
		if r.TypePathSource == nil {
			return nil, nil
		}
		return r.TypePathSource.NotePathsByType(ctx, noteTypes)
	}
	if r.PathSource != nil {
		paths, err := r.PathSource.NotePaths(ctx)
		if err == nil && len(paths) > 0 {
			return paths, nil
		}
		// Fall back to NoteReader if available when the path source is empty or errored.
		if err != nil && (r.NoteReader == nil || r.VaultDef.BasePath() == "") {
			return nil, err
		}
	}
	if r.NoteReader == nil || r.VaultDef.BasePath() == "" {
		return nil, nil
	}
	return r.NoteReader.GetNotesList(r.VaultDef)
}

func pathMatchesPrefix(path string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	path = filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
	for _, prefix := range prefixes {
		prefix = filepath.ToSlash(filepath.Clean(strings.TrimSpace(prefix)))
		prefix = strings.TrimSuffix(strings.TrimPrefix(prefix, "./"), "/")
		if prefix == "" || prefix == "." || prefix == "/" {
			return true
		}
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}
