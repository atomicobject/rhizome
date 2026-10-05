package retrieval

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/bmatcuk/doublestar/v4"
)

// CodeAnchorRefsRetriever expands note seeds to code anchors by using code-anchor scopes.
//
// This is the "note -> code" counterpart to coderef-driven doc_links ("code -> note").
// It is intentionally conservative (limited fanout, prefers definition-side anchors).
type CodeAnchorRefsRetriever struct {
	Store     *semdb.Store
	VaultPath string

	MaxAnchorsPerNote int
	MaxTargetsPerNote int
	MaxFQNsPerAnchor  int
}

func (r *CodeAnchorRefsRetriever) Name() string { return "code_anchor_refs" }

func (r *CodeAnchorRefsRetriever) Retrieve(ctx context.Context, spec search.QuerySpec) ([]search.Candidate, error) {
	if r.Store == nil || strings.TrimSpace(r.VaultPath) == "" || len(spec.Seeds) == 0 {
		return nil, nil
	}
	vaultPaths, err := paths.NewVaultPaths(r.VaultPath)
	if err != nil || vaultPaths.Root() == "" {
		return nil, nil
	}

	maxAnchorsPerNote := r.MaxAnchorsPerNote
	if maxAnchorsPerNote <= 0 {
		maxAnchorsPerNote = 8
	}
	maxTargetsPerNote := r.MaxTargetsPerNote
	if maxTargetsPerNote <= 0 {
		maxTargetsPerNote = 25
	}
	maxFQNsPerAnchor := r.MaxFQNsPerAnchor
	if maxFQNsPerAnchor <= 0 {
		maxFQNsPerAnchor = 12
	}

	const maxFilesToScanForGlobs = 4000

	type pending struct {
		noteRel string
	}
	var notes []pending
	for _, seed := range spec.Seeds {
		switch seed.Kind {
		case knowledge.KindNote, knowledge.KindNoteChunk:
			noteRel, err := vaultPaths.RelNotePathStrict(seed.ID)
			if err != nil || noteRel == "" {
				continue
			}
			notes = append(notes, pending{
				noteRel: noteRel.String(),
			})
		}
	}
	if len(notes) == 0 {
		return nil, nil
	}

	merged := make(map[string]search.Candidate)
	relPathForAbs := func(abs string) string {
		rel, err := vaultPaths.Rel(abs)
		if err != nil || rel == "" {
			return ""
		}
		return rel.String()
	}
	for _, n := range notes {
		anchorIDs, err := r.Store.AnchorIDsForNotePath(ctx, n.noteRel)
		if err != nil {
			return nil, err
		}
		if len(anchorIDs) == 0 {
			continue
		}
		if len(anchorIDs) > maxAnchorsPerNote {
			anchorIDs = anchorIDs[:maxAnchorsPerNote]
		}

		anchors, err := r.Store.AnchorsByIDs(ctx, anchorIDs)
		if err != nil {
			return nil, err
		}

		// Prefer more specific anchors first to keep fanout useful.
		sort.SliceStable(anchors, func(i, j int) bool {
			// Symbol-based anchors first, then path/glob.
			score := func(aKind string) int {
				switch strings.ToLower(aKind) {
				case "function", "baseclass", "annotation", "functionuse":
					return 3
				case "path", "glob":
					return 2
				default:
					return 1
				}
			}
			si := score(string(anchors[i].Kind))
			sj := score(string(anchors[j].Kind))
			if si != sj {
				return si > sj
			}
			return anchors[i].Label < anchors[j].Label
		})

		targetsEmitted := 0
		fileModuleEmitted := 0
		for _, a := range anchors {
			if targetsEmitted >= maxTargetsPerNote {
				break
			}

			switch a.Kind {
			case codeanchor.AnchorFunc, codeanchor.AnchorBaseClass, codeanchor.AnchorAnnotation:
				symbolFQNs, _, err := r.Store.AnchorScope(ctx, a.ID)
				if err != nil {
					return nil, err
				}
				if len(symbolFQNs) == 0 {
					continue
				}

				// Limit how many symbols we fan out per anchor.
				if len(symbolFQNs) > maxFQNsPerAnchor {
					symbolFQNs = symbolFQNs[:maxFQNsPerAnchor]
				}

				intelIDs, err := r.Store.IntelAnchorIDsByFQNs(ctx, symbolFQNs)
				if err != nil {
					return nil, err
				}
				intelByID, err := r.Store.IntelAnchorsByIDs(ctx, intelIDs)
				if err != nil {
					return nil, err
				}
				for _, id := range intelIDs {
					if targetsEmitted >= maxTargetsPerNote {
						break
					}
					intel, ok := intelByID[id]
					if !ok {
						continue
					}
					h := knowledge.AnchorHandle(id)
					key := h.String()

					c := search.Candidate{
						Handle: h,
						Owner:  h,
						Evidence: []search.Evidence{{
							Type:     "code_anchor",
							RawScore: 0.7,
							Source:   "code_anchor_refs",
							Details: map[string]string{
								"seed":    n.noteRel,
								"label":   a.Label,
								"snippet": "via code anchor: " + a.Label,
							},
						}},
						Type:       "anchor",
						Path:       intel.Path,
						Title:      intel.Symbol,
						Symbol:     intel.Symbol,
						FQN:        intel.FQN,
						Kind:       intel.Kind,
						AnchorID:   id,
						ChunkIndex: -1,
					}

					if existing, ok := merged[key]; ok {
						merged[key] = search.MergeCandidate(existing, c)
					} else {
						merged[key] = c
					}
					targetsEmitted++
				}
			case codeanchor.AnchorPath:
				// For path anchors, expand to module anchors for the matched files (low fanout).
				if fileModuleEmitted >= 10 {
					continue
				}
				files, err := r.Store.FilesByPathPrefix(ctx, a.PathPrefix, 50)
				if err != nil {
					return nil, err
				}
				for _, absFile := range files {
					if fileModuleEmitted >= 10 || targetsEmitted >= maxTargetsPerNote {
						break
					}
					rel := relPathForAbs(absFile)
					if rel == "" {
						continue
					}
					if id, ok := r.moduleAnchorForAbsFile(ctx, vaultPaths, absFile); ok {
						h := knowledge.AnchorHandle(id)
						key := h.String()
						c := search.Candidate{
							Handle: h,
							Owner:  h,
							Evidence: []search.Evidence{{
								Type:     "code_anchor",
								RawScore: 0.5,
								Source:   "code_anchor_refs",
								Details: map[string]string{
									"seed":    n.noteRel,
									"label":   a.Label,
									"snippet": "via code anchor: " + a.Label,
								},
							}},
							Type:       "anchor",
							Path:       rel,
							Title:      filepath.Base(absFile),
							Symbol:     filepath.Base(absFile),
							Kind:       "module",
							AnchorID:   id,
							ChunkIndex: -1,
						}
						if existing, ok := merged[key]; ok {
							merged[key] = search.MergeCandidate(existing, c)
						} else {
							merged[key] = c
						}
						fileModuleEmitted++
						targetsEmitted++
					}
				}
			case codeanchor.AnchorGlob:
				if fileModuleEmitted >= 10 || len(a.Globs) == 0 {
					continue
				}
				files, err := r.Store.ListFiles(ctx, maxFilesToScanForGlobs)
				if err != nil {
					return nil, err
				}
				for _, absFile := range files {
					if fileModuleEmitted >= 10 || targetsEmitted >= maxTargetsPerNote {
						break
					}
					relPath := relPathForAbs(absFile)
					absPath := filepath.ToSlash(absFile)
					matched := false
					for _, pat := range a.Globs {
						target := relPath
						if filepath.IsAbs(filepath.FromSlash(pat)) {
							target = absPath
						}
						ok, _ := doublestar.Match(pat, target)
						if ok {
							matched = true
							break
						}
					}
					if !matched {
						continue
					}
					if relPath == "" {
						continue
					}
					if id, ok := r.moduleAnchorForAbsFile(ctx, vaultPaths, absFile); ok {
						h := knowledge.AnchorHandle(id)
						key := h.String()
						c := search.Candidate{
							Handle: h,
							Owner:  h,
							Evidence: []search.Evidence{{
								Type:     "code_anchor",
								RawScore: 0.5,
								Source:   "code_anchor_refs",
								Details: map[string]string{
									"seed":    n.noteRel,
									"label":   a.Label,
									"snippet": "via code anchor: " + a.Label,
								},
							}},
							Type:       "anchor",
							Path:       relPath,
							Title:      filepath.Base(absFile),
							Symbol:     filepath.Base(absFile),
							Kind:       "module",
							AnchorID:   id,
							ChunkIndex: -1,
						}
						if existing, ok := merged[key]; ok {
							merged[key] = search.MergeCandidate(existing, c)
						} else {
							merged[key] = c
						}
						fileModuleEmitted++
						targetsEmitted++
					}
				}
			}
		}
	}

	out := make([]search.Candidate, 0, len(merged))
	for _, c := range merged {
		out = append(out, c)
	}
	return out, nil
}

func (r *CodeAnchorRefsRetriever) moduleAnchorForAbsFile(ctx context.Context, vaultPaths paths.VaultPaths, absFile string) (string, bool) {
	if r.Store == nil || strings.TrimSpace(absFile) == "" || vaultPaths.Root() == "" {
		return "", false
	}
	rel, err := vaultPaths.RelCode(absFile)
	if err != nil || rel == "" {
		return "", false
	}
	id, ok, err := r.Store.IntelModuleAnchorIDByPath(ctx, "", rel.String())
	if err != nil || !ok {
		return "", false
	}
	return id, true
}
