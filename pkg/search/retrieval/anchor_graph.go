package retrieval

import (
	"context"
	"fmt"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
)

// AnchorGraphRetriever expands anchor seeds via call graph and inheritance edges (code -> code).
// It is intentionally shallow and capped to avoid fanout explosions.
type AnchorGraphRetriever struct {
	Store *semdb.Store

	MaxAnchors int
}

func (r *AnchorGraphRetriever) Name() string { return "anchor_graph" }

func (r *AnchorGraphRetriever) Retrieve(ctx context.Context, spec search.QuerySpec) ([]search.Candidate, error) {
	if r.Store == nil || len(spec.Seeds) == 0 {
		return nil, nil
	}
	limit := r.MaxAnchors
	if limit <= 0 {
		limit = 50
	}

	type anchorMeta struct {
		id     string
		symbol string
		fqn    string
		path   string
		kind   string
	}

	var seedIDs []string
	for _, seed := range spec.Seeds {
		if seed.Kind != knowledge.KindAnchor {
			continue
		}
		id := strings.TrimSpace(seed.ID)
		if id == "" {
			continue
		}
		seedIDs = append(seedIDs, id)
	}
	if len(seedIDs) == 0 {
		return nil, nil
	}
	seedAnchors, err := r.Store.IntelAnchorsByIDs(ctx, seedIDs)
	if err != nil {
		return nil, err
	}
	toExpand := make([]anchorMeta, 0, len(seedIDs))
	for _, id := range seedIDs {
		if a, ok := seedAnchors[id]; ok {
			toExpand = append(toExpand, anchorMeta{id: id, symbol: a.Symbol, fqn: a.FQN, path: a.Path, kind: a.Kind})
		}
	}
	if len(toExpand) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{}, len(toExpand))
	var out []search.Candidate

	add := func(meta anchorMeta, score float64, reason string) {
		key := meta.id
		if key == "" {
			key = meta.fqn
		}
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		h := knowledge.AnchorHandle(meta.id)
		out = append(out, search.Candidate{
			Handle: h,
			Owner:  h,
			Evidence: []search.Evidence{{
				Type:     "anchor_graph_edge",
				RawScore: score,
				Source:   "anchor_graph",
				Details:  map[string]string{"reason": reason},
			}},
			Type:       "anchor",
			Path:       meta.path,
			Symbol:     meta.symbol,
			FQN:        meta.fqn,
			Kind:       meta.kind,
			ChunkIndex: -1,
			AnchorID:   meta.id,
		})
	}

	callFiles := make([]string, 0, len(toExpand))
	callFileSeen := make(map[string]struct{}, len(toExpand))
	seedFQNs := make([]string, 0, len(toExpand))
	seedFQNSeen := make(map[string]struct{}, len(toExpand))
	for _, meta := range toExpand {
		if path := strings.TrimSpace(meta.path); path != "" {
			if _, ok := callFileSeen[path]; !ok {
				callFileSeen[path] = struct{}{}
				callFiles = append(callFiles, path)
			}
		}
		if fqn := strings.TrimSpace(meta.fqn); fqn != "" {
			if _, ok := seedFQNSeen[fqn]; !ok {
				seedFQNSeen[fqn] = struct{}{}
				seedFQNs = append(seedFQNs, fqn)
			}
		}
	}

	callAnchorsByPath := map[string][]codeanchor.IntelAnchor{}
	if len(callFiles) > 0 {
		anchors, err := r.Store.CallAnchorsByPaths(ctx, callFiles, limit, 3)
		if err != nil {
			return nil, fmt.Errorf("call anchors for files: %w", err)
		}
		callAnchorsByPath = anchors
	}

	parentsBySeed := make(map[string][]string, len(seedFQNs))
	parentFQNs := make([]string, 0, len(seedFQNs))
	parentSeen := make(map[string]struct{})
	for _, fqn := range seedFQNs {
		parents, _ := r.Store.Ancestors(ctx, fqn)
		if len(parents) == 0 {
			continue
		}
		parentsBySeed[fqn] = parents
		for _, parent := range parents {
			parent = strings.TrimSpace(parent)
			if parent == "" {
				continue
			}
			if _, ok := parentSeen[parent]; ok {
				continue
			}
			parentSeen[parent] = struct{}{}
			parentFQNs = append(parentFQNs, parent)
		}
	}

	childrenBySeed := map[string][]string{}
	if len(seedFQNs) > 0 {
		children, _ := r.Store.ChildrenBatch(ctx, seedFQNs)
		childrenBySeed = children
	}
	childFQNs := make([]string, 0, len(seedFQNs))
	childSeen := make(map[string]struct{})
	for _, children := range childrenBySeed {
		for _, child := range children {
			child = strings.TrimSpace(child)
			if child == "" {
				continue
			}
			if _, ok := childSeen[child]; ok {
				continue
			}
			childSeen[child] = struct{}{}
			childFQNs = append(childFQNs, child)
		}
	}

	relatedAnchorsByFQN := map[string][]codeanchor.IntelAnchor{}
	relatedFQNs := append(parentFQNs, childFQNs...)
	if len(relatedFQNs) > 0 {
		anchorsByFQN, err := r.Store.IntelAnchorsByFQNsLimited(ctx, relatedFQNs, 2)
		if err != nil {
			return nil, err
		}
		relatedAnchorsByFQN = anchorsByFQN
	}

	for _, meta := range toExpand {
		if len(out) >= limit {
			break
		}
		callFile := strings.TrimSpace(meta.path)
		if callFile != "" {
			for _, anchor := range callAnchorsByPath[callFile] {
				if len(out) >= limit {
					break
				}
				add(anchorMeta{
					id:     anchor.AnchorID,
					symbol: anchor.Symbol,
					fqn:    anchor.FQN,
					path:   anchor.Path,
					kind:   anchor.Kind,
				}, 0.8, "callee")
			}
		}

		if meta.fqn == "" {
			continue
		}

		for _, parent := range parentsBySeed[meta.fqn] {
			if len(out) >= limit {
				break
			}
			for _, anchor := range relatedAnchorsByFQN[parent] {
				add(anchorMeta{
					id:     anchor.AnchorID,
					symbol: anchor.Symbol,
					fqn:    anchor.FQN,
					path:   anchor.Path,
					kind:   anchor.Kind,
				}, 0.6, "supertype")
				if len(out) >= limit {
					break
				}
			}
		}

		for _, child := range childrenBySeed[meta.fqn] {
			if len(out) >= limit {
				break
			}
			for _, anchor := range relatedAnchorsByFQN[child] {
				add(anchorMeta{
					id:     anchor.AnchorID,
					symbol: anchor.Symbol,
					fqn:    anchor.FQN,
					path:   anchor.Path,
					kind:   anchor.Kind,
				}, 0.6, "subtype")
				if len(out) >= limit {
					break
				}
			}
		}
	}
	return out, nil
}
