package graph

import (
	"sort"

	"time"
)

func summarizeCommunities(labels map[string]string, nodes map[string]GraphNode, tags map[string][]string, modTimes map[string]time.Time) []CommunitySummary {
	grouped := make(map[string][]string)
	for node, label := range labels {
		grouped[label] = append(grouped[label], node)
	}

	var summaries []CommunitySummary
	for id, members := range grouped {
		sort.Strings(members)
		topAuth := topAuthorityNodes(members, nodes, 5)
		topTags := topTagsForCommunity(members, tags, 5)
		anchor := anchorForCommunity(members, nodes)
		buckets, stats := authorityBuckets(members, nodes)
		recency := communityRecency(members, modTimes, communityRecencyWindowDays)
		summaries = append(summaries, CommunitySummary{
			ID:               communityID(id, anchor, members),
			Nodes:            members,
			TopAuthority:     topAuth,
			AuthorityBuckets: buckets,
			AuthorityStats:   stats,
			Recency:          recency,
			TopTags:          topTags,
			Anchor:           anchor,
			Density:          density(members, nodes),
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		ri := summaries[i].Recency
		rj := summaries[j].Recency

		// Prefer more recently updated communities (smaller age).
		if ri != nil && rj != nil && ri.LatestAgeDays != rj.LatestAgeDays {
			return ri.LatestAgeDays < rj.LatestAgeDays
		}
		if ri != nil && rj == nil {
			return true
		}
		if ri == nil && rj != nil {
			return false
		}

		// Fallback: larger communities first.
		if len(summaries[i].Nodes) == len(summaries[j].Nodes) {
			return summaries[i].ID < summaries[j].ID
		}
		return len(summaries[i].Nodes) > len(summaries[j].Nodes)
	})
	return summaries
}

// CommunityMembershipLookup returns a map from note path to its community summary for quick lookups.
func CommunityMembershipLookup(communities []CommunitySummary) map[string]*CommunitySummary {
	lookup := make(map[string]*CommunitySummary)
	for i := range communities {
		comm := &communities[i]
		for _, node := range comm.Nodes {
			lookup[node] = comm
		}
	}
	return lookup
}

// CommunityInternalEdges counts directed edges whose endpoints both live in the community.
func CommunityInternalEdges(comm *CommunitySummary, nodes map[string]GraphNode) int {
	if comm == nil {
		return 0
	}
	memberSet := make(map[string]struct{}, len(comm.Nodes))
	for _, n := range comm.Nodes {
		memberSet[n] = struct{}{}
	}

	edgeCount := 0
	for _, n := range comm.Nodes {
		node := nodes[n]
		for _, neigh := range node.Neighbors {
			if _, ok := memberSet[neigh]; ok {
				edgeCount++
			}
		}
	}
	return edgeCount
}
