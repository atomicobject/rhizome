package graph

import (
	"path/filepath"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// AuthorityScore captures a note and its authority/hub scores.
type AuthorityScore struct {
	Path      string
	Authority float64
	Hub       float64
}

// AuthorityBucket summarizes distribution of authority scores within a community.
type AuthorityBucket struct {
	Low     float64
	High    float64
	Count   int
	Example string
}

// GraphRecency summarizes modification recency for a group of notes.
type GraphRecency struct {
	LatestPath      string
	LatestAgeDays   float64
	LatestTimestamp time.Time `json:"-"`
	RecentCount     int
	WindowDays      int
}

// GraphTimings captures rough durations for major analysis phases.
type GraphTimings struct {
	LoadEntries time.Duration
	BuildGraph  time.Duration
	HITS        time.Duration
	LabelProp   time.Duration
	Recency     time.Duration
	Total       time.Duration
}

// GraphTimingsMillis exposes timings as milliseconds for JSON-friendly output.
type GraphTimingsMillis struct {
	LoadEntries int64 `json:"loadEntriesMs"`
	BuildGraph  int64 `json:"buildGraphMs"`
	HITS        int64 `json:"hitsMs"`
	LabelProp   int64 `json:"labelPropMs"`
	Recency     int64 `json:"recencyMs"`
	Total       int64 `json:"totalMs"`
}

// ToMillis converts durations to ms, clamping any non-zero value to at least 1ms.
func (t GraphTimings) ToMillis() GraphTimingsMillis {
	conv := func(d time.Duration) int64 {
		if d <= 0 {
			return 0
		}
		ms := d.Milliseconds()
		if ms == 0 {
			return 1
		}
		return ms
	}
	return GraphTimingsMillis{
		LoadEntries: conv(t.LoadEntries),
		BuildGraph:  conv(t.BuildGraph),
		HITS:        conv(t.HITS),
		LabelProp:   conv(t.LabelProp),
		Recency:     conv(t.Recency),
		Total:       conv(t.Total),
	}
}

// AuthorityStats captures coarse percentiles/mean for authority scores.
type AuthorityStats struct {
	Mean float64
	P50  float64
	P75  float64
	P90  float64
	P95  float64
	P99  float64
	Max  float64
}

// NodeStats captures inbound/outbound link counts for a note.
type NodeStats struct {
	Inbound  int
	Outbound int
}

// GraphStats contains degree counts and strongly connected components for a vault's wikilink graph.
type GraphStats struct {
	Nodes      map[string]NodeStats // keyed by normalized note path (with .md)
	Components [][]string           // strongly connected components (each sorted)
	Adjacency  map[string]map[string]struct{}
}

// Community represents a loosely connected cluster of notes.
type Community struct {
	ID    string
	Nodes []string
}

// CommunitySummary captures metadata for a detected community.
type CommunitySummary struct {
	ID               string
	Nodes            []string
	TopTags          []TagCount
	TopAuthority     []AuthorityScore
	AuthorityBuckets []AuthorityBucket
	AuthorityStats   *AuthorityStats
	Recency          *GraphRecency
	Anchor           string
	Density          float64
	Bridges          []string
}

// TagCount represents a tag and its count.
type TagCount struct {
	Tag   string
	Count int
}

// GraphNode captures node-centric details for analysis output.
type GraphNode struct {
	Path             string   `json:"path"`
	Title            string   `json:"title"`
	Kind             string   `json:"kind,omitempty"`
	Inbound          int      `json:"inbound"`
	Outbound         int      `json:"outbound"`
	Hub              float64  `json:"hub"`                        // HITS hub score: measures how well this note curates/aggregates links
	Authority        float64  `json:"authority"`                  // HITS authority score: measures how often this note is referenced
	ExternalRefCount int      `json:"externalRefCount,omitempty"` // Number of code references pointing to this note
	Community        string   `json:"community"`
	SCC              string   `json:"scc"`
	Neighbors        []string `json:"neighbors,omitempty"`
	Tags             []string `json:"tags,omitempty"`
	WeakCompID       string   `json:"weakComponent,omitempty"`
}

// GraphAnalysis is the result of vault-wide graph computation.
// Use ComputeGraphAnalysis to build; use RefreshGraphAnalysisDerived to recompute
// derived summaries (e.g., after authority factor adjustments) without re-scanning.
type GraphAnalysis struct {
	Nodes            map[string]GraphNode
	Communities      []CommunitySummary
	StrongComponents [][]string
	WeakComponents   [][]string
	Orphans          []string
	Stats            GraphStatsSummary
	EffectiveTimes   map[string]time.Time `json:"-"`
	Timings          GraphTimings         `json:"-"`
}

// GraphStatsSummary captures high-level totals.
type GraphStatsSummary struct {
	NodeCount int `json:"nodeCount"`
	EdgeCount int `json:"edgeCount"`
}

// GraphAnalysisOptions controls optional data.
type GraphAnalysisOptions struct {
	markdownsyntax.WikilinkOptions
	IncludeTags   bool
	ExcludedPaths map[string]struct{}
	IncludedPaths map[string]struct{}
	MinDegree     int
	MutualOnly    bool
	// RecencyCascade controls whether inferred recency is propagated beyond 1 hop.
	// When true (default), a bounded multi-pass propagation will cascade freshness.
	// When false, only direct neighbors can boost an undated note.
	RecencyCascade bool
	// RecencyCascadeSet distinguishes explicit caller choice from defaulting behavior.
	RecencyCascadeSet bool
	// IncludeDocsInGraph controls whether file_context docs (e.g., CONTEXT.md) are treated as graph notes.
	IncludeDocsInGraph bool
	// DocPatterns are filenames considered as docs when IncludeDocsInGraph is true.
	DocPatterns []string
	// DocMinBytes skips tiny stub docs when IncludeDocsInGraph is true (0 uses default).
	DocMinBytes int
}

// ComputeGraphAnalysis builds a vault-wide wikilink graph and computes derived signals:
//   - HITS hub/authority scores (who curates vs who is referenced)
//   - Communities via label propagation
//   - Recency (modification times, optionally cascaded to neighbors)
//   - Bridges (notes connecting communities)
//
// Inputs:
//   - vaultDef: vault path and excludes
//   - note: NoteReader or snapshot provider for reading content
//   - options: controls filtering (excludes/includes), doc inclusion, recency cascade
//
// Returns GraphAnalysis containing Nodes, Communities, SCCs, WeakComponents, Orphans.
// Timings are captured for performance debugging.
func ComputeGraphAnalysis(vaultDef obsidian.VaultDefinition, note obsidian.NoteReader, options GraphAnalysisOptions) (*GraphAnalysis, error) {
	loadStart := time.Now()
	snapshot, err := BuildGraphSnapshot(vaultDef, note, options)
	if err != nil {
		return nil, err
	}
	analysis := ComputeGraphAnalysisFromSnapshot(snapshot, options)
	analysis.Timings.LoadEntries = time.Since(loadStart)
	if analysis.Timings.LoadEntries == 0 {
		analysis.Timings.LoadEntries = time.Nanosecond
	}
	analysis.Timings.Total += analysis.Timings.LoadEntries
	return analysis, nil
}

// ComputeGraphAnalysisFromSnapshot runs graph algorithms over a canonical,
// source-neutral graph input.
func ComputeGraphAnalysisFromSnapshot(snapshot *GraphSnapshot, options GraphAnalysisOptions) *GraphAnalysis {
	startTotal := time.Now()
	var timings GraphTimings
	buildStart := time.Now()
	adjacency, tagMap, contentTimes := graphSnapshotAdjacency(snapshot, options, true)

	if options.MutualOnly {
		adjacency = filterMutualEdges(adjacency)
	}
	if options.MinDegree > 0 {
		adjacency = filterByMinDegree(adjacency, options.MinDegree)
	}
	timings.BuildGraph = time.Since(buildStart)

	nodes := make(map[string]NodeStats, len(adjacency))
	for node := range adjacency {
		nodes[node] = NodeStats{}
	}

	edgeCount := 0
	for src, targets := range adjacency {
		stats := nodes[src]
		stats.Outbound = len(targets)
		nodes[src] = stats
		edgeCount += len(targets)

		for dst := range targets {
			stats := nodes[dst]
			stats.Inbound++
			nodes[dst] = stats
		}
	}

	sccs := stronglyConnectedComponents(adjacency)
	sccID := assignIDs(sccs, "scc")
	weak := weakComponents(adjacency)
	weakID := assignIDs(weak, "comp")
	lpStart := time.Now()
	comms := labelPropagation(adjacency)
	timings.LabelProp = time.Since(lpStart)

	hitsStart := time.Now()
	hits := computeHITS(adjacency)
	timings.HITS = time.Since(hitsStart)

	graphNodes := make(map[string]GraphNode, len(nodes))
	for path, deg := range nodes {
		graphNodes[path] = GraphNode{
			Path:       path,
			Title:      obsidian.RemoveMdSuffix(filepath.Base(path)),
			Kind:       "note",
			Inbound:    deg.Inbound,
			Outbound:   deg.Outbound,
			Hub:        hits.Hubs[path],
			Authority:  hits.Authorities[path],
			Community:  comms[path],
			SCC:        sccID[path],
			Neighbors:  sortedKeys(adjacency[path]),
			Tags:       tagMap[path],
			WeakCompID: weakID[path],
		}
	}

	cascade := options.RecencyCascade
	if !options.RecencyCascadeSet {
		cascade = true
	}

	recencyStart := time.Now()
	effectiveTimes := applyNeighborRecency(adjacency, contentTimes, time.Now(), cascade)
	timings.Recency = time.Since(recencyStart)
	communities := summarizeCommunities(comms, graphNodes, tagMap, effectiveTimes)
	bridges := computeBridges(adjacency, graphNodes, communities)
	attachBridges(communities, bridges)

	timings.Total = time.Since(startTotal)

	// Ensure timings are non-zero to make them visible when marshaled.
	if timings.LoadEntries == 0 {
		timings.LoadEntries = 1 * time.Nanosecond
	}
	if timings.BuildGraph == 0 {
		timings.BuildGraph = 1 * time.Nanosecond
	}
	if timings.HITS == 0 {
		timings.HITS = 1 * time.Nanosecond
	}
	if timings.LabelProp == 0 {
		timings.LabelProp = 1 * time.Nanosecond
	}
	if timings.Recency == 0 {
		timings.Recency = 1 * time.Nanosecond
	}
	if timings.Total == 0 {
		timings.Total = 1 * time.Nanosecond
	}

	return &GraphAnalysis{
		Nodes:            graphNodes,
		Communities:      communities,
		StrongComponents: sccs,
		WeakComponents:   weak,
		Orphans:          (&GraphStats{Nodes: nodes}).Orphans(),
		Stats: GraphStatsSummary{
			NodeCount: len(nodes),
			EdgeCount: edgeCount,
		},
		EffectiveTimes: effectiveTimes,
		Timings:        timings,
	}
}

const (
	// neighborFreshWindow defines how far back neighbor activity can boost an undated note.
	// A neighbor older than this won't contribute to inferred recency.
	neighborFreshWindow = 180 * 24 * time.Hour

	// neighborStalenessOffset is subtracted from neighbor timestamps when inferring recency,
	// so a hub's inferred time is at least this many days older than its freshest neighbor.
	neighborStalenessOffset = 7 * 24 * time.Hour

	// neighborSampleLimit caps the number of neighbors considered for recency inference.
	neighborSampleLimit = 5

	// minSaneYear rejects dates before this year as implausible.
	minSaneYear = 1900

	// maxFutureTolerance allows dates up to this far in the future (for scheduled events).
	maxFutureTolerance = 365 * 24 * time.Hour

	// topLinesForDateDetection limits how many lines to scan for ISO dates near the top of a note.
	topLinesForDateDetection = 20

	// communityRecencyWindowDays is the window for counting "recent" notes in a community.
	// This differs from neighborFreshWindow intentionally: neighbor inference looks back 6 months
	// to boost undated hubs, while community RecentCount measures short-term activity (30 days).
	communityRecencyWindowDays = 30

	// recencyPropagationPasses bounds how many times inferred recency can cascade.
	recencyPropagationPasses = 2
)
