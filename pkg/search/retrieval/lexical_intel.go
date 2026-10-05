package retrieval

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
)

// IntelLexicalRetriever queries the code intel FTS index (BM25) for anchors and doc sections.
type IntelLexicalRetriever struct {
	Store     *semdb.Store
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

	queryTokens := tokenizeQuery(spec.Text)
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
	sectionIndexes := make(map[string]map[string]int)
	sectionsByID := make(map[string]map[string]codeanchor.IntelDocSection)
	sectionIndex := func(path, sectionID string) int {
		if path == "" || sectionID == "" {
			return -1
		}
		byID, ok := sectionIndexes[path]
		if !ok {
			sections, err := r.Store.IntelDocSectionsByPath(ctx, path)
			if err != nil {
				sectionIndexes[path] = nil
				return -1
			}
			byID = make(map[string]int, len(sections))
			bySectionID := make(map[string]codeanchor.IntelDocSection, len(sections))
			for i, section := range sections {
				if strings.TrimSpace(section.SectionID) != "" {
					byID[section.SectionID] = i
					bySectionID[section.SectionID] = section
				}
			}
			sectionIndexes[path] = byID
			sectionsByID[path] = bySectionID
		}
		index, ok := byID[sectionID]
		if !ok {
			return -1
		}
		return index
	}
	sectionFor := func(path, sectionID string) (codeanchor.IntelDocSection, bool) {
		sectionIndex(path, sectionID)
		section, ok := sectionsByID[path][sectionID]
		return section, ok
	}
	for _, row := range rows {
		score := bm25ToSimilarity(row.Score)
		row.Path = normalizePath(row.Path)
		switch row.Type {
		case "anchor":
			pathOnly := !row.TitleHit && !row.BodyHit
			if pathOnly && len(spec.Filters.ExactSymbols) == 0 && !strings.EqualFold(row.Kind, "module") {
				if modID, ok, err := r.Store.IntelModuleAnchorIDByPath(ctx, row.Lang, row.Path); err == nil && ok && modID != "" {
					h := knowledge.AnchorHandle(modID)
					out = append(out, search.Candidate{
						Handle: h,
						Owner:  h,
						Evidence: []search.Evidence{{
							Type:     "intel_fts_match",
							RawScore: score * 0.55,
							Source:   "pkg/anchors/sqlite",
							Details: map[string]string{
								"snippet":   row.Path,
								"pathOnly":  "true",
								"original":  row.ID,
								"origTitle": row.Title,
							},
						}},
						Type:       "anchor",
						Path:       row.Path,
						Title:      row.Path,
						Symbol:     row.Path,
						Kind:       "module",
						ChunkIndex: -1,
						AnchorID:   modID,
					})
					continue
				}
			}
			h := knowledge.AnchorHandle(row.ID)
			out = append(out, search.Candidate{
				Handle: h,
				Owner:  h,
				Evidence: []search.Evidence{{
					Type:     "intel_fts_match",
					RawScore: score,
					Source:   "pkg/anchors/sqlite",
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
				Type:       "anchor",
				Path:       row.Path,
				Title:      row.Title,
				Symbol:     row.Title,
				FQN:        row.FQN,
				Kind:       row.Kind,
				ChunkIndex: -1,
				AnchorID:   row.ID,
			})
		case "doc_section":
			fileHandle := knowledge.FileHandle(row.Path)
			h := fileHandle
			h.Fragments = []string{"doc", row.ID}
			section, _ := sectionFor(row.Path, row.ID)
			sectionRef := canonicalDocSectionRef(row.Path, section, row.Title)
			sectionRefJSON, _ := json.Marshal(sectionRef)
			owner := knowledge.NoteHandle(row.Path)
			evidence := []search.Evidence{{
				Type:     "intel_doc_match",
				RawScore: score,
				Source:   "pkg/anchors/sqlite",
				Details: map[string]string{
					"snippet": row.Snippet,
				},
			}}
			if len(queryTokens) > 0 {
				title := titleFromPath(row.Path)
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
			out = append(out, search.Candidate{
				Handle:        h,
				Owner:         owner,
				Evidence:      evidence,
				Type:          "doc_section",
				Path:          row.Path,
				Title:         row.Title,
				Kind:          row.Kind,
				ChunkIndex:    sectionIndex(row.Path, row.ID),
				NoteID:        row.Path,
				NodeID:        sectionRef.NodeID,
				NodeRefJSON:   string(sectionRefJSON),
				SourceLocator: sectionRef.String(),
				NodeKind:      string(sectionRef.Kind),
				NodeRef:       &sectionRef,
			})
		case "note_region_visible", "note_region_supplemental":
			h := knowledge.NoteHandle(row.Path)
			evidence := []search.Evidence{{
				Type:     "intel_doc_match",
				RawScore: score,
				Source:   "pkg/anchors/sqlite",
				Details: map[string]string{
					"snippet":    row.Snippet,
					"regionKind": strings.TrimPrefix(row.Type, "note_region_"),
				},
			}}
			if len(queryTokens) > 0 {
				title := titleFromPath(row.Path)
				if titleScore := tokenMatchScore(queryTokens, title); titleScore > 0 {
					evidence = append(evidence, search.Evidence{
						Type: "note_title_match", RawScore: titleScore, Source: "intel_lexical",
						Details: map[string]string{"noteTitle": title},
					})
				}
			}
			out = append(out, search.Candidate{
				Handle: h, Owner: h, Evidence: evidence,
				Type: "note", Path: row.Path, Title: row.Title,
				Kind: row.Kind, ChunkIndex: -1,
			})
		}
	}
	return out, nil
}

func canonicalDocSectionRef(path string, section codeanchor.IntelDocSection, fallbackTitle string) ontology.NodeRef {
	title := strings.TrimSpace(section.Title)
	if title == "" {
		title = strings.TrimSpace(fallbackTitle)
	}
	ref := ontology.NodeRef{
		NotePath: path,
		NodeID:   strings.TrimSpace(section.SectionID),
		Fragment: title,
		Kind:     ontology.NodeKindSection,
	}
	if section.StartByte >= int64(minIntValue()) && section.StartByte <= int64(maxIntValue()) {
		ref.StartByte = int(section.StartByte)
	}
	if section.EndByte >= int64(minIntValue()) && section.EndByte <= int64(maxIntValue()) {
		ref.EndByte = int(section.EndByte)
	}

	// Intel doc sections use a hash for their storage owner. Parse the stored
	// source section to recover the ontology parser's exact source identity,
	// which includes the heading's byte offset and therefore distinguishes
	// duplicate headings. The parser also preserves an authored block ID when
	// one is present.
	content := section.Content
	if strings.TrimSpace(content) == "" || !strings.HasPrefix(strings.TrimSpace(content), "#") {
		return noteRootRef(path, section)
	}
	nodes := ontology.ParseSections(path, content)
	if len(nodes) == 0 || nodes[0] == nil || nodes[0].StartByte != 0 {
		// A headingless note is indexed as one whole-file doc section. Its
		// canonical source identity is the note root, not a fabricated heading.
		return noteRootRef(path, section)
	}
	if section.StartByte >= 0 {
		if nodes[0].BlockID != "" {
			ref.NodeID = nodes[0].ID
		} else if section.StartByte <= int64(maxIntValue()) {
			ref.NodeID = ontology.CanonicalSectionID(path, title, int(section.StartByte))
		}
	}
	if strings.TrimSpace(ref.NodeID) == "" && title != "" && section.StartByte >= 0 && section.StartByte <= int64(maxIntValue()) {
		ref.NodeID = ontology.CanonicalSectionID(path, title, int(section.StartByte))
	}
	if fragment := strings.TrimPrefix(ref.NodeID, path+"#"); fragment != ref.NodeID {
		ref.Fragment = fragment
	}
	return ref
}

func noteRootRef(path string, section codeanchor.IntelDocSection) ontology.NodeRef {
	ref := ontology.NodeRef{
		NotePath: path,
		Kind:     ontology.NodeKindNote,
	}
	if section.StartByte >= int64(minIntValue()) && section.StartByte <= int64(maxIntValue()) {
		ref.StartByte = int(section.StartByte)
	}
	if section.EndByte >= int64(minIntValue()) && section.EndByte <= int64(maxIntValue()) {
		ref.EndByte = int(section.EndByte)
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
