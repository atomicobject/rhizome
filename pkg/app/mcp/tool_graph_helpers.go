package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/frontmatter"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func buildReverseNeighbors(nodes map[string]obsidian.GraphNode) map[string][]string {
	reverse := make(map[string][]string, len(nodes))
	for path := range nodes {
		reverse[path] = nil
	}
	for src, node := range nodes {
		for _, dst := range node.Neighbors {
			reverse[dst] = append(reverse[dst], src)
		}
	}
	for path := range reverse {
		sort.Strings(reverse[path])
	}
	return reverse
}

func crossCommunityEdgeCounts(nodes map[string]obsidian.GraphNode, reverse map[string][]string, membership map[string]*obsidian.CommunitySummary) map[string]int {
	counts := make(map[string]int, len(nodes))

	for src, node := range nodes {
		var srcComm string
		if comm := membership[src]; comm != nil {
			srcComm = comm.ID
		}
		for _, dst := range node.Neighbors {
			var dstComm string
			if comm := membership[dst]; comm != nil {
				dstComm = comm.ID
			}
			if srcComm != "" && dstComm != "" && srcComm != dstComm {
				counts[src]++
			}
		}
	}

	for dst, sources := range reverse {
		var dstComm string
		if comm := membership[dst]; comm != nil {
			dstComm = comm.ID
		}
		for _, src := range sources {
			var srcComm string
			if comm := membership[src]; comm != nil {
				srcComm = comm.ID
			}
			if srcComm != "" && dstComm != "" && srcComm != dstComm {
				counts[dst]++
			}
		}
	}

	return counts
}

func bridgePayloads(paths []string, counts map[string]int) []BridgePayload {
	if len(paths) == 0 {
		return nil
	}
	out := make([]BridgePayload, 0, len(paths))
	for _, p := range paths {
		out = append(out, BridgePayload{
			Path:                p,
			CrossCommunityEdges: counts[p],
		})
	}
	return out
}

func componentSummariesFromWeak(components [][]string, nodes map[string]obsidian.GraphNode, total int) []ComponentSummary {
	if len(components) == 0 {
		return nil
	}
	summaries := make([]ComponentSummary, 0, len(components))
	for idx, comp := range components {
		if len(comp) == 0 {
			summaries = append(summaries, ComponentSummary{
				ID:   fmt.Sprintf("comp%d", idx),
				Size: 0,
			})
			continue
		}
		id := nodes[comp[0]].WeakCompID
		if id == "" {
			id = fmt.Sprintf("comp%d", idx)
		}
		size := len(comp)
		summaries = append(summaries, ComponentSummary{
			ID:              id,
			Size:            size,
			FractionOfVault: fractionOfVault(size, total),
		})
	}
	return summaries
}

func fractionOfVault(size int, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(size) / float64(total)
}

func authorityScoresToPayload(scores []obsidian.AuthorityScore, limit int, config Config, noteMgr obsidian.NoteReader) []AuthorityScorePayload {
	if len(scores) == 0 {
		return nil
	}
	if limit > 0 && len(scores) > limit {
		scores = scores[:limit]
	}
	projected := authoritySourceSnapshots(scores, config, noteMgr)
	out := make([]AuthorityScorePayload, 0, len(scores))
	for _, s := range scores {
		// Authority scores originate from the legacy Markdown graph. Their
		// provenance, not a filename suffix, identifies them as notes. Formats
		// without graph projection, including descriptor-only HTML, do not enter
		// this input surface.
		kind := "note"
		payload := AuthorityScorePayload{
			Path:      s.Path,
			Authority: s.Authority,
			Hub:       s.Hub,
			Kind:      kind,
		}
		// Default behavior: surface lightweight title + blessed frontmatter
		// so agents can decide whether to fetch the full note.
		payload.Title = titleFromPath(s.Path)
		if snapshot, ok := projected[s.Path]; ok {
			if title := strings.TrimSpace(snapshot.Title); title != "" {
				payload.Title = title
			}
			payload.Frontmatter = frontmatter.FilterBlessed(snapshot.Frontmatter)
		}
		out = append(out, payload)
	}
	return out
}

func authoritySourceSnapshots(scores []obsidian.AuthorityScore, config Config, noteMgr obsidian.NoteReader) map[string]notemeta.NoteSourceSnapshot {
	store := config.GetIntelStore()
	if store == nil || noteMgr == nil || config.NoteMetadata.Validate() != nil {
		return nil
	}
	paths := make([]string, 0, len(scores))
	for _, score := range scores {
		if path := strings.TrimSpace(score.Path); path != "" {
			paths = append(paths, path)
		}
	}
	snapshots, err := config.NoteMetadata.LoadNoteSourceSnapshots(context.Background(), config.VaultDef, noteMgr, store, paths)
	if err != nil {
		return nil
	}
	out := make(map[string]notemeta.NoteSourceSnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		out[snapshot.Path.String()] = snapshot
	}
	return out
}

func authorityBucketsToPayload(buckets []obsidian.AuthorityBucket) []AuthorityBucketPayload {
	if len(buckets) == 0 {
		return nil
	}
	out := make([]AuthorityBucketPayload, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, AuthorityBucketPayload{
			Low:     b.Low,
			High:    b.High,
			Count:   b.Count,
			Example: b.Example,
		})
	}
	return out
}

func authorityStatsToPayload(stats *obsidian.AuthorityStats) *AuthorityStatsPayload {
	if stats == nil {
		return nil
	}
	return &AuthorityStatsPayload{
		Mean: stats.Mean,
		P50:  stats.P50,
		P75:  stats.P75,
		P90:  stats.P90,
		P95:  stats.P95,
		P99:  stats.P99,
		Max:  stats.Max,
	}
}

func recencyToPayload(r *obsidian.GraphRecency) *GraphRecencyPayload {
	if r == nil {
		return nil
	}
	age := r.LatestAgeDays
	if !r.LatestTimestamp.IsZero() {
		if d := time.Since(r.LatestTimestamp).Hours() / 24.0; d >= 0 {
			age = d
		}
	}
	return &GraphRecencyPayload{
		LatestPath:    r.LatestPath,
		LatestAgeDays: age,
		RecentCount:   r.RecentCount,
		WindowDays:    r.WindowDays,
	}
}
