package retrieval

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
)

var defaultRationaleFTSKinds = []string{
	string(codeanchor.RationaleWhy),
	string(codeanchor.RationaleRationale),
	string(codeanchor.RationaleImportant),
}

type rationaleFTSStore interface {
	SearchRationaleFTS(ctx context.Context, query string, kinds []string, limit int) ([]semdb.RationaleSearchRow, error)
	IntelAnchorsByFQNsLimited(ctx context.Context, fqns []string, limitPerFQN int) (map[string][]codeanchor.IntelAnchor, error)
	IntelAnchorsByPaths(ctx context.Context, paths []string) (map[string][]codeanchor.IntelAnchor, error)
}

// RationaleFTSRetriever treats rationale comments as supporting evidence for code
// entities. It deliberately maps matches onto existing anchor/file handles so
// WHY/RATIONALE comments boost addressable results without becoming a broad
// standalone result lane.
type RationaleFTSRetriever struct {
	Store     rationaleFTSStore
	VaultPath string
	Limit     int
	Kinds     []string
}

func (r *RationaleFTSRetriever) Name() string { return "rationale_fts" }

func (r *RationaleFTSRetriever) CostClass() search.RetrieverCostClass {
	return search.RetrieverCostIOBound
}

func (r *RationaleFTSRetriever) Retrieve(ctx context.Context, spec search.QuerySpec) ([]search.Candidate, error) {
	if r.Store == nil || strings.TrimSpace(spec.Text) == "" || !ShouldQueryRationaleEvidence(spec) {
		return nil, nil
	}
	limit := r.Limit
	if limit <= 0 {
		limit = spec.Limits.Total
	}
	if limit <= 0 {
		limit = 12
	}
	limit = clampInt(limit, 4, 30)
	kinds := r.Kinds
	if len(kinds) == 0 {
		kinds = defaultRationaleFTSKinds
	}

	rows, err := r.Store.SearchRationaleFTS(ctx, spec.Text, kinds, limit)
	if err != nil {
		return nil, fmt.Errorf("rationale fts search: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	normalizePath := rationalePathNormalizer(r.VaultPath)
	for i := range rows {
		rows[i].Path = normalizePath(rows[i].Path)
	}
	symbolAnchors, err := rationaleSymbolAnchors(ctx, r.Store, rows)
	if err != nil {
		return nil, err
	}
	anchorsByPath, err := rationaleAnchorsByPath(ctx, r.Store, rows)
	if err != nil {
		return nil, err
	}

	out := make([]search.Candidate, 0, len(rows))
	for _, row := range rows {
		score := bm25ToSimilarity(row.Score) * rationaleKindBoost(row.Kind)
		ev := search.Evidence{
			Type:     "rationale_fts_match",
			RawScore: score,
			Source:   "rationale_fts",
			Details: map[string]string{
				"rationaleID": row.ID,
				"kind":        row.Kind,
				"snippet":     firstRationaleNonEmpty(strings.TrimSpace(row.Snippet), trimRationaleOneLine(row.Content, 180)),
				"startLine":   strconv.FormatInt(row.StartLine, 10),
				"endLine":     strconv.FormatInt(row.EndLine, 10),
			},
		}
		if row.SymbolFQN != "" {
			ev.Details["symbolFQN"] = row.SymbolFQN
		}

		if row.SymbolFQN != "" {
			if matched := symbolAnchors[row.SymbolFQN]; len(matched) > 0 {
				a := matched[0]
				h := knowledge.AnchorHandle(a.AnchorID)
				out = append(out, search.Candidate{
					Handle:      h,
					Owner:       h,
					Evidence:    []search.Evidence{ev},
					SupportOnly: true,
					Type:        "anchor",
					Path:        normalizePath(a.Path),
					Title:       a.Symbol,
					Symbol:      a.Symbol,
					FQN:         a.FQN,
					Kind:        a.Kind,
					ChunkIndex:  -1,
					AnchorID:    a.AnchorID,
				})
				continue
			}
		}

		h := knowledge.FileHandle(row.Path)
		out = append(out, search.Candidate{
			Handle:      h,
			Owner:       h,
			Evidence:    []search.Evidence{ev},
			SupportOnly: true,
			Type:        "file",
			Path:        row.Path,
			Title:       row.Path,
			Kind:        "file",
			ChunkIndex:  -1,
		})
		if module := moduleAnchorCandidate(row.Path, anchorsByPath[row.Path], ev); module.Handle.String() != "" {
			out = append(out, module)
		}
	}
	return out, nil
}

func rationalePathNormalizer(vaultPath string) func(string) string {
	vaultPaths, _ := paths.NewVaultPaths(vaultPath)
	return func(raw string) string {
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
}

func rationaleSymbolAnchors(ctx context.Context, store rationaleFTSStore, rows []semdb.RationaleSearchRow) (map[string][]codeanchor.IntelAnchor, error) {
	fqns := make([]string, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.SymbolFQN) != "" {
			fqns = append(fqns, row.SymbolFQN)
		}
	}
	if len(fqns) == 0 {
		return map[string][]codeanchor.IntelAnchor{}, nil
	}
	return store.IntelAnchorsByFQNsLimited(ctx, fqns, 1)
}

func rationaleAnchorsByPath(ctx context.Context, store rationaleFTSStore, rows []semdb.RationaleSearchRow) (map[string][]codeanchor.IntelAnchor, error) {
	paths := make([]string, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Path) != "" {
			paths = append(paths, row.Path)
		}
	}
	if len(paths) == 0 {
		return map[string][]codeanchor.IntelAnchor{}, nil
	}
	return store.IntelAnchorsByPaths(ctx, paths)
}

func ShouldQueryRationaleEvidence(spec search.QuerySpec) bool {
	if search.IsPrecisionIntent(spec.Intent) {
		return false
	}
	switch spec.Intent {
	case search.IntentExplainSymbol, search.IntentDocsForCode, search.IntentCodeForDocs:
		return true
	}
	termSet := map[string]bool{
		"why": true, "rationale": true, "reason": true, "decision": true, "tradeoff": true,
		"constraint": true, "invariant": true, "important": true, "explain": true,
		"workaround": true, "hack": true, "todo": true, "fixme": true,
	}
	for _, term := range rationaleQueryTerms(spec.Text) {
		if termSet[term] {
			return true
		}
	}
	return false
}

func rationaleQueryTerms(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')
	})
}

func moduleAnchorCandidate(path string, anchors []codeanchor.IntelAnchor, ev search.Evidence) search.Candidate {
	for _, a := range anchors {
		if strings.EqualFold(a.Kind, "module") {
			h := knowledge.AnchorHandle(a.AnchorID)
			return search.Candidate{
				Handle:      h,
				Owner:       h,
				Evidence:    []search.Evidence{ev},
				SupportOnly: true,
				Type:        "anchor",
				Path:        a.Path,
				Title:       a.Symbol,
				Symbol:      a.Symbol,
				FQN:         a.FQN,
				Kind:        a.Kind,
				ChunkIndex:  -1,
				AnchorID:    a.AnchorID,
			}
		}
	}
	return search.Candidate{}
}

func rationaleKindBoost(kind string) float64 {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case string(codeanchor.RationaleWhy), string(codeanchor.RationaleRationale):
		return 0.75
	case string(codeanchor.RationaleImportant):
		return 0.65
	default:
		return 0.45
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func trimRationaleOneLine(s string, limit int) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if limit <= 0 || len(s) <= limit {
		return s
	}
	trimAt := limit
	for trimAt > 0 && !utf8.RuneStart(s[trimAt]) {
		trimAt--
	}
	return s[:trimAt] + "..."
}

func firstRationaleNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
