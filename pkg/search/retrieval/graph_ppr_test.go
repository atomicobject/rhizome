package retrieval

import (
	"context"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

type stubGraphStore struct {
	edgesByPath      map[string][]semdb.GraphDocEdge
	confidenceByPath map[string][]semdb.GraphDocEdge
	anchorsByID      map[string]codeanchor.IntelAnchor
	lastEdgePaths    []string
	lastConfLimit    int
}

type stubGraphFactsProvider struct {
	byPath map[string]noderead.GraphFactsResult
	byRef  map[string]noderead.GraphFactsResult
}

func (s stubGraphFactsProvider) GraphFacts(ctx context.Context, req noderead.GraphFactsRequest) (noderead.GraphFactsResult, error) {
	_ = ctx
	out := noderead.GraphFactsResult{}
	for _, path := range req.Paths {
		result := s.byPath[path]
		out.Nodes = append(out.Nodes, result.Nodes...)
		out.Edges = append(out.Edges, result.Edges...)
	}
	for _, ref := range req.Sources {
		result := s.byRef[ref.NodeID]
		out.Nodes = append(out.Nodes, result.Nodes...)
		out.Edges = append(out.Edges, result.Edges...)
	}
	return out, nil
}

func (s *stubGraphStore) GraphDocEdgesForPaths(ctx context.Context, paths []string, limit int, includeCalls bool) ([]semdb.GraphDocEdge, error) {
	_ = ctx
	s.lastEdgePaths = append([]string(nil), paths...)
	if limit <= 0 {
		limit = 500
	}
	var out []semdb.GraphDocEdge
	for _, path := range paths {
		edges := s.edgesByPath[path]
		for _, edge := range edges {
			if !includeCalls && edge.Kind == "calls" {
				continue
			}
			out = append(out, edge)
			if len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

func (s *stubGraphStore) GraphDocEdgesWithConfidenceForPathsLimit(ctx context.Context, paths []string, limit int) ([]semdb.GraphDocEdge, error) {
	_ = ctx
	s.lastConfLimit = limit
	if limit <= 0 {
		limit = 500
	}
	var out []semdb.GraphDocEdge
	for _, path := range paths {
		for _, edge := range s.confidenceByPath[path] {
			out = append(out, edge)
			if len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

func TestGraphPPRRetriever_CapsConfidenceLookupToEdgeBudget(t *testing.T) {
	store := &stubGraphStore{
		edgesByPath: map[string][]semdb.GraphDocEdge{
			"Notes/A.md": {
				{SrcPath: "Notes/A.md", DstPath: "Notes/B.md", Kind: "wikilink", Weight: 1},
				{SrcPath: "Notes/A.md", DstPath: "Notes/C.md", Kind: "wikilink", Weight: 1},
			},
		},
		confidenceByPath: map[string][]semdb.GraphDocEdge{
			"Notes/A.md": {
				{SrcPath: "Notes/A.md", DstPath: "Notes/B.md", Kind: "wikilink", Confidence: semdb.EdgeConfidenceExtracted, ConfidenceScore: 1.0},
				{SrcPath: "Notes/A.md", DstPath: "Notes/C.md", Kind: "wikilink", Confidence: semdb.EdgeConfidenceAmbiguous, ConfidenceScore: 0.2},
				{SrcPath: "Notes/A.md", DstPath: "Notes/D.md", Kind: "wikilink", Confidence: semdb.EdgeConfidenceAmbiguous, ConfidenceScore: 0.1},
			},
		},
	}

	r := &GraphPPRRetriever{
		Store: store,
		Options: GraphPPROptions{
			Depth:            1,
			PerNodeEdgeLimit: 1,
			MaxNodes:         50,
			MaxEdges:         1,
			ReturnLimit:      10,
			MinScore:         0.0,
			IncludeCalls:     false,
			IncludeSeedNodes: false,
		},
	}

	_, err := r.Retrieve(context.Background(), search.QuerySpec{
		Seeds: []knowledge.Handle{knowledge.NoteHandle("Notes/A.md")},
	})
	require.NoError(t, err)
	require.Equal(t, 1, store.lastConfLimit)
}

func (s *stubGraphStore) IntelAnchorByID(ctx context.Context, anchorID string) (codeanchor.IntelAnchor, bool, error) {
	_ = ctx
	a, ok := s.anchorsByID[anchorID]
	return a, ok, nil
}

func TestGraphPPRRetriever_SurfacesConnectedNodes(t *testing.T) {
	// A.md --wikilink--> B.md --wikilink--> D.md
	// A.md --mentions--> pkg/x.go
	store := &stubGraphStore{
		edgesByPath: map[string][]semdb.GraphDocEdge{
			"Notes/A.md": {
				{SrcPath: "Notes/A.md", DstPath: "Notes/B.md", Kind: "wikilink", Weight: 1},
				{SrcPath: "Notes/A.md", DstPath: "pkg/x.go", Kind: "mentions", Weight: 1},
			},
			"Notes/B.md": {
				{SrcPath: "Notes/B.md", DstPath: "Notes/D.md", Kind: "wikilink", Weight: 1},
			},
			"Notes/D.md": {},
			"pkg/x.go":   {},
		},
	}

	r := &GraphPPRRetriever{
		Store: store,
		Options: GraphPPROptions{
			Depth:            2,
			PerNodeEdgeLimit: 50,
			MaxNodes:         100,
			MaxEdges:         1000,
			ReturnLimit:      10,
			MinScore:         0.0,
			IncludeCalls:     false,
			IncludeSeedNodes: false,
		},
	}

	cands, err := r.Retrieve(context.Background(), search.QuerySpec{
		Seeds: []knowledge.Handle{knowledge.NoteHandle("Notes/A.md")},
	})
	require.NoError(t, err)

	// Should surface B and pkg/x.go (connected to seed), and may surface D (2 hops) as well.
	var hasB, hasX bool
	for _, c := range cands {
		if c.Path == "Notes/B.md" && c.Type == "note" {
			hasB = true
		}
		if c.Path == "pkg/x.go" && c.Type == "code" {
			hasX = true
		}
		if c.Path == "Notes/B.md" || c.Path == "pkg/x.go" {
			found := false
			for _, ev := range c.Evidence {
				if ev.Type == "graph_ppr" {
					found = true
					require.GreaterOrEqual(t, ev.RawScore, 0.0)
					require.LessOrEqual(t, ev.RawScore, 1.0)
				}
			}
			require.True(t, found, "missing graph_ppr evidence for %s", c.Path)
		}
	}
	require.True(t, hasB)
	require.True(t, hasX)
}

func TestGraphPPRRetriever_UsesGraphEndpointKindsForNonMarkdownPaths(t *testing.T) {
	provider := stubGraphFactsProvider{byPath: map[string]noderead.GraphFactsResult{
		"notes/Decision.mD": {Edges: []noderead.GraphFactEdge{
			{
				Source: "note:notes/Decision.mD", Target: "note:notes/Related.html",
				SourcePath: "notes/Decision.mD", TargetPath: "notes/Related.html",
				SourceKind: noderead.GraphEndpointNote, TargetKind: noderead.GraphEndpointNote,
				Kind: string(noderead.GraphEdgeKindOntology), Weight: 1, Confidence: 1,
			},
			{
				Source: "note:notes/Decision.mD", Target: "code:web/Decision.mD",
				SourcePath: "notes/Decision.mD", TargetPath: "web/Decision.mD",
				SourceKind: noderead.GraphEndpointNote, TargetKind: noderead.GraphEndpointCode,
				Kind: "mentions", Weight: 1, Confidence: 1,
			},
		}},
	}}
	r := &GraphPPRRetriever{GraphFacts: provider, Options: GraphPPROptions{
		Depth: 1, PerNodeEdgeLimit: 10, MaxNodes: 10, MaxEdges: 10, ReturnLimit: 10, IncludeSeedNodes: false,
	}}
	candidates, err := r.Retrieve(context.Background(), search.QuerySpec{
		Seeds: []knowledge.Handle{knowledge.NoteHandle("notes/Decision.mD")},
	})
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	gotKinds := map[string]string{}
	for _, candidate := range candidates {
		gotKinds[candidate.Path] = candidate.Type
	}
	require.Equal(t, "note", gotKinds["notes/Related.html"])
	require.Equal(t, "code", gotKinds["web/Decision.mD"])
}

func TestGraphPPRRetriever_WeightsConfidenceOnInducedEdges(t *testing.T) {
	store := &stubGraphStore{
		edgesByPath: map[string][]semdb.GraphDocEdge{
			"Notes/A.md": {
				{SrcPath: "Notes/A.md", DstPath: "Notes/B.md", Kind: "wikilink", Weight: 1},
				{SrcPath: "Notes/A.md", DstPath: "Notes/C.md", Kind: "wikilink", Weight: 1},
			},
		},
		confidenceByPath: map[string][]semdb.GraphDocEdge{
			"Notes/A.md": {
				{SrcPath: "Notes/A.md", DstPath: "Notes/B.md", Kind: "wikilink", Confidence: semdb.EdgeConfidenceExtracted, ConfidenceScore: 1.0},
				{SrcPath: "Notes/A.md", DstPath: "Notes/C.md", Kind: "wikilink", Confidence: semdb.EdgeConfidenceAmbiguous, ConfidenceScore: 0.2},
			},
		},
	}

	r := &GraphPPRRetriever{
		Store: store,
		Options: GraphPPROptions{
			Depth:            1,
			PerNodeEdgeLimit: 50,
			MaxNodes:         50,
			MaxEdges:         100,
			ReturnLimit:      10,
			MinScore:         0.0,
			IncludeCalls:     false,
			IncludeSeedNodes: false,
		},
	}

	cands, err := r.Retrieve(context.Background(), search.QuerySpec{
		Seeds: []knowledge.Handle{knowledge.NoteHandle("Notes/A.md")},
	})
	require.NoError(t, err)
	require.Len(t, cands, 2)
	require.Equal(t, "Notes/B.md", cands[0].Path)
	require.Equal(t, "Notes/C.md", cands[1].Path)
	require.Greater(t, cands[0].Evidence[0].RawScore, cands[1].Evidence[0].RawScore)
}

func TestGraphPPRRetriever_MapsAnchorSeedsToFileNodes(t *testing.T) {
	store := &stubGraphStore{
		anchorsByID: map[string]codeanchor.IntelAnchor{
			"anc-1": {AnchorID: "anc-1", Path: "pkg/seed.go"},
		},
		edgesByPath: map[string][]semdb.GraphDocEdge{
			"pkg/seed.go": {
				{SrcPath: "pkg/seed.go", DstPath: "Notes/Runbook.md", Kind: "coderef", Weight: 1},
			},
			"Notes/Runbook.md": {},
		},
	}

	r := &GraphPPRRetriever{
		Store: store,
		Options: GraphPPROptions{
			Depth:            1,
			PerNodeEdgeLimit: 50,
			MaxNodes:         50,
			MaxEdges:         100,
			ReturnLimit:      10,
			MinScore:         0.0,
			IncludeCalls:     false,
			IncludeSeedNodes: false,
		},
	}

	cands, err := r.Retrieve(context.Background(), search.QuerySpec{
		Seeds: []knowledge.Handle{knowledge.AnchorHandle("anc-1")},
	})
	require.NoError(t, err)

	var hasRunbook bool
	for _, c := range cands {
		if c.Type == "note" && c.Path == "Notes/Runbook.md" {
			hasRunbook = true
			break
		}
	}
	require.True(t, hasRunbook)
}

func TestGraphPPRRetriever_EmitsEmbeddedNodeCandidatesFromGraphFacts(t *testing.T) {
	storyRef := ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindEmbedded, TypeName: "UserStory", NodeID: "story-a", Fragment: "^story-a"}
	provider := stubGraphFactsProvider{byPath: map[string]noderead.GraphFactsResult{
		"docs/effort.md": {
			Edges: []noderead.GraphFactEdge{{
				Source:         "note:docs/effort.md",
				Target:         "embedded:story-a",
				SourcePath:     "docs/effort.md",
				TargetPath:     "docs/spec.md",
				SourceKind:     noderead.GraphEndpointNote,
				TargetKind:     noderead.GraphEndpointEmbedded,
				TargetRef:      storyRef,
				TargetNodeID:   "story-a",
				TargetTypeName: "UserStory",
				Kind:           string(noderead.GraphEdgeKindOntology),
				RelationName:   "frozenStories",
				Weight:         1,
				Confidence:     1,
			}},
		},
	}}
	r := &GraphPPRRetriever{
		GraphFacts: provider,
		Options: GraphPPROptions{
			Depth:            1,
			PerNodeEdgeLimit: 50,
			MaxNodes:         50,
			MaxEdges:         100,
			ReturnLimit:      10,
			MinScore:         0.0,
			IncludeSeedNodes: false,
		},
	}

	cands, err := r.Retrieve(context.Background(), search.QuerySpec{
		Seeds: []knowledge.Handle{knowledge.NoteHandle("docs/effort.md")},
	})
	require.NoError(t, err)
	require.Len(t, cands, 1)
	require.Equal(t, knowledge.KindNodeChunk, cands[0].Handle.Kind)
	require.Equal(t, "story-a", cands[0].NodeID)
	require.Equal(t, "docs/spec.md", cands[0].Path)
	require.Equal(t, "UserStory", cands[0].NodeType)
	require.Contains(t, cands[0].NodeRefJSON, `"notePath":"docs/spec.md"`)
	require.Contains(t, cands[0].NodeRefJSON, `"kind":"EMBEDDED"`)
}

func TestGraphPPRRetriever_NodeChunkSeedCarriesOwnerNoteRef(t *testing.T) {
	seed := knowledge.NodeChunkHandle("story-a", "docs/spec.md", "node_body", 1)

	nodes, err := seedDocNodes(context.Background(), nil, []knowledge.Handle{seed})
	require.NoError(t, err)

	meta, ok := nodes["embedded:story-a"]
	require.True(t, ok)
	require.Equal(t, "docs/spec.md", meta.Endpoint.NotePath)
	require.Equal(t, ontology.NodeKindEmbedded, meta.Endpoint.Ref.Kind)
	require.Equal(t, "docs/spec.md", meta.Endpoint.Ref.NotePath)
	require.Equal(t, "story-a", meta.Endpoint.Ref.NodeID)

	paths, refs := graphPPRFrontierRequest([]string{"embedded:story-a"}, map[string]graphNodeMeta{"embedded:story-a": meta})
	require.Empty(t, paths)
	require.Len(t, refs, 1)
	require.Equal(t, ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindEmbedded, NodeID: "story-a"}, refs[0])
}

func TestDocGraphFactsAdapter_SkipsEmbeddedEndpointSources(t *testing.T) {
	store := &stubGraphStore{
		edgesByPath: map[string][]semdb.GraphDocEdge{
			"docs/spec.md": {
				{SrcPath: "docs/spec.md", DstPath: "docs/other.md", Kind: "wikilink", Weight: 1},
			},
		},
	}
	adapter := docGraphFactsAdapter{store: store}

	result, err := adapter.GraphFacts(context.Background(), noderead.GraphFactsRequest{
		Sources: []ontology.NodeRef{{NotePath: "docs/spec.md", Kind: ontology.NodeKindEmbedded, NodeID: "story-a"}},
	})
	require.NoError(t, err)
	require.Empty(t, result.Edges)
	require.Empty(t, store.lastEdgePaths)
}

func TestGraphPPRRetriever_DepthLimitsReach(t *testing.T) {
	// A -> B -> D, but with Depth=1 we should not reach D (2 hops away).
	store := &stubGraphStore{
		edgesByPath: map[string][]semdb.GraphDocEdge{
			"Notes/A.md": {
				{SrcPath: "Notes/A.md", DstPath: "Notes/B.md", Kind: "wikilink", Weight: 1},
			},
			"Notes/B.md": {
				{SrcPath: "Notes/B.md", DstPath: "Notes/D.md", Kind: "wikilink", Weight: 1},
			},
		},
	}
	r := &GraphPPRRetriever{
		Store: store,
		Options: GraphPPROptions{
			Depth:            1,
			PerNodeEdgeLimit: 50,
			MaxNodes:         100,
			MaxEdges:         1000,
			ReturnLimit:      20,
			MinScore:         0.0,
			IncludeCalls:     false,
			IncludeSeedNodes: false,
		},
	}

	cands, err := r.Retrieve(context.Background(), search.QuerySpec{
		Seeds: []knowledge.Handle{knowledge.NoteHandle("Notes/A.md")},
	})
	require.NoError(t, err)

	// B should be reachable; D should not be discovered at depth 1.
	var hasB, hasD bool
	for _, c := range cands {
		if c.Type == "note" && c.Path == "Notes/B.md" {
			hasB = true
		}
		if c.Type == "note" && c.Path == "Notes/D.md" {
			hasD = true
		}
	}
	require.True(t, hasB)
	require.False(t, hasD)
}

type recordingDiffuser struct {
	called    bool
	lastSeeds map[string]float64
	// fixedRanks are returned regardless of input (used to test MinScore behavior deterministically).
	fixedRanks map[string]float64
}

func (d *recordingDiffuser) Diffuse(adjacency map[string]map[string]float64, seeds map[string]float64) map[string]float64 {
	d.called = true
	d.lastSeeds = make(map[string]float64, len(seeds))
	for k, v := range seeds {
		d.lastSeeds[k] = v
	}
	out := make(map[string]float64, len(d.fixedRanks))
	for k, v := range d.fixedRanks {
		out[k] = v
	}
	return out
}

func TestGraphPPRRetriever_UsesPluggableDiffuserAndHonorsMinScore(t *testing.T) {
	store := &stubGraphStore{
		edgesByPath: map[string][]semdb.GraphDocEdge{
			"Notes/A.md": {
				{SrcPath: "Notes/A.md", DstPath: "Notes/B.md", Kind: "wikilink", Weight: 1},
				{SrcPath: "Notes/A.md", DstPath: "Notes/C.md", Kind: "wikilink", Weight: 1},
			},
		},
	}
	diff := &recordingDiffuser{
		fixedRanks: map[string]float64{
			"Notes/A.md": 1.0,  // seed (should be excluded)
			"Notes/B.md": 0.10, // below threshold after normalization vs max (0.10/0.40 = 0.25)
			"Notes/C.md": 0.40, // max
		},
	}
	r := &GraphPPRRetriever{
		Store:    store,
		Diffuser: diff,
		Options: GraphPPROptions{
			Depth:            1,
			PerNodeEdgeLimit: 50,
			MaxNodes:         50,
			MaxEdges:         200,
			ReturnLimit:      10,
			MinScore:         0.30,
			IncludeCalls:     false,
			IncludeSeedNodes: false,
		},
	}

	cands, err := r.Retrieve(context.Background(), search.QuerySpec{
		Seeds: []knowledge.Handle{knowledge.NoteHandle("Notes/A.md")},
	})
	require.NoError(t, err)
	require.True(t, diff.called)
	require.Contains(t, diff.lastSeeds, "Notes/A.md")

	// Only C should survive MinScore.
	require.Len(t, cands, 1)
	require.Equal(t, "Notes/C.md", cands[0].Path)
	require.Equal(t, "note", cands[0].Type)
}

func TestGraphPPRRetriever_CanExcludeCallEdges(t *testing.T) {
	// The only connection from seed to target is via a calls edge. If IncludeCalls is false,
	// the induced graph should not include the target and we should return no candidates.
	store := &stubGraphStore{
		edgesByPath: map[string][]semdb.GraphDocEdge{
			"pkg/a.go": {
				{SrcPath: "pkg/a.go", DstPath: "pkg/b.go", Kind: "calls", Weight: 3},
			},
			"pkg/b.go": {},
		},
	}
	r := &GraphPPRRetriever{
		Store: store,
		Options: GraphPPROptions{
			Depth:            1,
			PerNodeEdgeLimit: 50,
			MaxNodes:         50,
			MaxEdges:         200,
			ReturnLimit:      10,
			MinScore:         0.0,
			IncludeCalls:     false,
			IncludeSeedNodes: false,
		},
	}
	cands, err := r.Retrieve(context.Background(), search.QuerySpec{
		Seeds: []knowledge.Handle{knowledge.FileHandle("pkg/a.go")},
	})
	require.NoError(t, err)
	require.Empty(t, cands)
	r.Options.IncludeCalls = true
	cands, err = r.Retrieve(context.Background(), search.QuerySpec{Seeds: []knowledge.Handle{knowledge.FileHandle("pkg/a.go")}})
	require.NoError(t, err)
	require.Len(t, cands, 1)
	require.Equal(t, "pkg/b.go", cands[0].Path)
}

func TestGraphPPRRetriever_IncludeSeedNodes_EmitsAnchorDerivedFile(t *testing.T) {
	store := &stubGraphStore{
		anchorsByID: map[string]codeanchor.IntelAnchor{
			"anc-1": {AnchorID: "anc-1", Path: "pkg/seed.go"},
		},
		edgesByPath: map[string][]semdb.GraphDocEdge{
			"pkg/seed.go": {
				{SrcPath: "pkg/seed.go", DstPath: "Notes/Runbook.md", Kind: "coderef", Weight: 1},
			},
			"Notes/Runbook.md": {},
		},
	}

	r := &GraphPPRRetriever{
		Store: store,
		Options: GraphPPROptions{
			Depth:            1,
			PerNodeEdgeLimit: 50,
			MaxNodes:         50,
			MaxEdges:         200,
			ReturnLimit:      10,
			MinScore:         0.0,
			IncludeCalls:     false,
			IncludeSeedNodes: true,
		},
	}

	cands, err := r.Retrieve(context.Background(), search.QuerySpec{
		Seeds: []knowledge.Handle{knowledge.AnchorHandle("anc-1")},
	})
	require.NoError(t, err)

	var hasSeedFile bool
	for _, c := range cands {
		if c.Type == "code" && c.Path == "pkg/seed.go" {
			hasSeedFile = true
			// Also ensure it's marked as a seed for debugging.
			for _, ev := range c.Evidence {
				if ev.Type == "graph_ppr" && ev.Details != nil && ev.Details["seed"] == "true" {
					return
				}
			}
			t.Fatalf("expected seed file candidate to have graph_ppr details seed=true")
		}
	}
	require.True(t, hasSeedFile)
}
