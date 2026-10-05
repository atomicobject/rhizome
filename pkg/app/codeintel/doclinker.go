package codeintel

import (
	"context"
	"fmt"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type DocLinker struct {
	cache *obsidian.NotePathCache
}

// NewDocLinker builds a DocLinker that resolves code→note references
// by filename only. Prefer NewDocLinkerWithStore when a SQLite note
// index is available so alias-based coderefs (e.g. `[[SPEC-001]]`
// in a comment) also resolve.
func NewDocLinker(vaultPath string) *DocLinker {
	return NewDocLinkerWithStore(vaultPath, nil)
}

// NewDocLinkerWithStore builds a DocLinker and pulls frontmatter
// aliases from store so wikilink-style coderefs that target a stable
// identifier still resolve to the underlying note path.
func NewDocLinkerWithStore(vaultPath string, store *semdb.Store) *DocLinker {
	nm := obsidian.Note{}
	notes, err := nm.GetNotesList(obsidian.VaultDefinition{Path: vaultPath})
	if err != nil {
		return nil
	}
	notePaths := make([]paths.NotePath, 0, len(notes))
	for _, note := range notes {
		notePaths = append(notePaths, paths.NotePath(note))
	}
	linker, err := NewDocLinkerFromNotePaths(notePaths, store)
	if err != nil {
		return nil
	}
	return linker
}

// NewDocLinkerFromNotePaths builds a DocLinker from an already-discovered,
// canonical note path snapshot. It does no filesystem discovery, so unified
// indexing can reuse its single ownership walk rather than starting a second
// note scan just to resolve code→note references. Every supplied path must be
// a canonical vault-relative NotePath; authored extension and casing are kept
// exactly as supplied.
//
// When store is available, only aliases for the supplied paths participate in
// resolution. This prevents stale or currently non-note ownership from
// becoming coderef destinations during a transition.
func NewDocLinkerFromNotePaths(notePaths []paths.NotePath, store *semdb.Store) (*DocLinker, error) {
	canonicalPaths := make([]string, 0, len(notePaths))
	allowedPaths := make(map[string]struct{}, len(notePaths))
	for _, notePath := range notePaths {
		canonical, err := paths.CleanNotePath(notePath.String())
		if err != nil {
			return nil, fmt.Errorf("validate supplied note path %q: %w", notePath, err)
		}
		if canonical != notePath {
			return nil, fmt.Errorf("supplied note path %q is not canonical", notePath)
		}
		if _, duplicate := allowedPaths[canonical.String()]; duplicate {
			continue
		}
		allowedPaths[canonical.String()] = struct{}{}
		canonicalPaths = append(canonicalPaths, canonical.String())
	}

	aliasesByPath := aliasesForPaths(notemeta.LoadAliasMap(context.Background(), store), allowedPaths)
	return &DocLinker{cache: obsidian.BuildNotePathCacheWithAliases(canonicalPaths, aliasesByPath)}, nil
}

func aliasesForPaths(aliasesByPath map[string][]string, allowedPaths map[string]struct{}) map[string][]string {
	if len(aliasesByPath) == 0 || len(allowedPaths) == 0 {
		return nil
	}
	filtered := make(map[string][]string, len(aliasesByPath))
	for notePath, aliases := range aliasesByPath {
		if _, allowed := allowedPaths[notePath]; !allowed {
			continue
		}
		filtered[notePath] = append([]string(nil), aliases...)
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func (l *DocLinker) LinksForCode(path string, content []byte, fc codeanchor.FileContext) []codeanchor.DocLink {
	if l == nil || l.cache == nil {
		return nil
	}
	// Coderef scanning is resolver-backed, so every emitted doc link points to a
	// real normalized note path. The FileContext argument is reserved for future
	// code-anchor-aware links; do not infer note targets from it here.
	refs, err := coderefs.ScanFile(path, content, l.cache)
	if err != nil || len(refs) == 0 {
		return nil
	}
	ts := time.Now().Unix()
	out := make([]codeanchor.DocLink, 0, len(refs))
	for _, r := range refs {
		out = append(out, codeanchor.DocLink{
			SrcType:   "code",
			SrcPath:   path,
			DstKind:   "note",
			DstPath:   string(paths.NormalizeNotePath(r.Target)),
			Snippet:   r.Snippet,
			MetaJSON:  "",
			UpdatedAt: ts,
		})
	}
	return out
}

func (l *DocLinker) LinksForNote(note codeanchor.Note) []codeanchor.DocLink {
	return nil
}
