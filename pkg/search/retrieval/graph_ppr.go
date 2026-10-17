package retrieval

import (
	"context"
	"sort"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/graph/algorithms"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/search"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

type graphDocEdgeStore interface {
	GraphDocEdgesForPaths(ctx context.Context, paths []string, limit int, includeCalls bool) ([]semdb.GraphDocEdge, error)
	GraphDocEdgesWithConfidenceForPathsLimit(ctx context.Context, paths []string, limit int) ([]semdb.GraphDocEdge, error)
	IntelAnchorByID(ctx context.Context, anchorID string) (codeanchor.IntelAnchor, bool, error)
}

type GraphFactsProvider interface {
	GraphFacts(ctx context.Context, req noderead.GraphFactsRequest) (noderead.GraphFactsResult, error)
}

type graphNodeMeta struct {
	Endpoint noderead.GraphEndpoint
}

type graphSeedNodes map[string]graphNodeMeta

type edgeKey struct {
	src  string
	dst  string
	kind string
}

// GraphDiffuser is a pluggable diffusion algorithm over a doc graph.
type GraphDiffuser interface {
	Diffuse(adjacency map[string]map[string]float64, seeds map[string]float64) map[string]float64
}

// PPRDiffuser implements GraphDiffuser via graphalg.PersonalizedPageRank.
type PPRDiffuser struct {
	Options algorithms.PPROptions
}

func (d PPRDiffuser) Diffuse(adjacency map[string]map[string]float64, seeds map[string]float64) map[string]float64 {
	return algorithms.PersonalizedPageRank(adjacency, seeds, d.Options)
}

// GraphPPROptions controls GraphPPRRetriever behavior.
type GraphPPROptions struct {
	// Depth controls how many BFS hops to include in the induced subgraph.
	Depth int
	// PerNodeEdgeLimit caps edges fetched per expanded node.
	PerNodeEdgeLimit int
	// MaxNodes caps the number of nodes included in the induced subgraph.
	MaxNodes int
	// MaxEdges caps the number of edges included in the induced subgraph.
	MaxEdges int
	// ReturnLimit caps the number of candidates returned.
	ReturnLimit int
	// MinScore drops candidates below this normalized score (0..1).
	MinScore float64
	// IncludeCalls includes code->code call edges in the subgraph (can be noisy).
	IncludeCalls bool
	// IncludeSeedNodes emits seed doc nodes (notes/files) as candidates. This is useful
	// when seeds come from anchors/chunks and the owning file/note itself did not hit
	// in the first-stage retrieval.
	IncludeSeedNodes bool
	// MaxNotes and MaxFiles enforce a simple type budget to keep diffusion results
	// balanced for context packing. Zero means "no cap" for that type.
	MaxNotes int
	MaxFiles int
}

func defaultGraphPPROptions() GraphPPROptions {
	return GraphPPROptions{
		Depth:            2,
		PerNodeEdgeLimit: 250,
		MaxNodes:         800,
		MaxEdges:         4000,
		ReturnLimit:      25,
		MinScore:         0.05,
		IncludeCalls:     true,
		IncludeSeedNodes: true,
		// Leave MaxNotes/MaxFiles uncapped by default. The caller-facing packer
		// should decide how much breadth to include based on budget.
		MaxNotes: 0,
		MaxFiles: 0,
	}
}

// GraphPPRRetriever expands doc candidates by running query-time diffusion (personalized PageRank)
// on a bounded induced doc graph constructed from the intel store.
//
// It is designed to run in the AutoExpand stage: spec.Seeds are treated as restart seeds,
// and additional note/file candidates are surfaced even if they did not match text/vector retrieval.
type GraphPPRRetriever struct {
	Store      graphDocEdgeStore
	GraphFacts GraphFactsProvider
	// VaultPath is used only for stable title formatting; it can be empty.
	VaultPath string

	Options  GraphPPROptions
	Diffuser GraphDiffuser
}

func (r *GraphPPRRetriever) Name() string { return "graph_ppr" }

func (r *GraphPPRRetriever) Retrieve(ctx context.Context, spec search.QuerySpec) ([]search.Candidate, error) {
	if r.Store == nil && r.GraphFacts == nil || len(spec.Seeds) == 0 {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	opts := r.Options
	if opts == (GraphPPROptions{}) {
		opts = defaultGraphPPROptions()
	}
	if opts.Depth <= 0 {
		return nil, nil
	}
	if opts.PerNodeEdgeLimit <= 0 {
		opts.PerNodeEdgeLimit = 250
	}
	if opts.MaxNodes <= 0 {
		opts.MaxNodes = 800
	}
	if opts.MaxEdges <= 0 {
		opts.MaxEdges = 4000
	}
	if opts.ReturnLimit <= 0 {
		opts.ReturnLimit = 25
	}
	if opts.MinScore < 0 {
		opts.MinScore = 0
	}

	diffuser := r.Diffuser
	if diffuser == nil {
		diffuser = PPRDiffuser{Options: algorithms.DefaultPPROptions()}
	}

	seedNodes, err := seedDocNodes(ctx, r.Store, spec.Seeds)
	if err != nil {
		return nil, err
	}
	if len(seedNodes) == 0 {
		return nil, nil
	}

	seedCode := make(map[string]struct{})
	for n, meta := range seedNodes {
		if meta.Endpoint.Kind == noderead.GraphEndpointCode {
			seedCode[n] = struct{}{}
		}
	}

	startedGraph := time.Now()
	graphFacts := r.GraphFacts
	if graphFacts == nil && r.Store != nil {
		graphFacts = docGraphFactsAdapter{store: r.Store}
	}
	adj, nodes, edges, noteLinkedToSeedCode, nodeMeta, err := buildInducedDocGraph(ctx, graphFacts, seedNodes, seedCode, opts)
	search.RecordTiming(ctx, search.TimingEvent{
		Name:     "graph_ppr.induced_graph",
		Kind:     "retriever",
		Started:  startedGraph,
		Duration: time.Since(startedGraph),
		Status:   search.TimingStatus(err),
		Err:      search.TimingError(err),
	})
	if err != nil {
		return nil, err
	}
	if len(adj) == 0 {
		return nil, nil
	}

	seeds := make(map[string]float64, len(seedNodes))
	for n := range seedNodes {
		seeds[n] = 1
	}

	startedDiffuse := time.Now()
	ranks := diffuser.Diffuse(adj, seeds)
	search.RecordTiming(ctx, search.TimingEvent{
		Name:     "graph_ppr.diffuse",
		Kind:     "retriever",
		Started:  startedDiffuse,
		Duration: time.Since(startedDiffuse),
		Status:   "ok",
	})
	if len(ranks) == 0 {
		return nil, nil
	}

	type scored struct {
		node  string
		score float64
		adj   float64
		seed  bool
	}
	items := make([]scored, 0, len(ranks))
	var max float64
	var maxAdj float64
	for node, v := range ranks {
		_, isSeed := seedNodes[node]
		if isSeed && !opts.IncludeSeedNodes {
			continue
		}
		if isSeed && opts.IncludeSeedNodes {
			// Seed nodes are normalized against the non-seed max so they don't squash
			// the rest of the neighborhood. We'll treat them as norm=1.0 later.
			items = append(items, scored{node: node, score: v, adj: v, seed: true})
			continue
		}
		if v > max {
			max = v
		}
		items = append(items, scored{node: node, score: v, adj: v, seed: false})
	}
	if max <= 0 || len(items) == 0 {
		return nil, nil
	}

	// Prefer notes that are directly linked to seeded code via code anchors (mentions edges)
	// or coderefs. This helps surface "docs for code" even when the text query doesn't hit them.
	//
	// We scale the bonus relative to the max base score so it behaves consistently across graphs.
	const linkedNoteBonusFrac = 0.10
	for i := range items {
		if items[i].seed {
			continue
		}
		if !isGraphNoteNode(nodeMeta[items[i].node]) {
			continue
		}
		if _, ok := noteLinkedToSeedCode[items[i].node]; ok {
			items[i].adj = items[i].score + linkedNoteBonusFrac*max
		}
		if items[i].adj > maxAdj {
			maxAdj = items[i].adj
		}
	}
	if maxAdj <= 0 {
		maxAdj = max
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].adj != items[j].adj {
			return items[i].adj > items[j].adj
		}
		return items[i].node < items[j].node
	})
	if len(items) > opts.ReturnLimit {
		items = items[:opts.ReturnLimit]
	}

	out := make([]search.Candidate, 0, len(items))
	notesAdded := 0
	filesAdded := 0
	for _, it := range items {
		norm := it.adj / maxAdj
		if it.seed {
			norm = 1.0
		}
		if norm < opts.MinScore {
			continue
		}

		// Enforce simple type quotas (if configured).
		if isGraphNoteNode(nodeMeta[it.node]) {
			if opts.MaxNotes > 0 && notesAdded >= opts.MaxNotes {
				continue
			}
		} else {
			if opts.MaxFiles > 0 && filesAdded >= opts.MaxFiles {
				continue
			}
		}

		linked := false
		if _, ok := noteLinkedToSeedCode[it.node]; ok {
			linked = true
		}
		c, ok := candidateForGraphNode(it.node, nodeMeta[it.node], norm, nodes, edges, it.seed, linked)
		if !ok {
			continue
		}
		out = append(out, c)
		if c.Type == "note" {
			notesAdded++
		} else if c.Type == "code" {
			filesAdded++
		}
	}
	return out, nil
}
