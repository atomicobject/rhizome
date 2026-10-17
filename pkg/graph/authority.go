package graph

import (
	"crypto/sha1"
	"encoding/hex"

	"io"
	"math"

	"sort"
	"strings"
)

func topAuthorityNodes(members []string, nodes map[string]GraphNode, limit int) []AuthorityScore {
	type pr struct {
		path      string
		authority float64
		hub       float64
	}
	var list []pr
	for _, m := range members {
		list = append(list, pr{path: m, authority: nodes[m].Authority, hub: nodes[m].Hub})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].authority == list[j].authority {
			return list[i].path < list[j].path
		}
		return list[i].authority > list[j].authority
	})
	if len(list) > limit {
		list = list[:limit]
	}
	out := make([]AuthorityScore, len(list))
	for i, item := range list {
		out[i] = AuthorityScore{
			Path:      item.path,
			Authority: item.authority,
			Hub:       item.hub,
		}
	}
	return out
}

func authorityBuckets(members []string, nodes map[string]GraphNode) ([]AuthorityBucket, *AuthorityStats) {
	if len(members) == 0 {
		return nil, nil
	}
	type pr struct {
		path string
		val  float64
	}
	values := make([]pr, 0, len(members))
	for _, m := range members {
		values = append(values, pr{path: m, val: nodes[m].Authority})
	}
	sort.Slice(values, func(i, j int) bool { return values[i].val > values[j].val })

	bucketCount := bucketCountFor(len(values))
	buckets := make([]AuthorityBucket, 0, bucketCount)
	size := int(math.Ceil(float64(len(values)) / float64(bucketCount)))
	for i := 0; i < len(values); i += size {
		end := i + size
		if end > len(values) {
			end = len(values)
		}
		segment := values[i:end]
		high := segment[0].val
		low := segment[len(segment)-1].val
		example := segment[0].path
		buckets = append(buckets, AuthorityBucket{
			Low:     low,
			High:    high,
			Count:   len(segment),
			Example: example,
		})
	}

	// Convert to simple slice for stats
	vals := make([]struct {
		path string
		val  float64
	}, len(values))
	for i, v := range values {
		vals[i] = struct {
			path string
			val  float64
		}{path: v.path, val: v.val}
	}
	stats := authorityStatsFromValues(vals)
	return buckets, stats
}

func bucketCountFor(size int) int {
	if size <= 0 {
		return 0
	}
	c := int(math.Ceil(math.Sqrt(float64(size))))
	if c < 5 {
		c = 5
	}
	if c > 10 {
		c = 10
	}
	return c
}

func authorityStatsFromValues(values []struct {
	path string
	val  float64
}) *AuthorityStats {
	if len(values) == 0 {
		return nil
	}
	vals := make([]float64, len(values))
	sum := 0.0
	for i, v := range values {
		vals[i] = v.val
		sum += v.val
	}
	mean := sum / float64(len(vals))
	sort.Float64s(vals) // ascending
	p := func(q float64) float64 {
		if len(vals) == 1 {
			return vals[0]
		}
		idx := int(math.Ceil(q*float64(len(vals)))) - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(vals) {
			idx = len(vals) - 1
		}
		return vals[idx]
	}
	return &AuthorityStats{
		Mean: mean,
		P50:  p(0.50),
		P75:  p(0.75),
		P90:  p(0.90),
		P95:  p(0.95),
		P99:  p(0.99),
		Max:  vals[len(vals)-1],
	}
}

func anchorForCommunity(members []string, nodes map[string]GraphNode) string {
	if len(members) == 0 {
		return ""
	}
	type pr struct {
		path string
		val  float64
	}
	var list []pr
	for _, m := range members {
		list = append(list, pr{path: m, val: nodes[m].Authority})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].val == list[j].val {
			return list[i].path < list[j].path
		}
		return list[i].val > list[j].val
	})
	return list[0].path
}

func density(members []string, nodes map[string]GraphNode) float64 {
	if len(members) < 2 {
		return 0
	}
	// Approximate density using average degree within the community / (n-1)
	// (We don't keep intra-community edge counts here; this is a simple heuristic.)
	var totalDegree int
	for _, m := range members {
		totalDegree += nodes[m].Inbound + nodes[m].Outbound
	}
	n := float64(len(members))
	return (float64(totalDegree) / n) / (n - 1)
}

func communityID(label, anchor string, members []string) string {
	h := sha1.New()
	io.WriteString(h, label)
	io.WriteString(h, "|")
	io.WriteString(h, anchor)
	io.WriteString(h, "|")
	for _, m := range members {
		io.WriteString(h, m)
		io.WriteString(h, ";")
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if len(sum) > 8 {
		sum = sum[:8]
	}
	return "c" + sum
}

func topTagsForCommunity(members []string, tags map[string][]string, limit int) []TagCount {
	counts := make(map[string]int)
	for _, m := range members {
		for _, t := range tags[m] {
			counts[strings.ToLower(t)]++
		}
	}
	type kv struct {
		tag   string
		count int
	}
	var list []kv
	for tag, count := range counts {
		list = append(list, kv{tag: tag, count: count})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].count == list[j].count {
			return list[i].tag < list[j].tag
		}
		return list[i].count > list[j].count
	})
	if len(list) > limit {
		list = list[:limit]
	}
	out := make([]TagCount, len(list))
	for i, item := range list {
		out[i] = TagCount{Tag: item.tag, Count: item.count}
	}
	return out
}
