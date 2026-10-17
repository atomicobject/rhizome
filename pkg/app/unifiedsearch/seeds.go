package unifiedsearch

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

func ResolveSeedHandles(ctx context.Context, raw []string, vaultPath string, intelStore *semdb.Store, seedLimit int) ([]knowledge.Handle, error) {
	canonicalRaw, err := canonicalizeRawSeedPaths(raw, vaultPath)
	if err != nil {
		return nil, err
	}
	corpus, err := search.LoadCorpusSnapshot(ctx, intelStore, search.CorpusMetadata)
	if err != nil {
		return nil, err
	}
	return resolveSeedHandlesWithCorpus(ctx, canonicalRaw, corpus.PathKinds, intelStore, seedLimit, corpus)
}

// ResolveSeedHandlesWithPathKinds preserves resolved handle identity. Raw
// paths need caller-supplied configured or persisted ownership; explicit
// Markdown remains the sole legacy raw-path compatibility boundary.
func ResolveSeedHandlesWithPathKinds(ctx context.Context, raw []string, pathKinds map[string]search.PathKind, intelStore *semdb.Store, seedLimit int) ([]knowledge.Handle, error) {
	corpus, err := search.LoadCorpusSnapshot(ctx, intelStore, search.CorpusMetadata)
	if err != nil {
		return nil, err
	}
	return resolveSeedHandlesWithCorpus(ctx, raw, pathKinds, intelStore, seedLimit, corpus)
}

func resolveSeedHandlesWithCorpus(ctx context.Context, raw []string, pathKinds map[string]search.PathKind, intelStore *semdb.Store, seedLimit int, corpus *search.CorpusSnapshot) ([]knowledge.Handle, error) {
	out := make([]knowledge.Handle, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	if seedLimit <= 0 {
		seedLimit = 10
	}
	persistedNoteDirectories, err := persistedNoteDirectorySeeds(raw, seedLimit, corpus)
	if err != nil {
		return nil, err
	}
	add := func(h knowledge.Handle) {
		if _, ok := seen[h.String()]; ok {
			return
		}
		seen[h.String()] = struct{}{}
		out = append(out, h)
	}
	for _, token := range raw {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if strings.HasPrefix(token, "fqn:") {
			if intelStore == nil {
				return nil, fmt.Errorf("seed %q requires code intel index", token)
			}
			fqn := strings.TrimSpace(strings.TrimPrefix(token, "fqn:"))
			ids, err := intelStore.IntelAnchorIDsByFQN(ctx, fqn, seedLimit)
			if err != nil {
				return nil, err
			}
			for _, id := range ids {
				h := knowledge.AnchorHandle(id)
				if _, ok := seen[h.String()]; ok {
					continue
				}
				seen[h.String()] = struct{}{}
				out = append(out, h)
			}
			continue
		}
		if strings.HasPrefix(token, "symbol:") {
			if intelStore == nil {
				return nil, fmt.Errorf("seed %q requires code intel index", token)
			}
			sym := strings.TrimSpace(strings.TrimPrefix(token, "symbol:"))
			ids, err := intelStore.IntelAnchorIDsBySymbol(ctx, sym, seedLimit)
			if err != nil {
				return nil, err
			}
			for _, id := range ids {
				h := knowledge.AnchorHandle(id)
				if _, ok := seen[h.String()]; ok {
					continue
				}
				seen[h.String()] = struct{}{}
				out = append(out, h)
			}
			continue
		}

		if strings.Contains(token, ":") {
			h, err := knowledge.ParseHandle(token)
			if err != nil {
				return nil, err
			}
			if _, ok := seen[h.String()]; ok {
				continue
			}
			seen[h.String()] = struct{}{}
			out = append(out, h)
			continue
		}

		path := search.NormalizeLocalityPath(token)
		if directory, ok := persistedNoteDirectories[path]; ok {
			if directory.hasCode {
				add(knowledge.FileHandle(path))
			}
			for _, notePath := range directory.notePaths {
				add(knowledge.NoteHandle(notePath))
			}
			continue
		}
		switch pathKinds[path] {
		case search.PathKindCode:
			add(knowledge.FileHandle(path))
			continue
		case search.PathKindNote:
			add(knowledge.NoteHandle(path))
			continue
		}

		if !isExplicitMarkdownRawSeedCompatibilityPath(path) {
			return nil, fmt.Errorf("seed %q requires configured or persisted path ownership", token)
		}
		add(knowledge.NoteHandle(path))
	}
	return out, nil
}

type persistedNoteDirectorySeed struct {
	notePaths []string
	hasCode   bool
}

// persistedNoteDirectorySeeds expands a raw directory only to current persisted
// notes, sorted and capped to the caller's seed limit. Code
// directories retain their file handle so explicit code-directory retrieval
// continues to sample indexed code.
func persistedNoteDirectorySeeds(raw []string, seedLimit int, corpus *search.CorpusSnapshot) (map[string]persistedNoteDirectorySeed, error) {
	if corpus == nil {
		return nil, nil
	}
	targets := make(map[string]struct{}, len(raw))
	for _, token := range raw {
		token = strings.TrimSpace(token)
		if token == "" || strings.Contains(token, ":") {
			continue
		}
		if path := search.NormalizeLocalityPath(token); path != "" {
			targets[path] = struct{}{}
		}
	}
	if len(targets) == 0 {
		return nil, nil
	}
	out := make(map[string]persistedNoteDirectorySeed)
	for _, note := range corpus.Notes {
		if note.Projection.Status != semdb.NoteProjectionStatusCurrent {
			continue
		}
		notePath := search.NormalizeLocalityPath(note.Path)
		for directory := range targets {
			if !strings.HasPrefix(notePath, directory+"/") {
				continue
			}
			entry := out[directory]
			entry.notePaths = append(entry.notePaths, notePath)
			out[directory] = entry
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	for _, codePath := range corpus.CodePaths {
		codePath = search.NormalizeLocalityPath(codePath)
		for directory, entry := range out {
			if strings.HasPrefix(codePath, directory+"/") {
				entry.hasCode = true
				out[directory] = entry
			}
		}
	}
	for directory, entry := range out {
		sort.Strings(entry.notePaths)
		notePaths := entry.notePaths[:0]
		for _, notePath := range entry.notePaths {
			if len(notePaths) > 0 && notePath == notePaths[len(notePaths)-1] {
				continue
			}
			notePaths = append(notePaths, notePath)
		}
		if len(notePaths) > seedLimit {
			notePaths = notePaths[:seedLimit]
		}
		entry.notePaths = notePaths
		out[directory] = entry
	}
	return out, nil
}

// isExplicitMarkdownRawSeedCompatibilityPath is the sole raw-path extension
// compatibility rule. All other raw paths require configured or persisted
// ownership in PathKinds; parsed knowledge handles are already typed.
func isExplicitMarkdownRawSeedCompatibilityPath(path string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(path)), ".md")
}

// canonicalizeRawSeedPaths converts untyped raw inputs into durable vault-relative
// keys. Typed handles and symbol lookups already carry their own identity.
func canonicalizeRawSeedPaths(raw []string, vaultPath string) ([]string, error) {
	vaultPaths, err := paths.NewVaultPaths(vaultPath)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(raw))
	for _, token := range raw {
		token = strings.TrimSpace(token)
		if token == "" || (strings.Contains(token, ":") && !filepath.IsAbs(token)) {
			out = append(out, token)
			continue
		}
		rel, err := vaultPaths.RelStrict(token)
		if err != nil || rel == "" {
			if err == nil {
				err = paths.ErrOutsideVault
			}
			return nil, fmt.Errorf("seed %q must be within vault: %w", token, err)
		}
		out = append(out, rel.String())
	}
	return out, nil
}
