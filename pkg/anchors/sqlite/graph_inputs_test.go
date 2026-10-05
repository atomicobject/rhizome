package sqlite

import (
	"context"
	"fmt"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/stretchr/testify/require"
)

func TestAllGraphDocEdges_ReturnsCombinedGraph(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "graph-inputs.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "notes/a.md", GraphDocEdgeKindWikilink, []string{"notes/b.md"}))

	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/a.go", []codeanchor.IntelAnchor{{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "src/a.go",
		Symbol:      "FuncA",
		FQN:         "src.a.FuncA",
		Fingerprint: "fp-a1",
	}}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/b.go", []codeanchor.IntelAnchor{{
		AnchorID:    "b1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "src/b.go",
		Symbol:      "FuncB",
		FQN:         "src.b.FuncB",
		Fingerprint: "fp-b1",
	}}, []codeanchor.IntelEdge{{
		SrcID: "b1",
		DstID: "a1",
		Kind:  "calls",
	}}, nil))

	require.NoError(t, store.ReplaceIntelDocSections(ctx, "notes/a.md", []codeanchor.IntelDocSection{{
		SectionID:   "s1",
		Path:        "notes/a.md",
		Title:       "A",
		Level:       1,
		Content:     "See code",
		Fingerprint: "fp-s1",
	}}, []codeanchor.IntelEdge{{
		SrcID: "s1",
		DstID: "a1",
		Kind:  "mentions",
	}}, nil))

	require.NoError(t, store.ReplaceDocLinksForPath(ctx, "src/a.go", []codeanchor.DocLink{{
		SrcType: "code",
		SrcPath: "src/a.go",
		DstKind: "note",
		DstPath: "notes/b.md",
	}}))

	edges, err := store.AllGraphDocEdges(ctx)
	require.NoError(t, err)

	edgeMap := make(map[string]GraphDocEdge, len(edges))
	for _, edge := range edges {
		edgeMap[edge.SrcPath+"->"+edge.DstPath+":"+edge.Kind] = edge
	}

	require.Equal(t, 1, edgeMap["notes/a.md->notes/b.md:wikilink"].Weight)
	require.Equal(t, 1, edgeMap["notes/a.md->src/a.go:mentions"].Weight)
	require.Equal(t, 1, edgeMap["src/a.go->notes/b.md:coderef"].Weight)
	require.Equal(t, 1, edgeMap["src/b.go->src/a.go:calls"].Weight)
}

func TestGraphWebFingerprint_ChangesWhenGraphStateChanges(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "graph-fingerprint.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	fp0, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)

	require.NoError(t, store.UpsertNoteMeta(ctx, "notes/a.md", "hash-a", "v1", 1))
	require.NoError(t, store.ReplaceIntelDocSections(ctx, "notes/a.md", []codeanchor.IntelDocSection{{
		SectionID:   "s1",
		Path:        "notes/a.md",
		Title:       "A",
		Level:       1,
		Content:     "hello",
		Fingerprint: "fp-s1",
	}}, nil, nil))
	fp1, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.NotEqual(t, fp0, fp1)

	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/a.go", []codeanchor.IntelAnchor{{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "src/a.go",
		Symbol:      "FuncA",
		FQN:         "src.a.FuncA",
		Fingerprint: "fp-a1",
	}}, nil, nil))
	fp2, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.NotEqual(t, fp1, fp2)

	require.NoError(t, store.ReplaceGraphDocScores(ctx, []GraphDocScore{{
		DocPath:   "notes/a.md",
		DocType:   "note",
		Authority: 1,
		UpdatedAt: 10,
	}}))
	fp3, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.NotEqual(t, fp2, fp3)

	require.NoError(t, store.ReplaceAnchorScores(ctx, []AnchorScore{{
		AnchorID: "a1",
		PageRank: 1,
		Updated:  20,
	}}))
	fp4, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.NotEqual(t, fp3, fp4)

	require.NoError(t, store.ReplaceOntologySnapshot(ctx, OntologySnapshot{
		Edges: []OntologyEdgeRow{{
			SrcPath:      "notes/a.md",
			RelationName: "related",
			DstPath:      "notes/b.md",
			DstType:      "Spec",
			Provenance:   "body_link",
			UpdatedAt:    30,
		}},
	}))
	fp5, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.NotEqual(t, fp4, fp5)

	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{"notes/a.md"}, []codeanchor.IntelOntologyNode{{
		NodeID:        "story-a",
		NotePath:      "notes/a.md",
		NodeRefJSON:   `{"notePath":"notes/a.md","fragment":"^story-a","nodeId":"story-a","kind":"EMBEDDED","typeName":"UserStory"}`,
		NodeKind:      "EMBEDDED",
		TypeName:      "UserStory",
		SourceLocator: "notes/a.md#^story-a",
		UpdatedAt:     40,
	}}))
	fp6, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.NotEqual(t, fp5, fp6)
}

func TestGraphOntologyEdgesAppliesPrefixBeforeLimit(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "graph-prefix.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceOntologySnapshot(ctx, OntologySnapshot{
		Edges: []OntologyEdgeRow{
			{
				SrcPath:      "aaa/outside.md",
				RelationName: "related",
				DstPath:      "aaa/target.md",
				DstType:      "Spec",
				Structural:   true,
				UpdatedAt:    1,
			},
			{
				SrcPath:      "pkg/spec.md",
				RelationName: "related",
				DstPath:      "pkg/target.md",
				DstType:      "Spec",
				Structural:   true,
				UpdatedAt:    1,
			},
		},
		SchemaState: OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))

	edges, err := store.GraphOntologyEdges(ctx, readmodel.GraphEdgeQuery{
		PathPrefixes:   []string{"pkg"},
		IncludeAmbient: true,
		Limit:          1,
	})
	require.NoError(t, err)
	require.Len(t, edges, 1)
	require.Equal(t, "pkg/spec.md", edges[0].SrcPath)
}

func TestGraphWebFingerprintChangesOnSameCountRewire(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "graph-rewire-fingerprint.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "notes/a.md", GraphDocEdgeKindWikilink, []string{"notes/b.md"}))
	fp1, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)

	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "notes/a.md", GraphDocEdgeKindWikilink, []string{"notes/c.md"}))
	fp2, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.NotEqual(t, fp1, fp2)

	require.NoError(t, store.ReplaceOntologySnapshot(ctx, OntologySnapshot{
		Edges: []OntologyEdgeRow{{
			SrcPath:      "notes/a.md",
			RelationName: "related",
			DstPath:      "notes/b.md",
			DstType:      "Spec",
			Structural:   true,
			UpdatedAt:    1,
		}},
		SchemaState: OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	fp3, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, OntologySnapshot{
		Edges: []OntologyEdgeRow{{
			SrcPath:      "notes/a.md",
			RelationName: "related",
			DstPath:      "notes/c.md",
			DstType:      "Spec",
			Structural:   true,
			UpdatedAt:    1,
		}},
		SchemaState: OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	fp4, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.NotEqual(t, fp3, fp4)
}

func TestGraphOntologyNodesAppliesPrefixBeforeLimit(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "graph-node-prefix.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{"aaa/outside.md", "pkg/spec.md"}, []codeanchor.IntelOntologyNode{
		{
			NodeID:        "outside",
			NotePath:      "aaa/outside.md",
			NodeRefJSON:   `{"notePath":"aaa/outside.md","kind":"NOTE"}`,
			NodeKind:      "NOTE",
			TypeName:      "TechnicalSpec",
			SourceLocator: "aaa/outside.md",
			UpdatedAt:     1,
		},
		{
			NodeID:        "inside",
			NotePath:      "pkg/spec.md",
			NodeRefJSON:   `{"notePath":"pkg/spec.md","kind":"NOTE"}`,
			NodeKind:      "NOTE",
			TypeName:      "TechnicalSpec",
			SourceLocator: "pkg/spec.md",
			UpdatedAt:     1,
		},
	}))

	nodes, err := store.GraphOntologyNodes(ctx, readmodel.GraphNodeQuery{PathPrefixes: []string{"pkg"}, Limit: 1})
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	require.Equal(t, "pkg/spec.md", nodes[0].NotePath)
}

func TestGraphDocEdgesForPrefixSkipsCodeQueriesWhenCodeExcluded(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "graph-doc-prefix.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "pkg/spec.md", GraphDocEdgeKindWikilink, []string{"pkg/related.md"}))
	require.NoError(t, store.ReplaceIntelDocSections(ctx, "pkg/spec.md", []codeanchor.IntelDocSection{{
		SectionID:   "s1",
		Path:        "pkg/spec.md",
		Title:       "Spec",
		Level:       1,
		EndByte:     4,
		Content:     "Spec",
		Fingerprint: "fp-section",
	}}, []codeanchor.IntelEdge{{
		SrcID: "s1",
		DstID: "a1",
		Kind:  "mentions",
	}}, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/code.go", []codeanchor.IntelAnchor{{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/code.go",
		Symbol:      "Code",
		FQN:         "pkg.Code",
		Fingerprint: "fp-anchor",
	}}, nil, nil))

	edges, err := store.GraphDocEdges(ctx, readmodel.GraphDocEdgeQuery{PathPrefixes: []string{"pkg"}, IncludeCode: false, Limit: 10})
	require.NoError(t, err)
	require.Len(t, edges, 1)
	require.Equal(t, GraphDocEdgeKindWikilink, edges[0].Kind)
}

func TestGraphDocEdgesReadmodel_PreservesTypedMixedCaseNotePaths(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "graph-doc-mixed-case.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// The persisted wikilink fact is typed note-to-note. A suffix-based filter
	// used to drop these authored paths because neither ends in lowercase .md.
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "notes/Decision.mD", GraphDocEdgeKindWikilink, []string{"notes/Target.MD"}))

	edges, err := store.GraphDocEdges(ctx, readmodel.GraphDocEdgeQuery{IncludeCode: false})
	require.NoError(t, err)
	require.Equal(t, []readmodel.GraphDocEdgeRow{{
		SrcPath: "notes/Decision.mD", DstPath: "notes/Target.MD", Kind: GraphDocEdgeKindWikilink, Weight: 1,
		SourceKind: "note", TargetKind: "note", Confidence: EdgeConfidenceExtracted, ConfidenceScore: 1,
	}}, edges)
}

func TestGraphDocEdgesReadmodel_PreservesDetailedNoteLinkEdges(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "graph-doc-detailed-note-links.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Detailed note-link rows are source-owned note facts. They must not be
	// classified by their edge-kind spelling or by a Markdown filename suffix.
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "notes/Source.HTML", "note_link:wikilink:alias", []string{"notes/Target.HTML"}))

	edges, err := store.GraphDocEdges(ctx, readmodel.GraphDocEdgeQuery{IncludeCode: false})
	require.NoError(t, err)
	require.Equal(t, []readmodel.GraphDocEdgeRow{{
		SrcPath: "notes/Source.HTML", DstPath: "notes/Target.HTML", Kind: "note_link:wikilink:alias", Weight: 1,
		SourceKind: "note", TargetKind: "note", Confidence: EdgeConfidenceExtracted, ConfidenceScore: 1,
	}}, edges)
}

func TestGraphDocEdgesReadmodel_SkipsUnknownEdgeOwnership(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "graph-doc-unknown-ownership.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "notes/source.md", "unsupported_relation", []string{"notes/target.md"}))

	edges, err := store.GraphDocEdges(ctx, readmodel.GraphDocEdgeQuery{IncludeCode: true, IncludeCodeEdges: true})
	require.NoError(t, err)
	require.Empty(t, edges)
}

func TestGraphDocEdgesReadmodelBoundsAllEdgesAndSeparatesCodeClasses(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "graph-doc-readmodel.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "notes/a.md", GraphDocEdgeKindWikilink, []string{"notes/b.md", "notes/c.md"}))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/a.go", []codeanchor.IntelAnchor{{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "src/a.go",
		Symbol:      "A",
		FQN:         "src.A",
		Fingerprint: "fp-a",
	}}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/b.go", []codeanchor.IntelAnchor{{
		AnchorID:    "b1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "src/b.go",
		Symbol:      "B",
		FQN:         "src.B",
		Fingerprint: "fp-b",
	}}, []codeanchor.IntelEdge{{
		SrcID: "b1",
		DstID: "a1",
		Kind:  "calls",
	}}, nil))
	require.NoError(t, store.ReplaceIntelDocSections(ctx, "notes/a.md", []codeanchor.IntelDocSection{{
		SectionID:   "s1",
		Path:        "notes/a.md",
		Title:       "A",
		Level:       1,
		Content:     "Mentions code",
		Fingerprint: "fp-s1",
	}}, []codeanchor.IntelEdge{{
		SrcID: "s1",
		DstID: "a1",
		Kind:  "mentions",
	}}, nil))

	limited, err := store.GraphDocEdges(ctx, readmodel.GraphDocEdgeQuery{Limit: 1})
	require.NoError(t, err)
	require.Len(t, limited, 1)
	require.Equal(t, GraphDocEdgeKindWikilink, limited[0].Kind)

	codeNoCalls, err := store.GraphDocEdges(ctx, readmodel.GraphDocEdgeQuery{IncludeCode: true, Limit: 10})
	require.NoError(t, err)
	require.Contains(t, graphDocEdgeKinds(codeNoCalls), "mentions")
	require.NotContains(t, graphDocEdgeKinds(codeNoCalls), "calls")

	codeWithCalls, err := store.GraphDocEdges(ctx, readmodel.GraphDocEdgeQuery{IncludeCode: true, IncludeCodeEdges: true, Limit: 10})
	require.NoError(t, err)
	require.Contains(t, graphDocEdgeKinds(codeWithCalls), "calls")
}

func TestGraphDocEdgesReadmodelZeroLimitMeansUnlimited(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "graph-doc-unlimited.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	dsts := make([]string, 0, 501)
	for i := 0; i < 501; i++ {
		dsts = append(dsts, "notes/target-"+fmt.Sprintf("%03d", i)+".md")
	}
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "notes/src.md", GraphDocEdgeKindWikilink, dsts))

	edges, err := store.GraphDocEdges(ctx, readmodel.GraphDocEdgeQuery{})
	require.NoError(t, err)
	require.Len(t, edges, 501)
}

func TestGraphDocNoteNeighborhoodForPathsBoundsEdgesAndPreservesDegreesAndAuthorityOrder(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "graph-note-neighborhood.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	const neighborCount = 40
	outbound := make([]string, 0, neighborCount)
	scores := make([]GraphDocScore, 0, neighborCount*2)
	for i := 0; i < neighborCount; i++ {
		inPath := fmt.Sprintf("notes/in-%02d.md", i)
		outPath := fmt.Sprintf("notes/out-%02d.md", i)
		require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, inPath, GraphDocEdgeKindWikilink, []string{"notes/target.md"}))
		outbound = append(outbound, outPath)
		scores = append(scores,
			GraphDocScore{DocPath: inPath, DocType: "note", Authority: float64(i)},
			GraphDocScore{DocPath: outPath, DocType: "note", Authority: float64(i)},
		)
	}
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "notes/target.md", GraphDocEdgeKindWikilink, outbound))
	require.NoError(t, store.ReplaceGraphDocScores(ctx, scores))

	got, err := store.GraphDocNoteNeighborhoodForPaths(ctx, []string{"notes/target.md"}, 5)
	require.NoError(t, err)
	require.Len(t, got.Edges, 10, "the read model must return only the requested top-k edges per direction")
	require.Equal(t, GraphDocDegree{Inbound: neighborCount, Outbound: neighborCount}, got.Degrees["notes/target.md"])

	inboundPaths := make([]string, 0, 5)
	outboundPaths := make([]string, 0, 5)
	for _, edge := range got.Edges {
		switch {
		case edge.DstPath == "notes/target.md":
			inboundPaths = append(inboundPaths, edge.SrcPath)
		case edge.SrcPath == "notes/target.md":
			outboundPaths = append(outboundPaths, edge.DstPath)
		default:
			t.Fatalf("unexpected non-incident edge: %+v", edge)
		}
	}
	require.ElementsMatch(t, []string{
		"notes/in-39.md", "notes/in-38.md", "notes/in-37.md", "notes/in-36.md", "notes/in-35.md",
	}, inboundPaths)
	require.ElementsMatch(t, []string{
		"notes/out-39.md", "notes/out-38.md", "notes/out-37.md", "notes/out-36.md", "notes/out-35.md",
	}, outboundPaths)
}

func TestGraphOntologyEndpointSelectorsBoundNodesAndIncidentEdges(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "graph-endpoints.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceOntologySnapshot(ctx, OntologySnapshot{
		Edges: []OntologyEdgeRow{
			{SrcPath: "docs/spec.md", RelationName: "noteOwner", DstPath: "people/note.md", DstType: "Person", Structural: true},
			{SrcPath: "docs/spec.md", SrcNodeID: "story-a", RelationName: "storyOwner", DstPath: "people/alice.md", DstType: "Person", Structural: true},
			{SrcPath: "docs/spec.md", SrcNodeID: "story-b", RelationName: "storyOwner", DstPath: "people/bob.md", DstType: "Person", Structural: true},
		},
		SchemaState: OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{"docs/spec.md"}, []codeanchor.IntelOntologyNode{
		{NodeID: "story-a", NotePath: "docs/spec.md", NodeRefJSON: `{"notePath":"docs/spec.md","nodeId":"story-a","kind":"EMBEDDED"}`, NodeKind: "EMBEDDED", TypeName: "UserStory", SourceLocator: "docs/spec.md#^story-a", BlockID: "story-a", UpdatedAt: 1},
		{NodeID: "story-b", NotePath: "docs/spec.md", NodeRefJSON: `{"notePath":"docs/spec.md","nodeId":"story-b","kind":"EMBEDDED"}`, NodeKind: "EMBEDDED", TypeName: "UserStory", SourceLocator: "docs/spec.md#^story-b", BlockID: "story-b", UpdatedAt: 1},
	}))

	selector := readmodel.GraphEndpointSelector{Path: "docs/spec.md", NodeID: "story-a", Kind: "EMBEDDED"}
	nodes, err := store.GraphOntologyNodes(ctx, readmodel.GraphNodeQuery{EndpointSelectors: []readmodel.GraphEndpointSelector{selector}})
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	require.Equal(t, "story-a", nodes[0].NodeID)

	edges, err := store.GraphOntologyEdges(ctx, readmodel.GraphEdgeQuery{EndpointSelectors: []readmodel.GraphEndpointSelector{selector}, IncludeAmbient: true})
	require.NoError(t, err)
	require.Len(t, edges, 1)
	require.Equal(t, "storyOwner", edges[0].RelationName)
	require.Equal(t, "people/alice.md", edges[0].DstPath)
}

func TestGraphOntologyEndpointSelectorsResolveRawNodeIDAgainstHashedCatalogID(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "graph-endpoints-hashed.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	hashedID := "catalog-hashed-story-a"
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, OntologySnapshot{
		Edges: []OntologyEdgeRow{
			{SrcPath: "docs/spec.md", SrcNodeID: "story-a", RelationName: "storyOwner", DstPath: "people/alice.md", DstType: "Person", Structural: true},
		},
		SchemaState: OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{"docs/spec.md"}, []codeanchor.IntelOntologyNode{
		{NodeID: hashedID, NotePath: "docs/spec.md", NodeRefJSON: `{"notePath":"docs/spec.md","fragment":"^story-a","nodeId":"story-a","kind":"EMBEDDED","typeName":"UserStory"}`, NodeKind: "EMBEDDED", TypeName: "UserStory", SourceLocator: "docs/spec.md#^story-a", BlockID: "story-a", UpdatedAt: 1},
	}))

	selector := readmodel.GraphEndpointSelector{Path: "docs/spec.md", NodeID: "story-a", Kind: "EMBEDDED"}
	nodes, err := store.GraphOntologyNodes(ctx, readmodel.GraphNodeQuery{EndpointSelectors: []readmodel.GraphEndpointSelector{selector}})
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	require.Equal(t, hashedID, nodes[0].NodeID)

	edges, err := store.GraphOntologyEdges(ctx, readmodel.GraphEdgeQuery{EndpointSelectors: []readmodel.GraphEndpointSelector{selector}, IncludeAmbient: true})
	require.NoError(t, err)
	require.Len(t, edges, 1)
	require.Equal(t, "storyOwner", edges[0].RelationName)
}

func graphDocEdgeKinds(edges []readmodel.GraphDocEdgeRow) []string {
	out := make([]string, 0, len(edges))
	for _, edge := range edges {
		out = append(out, edge.Kind)
	}
	return out
}
