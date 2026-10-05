package retrieval

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
)

// DocLinksRetriever expands seeds via persisted doc_links (coderefs + curated doc links).
type DocLinksRetriever struct {
	Store *semdb.Store
	// VaultPath allows converting doc link source paths into vault-relative handles.
	VaultPath string
	Limit     int
}

func (r *DocLinksRetriever) Name() string { return "doc_links" }

func (r *DocLinksRetriever) Retrieve(ctx context.Context, spec search.QuerySpec) ([]search.Candidate, error) {
	if r.Store == nil || len(spec.Seeds) == 0 {
		return nil, nil
	}
	limit := r.Limit
	if limit <= 0 {
		limit = 10
	}

	vaultPaths, _ := paths.NewVaultPaths(r.VaultPath)

	var out []search.Candidate
	seen := make(map[string]struct{})
	var noteSeeds []string
	var anchorSeeds []string
	var fileSeeds []string
	noteSeedSeen := make(map[string]struct{})
	anchorSeedSeen := make(map[string]struct{})
	fileSeedSeen := make(map[string]struct{})

	addSeed := func(bucket *[]string, seen map[string]struct{}, key string) {
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		*bucket = append(*bucket, key)
	}

	for _, seed := range spec.Seeds {
		switch seed.Kind {
		case knowledge.KindNote, knowledge.KindNoteChunk:
			if notePath, ok := cleanTypedNotePath(seed.ID); ok {
				addSeed(&noteSeeds, noteSeedSeen, notePath)
			}

		case knowledge.KindAnchor:
			addSeed(&anchorSeeds, anchorSeedSeen, strings.TrimSpace(seed.ID))
		case knowledge.KindFile:
			addSeed(&fileSeeds, fileSeedSeen, strings.TrimSpace(seed.ID))
		}
	}

	if len(noteSeeds) > 0 {
		noteLinks, err := r.Store.DocLinksForNotes(ctx, noteSeeds, limit)
		if err != nil {
			return nil, fmt.Errorf("doc links for notes: %w", err)
		}
		for _, notePath := range noteSeeds {
			links := noteLinks[notePath]
			for _, l := range links {
				srcPath, ok := relCodeDocPath(vaultPaths, l.SrcPath)
				if !ok {
					continue
				}
				h := knowledge.FileHandle(srcPath)
				key := h.String()
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				out = append(out, search.Candidate{
					Handle: h,
					Owner:  h,
					Evidence: []search.Evidence{{
						Type:     "code_ref",
						RawScore: 1.0,
						Source:   "doc_links",
						Details: map[string]string{
							"seed":    notePath,
							"snippet": l.Snippet,
						},
					}},
					Type:  "file",
					Path:  srcPath,
					Title: srcPath,
				})
			}
		}
	}

	if len(anchorSeeds) > 0 {
		anchorLinks, err := r.Store.DocLinksForAnchors(ctx, anchorSeeds, limit)
		if err != nil {
			return nil, fmt.Errorf("doc links for anchors: %w", err)
		}
		for _, anchorID := range anchorSeeds {
			links := anchorLinks[anchorID]
			for _, l := range links {
				// These are typically note->anchor links; surface the note side.
				notePath, ok := relNoteDocPath(vaultPaths, l.SrcPath)
				if !ok {
					continue
				}
				h := knowledge.NoteHandle(notePath)
				key := h.String()
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				out = append(out, search.Candidate{
					Handle: h,
					Owner:  h,
					Evidence: []search.Evidence{{
						Type:     "doc_link",
						RawScore: 1.0,
						Source:   "doc_links",
						Details: map[string]string{
							"seed":    anchorID,
							"snippet": l.Snippet,
						},
					}},
					Type:       "note",
					NoteID:     notePath,
					Path:       notePath,
					Title:      nodeTitle(notePath),
					ChunkIndex: -1,
				})
			}
		}
	}

	for _, srcPath := range fileSeeds {
		if srcPath == "" {
			continue
		}
		absSeed := srcPath
		if !filepath.IsAbs(absSeed) && strings.TrimSpace(r.VaultPath) != "" {
			absSeed = filepath.Join(r.VaultPath, filepath.FromSlash(absSeed))
		}

		filePaths := []string{absSeed}
		// If the seed is a directory, expand to files within it (best-effort, capped).
		if info, err := os.Stat(absSeed); err == nil && info.IsDir() {
			if files, err := r.Store.FilesByPathPrefix(ctx, absSeed, limit); err == nil && len(files) > 0 {
				filePaths = files
			}
		}
		links := make([]codeanchor.DocLink, 0, limit)
		for _, p := range filePaths {
			if len(links) >= limit {
				break
			}
			perPath := limit - len(links)
			sub, err := r.Store.DocLinksFromCodePath(ctx, p, perPath)
			if err != nil {
				return nil, fmt.Errorf("doc links from code %s: %w", p, err)
			}
			links = append(links, sub...)
		}
		for _, l := range links {
			if l.DstKind != "note" {
				continue
			}
			notePath, ok := cleanTypedNotePath(l.DstPath)
			if !ok {
				continue
			}
			h := knowledge.NoteHandle(notePath)
			key := h.String()
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, search.Candidate{
				Handle: h,
				Owner:  h,
				Evidence: []search.Evidence{{
					Type:     "code_ref",
					RawScore: 1.0,
					Source:   "doc_links",
					Details: map[string]string{
						"seed":    srcPath,
						"snippet": l.Snippet,
					},
				}},
				Type:       "note",
				NoteID:     notePath,
				Path:       notePath,
				Title:      nodeTitle(notePath),
				ChunkIndex: -1,
			})
		}
	}

	return out, nil
}

func relCodeDocPath(vaultPaths paths.VaultPaths, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if vaultPaths.Root() != "" {
		if rel, err := vaultPaths.RelCodeStrict(raw); err == nil && rel.String() != "" {
			return rel.String(), true
		}
	}
	if rel, err := paths.CleanRelPath(raw); err == nil && rel.String() != "" {
		return rel.String(), true
	}
	return "", false
}

func relNoteDocPath(vaultPaths paths.VaultPaths, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if vaultPaths.Root() != "" {
		if rel, err := vaultPaths.RelNotePathStrict(raw); err == nil && rel.String() != "" {
			return rel.String(), true
		}
	}
	if path, ok := cleanTypedNotePath(raw); ok {
		return path, true
	}
	return "", false
}
