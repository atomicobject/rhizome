package retrieval

import (
	"context"
	"path/filepath"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
)

// DefinitionRetriever resolves symbol/file inputs to definition anchors.
// Intended for "go_to_def" style intents where precision matters.
type DefinitionRetriever struct {
	Store     *semdb.Store
	VaultPath string
	Limit     int
}

func (r *DefinitionRetriever) Name() string { return "definition" }

func (r *DefinitionRetriever) Retrieve(ctx context.Context, spec search.QuerySpec) ([]search.Candidate, error) {
	if r.Store == nil {
		return nil, nil
	}
	limit := r.Limit
	if limit <= 0 {
		limit = 12
	}

	var (
		anchorSeeds []string
		fileSeeds   []string
	)

	for _, seed := range spec.Seeds {
		switch seed.Kind {
		case knowledge.KindAnchor:
			if id := strings.TrimSpace(seed.ID); id != "" {
				anchorSeeds = append(anchorSeeds, id)
			}
		case knowledge.KindFile:
			if p := strings.TrimSpace(seed.ID); p != "" {
				fileSeeds = append(fileSeeds, filepath.ToSlash(p))
			}
		}
	}

	anchorsByID := map[string]codeanchor.IntelAnchor{}
	if len(anchorSeeds) > 0 {
		found, err := r.Store.IntelAnchorsByIDs(ctx, anchorSeeds)
		if err != nil {
			return nil, err
		}
		anchorsByID = found
	}
	anchorSeedPaths := make(map[string]struct{}, len(anchorsByID))
	for _, anchor := range anchorsByID {
		if path := search.NormalizeLocalityPath(anchor.Path); path != "" {
			anchorSeedPaths[path] = struct{}{}
		}
	}

	var (
		seedFQNs    []string
		seedSymbols []string
	)

	out := make([]search.Candidate, 0, limit)
	seen := make(map[string]struct{}, limit)

	addCandidate := func(a codeanchor.IntelAnchor, evType, seed string) {
		if a.AnchorID == "" {
			return
		}
		h := knowledge.AnchorHandle(a.AnchorID)
		key := h.String()
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, search.Candidate{
			Handle: h,
			Owner:  h,
			Evidence: []search.Evidence{{
				Type:     evType,
				RawScore: 1.1,
				Source:   "definition",
				Details: map[string]string{
					"seed": seed,
				},
			}},
			Type:       "anchor",
			Path:       a.Path,
			Title:      a.Symbol,
			Symbol:     a.Symbol,
			FQN:        a.FQN,
			Kind:       a.Kind,
			ChunkIndex: -1,
			AnchorID:   a.AnchorID,
		})
	}

	for _, id := range anchorSeeds {
		a, ok := anchorsByID[id]
		if !ok {
			continue
		}
		addCandidate(a, "definition_anchor", id)
		if fqn := strings.TrimSpace(a.FQN); fqn != "" {
			seedFQNs = append(seedFQNs, fqn)
		}
		if sym := strings.TrimSpace(a.Symbol); sym != "" {
			seedSymbols = append(seedSymbols, sym)
		}
		if len(out) >= limit {
			return out, nil
		}
	}

	vaultPaths, _ := paths.NewVaultPaths(r.VaultPath)
	for _, path := range fileSeeds {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if vaultPaths.Root() != "" && filepath.IsAbs(path) {
			if rel, err := vaultPaths.RelCodeStrict(path); err == nil && rel.String() != "" {
				path = rel.String()
			}
		}
		evidenceType := "definition_anchor"
		if _, containsSelectedSymbol := anchorSeedPaths[search.NormalizeLocalityPath(path)]; containsSelectedSymbol {
			// Target resolution adds the containing file beside an inferred symbol.
			// Its module anchor is useful locality, but it is not a second
			// definition of the selected symbol.
			evidenceType = "code_anchor"
		}
		if modID, ok, err := r.Store.IntelModuleAnchorIDByPath(ctx, "", path); err == nil && ok && modID != "" {
			if a, ok := anchorsByID[modID]; ok {
				addCandidate(a, evidenceType, path)
			} else if found, err := r.Store.IntelAnchorsByIDs(ctx, []string{modID}); err == nil {
				if anchor, ok := found[modID]; ok {
					addCandidate(anchor, evidenceType, path)
				}
			}
		}
		if len(out) >= limit {
			return out, nil
		}
	}

	fqns, symbols := extractSymbolTokens(spec.Text)
	seedFQNs = append(seedFQNs, fqns...)
	seedSymbols = append(seedSymbols, symbols...)

	seedFQNs = dedupeStrings(seedFQNs)
	seedSymbols = dedupeStrings(seedSymbols)

	if len(seedFQNs) > 0 {
		found, err := r.Store.IntelAnchorsByFQNsLimited(ctx, seedFQNs, 3)
		if err != nil {
			return nil, err
		}
		for _, fqn := range seedFQNs {
			for _, anchor := range found[fqn] {
				addCandidate(anchor, "definition_anchor", fqn)
				if len(out) >= limit {
					return out, nil
				}
			}
		}
	}

	if len(seedSymbols) > 0 && len(out) < limit {
		for _, sym := range seedSymbols {
			if len(out) >= limit {
				break
			}
			anchors, err := r.Store.IntelAnchorsBySymbol(ctx, sym, 5)
			if err != nil {
				return nil, err
			}
			for _, anchor := range anchors {
				addCandidate(anchor, "definition_anchor", sym)
				if len(out) >= limit {
					break
				}
			}
		}
	}

	return out, nil
}
