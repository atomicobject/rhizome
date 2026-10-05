// Docs: [Indexing pipeline - Graph signals (doc scores + anchor PageRank)](docs/reference/analysis/Indexing pipeline - Graph signals (doc scores + anchor PageRank).md)
package graphdb

import (
	"context"
	"strings"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/graphalg"
)

// ComputeAnchorPageRank builds a call graph and computes PageRank over anchors + caller files.
//
// Graph construction:
// - Nodes: anchor_id (definitions)
// - Edges: caller anchor -> callee anchor for each intel_edges(kind='calls')
//
// The resulting PageRank scores are persisted via Store.ReplaceAnchorScores and used by
// GraphAnchorScoreRanker to boost frequently-called anchors in search results.
//
// See [Indexing pipeline - Graph signals (doc scores + anchor PageRank)](docs/reference/analysis/Indexing pipeline - Graph signals (doc scores + anchor PageRank).md) for design details.
func ComputeAnchorPageRank(ctx context.Context, store *semdb.Store) ([]semdb.AnchorScore, error) {
	if store == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	anchors, err := store.IntelAnchors(ctx)
	if err != nil {
		return nil, err
	}
	if len(anchors) == 0 {
		return nil, nil
	}

	anchorSet := make(map[string]struct{}, len(anchors))
	for _, a := range anchors {
		anchorSet[a.AnchorID] = struct{}{}
	}

	rows, err := store.DB().QueryContext(ctx, `
		SELECT src.anchor_id, dst.anchor_id
		FROM intel_edges e
		JOIN intel_code_anchors src ON src.id = e.src_row_id
		JOIN intel_code_anchors dst ON dst.id = e.dst_row_id
		WHERE e.kind = 'calls'
		  AND e.src_type = 'anchor'
		  AND e.dst_type = 'anchor'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	adj := make(map[string]map[string]struct{})
	ensure := func(node string) {
		if _, ok := adj[node]; !ok {
			adj[node] = make(map[string]struct{})
		}
	}
	for rows.Next() {
		var srcID, dstID string
		if scanErr := rows.Scan(&srcID, &dstID); scanErr != nil {
			return nil, scanErr
		}
		if strings.TrimSpace(srcID) == "" || strings.TrimSpace(dstID) == "" {
			continue
		}
		if _, ok := anchorSet[srcID]; !ok {
			continue
		}
		if _, ok := anchorSet[dstID]; !ok {
			continue
		}
		ensure(srcID)
		ensure(dstID)
		adj[srcID][dstID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	ranks := graphalg.PageRank(adj)
	ts := time.Now().Unix()
	out := make([]semdb.AnchorScore, 0, len(ranks))
	for node, pr := range ranks {
		out = append(out, semdb.AnchorScore{
			AnchorID: node,
			PageRank: pr,
			Updated:  ts,
		})
	}
	return out, nil
}
