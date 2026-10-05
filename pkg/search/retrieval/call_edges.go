package retrieval

import (
	"context"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
)

type CallEdgeMode int

const (
	CallEdgesCallers CallEdgeMode = iota
	CallEdgesCallees
	CallEdgesBoth
)

// CallEdgesRetriever expands call graph edges for caller/callee intents.
type CallEdgesRetriever struct {
	Store *semdb.Store
	Mode  CallEdgeMode

	Limit        int
	PerSeedLimit int
}

func (r *CallEdgesRetriever) Name() string { return "call_edges" }

func (r *CallEdgesRetriever) Retrieve(ctx context.Context, spec search.QuerySpec) ([]search.Candidate, error) {
	if r.Store == nil {
		return nil, nil
	}
	if len(spec.Seeds) == 0 && strings.TrimSpace(spec.Text) == "" {
		return nil, nil
	}
	limit := r.Limit
	if limit <= 0 {
		limit = 25
	}
	perSeed := r.PerSeedLimit
	if perSeed <= 0 {
		perSeed = 8
	}

	fileSeeds := make([]string, 0, len(spec.Seeds))
	anchorSeeds := make([]string, 0, len(spec.Seeds))
	for _, seed := range spec.Seeds {
		switch seed.Kind {
		case knowledge.KindFile:
			if p := strings.TrimSpace(seed.ID); p != "" {
				fileSeeds = append(fileSeeds, string(paths.NormalizeCode(p)))
			}
		case knowledge.KindAnchor:
			if id := strings.TrimSpace(seed.ID); id != "" {
				anchorSeeds = append(anchorSeeds, id)
			}
		}
	}
	fileSeeds = dedupeStrings(fileSeeds)
	anchorSeeds = dedupeStrings(anchorSeeds)
	resolvedFQN := ""
	if spec.ResolvedTarget != nil {
		resolvedFQN = strings.TrimSpace(spec.ResolvedTarget.FQN)
	}

	anchorsByID := map[string]codeanchor.IntelAnchor{}
	if len(anchorSeeds) > 0 {
		found, err := r.Store.IntelAnchorsByIDs(ctx, anchorSeeds)
		if err != nil {
			return nil, err
		}
		anchorsByID = found
	}

	fqnSeeds := make([]string, 0, len(anchorSeeds)+len(fileSeeds))
	filePaths := append([]string{}, fileSeeds...)
	callerIDs := make([]string, 0, len(anchorSeeds))
	if resolvedFQN != "" {
		// Target resolution is authoritative for relationship intents. File and
		// anchor seeds may describe useful context, but must not broaden an exact
		// symbol request to sibling symbols from the same file.
		fqnSeeds = append(fqnSeeds, resolvedFQN)
	} else {
		for _, id := range anchorSeeds {
			a, ok := anchorsByID[id]
			if !ok {
				continue
			}
			if f := strings.TrimSpace(a.FQN); f != "" {
				fqnSeeds = append(fqnSeeds, f)
			}
			callerIDs = append(callerIDs, a.AnchorID)
		}
	}
	filePaths = dedupeStrings(filePaths)
	callerIDs = dedupeStrings(callerIDs)

	if resolvedFQN == "" && len(filePaths) > 0 {
		for _, path := range filePaths {
			if path == "" {
				continue
			}
			fqns, err := r.Store.SymbolsByFile(ctx, path)
			if err != nil {
				continue
			}
			fqnSeeds = append(fqnSeeds, fqns...)
		}
	}

	var textFQNs, textSymbols []string
	if resolvedFQN == "" {
		textFQNs, textSymbols = extractSymbolTokens(spec.Text)
		fqnSeeds = append(fqnSeeds, textFQNs...)
	}
	fqnSeeds = dedupeStrings(fqnSeeds)

	out := make([]search.Candidate, 0, limit)
	seen := make(map[string]struct{}, limit)

	addAnchor := func(anchor codeanchor.IntelAnchor, seed, direction string) {
		if anchor.AnchorID == "" {
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
			Evidence: []search.Evidence{{
				Type:     "call_edge",
				RawScore: 1.0,
				Source:   "call_edges",
				Details: map[string]string{
					"seed":      seed,
					"direction": direction,
				},
			}},
			Type:       "anchor",
			Path:       anchor.Path,
			Title:      anchor.Symbol,
			Symbol:     anchor.Symbol,
			FQN:        anchor.FQN,
			Kind:       anchor.Kind,
			ChunkIndex: -1,
			AnchorID:   anchor.AnchorID,
		})
	}

	if r.Mode == CallEdgesCallees || r.Mode == CallEdgesBoth {
		if len(callerIDs) == 0 && len(fqnSeeds) == 0 && len(textSymbols) > 0 {
			for _, sym := range textSymbols {
				anchors, err := r.Store.IntelAnchorsBySymbol(ctx, sym, perSeed)
				if err != nil {
					return nil, err
				}
				for _, anchor := range anchors {
					if f := strings.TrimSpace(anchor.FQN); f != "" {
						fqnSeeds = append(fqnSeeds, f)
					}
					if anchor.AnchorID != "" {
						callerIDs = append(callerIDs, anchor.AnchorID)
					}
				}
			}
			fqnSeeds = dedupeStrings(fqnSeeds)
			callerIDs = dedupeStrings(callerIDs)
		}
		if len(fqnSeeds) > 0 {
			ids := make([]string, 0, len(fqnSeeds))
			for _, fqn := range fqnSeeds {
				found, err := r.Store.IntelAnchorIDsByFQN(ctx, fqn, 4)
				if err != nil {
					return nil, err
				}
				ids = append(ids, found...)
			}
			if len(ids) > 0 {
				callerIDs = append(callerIDs, ids...)
				callerIDs = dedupeStrings(callerIDs)
			}
		}
		if len(callerIDs) > 0 {
			callees, err := r.Store.CallAnchorsByCallerIDs(ctx, callerIDs, perSeed, 3)
			if err != nil {
				return nil, err
			}
			for round := 0; ; round++ {
				added := false
				for _, callerID := range callerIDs {
					anchors := callees[callerID]
					if round >= len(anchors) {
						continue
					}
					addAnchor(anchors[round], callerID, "callee")
					added = true
					if len(out) >= limit {
						return out, nil
					}
				}
				if !added {
					break
				}
			}
		} else if len(filePaths) > 0 {
			callees, err := r.Store.CallAnchorsByPaths(ctx, filePaths, perSeed, 3)
			if err != nil {
				return nil, err
			}
			for round := 0; ; round++ {
				added := false
				for _, seed := range filePaths {
					anchors := callees[seed]
					if round >= len(anchors) {
						continue
					}
					addAnchor(anchors[round], seed, "callee")
					added = true
					if len(out) >= limit {
						return out, nil
					}
				}
				if !added {
					break
				}
			}
		}
	}

	if r.Mode == CallEdgesCallers || r.Mode == CallEdgesBoth {
		if len(fqnSeeds) == 0 && len(textSymbols) > 0 {
			for _, sym := range textSymbols {
				anchors, err := r.Store.IntelAnchorsBySymbol(ctx, sym, perSeed)
				if err != nil {
					return nil, err
				}
				for _, a := range anchors {
					if f := strings.TrimSpace(a.FQN); f != "" {
						fqnSeeds = append(fqnSeeds, f)
					}
				}
			}
		}
		fqnSeeds = dedupeStrings(fqnSeeds)
		calleeIDs := make([]string, 0, len(fqnSeeds))
		for _, fqn := range fqnSeeds {
			ids, err := r.Store.IntelAnchorIDsByFQN(ctx, fqn, 4)
			if err != nil {
				return nil, err
			}
			calleeIDs = append(calleeIDs, ids...)
		}
		calleeIDs = dedupeStrings(calleeIDs)
		if len(calleeIDs) > 0 {
			callers, err := r.Store.CallerAnchorsByCalleeIDs(ctx, calleeIDs, perSeed)
			if err != nil {
				return nil, err
			}
			for round := 0; ; round++ {
				added := false
				for _, calleeID := range calleeIDs {
					anchors := callers[calleeID]
					if round >= len(anchors) {
						continue
					}
					addAnchor(anchors[round], calleeID, "caller")
					added = true
					if len(out) >= limit {
						return out, nil
					}
				}
				if !added {
					break
				}
			}
		}
	}

	return out, nil
}
