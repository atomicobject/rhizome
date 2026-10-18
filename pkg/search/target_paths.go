package search

import (
	"context"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
)

// explicitPathHintTokens preserves path/filename syntax before deciding
// whether an indexed path should outrank a colliding code symbol.
func explicitPathHintTokens(query string) []string {
	var out []string
	for _, field := range strings.Fields(query) {
		token := trimTargetFieldPunctuation(field)
		if token == "" {
			continue
		}
		extension := filepath.Ext(token)
		if strings.ContainsAny(token, `/\`) || (extension != "" && extension == strings.ToLower(extension)) {
			out = append(out, token)
		}
	}
	return out
}

func hasExplicitPathHint(query string, pathMatches []string) bool {
	for _, token := range explicitPathHintTokens(query) {
		if strings.ContainsAny(token, `/\`) {
			return true
		}
		for _, match := range pathMatches {
			if strings.EqualFold(filepath.Base(match), token) {
				return true
			}
		}
	}
	return false
}

func inferExactPathMatches(pathKinds map[string]PathKind, text string, limit int) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}
	tokens := extractTargetWordTokens(text)
	if len(tokens) == 0 {
		return nil
	}
	return inferPathKindMatches(pathKinds, tokens, limit)
}

func pathKindsFromTargetResolver(ctx context.Context, store TargetResolverStore) map[string]PathKind {
	if !targetResolverStoreAvailable(store) {
		return nil
	}
	out := make(map[string]PathKind)
	if notePaths, err := store.IntelNotePaths(ctx); err == nil {
		for _, path := range notePaths {
			if path = NormalizeLocalityPath(path); path != "" {
				out[path] = PathKindNote
			}
		}
	}
	if filePaths, err := store.ListFiles(ctx, 100000); err == nil {
		for _, path := range filePaths {
			if path = NormalizeLocalityPath(path); path != "" {
				if _, isNote := out[path]; !isNote {
					out[path] = PathKindCode
				}
			}
		}
	}
	return out
}

func inferPathKindMatches(pathKinds map[string]PathKind, tokens []string, limit int) []string {
	if len(pathKinds) == 0 || len(tokens) == 0 {
		return nil
	}

	seen := map[string]struct{}{}
	var paths []string
	for path, kind := range pathKinds {
		if kind != PathKindNote && kind != PathKindCode {
			continue
		}
		addIndexedPathCandidate(path, &paths, seen)
	}
	if len(paths) == 0 {
		return nil
	}

	exact := make([]string, 0, limit)
	matched := make([]string, 0, limit)
	for _, path := range paths {
		if indexedPathExactlyMatchesTokens(path, tokens) {
			exact = append(exact, path)
		} else if indexedPathMatchesTokens(path, tokens) {
			matched = append(matched, path)
		}
	}
	if len(exact) > 0 {
		matched = exact
	}
	matched = collapseDescendantPathMatches(matched)
	if len(matched) > limit {
		matched = matched[:limit]
	}
	return matched
}

func indexedPathExactlyMatchesTokens(path string, tokens []string) bool {
	path = strings.ToLower(NormalizeLocalityPath(path))
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	full := strings.ToLower(filepath.Base(path))
	for _, token := range tokens {
		if token == path || token == base || token == full {
			return true
		}
	}
	return false
}

// inferExactDocumentMatch returns the one indexed note whose title/slug equals
// the query phrase. The token-based path lane compares single words to whole
// path segments, so a multi-word document name can only match here.
func inferExactDocumentMatch(pathKinds map[string]PathKind, text string) string {
	phrase := normalizeDocumentPhrase(text)
	if len(pathKinds) == 0 || !strings.Contains(phrase, " ") {
		return ""
	}
	var match string
	for path, kind := range pathKinds {
		if kind != PathKindNote || !documentPathMatchesPhrase(path, phrase) {
			continue
		}
		if normalized := NormalizeLocalityPath(path); normalized != match {
			if match != "" {
				return ""
			}
			match = normalized
		}
	}
	return match
}

func documentPathMatchesPhrase(path, phrase string) bool {
	path = NormalizeLocalityPath(path)
	if normalizeDocumentPhrase(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))) == phrase {
		return true
	}
	for _, segment := range strings.Split(path, "/") {
		if normalizeDocumentPhrase(segment) == phrase {
			return true
		}
	}
	return false
}

// normalizeDocumentPhrase reduces a slug, path segment, or typed request to its
// meaningful words so "search-quality-corpus" and "the search quality corpus"
// compare equal.
func normalizeDocumentPhrase(value string) string {
	separated := strings.Map(func(r rune) rune {
		switch r {
		case '-', '_', '.', '/', '\\', ':':
			return ' '
		default:
			return r
		}
	}, value)
	return strings.Join(extractTargetWordTokens(separated), " ")
}

func targetResolverStoreAvailable(store TargetResolverStore) bool {
	if store == nil {
		return false
	}
	value := reflect.ValueOf(store)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return !value.IsNil()
	default:
		return true
	}
}

func collapseDescendantPathMatches(paths []string) []string {
	if len(paths) < 2 {
		return paths
	}
	slices.Sort(paths)
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		isDescendant := false
		for _, parent := range out {
			if strings.HasPrefix(path, parent+"/") {
				isDescendant = true
				break
			}
		}
		if !isDescendant {
			out = append(out, path)
		}
	}
	return out
}

func addIndexedPathCandidate(path string, out *[]string, seen map[string]struct{}) {
	path = NormalizeLocalityPath(path)
	if path == "" {
		return
	}
	appendUniqueIndexedPath(path, out, seen)
	for dir := NormalizeLocalityPath(filepath.Dir(path)); dir != "" && dir != "."; dir = NormalizeLocalityPath(filepath.Dir(dir)) {
		appendUniqueIndexedPath(dir, out, seen)
	}
}

func appendUniqueIndexedPath(path string, out *[]string, seen map[string]struct{}) {
	if _, ok := seen[path]; ok {
		return
	}
	seen[path] = struct{}{}
	*out = append(*out, path)
}

func indexedPathMatchesTokens(path string, tokens []string) bool {
	path = strings.ToLower(NormalizeLocalityPath(path))
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	full := strings.ToLower(filepath.Base(path))
	for _, token := range tokens {
		if token == path || token == base || token == full || strings.Contains(path, "/"+token) || strings.HasSuffix(path, "/"+token) {
			return true
		}
	}
	return false
}

func dominantModulePath(paths []string) string {
	if len(paths) < 2 {
		return ""
	}
	prefix := NormalizeLocalityPath(filepath.Dir(paths[0]))
	for _, path := range paths[1:] {
		prefix = longestCommonPathPrefix(prefix, NormalizeLocalityPath(filepath.Dir(path)))
		if prefix == "" {
			return ""
		}
	}
	if prefix == "" {
		return ""
	}
	segments := strings.Split(prefix, "/")
	if len(segments) < 2 && (segments[0] == "pkg" || segments[0] == "cmd" || segments[0] == "docs" || segments[0] == "web") {
		return ""
	}
	return prefix
}

func longestCommonPathPrefix(a, b string) string {
	a = NormalizeLocalityPath(a)
	b = NormalizeLocalityPath(b)
	if a == "" || b == "" {
		return ""
	}
	partsA := strings.Split(a, "/")
	partsB := strings.Split(b, "/")
	n := min(len(partsA), len(partsB))
	var shared []string
	for i := 0; i < n; i++ {
		if partsA[i] != partsB[i] {
			break
		}
		shared = append(shared, partsA[i])
	}
	return strings.Join(shared, "/")
}

func targetCandidatesFromPaths(paths []string, reason string) []TargetCandidate {
	out := make([]TargetCandidate, 0, len(paths))
	for _, path := range paths {
		out = append(out, TargetCandidate{Path: NormalizeLocalityPath(path), Reason: reason})
	}
	return out
}

// Indexed code anchors establish their exact file/directory ownership without a
// repository scan. The caller's existing note/configured ownership keeps priority.
func codeAnchorPathKinds(paths []string) map[string]PathKind {
	kinds := make(map[string]PathKind, len(paths))
	for _, raw := range paths {
		for path := NormalizeLocalityPath(raw); path != "" && path != "."; path = NormalizeLocalityPath(filepath.Dir(path)) {
			kinds[path] = PathKindCode
		}
	}
	return kinds
}
