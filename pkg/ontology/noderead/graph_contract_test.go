package noderead

import (
	"context"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestScopeGraphMergesOntologyEmbeddedAndUntypedEdges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	storyRef := ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindEmbedded, TypeName: "UserStory", NodeID: "story-a", Fragment: "^story-a"}
	criteriaSectionRef := ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindSection, TypeName: "AcceptanceCriteriaSection", NodeID: "criteria-section", Fragment: "Acceptance Criteria"}
	criterionRef := ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindEmbedded, TypeName: "AcceptanceCriterion", NodeID: "criterion-a", Fragment: "^criterion-a"}
	specRef := ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindNote, TypeName: "TechnicalSpec"}
	storyRefJSON := mustNodeRefJSON(t, storyRef)
	criteriaSectionRefJSON := mustNodeRefJSON(t, criteriaSectionRef)
	criterionRefJSON := mustNodeRefJSON(t, criterionRef)
	specRefJSON := mustNodeRefJSON(t, specRef)
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "docs/effort.md", RelationName: "frozenSpecs", DstPath: "docs/spec.md", DstType: "TechnicalSpec", Provenance: "field", Structural: true},
			{SrcPath: "docs/effort.md", RelationName: "frozenStories", DstPath: "docs/spec.md", DstNodeID: ontology.OntologyNodeID(storyRef), DstType: "UserStory", Provenance: "field", Structural: true},
			{SrcPath: "docs/spec.md", SrcNodeID: "story-a", RelationName: "owner", DstPath: "docs/person.md", DstType: "ReferenceDoc", Provenance: "field", Structural: true},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{"docs/spec.md"}, []readmodel.NodeRow{
		{NodeID: ontology.OntologyNodeID(specRef), NotePath: "docs/spec.md", NodeRefJSON: specRefJSON, NodeKind: string(ontology.NodeKindNote), TypeName: "TechnicalSpec", DisplayLabel: "Spec", SourceLocator: "docs/spec.md", LocatorStatus: string(ontology.NodeLocatorLinkable), UpdatedAt: 1},
		{NodeID: ontology.OntologyNodeID(storyRef), NotePath: "docs/spec.md", NodeRefJSON: storyRefJSON, NodeKind: string(ontology.NodeKindEmbedded), TypeName: "UserStory", ParentNodeID: ontology.OntologyNodeID(specRef), DisplayLabel: "Story A", SourceLocator: "docs/spec.md#Story A", Fragment: "Story A", BlockID: "story-a", LocatorStatus: string(ontology.NodeLocatorLinkable), UpdatedAt: 1},
		{NodeID: ontology.OntologyNodeID(criteriaSectionRef), NotePath: "docs/spec.md", NodeRefJSON: criteriaSectionRefJSON, NodeKind: string(ontology.NodeKindSection), TypeName: "AcceptanceCriteriaSection", ParentNodeID: ontology.OntologyNodeID(storyRef), DisplayLabel: "Acceptance Criteria", SourceLocator: "docs/spec.md#Acceptance Criteria", Fragment: "Acceptance Criteria", LocatorStatus: string(ontology.NodeLocatorLinkable), UpdatedAt: 1},
		{NodeID: ontology.OntologyNodeID(criterionRef), NotePath: "docs/spec.md", NodeRefJSON: criterionRefJSON, NodeKind: string(ontology.NodeKindEmbedded), TypeName: "AcceptanceCriterion", ParentNodeID: ontology.OntologyNodeID(criteriaSectionRef), DisplayLabel: "Criterion A", SourceLocator: "docs/spec.md#^criterion-a", Fragment: "^criterion-a", BlockID: "criterion-a", LocatorStatus: string(ontology.NodeLocatorLinkable), UpdatedAt: 1},
	}))
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "docs/untyped.md", semdb.GraphDocEdgeKindWikilink, []string{"docs/spec.md"}))
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "docs/effort.md", semdb.GraphDocEdgeKindWikilink, []string{"docs/spec.md"}))

	scope := NewService(obsidian.VaultDefinition{Path: root}, nil, store, nil).NewScope(ScopeOptions{})
	graph, err := scope.Graph(ctx, GraphRequest{
		Sources:     []ontology.NodeRef{{NotePath: "docs/spec.md", Kind: ontology.NodeKindNote}, {NotePath: "docs/effort.md", Kind: ontology.NodeKindNote}, {NotePath: "docs/untyped.md", Kind: ontology.NodeKindNote}},
		Profile:     GraphProfileNotesOnly,
		Diagnostics: true,
	})
	require.NoError(t, err)
	storyEndpoint := "embedded:" + ontology.OntologyNodeID(storyRef)
	criterionEndpoint := "embedded:" + ontology.OntologyNodeID(criterionRef)
	require.Contains(t, graphEndpointIDs(graph.Nodes), "embedded:"+ontology.OntologyNodeID(storyRef))
	require.Contains(t, graphEndpointIDs(graph.Nodes), criterionEndpoint)
	require.Contains(t, graphEndpointIDs(graph.Nodes), "note:docs/untyped.md")
	require.Contains(t, graphEdgeKinds(graph.Edges), "embeds")
	require.Contains(t, graphEdgePairKinds(graph.Edges), storyEndpoint+"|"+criterionEndpoint+"|embeds")
	require.NotContains(t, graphEdgePairKinds(graph.Edges), "note:docs/spec.md|"+criterionEndpoint+"|embeds")
	require.Contains(t, graphEdgeRelationNames(graph.Edges), "frozenStories")
	require.Contains(t, graphEdgeRelationNames(graph.Edges), "frozenSpecs")
	require.Contains(t, graphEdgeRelationNames(graph.Edges), "owner")
	require.Contains(t, graphEdgePairKinds(graph.Edges), "embedded:"+ontology.OntologyNodeID(storyRef)+"|note:docs/person.md|ontology")
	require.NotContains(t, graphEdgePairKinds(graph.Edges), "note:docs/effort.md|note:docs/spec.md|wikilink")
	require.Contains(t, graphEdgePairKinds(graph.Edges), "note:docs/untyped.md|note:docs/spec.md|wikilink")
	require.Equal(t, "docs/spec.md#^story-a", graphEndpointByID(graph.Nodes, "embedded:"+ontology.OntologyNodeID(storyRef)).SourceLocator)
	require.NotNil(t, graph.Diagnostics)
	require.NotEmpty(t, graph.Diagnostics.Nodes)
	require.NotEmpty(t, graph.Diagnostics.Edges)
	require.NotEmpty(t, graph.Diagnostics.Dedupe)
}

func TestScopeGraphFactsCarriesTypedEmbeddedAndDocEdgeMetadata(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	storyRef := ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindEmbedded, TypeName: "UserStory", NodeID: "story-a", Fragment: "^story-a"}
	specRef := ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindNote, TypeName: "TechnicalSpec"}
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "docs/effort.md", RelationName: "frozenStories", DstPath: "docs/spec.md", DstNodeID: "story-a", DstType: "UserStory", Provenance: "field", Structural: true},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{"docs/spec.md"}, []readmodel.NodeRow{
		{NodeID: ontology.OntologyNodeID(specRef), NotePath: "docs/spec.md", NodeRefJSON: mustNodeRefJSON(t, specRef), NodeKind: string(ontology.NodeKindNote), TypeName: "TechnicalSpec", DisplayLabel: "Spec", SourceLocator: "docs/spec.md", LocatorStatus: string(ontology.NodeLocatorLinkable), UpdatedAt: 1},
		{NodeID: ontology.OntologyNodeID(storyRef), NotePath: "docs/spec.md", NodeRefJSON: mustNodeRefJSON(t, storyRef), NodeKind: string(ontology.NodeKindEmbedded), TypeName: "UserStory", ParentNodeID: ontology.OntologyNodeID(specRef), DisplayLabel: "Story A", SourceLocator: "docs/spec.md#^story-a", BlockID: "story-a", LocatorStatus: string(ontology.NodeLocatorLinkable), UpdatedAt: 1},
	}))
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "docs/untyped.md", semdb.GraphDocEdgeKindWikilink, []string{"docs/spec.md"}))

	scope := NewService(obsidian.VaultDefinition{Path: root}, nil, store, nil).NewScope(ScopeOptions{})
	facts, err := scope.GraphFacts(ctx, GraphFactsRequest{
		Paths:           []string{"docs/effort.md", "docs/spec.md", "docs/untyped.md"},
		IncludeOntology: true,
		IncludeDocLinks: true,
		IncludeEmbedded: true,
	})
	require.NoError(t, err)

	storyEndpoint := "embedded:" + ontology.OntologyNodeID(storyRef)
	require.Contains(t, graphFactPairKinds(facts.Edges), "note:docs/effort.md|"+storyEndpoint+"|ontology")
	require.Contains(t, graphFactPairKinds(facts.Edges), "note:docs/spec.md|"+storyEndpoint+"|embeds")
	require.Contains(t, graphFactPairKinds(facts.Edges), "note:docs/untyped.md|note:docs/spec.md|wikilink")
	frozen := graphFactByPairKind(facts.Edges, "note:docs/effort.md", storyEndpoint, "ontology")
	require.Equal(t, "frozenStories", frozen.RelationName)
	require.Equal(t, ontology.NodeKindEmbedded, frozen.TargetRef.Kind)
	require.Equal(t, "docs/spec.md#^story-a", frozen.TargetLocator)
	require.Equal(t, 1.0, frozen.Confidence)
}

func TestScopeGraphDoesNotLetEmbeddedRowsOverwriteNoteEndpoint(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	embeddedRef := ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindEmbedded, TypeName: "UserStory", NodeID: "aaa-story", Fragment: "^story-a"}
	noteRef := ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindNote, TypeName: "TechnicalSpec"}
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{"docs/spec.md"}, []readmodel.NodeRow{
		{
			NodeID:        "aaa-embedded",
			NotePath:      "docs/spec.md",
			NodeRefJSON:   mustNodeRefJSON(t, embeddedRef),
			NodeKind:      string(ontology.NodeKindEmbedded),
			TypeName:      "UserStory",
			ParentNodeID:  "zzz-note",
			DisplayLabel:  "Story A",
			SourceLocator: "docs/spec.md#^story-a",
			BlockID:       "story-a",
			LocatorStatus: string(ontology.NodeLocatorLinkable),
			UpdatedAt:     1,
		},
		{
			NodeID:        "zzz-note",
			NotePath:      "docs/spec.md",
			NodeRefJSON:   mustNodeRefJSON(t, noteRef),
			NodeKind:      string(ontology.NodeKindNote),
			TypeName:      "TechnicalSpec",
			DisplayLabel:  "Spec",
			SourceLocator: "docs/spec.md",
			LocatorStatus: string(ontology.NodeLocatorLinkable),
			UpdatedAt:     1,
		},
	}))

	scope := NewService(obsidian.VaultDefinition{Path: root}, nil, store, nil).NewScope(ScopeOptions{})
	graph, err := scope.Graph(ctx, GraphRequest{
		Sources: []ontology.NodeRef{{NotePath: "docs/spec.md", Kind: ontology.NodeKindNote}},
		Profile: GraphProfileNotesOnly,
	})
	require.NoError(t, err)

	note := graphEndpointByID(graph.Nodes, "note:docs/spec.md")
	require.Equal(t, "TechnicalSpec", note.TypeName)
	require.Equal(t, "zzz-note", note.NodeID)
	require.Equal(t, ontology.NodeKindNote, note.Ref.Kind)
	require.Equal(t, "docs/spec.md", note.SourceLocator)
}

func TestScopeGraphEmbeddedSourceIsEndpointScoped(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	storyRef := ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindEmbedded, TypeName: "UserStory", NodeID: "story-a", Fragment: "^story-a"}
	specRef := ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindNote, TypeName: "TechnicalSpec"}
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "docs/spec.md", RelationName: "related", DstPath: "docs/whole-note.md", DstType: "ReferenceDoc", Provenance: "field", Structural: true},
			{SrcPath: "docs/spec.md", SrcNodeID: "story-a", RelationName: "owner", DstPath: "docs/person.md", DstType: "ReferenceDoc", Provenance: "field", Structural: true},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{"docs/spec.md"}, []readmodel.NodeRow{
		{NodeID: ontology.OntologyNodeID(specRef), NotePath: "docs/spec.md", NodeRefJSON: mustNodeRefJSON(t, specRef), NodeKind: string(ontology.NodeKindNote), TypeName: "TechnicalSpec", DisplayLabel: "Spec", SourceLocator: "docs/spec.md", LocatorStatus: string(ontology.NodeLocatorLinkable), UpdatedAt: 1},
		{NodeID: ontology.OntologyNodeID(storyRef), NotePath: "docs/spec.md", NodeRefJSON: mustNodeRefJSON(t, storyRef), NodeKind: string(ontology.NodeKindEmbedded), TypeName: "UserStory", ParentNodeID: ontology.OntologyNodeID(specRef), DisplayLabel: "Story A", SourceLocator: "docs/spec.md#^story-a", BlockID: "story-a", LocatorStatus: string(ontology.NodeLocatorLinkable), UpdatedAt: 1},
	}))
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "docs/spec.md", semdb.GraphDocEdgeKindWikilink, []string{"docs/parent-link.md"}))

	scope := NewService(obsidian.VaultDefinition{Path: root}, nil, store, nil).NewScope(ScopeOptions{})
	graph, err := scope.Graph(ctx, GraphRequest{
		Sources: []ontology.NodeRef{storyRef},
		Profile: GraphProfileCodeAware,
	})
	require.NoError(t, err)

	pairs := graphEdgePairKinds(graph.Edges)
	storyEndpoint := "embedded:" + ontology.OntologyNodeID(storyRef)
	require.Contains(t, pairs, storyEndpoint+"|note:docs/person.md|ontology")
	require.Contains(t, pairs, "note:docs/spec.md|"+storyEndpoint+"|embeds")
	require.NotContains(t, pairs, "note:docs/spec.md|note:docs/whole-note.md|ontology")
	require.NotContains(t, pairs, "note:docs/spec.md|note:docs/parent-link.md|wikilink")
}

func TestScopeGraphUnresolvedEndpointSourceDoesNotWidenScope(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "docs/spec.md", RelationName: "related", DstPath: "docs/whole-note.md", DstType: "ReferenceDoc", Provenance: "field", Structural: true},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "docs/spec.md", semdb.GraphDocEdgeKindWikilink, []string{"docs/parent-link.md"}))

	scope := NewService(obsidian.VaultDefinition{Path: root}, nil, store, nil).NewScope(ScopeOptions{})
	graph, err := scope.Graph(ctx, GraphRequest{
		Sources: []ontology.NodeRef{{NotePath: "docs/spec.md", Kind: ontology.NodeKindEmbedded, NodeID: "missing-story"}},
		Profile: GraphProfileCodeAware,
	})
	require.NoError(t, err)
	require.Empty(t, graph.Edges)
}

func TestScopeGraphFactsAppliesLimitsAfterKindFiltering(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "docs/a.md", RelationName: "typed", DstPath: "docs/b.md", DstType: "Spec", Provenance: "field", Structural: true},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "docs/c.md", semdb.GraphDocEdgeKindWikilink, []string{"docs/d.md"}))

	scope := NewService(obsidian.VaultDefinition{Path: root}, nil, store, nil).NewScope(ScopeOptions{})
	facts, err := scope.GraphFacts(ctx, GraphFactsRequest{
		IncludeOntology: false,
		IncludeDocLinks: true,
		EdgeLimit:       1,
	})
	require.NoError(t, err)
	require.Len(t, facts.Edges, 1)
	require.Equal(t, "wikilink", facts.Edges[0].Kind)
	require.Equal(t, "docs/c.md", facts.Edges[0].SourcePath)
}

func TestScopeCodeGraphLinksReadsIncidentCodeLinks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.ReplaceDocLinksForPath(ctx, "pkg/app.go", []codeanchor.DocLink{
		{
			SrcType: "code",
			SrcPath: "pkg/app.go",
			DstKind: "note",
			DstPath: "docs/spec.md",
		},
	}))
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "docs/spec.md", semdb.GraphDocEdgeKindWikilink, []string{"docs/related.md"}))

	scope := NewService(obsidian.VaultDefinition{Path: root}, nil, store, nil).NewScope(ScopeOptions{})
	links, err := scope.CodeGraphLinks(ctx, ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindNote}, 50)
	require.NoError(t, err)
	require.Len(t, links, 1)
	require.Equal(t, GraphCodeLink{
		Path:       "pkg/app.go",
		Kind:       "coderef",
		Provenance: "coderef",
		Weight:     1,
	}, links[0])
}

func TestScopeGraphFactsIncludesCodeCodeEdgesOnlyWhenCallsIncluded(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/a.go", []codeanchor.IntelAnchor{{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/a.go",
		Symbol:      "A",
		FQN:         "pkg.A",
		Fingerprint: "fp-a",
	}}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/b.go", []codeanchor.IntelAnchor{{
		AnchorID:    "b1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/b.go",
		Symbol:      "B",
		FQN:         "pkg.B",
		Fingerprint: "fp-b",
	}}, []codeanchor.IntelEdge{{
		SrcID: "b1",
		DstID: "a1",
		Kind:  "calls",
	}}, nil))
	require.NoError(t, store.ReplaceIntelDocSections(ctx, "docs/spec.md", []codeanchor.IntelDocSection{{
		SectionID:   "s1",
		Path:        "docs/spec.md",
		Title:       "Spec",
		Level:       1,
		Content:     "Calls A",
		Fingerprint: "fp-section",
	}}, []codeanchor.IntelEdge{{
		SrcID: "s1",
		DstID: "a1",
		Kind:  "mentions",
	}}, nil))

	scope := NewService(obsidian.VaultDefinition{Path: root}, nil, store, nil).NewScope(ScopeOptions{})
	withoutCalls, err := scope.GraphFacts(ctx, GraphFactsRequest{
		Paths:           []string{"docs/spec.md", "pkg/a.go", "pkg/b.go"},
		IncludeDocLinks: true,
		IncludeCode:     true,
	})
	require.NoError(t, err)
	require.Contains(t, graphFactPairKinds(withoutCalls.Edges), "note:docs/spec.md|code:pkg/a.go|mentions")
	require.NotContains(t, graphFactPairKinds(withoutCalls.Edges), "code:pkg/b.go|code:pkg/a.go|calls")

	withCalls, err := scope.GraphFacts(ctx, GraphFactsRequest{
		Paths:           []string{"docs/spec.md", "pkg/a.go", "pkg/b.go"},
		IncludeDocLinks: true,
		IncludeCode:     true,
		IncludeCalls:    true,
	})
	require.NoError(t, err)
	require.Contains(t, graphFactPairKinds(withCalls.Edges), "code:pkg/b.go|code:pkg/a.go|calls")
}
