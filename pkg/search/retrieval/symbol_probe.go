package retrieval

import (
	"context"
	"strings"
	"unicode"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
)

// SymbolProbeRetriever adds cheap exact-symbol recall to broad text searches.
// It is intentionally conservative: dotted/FQN tokens are probed directly, while
// plain lowercase words are ignored to avoid turning concept queries into noisy
// symbol lookups.
type SymbolProbeRetriever struct {
	Store *semdb.Store
	Limit int
}

func (r *SymbolProbeRetriever) Name() string { return "symbol_probe" }

func (r *SymbolProbeRetriever) Retrieve(ctx context.Context, spec search.QuerySpec) ([]search.Candidate, error) {
	if r.Store == nil || strings.TrimSpace(spec.Text) == "" {
		return nil, nil
	}
	limit := r.Limit
	if limit <= 0 {
		limit = 12
	}

	fqns, symbols := extractSymbolTokens(spec.Text)
	symbols = conservativeSymbolTokens(symbols)
	if len(fqns) == 0 && len(symbols) == 0 {
		return nil, nil
	}

	out := make([]search.Candidate, 0, limit)
	seen := make(map[string]struct{}, limit)
	add := func(anchor codeanchor.IntelAnchor, evType string, rawScore float64, seed string) {
		if anchor.AnchorID == "" || len(out) >= limit {
			return
		}
		h := knowledge.AnchorHandle(anchor.AnchorID)
		key := h.String()
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, search.Candidate{
			Handle: h,
			Owner:  h,
			Evidence: []search.Evidence{
				{
					Type:     evType,
					RawScore: rawScore,
					Source:   "symbol_probe",
					Details: map[string]string{
						"seed": seed,
					},
				},
				{
					Type:     "query_specificity",
					RawScore: 1.0,
					Source:   "symbol_probe",
					Details: map[string]string{
						"seed": seed,
					},
				},
			},
			Type:        "code",
			Path:        anchor.Path,
			Title:       anchor.Symbol,
			Symbol:      anchor.Symbol,
			FQN:         anchor.FQN,
			Kind:        anchor.Kind,
			Granularity: "symbol",
			ChunkIndex:  -1,
			AnchorID:    anchor.AnchorID,
		})
	}

	if len(fqns) > 0 {
		found, err := r.Store.IntelAnchorsByFQNsLimited(ctx, fqns, 3)
		if err != nil {
			return nil, err
		}
		for _, fqn := range fqns {
			for _, anchor := range found[fqn] {
				add(anchor, "symbol_exact", 1.0, fqn)
			}
		}
	}

	for _, sym := range symbols {
		if len(out) >= limit {
			break
		}
		anchors, err := r.Store.IntelAnchorsBySymbol(ctx, sym, 5)
		if err != nil {
			return nil, err
		}
		for _, anchor := range anchors {
			add(anchor, "symbol_match", 0.9, sym)
		}
	}

	return out, nil
}

func conservativeSymbolTokens(tokens []string) []string {
	out := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		if tok == "" || !looksLikeCodeSymbol(tok) {
			continue
		}
		out = append(out, tok)
	}
	return dedupeStrings(out)
}

func looksLikeCodeSymbol(tok string) bool {
	hasLetter := false
	prevWasLetter := false
	for i, r := range tok {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
			if i > 0 && unicode.IsUpper(r) && prevWasLetter {
				return true
			}
			prevWasLetter = true
		case unicode.IsDigit(r):
			return hasLetter
		default:
			prevWasLetter = false
		}
		if r == '_' {
			return true
		}
	}
	return false
}
