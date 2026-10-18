package retrieval

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/search/queryframe"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

// IntelLexicalStore supplies the persisted lexical projection.
type IntelLexicalStore interface {
	SearchIntelFTSFiltered(context.Context, string, int, semdb.IntelSearchFilters) ([]semdb.IntelSearchRow, error)
	ModuleAnchorIDsByPaths(context.Context, []string) (map[string]string, error)
}

// IntelLexicalRetriever queries the code intel FTS index (BM25) for anchors and doc sections.
type IntelLexicalRetriever struct {
	Store     IntelLexicalStore
	VaultPath string
}

func (r *IntelLexicalRetriever) Name() string { return "intel_lexical" }

func (r *IntelLexicalRetriever) Retrieve(ctx context.Context, spec search.QuerySpec) ([]search.Candidate, error) {
	if strings.TrimSpace(spec.Text) == "" {
		return nil, nil
	}
	if r.Store == nil {
		return nil, nil
	}
	limit := spec.Limits.Total
	if limit <= 0 {
		limit = 25
	}

	queryTokens := queryframe.IdentifierTerms(spec.Text)
	if filtered := filterDocTokens(queryTokens); len(filtered) > 0 {
		queryTokens = filtered
	}

	rows, err := r.Store.SearchIntelFTSFiltered(ctx, spec.Text, limit, semdb.IntelSearchFilters{
		Types:        spec.Filters.Types,
		PathPrefixes: spec.Filters.PathPrefixes,
		NoteTypes:    spec.Filters.NoteTypes,
		ExactSymbols: spec.Filters.ExactSymbols,
		TestsOnly:    spec.Filters.TestsOnly,
		ExcludeTests: spec.Filters.ExcludeTests,
	})
	if err != nil {
		return nil, fmt.Errorf("intel fts search: %w", err)
	}

	vaultPaths, _ := paths.NewVaultPaths(r.VaultPath)
	normalizePath := func(raw string) string {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return ""
		}
		if vaultPaths.Root() != "" {
			if rel, err := vaultPaths.RelStrict(raw); err == nil && rel.String() != "" {
				return rel.String()
			}
		}
		return filepath.ToSlash(raw)
	}

	out := make([]search.Candidate, 0, len(rows))
	for i := range rows {
		rows[i].Path = normalizePath(rows[i].Path)
	}
	modulePaths := make([]string, 0)
	if len(spec.Filters.ExactSymbols) == 0 {
		for _, row := range rows {
			if row.Type == "anchor" && !row.TitleHit && !row.BodyHit && !strings.EqualFold(row.Kind, "module") {
				modulePaths = append(modulePaths, row.Path)
			}
		}
	}
	modules := map[string]string{}
	if len(modulePaths) > 0 {
		modules, err = r.Store.ModuleAnchorIDsByPaths(ctx, modulePaths)
		if err != nil {
			return nil, fmt.Errorf("intel module anchors: %w", err)
		}
	}
	for _, row := range rows {
		score := bm25ToSimilarity(row.Score)
		switch row.Type {
		case "anchor":
			pathOnly := !row.TitleHit && !row.BodyHit
			if pathOnly && len(spec.Filters.ExactSymbols) == 0 && !strings.EqualFold(row.Kind, "module") {
				if modID := modules[row.Path]; modID != "" {
					h := knowledge.AnchorHandle(modID)
					out = append(out, search.Candidate{SourceRef: search.SourceRef{Path: row.Path,
						Title:      row.Path,
						Symbol:     row.Path,
						Kind:       "module",
						ChunkIndex: -1,
						AnchorID:   modID}, Handle: h,
						Owner: h,
						Evidence: []search.Evidence{{
							Type:     "intel_fts_match",
							RawScore: score * 0.55,
							Source:   "pkg/store",
							Details: map[string]string{
								"snippet":   row.Path,
								"pathOnly":  "true",
								"original":  row.ID,
								"origTitle": row.Title,
							},
						}},
						Type: "anchor",
					})
					continue
				}
			}
			h := knowledge.AnchorHandle(row.ID)
			out = append(out, search.Candidate{SourceRef: search.SourceRef{Path: row.Path,
				Title:      row.Title,
				Symbol:     row.Title,
				FQN:        row.FQN,
				Kind:       row.Kind,
				ChunkIndex: -1,
				AnchorID:   row.ID}, Handle: h,
				Owner: h,
				Evidence: []search.Evidence{{
					Type:     "intel_fts_match",
					RawScore: score,
					Source:   "pkg/store",
					Details: map[string]string{
						"snippet": row.Snippet,
						"pathOnly": func() string {
							if pathOnly {
								return "true"
							}
							return "false"
						}(),
					},
				}},
				Type: "anchor",
			})
		case "doc_section":
			fileHandle := knowledge.FileHandle(row.Path)
			h := fileHandle
			h.Fragments = []string{"doc", row.ID}
			sectionRef := persistedDocSectionRef(row)
			owner := knowledge.NoteHandle(row.Path)
			evidence := []search.Evidence{{
				Type:     "intel_doc_match",
				RawScore: score,
				Source:   "pkg/store",
				Details: map[string]string{
					"snippet": row.Snippet,
				},
			}}
			if len(queryTokens) > 0 {
				title := paths.NoteTitle(row.Path)
				if titleScore := tokenMatchScore(queryTokens, title); titleScore > 0 {
					evidence = append(evidence, search.Evidence{
						Type:     "note_title_match",
						RawScore: titleScore,
						Source:   "intel_lexical",
						Details: map[string]string{
							"noteTitle": title,
						},
					})
				}
			}
			out = append(out, search.Candidate{SourceRef: search.SourceRef{Path: row.Path,
				Title:      row.Title,
				Kind:       row.Kind,
				ChunkIndex: row.SectionIndex,
				NoteID:     row.Path,
				NodeID:     sectionRef.NodeID,
				NodeRef:    &sectionRef}, Handle: h,
				Owner:    owner,
				Evidence: evidence,
				Type:     "doc_section",
			})
		case "note_region_visible", "note_region_supplemental":
			h := knowledge.NoteHandle(row.Path)
			evidence := []search.Evidence{{
				Type:     "intel_doc_match",
				RawScore: score,
				Source:   "pkg/store",
				Details: map[string]string{
					"snippet":    row.Snippet,
					"regionKind": strings.TrimPrefix(row.Type, "note_region_"),
				},
			}}
			if len(queryTokens) > 0 {
				title := paths.NoteTitle(row.Path)
				if titleScore := tokenMatchScore(queryTokens, title); titleScore > 0 {
					evidence = append(evidence, search.Evidence{
						Type: "note_title_match", RawScore: titleScore, Source: "intel_lexical",
						Details: map[string]string{"noteTitle": title},
					})
				}
			}
			out = append(out, search.Candidate{SourceRef: search.SourceRef{Path: row.Path, Title: row.Title,
				Kind: row.Kind, ChunkIndex: -1}, Handle: h, Owner: h, Evidence: evidence,
				Type: "note",
			})
		}
	}
	return out, nil
}

func persistedDocSectionRef(row semdb.IntelSearchRow) ontology.NodeRef {
	ref := ontology.NodeRef{NotePath: row.Path, NodeID: row.SourceNodeID, Kind: ontology.NodeKindNote}
	if row.SourceNodeID != "" {
		ref.Kind = ontology.NodeKindSection
		ref.Fragment = strings.TrimPrefix(row.SourceNodeID, row.Path+"#")
	}
	if row.StartByte >= int64(minIntValue()) && row.StartByte <= int64(maxIntValue()) {
		ref.StartByte = int(row.StartByte)
	}
	if row.EndByte >= int64(minIntValue()) && row.EndByte <= int64(maxIntValue()) {
		ref.EndByte = int(row.EndByte)
	}
	return ref
}

func maxIntValue() int {
	return int(^uint(0) >> 1)
}

func minIntValue() int {
	return -maxIntValue() - 1
}

func bm25ToSimilarity(bm25 float64) float64 {
	// bm25() is unbounded, and in SQLite FTS5 more relevant matches can be *more negative*.
	// Convert to a (0,1) similarity where higher is better.
	if math.IsNaN(bm25) || math.IsInf(bm25, 0) {
		return 0
	}
	const scale = 2.0
	s := 1 / (1 + math.Exp(bm25/scale))
	if s < 0 {
		return 0
	}
	if s > 1 {
		return 1
	}
	return s
}
