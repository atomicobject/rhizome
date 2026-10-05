package retrieval

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
)

// CodeAnchorNotesRetriever expands code file seeds to notes by evaluating code anchors
// (the note->code side) using the persisted code index + anchor scopes.
//
// This is intended to make `rzm search --seed/--file <code>` surface the docs that
// explicitly apply to the seeded code, even when the code has no coderef back to the note.
type CodeAnchorNotesRetriever struct {
	Store     *semdb.Store
	VaultPath string
	Limit     int
}

func (r *CodeAnchorNotesRetriever) Name() string { return "code_anchor_notes" }

func (r *CodeAnchorNotesRetriever) Retrieve(ctx context.Context, spec search.QuerySpec) ([]search.Candidate, error) {
	if r.Store == nil || strings.TrimSpace(r.VaultPath) == "" || len(spec.Seeds) == 0 {
		return nil, nil
	}
	vaultPaths, err := paths.NewVaultPaths(r.VaultPath)
	if err != nil || vaultPaths.Root() == "" {
		return nil, nil
	}

	limit := r.Limit
	if limit <= 0 {
		limit = 50
	}

	svc := codeanchor.NewServiceWithOptions(
		r.Store,
		nil,
		codeanchor.WithBasePath(vaultPaths.Root()),
		codeanchor.WithoutWarmCache(),
	)

	type noteHit struct {
		noteRel string
		title   string
		anchor  string
	}
	seenNotes := make(map[string]struct{})
	var hits []noteHit

	addNote := func(noteAbs, title, anchorLabel string) {
		noteRel, err := vaultPaths.RelNotePathStrict(noteAbs)
		if err != nil || noteRel == "" {
			return
		}
		key := noteRel.String() + "|" + anchorLabel
		if _, ok := seenNotes[key]; ok {
			return
		}
		seenNotes[key] = struct{}{}
		hits = append(hits, noteHit{noteRel: noteRel.String(), title: title, anchor: anchorLabel})
	}

	for _, seed := range spec.Seeds {
		if len(hits) >= limit {
			break
		}
		if seed.Kind != knowledge.KindFile {
			continue
		}
		seedPath := strings.TrimSpace(seed.ID)
		if seedPath == "" {
			continue
		}

		var absSeed string
		if filepath.IsAbs(seedPath) {
			absSeed = seedPath
		} else {
			rel, err := vaultPaths.RelCode(seedPath)
			if err != nil || rel == "" {
				continue
			}
			abs, err := vaultPaths.AbsCode(rel)
			if err != nil || abs == "" {
				continue
			}
			absSeed = abs.String()
		}
		if absSeed == "" {
			continue
		}

		filePaths := []string{absSeed}
		if info, err := os.Stat(absSeed); err == nil && info.IsDir() {
			maxScan := 200
			if limit < maxScan {
				maxScan = limit
			}
			if files, err := r.Store.FilesByPathPrefix(ctx, absSeed, maxScan); err == nil && len(files) > 0 {
				filePaths = files
			}
		}

		for _, p := range filePaths {
			if len(hits) >= limit {
				break
			}
			fc, err := svc.NotesForFile(ctx, p)
			if err != nil {
				continue
			}
			if len(fc.AnchorNotes) > 0 {
				for _, an := range fc.AnchorNotes {
					label := strings.TrimSpace(an.Label)
					if label == "" {
						continue
					}
					for _, n := range an.Notes {
						addNote(n.Path, n.Title, label)
						if len(hits) >= limit {
							break
						}
					}
					if len(hits) >= limit {
						break
					}
				}
				continue
			}
			// Fallback (older stores/services): no per-anchor attribution.
			for _, n := range fc.Notes {
				addNote(n.Path, n.Title, "")
				if len(hits) >= limit {
					break
				}
			}
		}
	}

	if len(hits) == 0 {
		return nil, nil
	}

	out := make([]search.Candidate, 0, len(hits))
	for _, h := range hits {
		noteID, ok := cleanTypedNotePath(h.noteRel)
		if !ok {
			continue
		}
		nh := knowledge.NoteHandle(noteID)

		details := map[string]string{}
		if h.anchor != "" {
			details["anchor"] = h.anchor
		}
		out = append(out, search.Candidate{
			Handle: nh,
			Owner:  nh,
			Evidence: []search.Evidence{{
				Type:     "code_anchor",
				RawScore: 1.0,
				Source:   "code_anchor_notes",
				Details:  details,
			}},
			Type:       "note",
			NoteID:     noteID,
			Path:       noteID,
			Title:      firstNonEmptyString(h.title, nodeTitle(noteID)),
			ChunkIndex: -1,
		})
	}
	return out, nil
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
