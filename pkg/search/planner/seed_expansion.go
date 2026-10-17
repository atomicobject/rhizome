package planner

import (
	"context"
	"os"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// expandDirectoryFileSeeds expands directory seeds into representative files (via PageRank
// if intel is available, with an optional filesystem fallback). Limits depth and honors
// vault ignore rules when fallback is allowed.
//
// Expansion strategy (in order):
//  1. If intel store available: use TopCodePathsByPageRankPrefix (high-centrality files)
//  2. When policy allows it, fallback to a filesystem walk (max depth 3, uses unified ignore
//     matcher + vault excludes). Indexed-only callers preserve the unresolved directory seed.
//
// Limits: maxFilesPerDir defaults to 12, clamped to spec.Limits.Total if provided.
// Also derives top anchor handles from expanded files (max 10, or maxFilesPerDir/2).
//
// Fragments: directory-expanded files get Fragments: ["dirseed"] for debugging.
// See [Search - Seed expansion strategies](docs/reference/analysis/Search - Seed expansion strategies.md) for details.
func expandDirectoryFileSeeds(ctx context.Context, seeds []knowledge.Handle, vaultDef obsidian.VaultDefinition, intelStore *semdb.Store, maxFilesPerDir int, policy DirectorySeedExpansionPolicy) []knowledge.Handle {
	if len(seeds) == 0 {
		return nil
	}
	if maxFilesPerDir <= 0 {
		maxFilesPerDir = 12
	}
	vaultPath := vaultDef.BasePath()
	vaultPaths, _ := paths.NewVaultPaths(vaultPath)

	out := make([]knowledge.Handle, 0, len(seeds))
	seen := make(map[string]struct{}, len(seeds))

	add := func(h knowledge.Handle) {
		if h.String() == "" {
			return
		}
		if _, ok := seen[h.String()]; ok {
			return
		}
		seen[h.String()] = struct{}{}
		out = append(out, h)
	}

	for _, seed := range seeds {
		if seed.Kind != knowledge.KindFile {
			add(seed)
			continue
		}
		seedPath := strings.TrimSpace(seed.ID)
		if seedPath == "" {
			continue
		}

		ref, err := paths.ResolveCodeRefWithVaultPaths(vaultPaths, seedPath)
		if err == nil && ref.Abs != "" {
			if info, err := os.Stat(ref.Abs.String()); err == nil && info.IsDir() {
				expanded := directorySeedFiles(ctx, ref.Abs.String(), vaultDef, intelStore, maxFilesPerDir, policy)
				if len(expanded) > 0 {
					derivedPaths := make([]string, 0, len(expanded))
					for _, rel := range expanded {
						derivedPaths = append(derivedPaths, rel)
						derived := knowledge.FileHandle(rel)
						derived.Fragments = []string{"dirseed"}
						add(derived)
					}
					if intelStore != nil {
						maxAnchors := min(10, max(2, maxFilesPerDir/2))
						if ids, err := intelStore.TopModuleAnchorIDsByPaths(ctx, derivedPaths, maxAnchors); err == nil && len(ids) > 0 {
							for _, id := range ids {
								add(knowledge.AnchorHandle(id))
							}
						}
					}
					continue
				}
			}
		}

		add(seed)
	}
	return out
}

func directorySeedFiles(ctx context.Context, absDir string, vaultDef obsidian.VaultDefinition, intelStore *semdb.Store, maxFiles int, policy DirectorySeedExpansionPolicy) []string {
	if maxFiles <= 0 {
		return nil
	}
	vaultPath := vaultDef.BasePath()
	if strings.TrimSpace(vaultPath) == "" {
		// Best-effort filesystem fallback still requires a vault root so we can return stable, relative seeds.
		return nil
	}
	vaultPaths, _ := paths.NewVaultPaths(vaultPath)

	// The indexed code paths are the ownership boundary. Do not inspect the
	// filesystem or select descendants by extension: a typed file directory does
	// not establish ownership of every file below it.
	if intelStore != nil {
		if rel, err := vaultPaths.RelCodeStrict(absDir); err == nil && rel.String() != "" {
			// Use anchor PageRank to pick a few high-centrality files within the directory. This tends to
			// select entrypoints and "important" modules without scanning everything.
			if paths, err := intelStore.TopCodePathsByPageRankPrefix(ctx, rel.String(), maxFiles); err == nil && len(paths) > 0 {
				return paths
			}
			if files, err := intelStore.FilesByPathPrefix(ctx, rel.String(), maxFiles); err == nil && len(files) > 0 {
				// FilesByPathPrefix is the durable code ownership boundary.
				return files
			}
		}
	}
	if policy == DirectorySeedExpansionIndexedOnly {
		return nil
	}

	return nil
}

// expandNoteSeedsFromDocLinks derives note seeds from code→note doc links for file seeds.
//
// Queries DocLinksFromCodePath for each file seed (and files within directory seeds).
// Limits expansion to prevent explosion: default 50 total note handles, 10 per path.
// Deduplicates by the authored typed note path.
//
// Returns nil if store is nil or no seeds provided.
func expandNoteSeedsFromDocLinks(ctx context.Context, seeds []knowledge.Handle, store *semdb.Store, vaultPath string, limit int) []knowledge.Handle {
	if store == nil || len(seeds) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 50
	}
	vaultPaths, _ := paths.NewVaultPaths(vaultPath)
	seen := make(map[string]struct{}, len(seeds))
	for _, s := range seeds {
		if s.Kind == knowledge.KindNote || s.Kind == knowledge.KindNoteChunk {
			seen[s.ID] = struct{}{}
		}
	}

	addNote := func(notePath string) []knowledge.Handle {
		ref, err := paths.ResolveNotePathRefWithVaultPaths(vaultPaths, notePath)
		if err != nil || ref.Rel == "" {
			return nil
		}
		noteID := ref.Rel.String()
		if _, ok := seen[noteID]; ok {
			return nil
		}
		seen[noteID] = struct{}{}
		return []knowledge.Handle{knowledge.NoteHandle(noteID)}
	}

	var out []knowledge.Handle
	for _, seed := range seeds {
		if len(out) >= limit {
			break
		}
		if seed.Kind != knowledge.KindFile {
			continue
		}
		seedPath := strings.TrimSpace(seed.ID)
		if seedPath == "" {
			continue
		}

		ref, err := paths.ResolveCodeRefWithVaultPaths(vaultPaths, seedPath)
		if err != nil || ref.Rel == "" {
			continue
		}

		codePaths := []string{ref.Rel.String()}
		if ref.Abs != "" {
			if info, err := os.Stat(ref.Abs.String()); err == nil && info.IsDir() {
				if files, err := store.FilesByPathPrefix(ctx, ref.Rel.String(), limit); err == nil && len(files) > 0 {
					codePaths = files
				}
			}
		}
		for _, p := range codePaths {
			if len(out) >= limit {
				break
			}
			perPath := limit - len(out)
			links, err := store.DocLinksFromCodePath(ctx, p, min(perPath, 10))
			if err != nil {
				continue
			}
			for _, l := range links {
				if strings.ToLower(strings.TrimSpace(l.DstKind)) != "note" {
					continue
				}
				added := addNote(l.DstPath)
				out = append(out, added...)
				if len(out) >= limit {
					break
				}
			}
		}
	}
	return out
}

// expandAnchorSeedsFromFiles derives anchor seeds from files via intel store lookup.
//
// Expands directory seeds into their contained files first (via FilesByPathPrefix),
// then queries IntelAnchorsByPath for each file path. Limits expansion: default 120
// total anchor handles. Deduplicates by anchor ID.
//
// Returns nil if store is nil or no file seeds provided.
func expandAnchorSeedsFromFiles(ctx context.Context, seeds []knowledge.Handle, store *semdb.Store, vaultPath string, limit int) []knowledge.Handle {
	if store == nil || len(seeds) == 0 {
		return nil
	}
	vaultPaths, _ := paths.NewVaultPaths(vaultPath)
	if limit <= 0 {
		limit = 120
	}

	seen := make(map[string]struct{}, len(seeds))
	for _, s := range seeds {
		seen[s.String()] = struct{}{}
	}

	var out []knowledge.Handle
	add := func(anchorID string) {
		h := knowledge.AnchorHandle(anchorID)
		if h.String() == "" {
			return
		}
		if _, ok := seen[h.String()]; ok {
			return
		}
		seen[h.String()] = struct{}{}
		out = append(out, h)
	}

	filePaths := make([]string, 0, len(seeds))
	for _, s := range seeds {
		if s.Kind != knowledge.KindFile {
			continue
		}
		ref, err := paths.ResolveCodeRefWithVaultPaths(vaultPaths, strings.TrimSpace(s.ID))
		if err != nil || ref.Rel == "" {
			continue
		}
		filePaths = append(filePaths, ref.Rel.String())

		if ref.Abs != "" {
			if info, err := os.Stat(ref.Abs.String()); err == nil && info.IsDir() {
				if files, err := store.FilesByPathPrefix(ctx, ref.Rel.String(), limit); err == nil && len(files) > 0 {
					filePaths = append(filePaths, files...)
				}
			}
		}
	}

	for _, p := range filePaths {
		if len(out) >= limit {
			break
		}
		anchors, err := store.IntelAnchorsByPath(ctx, p)
		if err != nil {
			continue
		}
		for _, a := range anchors {
			add(a.AnchorID)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}
