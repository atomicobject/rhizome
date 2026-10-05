package actions

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/bmatcuk/doublestar/v4"
)

// CodeAnchorListStore supplies the indexed data needed to assemble code-anchor list items.
type CodeAnchorListStore interface {
	IndexedFilePaths(context.Context) ([]string, error)
	NotesForAnchor(context.Context, int64) ([]codeanchor.Note, error)
	AnchorScope(context.Context, int64) ([]string, []string, error)
	FilesDefiningSymbolFQN(context.Context, string) ([]string, error)
}

// CodeAnchorListItem is the presentation-neutral result for one indexed code anchor.
type CodeAnchorListItem struct {
	Label             string                `json:"label"`
	Kind              codeanchor.AnchorKind `json:"kind"`
	Lang              codeanchor.Lang       `json:"lang,omitempty"`
	PathPrefix        string                `json:"pathPrefix,omitempty"`
	Globs             []string              `json:"globs,omitempty"`
	NotePaths         []string              `json:"notePaths,omitempty"`
	SymbolCount       int                   `json:"symbolCount,omitempty"`
	CallCount         int                   `json:"callCount,omitempty"`
	Symbols           []string              `json:"symbols,omitempty"`
	CallFiles         []string              `json:"callFiles,omitempty"`
	DefinitionFiles   []string              `json:"definitionFiles,omitempty"`
	MatchedFiles      []string              `json:"matchedFiles,omitempty"`
	MatchedFilesCount int                   `json:"matchedFilesCount,omitempty"`
	Truncated         bool                  `json:"truncated,omitempty"`
}

// BuildCodeAnchorListItems aggregates indexed anchor data for CLI renderers.
func BuildCodeAnchorListItems(ctx context.Context, store CodeAnchorListStore, anchors []codeanchor.Anchor, limit int) ([]CodeAnchorListItem, error) {
	indexedFiles, err := store.IndexedFilePaths(ctx)
	if err != nil {
		return nil, err
	}

	orderedAnchors := append([]codeanchor.Anchor(nil), anchors...)
	sort.Slice(orderedAnchors, func(i, j int) bool { return orderedAnchors[i].Label < orderedAnchors[j].Label })

	items := make([]CodeAnchorListItem, 0, len(orderedAnchors))
	for _, anchor := range orderedAnchors {
		notes, err := store.NotesForAnchor(ctx, anchor.ID)
		if err != nil {
			return nil, err
		}
		notePaths := make([]string, 0, len(notes))
		for _, note := range notes {
			notePaths = append(notePaths, note.Path)
		}
		sort.Strings(notePaths)

		symbols, callFiles, err := store.AnchorScope(ctx, anchor.ID)
		if err != nil {
			return nil, err
		}
		sort.Strings(symbols)
		sort.Strings(callFiles)

		item := CodeAnchorListItem{
			Label:       anchor.Label,
			Kind:        anchor.Kind,
			Lang:        anchor.Lang,
			PathPrefix:  anchor.PathPrefix,
			Globs:       anchor.Globs,
			NotePaths:   notePaths,
			SymbolCount: len(symbols),
			CallCount:   len(callFiles),
		}

		switch anchor.Kind {
		case codeanchor.AnchorFunc:
			item.Symbols = truncateCodeAnchorStrings(symbols, limit, &item.Truncated)
			item.CallFiles = truncateCodeAnchorStrings(callFiles, limit, &item.Truncated)
		case codeanchor.AnchorPath:
			item.MatchedFiles, item.MatchedFilesCount = matchCodeAnchorFilesByPrefix(indexedFiles, anchor.PathPrefix, limit)
			item.Truncated = item.MatchedFilesCount > len(item.MatchedFiles)
		case codeanchor.AnchorGlob:
			item.MatchedFiles, item.MatchedFilesCount = matchCodeAnchorFilesByGlobs(indexedFiles, anchor.Globs, limit)
			item.Truncated = item.MatchedFilesCount > len(item.MatchedFiles)
		default:
			item.Symbols = truncateCodeAnchorStrings(symbols, limit, &item.Truncated)
		}

		if len(symbols) > 0 && (anchor.Kind == codeanchor.AnchorFunc || anchor.Kind == codeanchor.AnchorBaseClass || anchor.Kind == codeanchor.AnchorAnnotation) {
			definitionSet := map[string]bool{}
			for _, fqn := range symbols {
				definitionFiles, err := store.FilesDefiningSymbolFQN(ctx, fqn)
				if err != nil {
					return nil, err
				}
				for _, file := range definitionFiles {
					definitionSet[file] = true
				}
			}
			item.DefinitionFiles = truncateCodeAnchorStrings(sortedCodeAnchorSet(definitionSet), limit, &item.Truncated)
		}

		items = append(items, item)
	}
	return items, nil
}

func truncateCodeAnchorStrings(items []string, limit int, truncated *bool) []string {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	if truncated != nil {
		*truncated = true
	}
	return items[:limit]
}

func matchCodeAnchorFilesByPrefix(files []string, prefix string, limit int) ([]string, int) {
	prefix = normalizeCodeAnchorPath(strings.TrimSpace(prefix))
	if prefix == "" {
		return nil, 0
	}
	var matches []string
	count := 0
	for _, file := range files {
		candidate := normalizeCodeAnchorPath(file)
		if candidate == prefix || strings.HasPrefix(candidate, prefix+"/") {
			count++
			if limit <= 0 || len(matches) < limit {
				matches = append(matches, file)
			}
		}
	}
	return matches, count
}

func matchCodeAnchorFilesByGlobs(files []string, globs []string, limit int) ([]string, int) {
	if len(globs) == 0 {
		return nil, 0
	}
	var matches []string
	count := 0
	for _, file := range files {
		candidate := normalizeCodeAnchorPath(file)
		for _, glob := range globs {
			pattern := normalizeCodeAnchorPath(glob)
			matched, _ := doublestar.Match(pattern, candidate)
			if matched {
				count++
				if limit <= 0 || len(matches) < limit {
					matches = append(matches, file)
				}
				break
			}
		}
	}
	return matches, count
}

func normalizeCodeAnchorPath(value string) string {
	return strings.ReplaceAll(filepath.ToSlash(value), "\\", "/")
}

func sortedCodeAnchorSet(set map[string]bool) []string {
	items := make([]string, 0, len(set))
	for item := range set {
		items = append(items, item)
	}
	sort.Strings(items)
	return items
}
