package operations

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	vaultgraph "github.com/atomicobject/rhizome/pkg/graph"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type ontologyTextContext struct {
	Schema *ontoschema.Schema
	Store  *semdb.Store
	Scope  *noderead.Scope
	Issues []ontology.ValidationIssue
}

func loadOntologyTextContextFromProvider(ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, provider OntologyRuntimeProvider) (*ontologyTextContext, error) {
	runtime, err := provider.LoadOntologyRuntime(ctx)
	if runtime == nil || runtime.Store == nil {
		return nil, err
	}
	scope := noderead.NewService(vaultDef, noteMgr, runtime.Store, runtime.Schema).NewScope(noderead.ScopeOptions{})
	return &ontologyTextContext{
		Schema: runtime.Schema,
		Store:  runtime.Store,
		Scope:  scope,
		Issues: append([]ontology.ValidationIssue(nil), runtime.Issues...),
	}, err
}

func loadOntologyTextContext(vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, noteMetadata notemeta.Indexer, store *semdb.Store) (*ontologyTextContext, func(), error) {
	if err := noteMetadata.Validate(); err != nil {
		return nil, nil, err
	}
	if store != nil {
		runtime, err := ontology.PublishedRuntimeWithStore(context.Background(), noteMetadata, vaultDef, store)
		if runtime == nil || runtime.Store == nil {
			return nil, nil, err
		}
		scope := noderead.NewService(vaultDef, noteMgr, runtime.Store, runtime.Schema).NewScope(noderead.ScopeOptions{})
		return &ontologyTextContext{
			Schema: runtime.Schema,
			Store:  runtime.Store,
			Scope:  scope,
			Issues: append([]ontology.ValidationIssue(nil), runtime.Issues...),
		}, nil, err
	}
	runtime, cleanup, err := ontology.OpenPublishedRuntime(context.Background(), noteMetadata, vaultDef)
	if runtime == nil || runtime.Store == nil {
		return nil, cleanup, err
	}
	scope := noderead.NewService(vaultDef, noteMgr, runtime.Store, runtime.Schema).NewScope(noderead.ScopeOptions{})
	return &ontologyTextContext{
		Schema: runtime.Schema,
		Store:  runtime.Store,
		Scope:  scope,
		Issues: append([]ontology.ValidationIssue(nil), runtime.Issues...),
	}, cleanup, err
}

// loadIndexedOntologyTextContext creates only a request-scoped reader over the
// existing index. Authored SDL is presentation metadata only when the persisted
// ontology snapshot is ready and was materialized from that exact schema.
// Missing, invalid, or stale state degrades to graph-only context.
func loadIndexedOntologyTextContext(ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, noteMetadata notemeta.Indexer, store *semdb.Store) *ontologyTextContext {
	if store == nil {
		return nil
	}
	schema, err := ontoschema.LoadSchema(vaultDef.BasePath())
	if err != nil || schema == nil {
		return nil
	}
	formats, err := noteMetadata.FormatRuntime()
	if err != nil {
		return nil
	}
	readiness, err := store.GetOntologyReadiness(ctx, ontology.OntologyProviderHash(formats))
	state := readiness.SchemaState
	if err != nil ||
		!readiness.Ready ||
		state.SchemaHash != schema.Hash ||
		state.MaterializationVersion != ontology.OntologyMaterializationVersion {
		return nil
	}
	scope := noderead.NewService(vaultDef, noteMgr, store, schema).NewScope(noderead.ScopeOptions{})
	return &ontologyTextContext{Schema: schema, Store: store, Scope: scope}
}

// loadIndexedNoteGraphContext materializes only requested note rows and a
// bounded one-hop wikilink neighborhood. GraphDocScores owns degree/HITS data
// when present; SQL window counts preserve complete degrees for scoreless
// requested nodes without hydrating every incident edge.
func loadIndexedNoteGraphContext(ctx context.Context, store *semdb.Store, notePaths []string) (*vaultgraph.GraphAnalysis, map[string][]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if store == nil || len(notePaths) == 0 {
		return nil, nil, nil
	}
	notePaths = uniqueNormalizedStrings(notePaths)
	const neighborLimit = 5
	neighborhood, edgeErr := store.GraphDocNoteNeighborhoodForPaths(ctx, notePaths, neighborLimit)
	if edgeErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, nil, ctxErr
		}
		neighborhood = semdb.GraphDocNoteNeighborhood{
			Edges:   []semdb.GraphDocEdge{},
			Degrees: map[string]semdb.GraphDocDegree{},
		}
	}
	edges := neighborhood.Edges

	scorePaths := append([]string(nil), notePaths...)
	for _, edge := range edges {
		scorePaths = append(scorePaths, edge.SrcPath, edge.DstPath)
	}
	scores, scoreErr := store.GraphDocScoresByPaths(ctx, uniqueNormalizedStrings(scorePaths))
	if scoreErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, nil, ctxErr
		}
		scores = map[string]semdb.GraphDocScore{}
	}

	nodes := make(map[string]vaultgraph.GraphNode, len(scores))
	for path, score := range scores {
		kind := score.DocType
		if kind == "" {
			kind = "note"
		}
		nodes[path] = vaultgraph.GraphNode{
			Path:      path,
			Title:     paths.NoteTitle(path),
			Kind:      kind,
			Inbound:   score.Inbound,
			Outbound:  score.Outbound,
			Hub:       score.Hub,
			Authority: score.Authority,
			Community: score.Community,
		}
	}
	ensureNode := func(path string) {
		path = strings.TrimSpace(filepath.ToSlash(path))
		if path == "" {
			return
		}
		if _, ok := nodes[path]; ok {
			return
		}
		nodes[path] = vaultgraph.GraphNode{
			Path:  path,
			Title: paths.NoteTitle(path),
			Kind:  "note",
		}
	}
	for _, path := range notePaths {
		ensureNode(path)
	}
	applyIndexedGraphEdges(nodes, scores, edges, ensureNode)
	for path, degree := range neighborhood.Degrees {
		if _, hasScore := scores[path]; hasScore {
			continue
		}
		node := nodes[path]
		node.Inbound = degree.Inbound
		node.Outbound = degree.Outbound
		nodes[path] = node
	}
	if len(nodes) == 0 {
		return nil, nil, nil
	}
	analysis := &vaultgraph.GraphAnalysis{Nodes: nodes}
	return analysis, buildReverseNeighborsForText(nodes), nil
}

func applyIndexedGraphEdges(
	nodes map[string]vaultgraph.GraphNode,
	scores map[string]semdb.GraphDocScore,
	edges []semdb.GraphDocEdge,
	ensureNode func(string),
) {
	inbound := make(map[string]map[string]struct{})
	outbound := make(map[string]map[string]struct{})
	for _, edge := range edges {
		src := strings.TrimSpace(filepath.ToSlash(edge.SrcPath))
		dst := strings.TrimSpace(filepath.ToSlash(edge.DstPath))
		if src == "" || dst == "" || src == dst {
			continue
		}
		ensureNode(src)
		ensureNode(dst)
		if outbound[src] == nil {
			outbound[src] = make(map[string]struct{})
		}
		outbound[src][dst] = struct{}{}
		if inbound[dst] == nil {
			inbound[dst] = make(map[string]struct{})
		}
		inbound[dst][src] = struct{}{}
	}
	for path, node := range nodes {
		node.Neighbors = make([]string, 0, len(outbound[path]))
		for neighbor := range outbound[path] {
			node.Neighbors = append(node.Neighbors, neighbor)
		}
		sort.Strings(node.Neighbors)
		if _, hasScore := scores[path]; !hasScore {
			node.Inbound = len(inbound[path])
			node.Outbound = len(outbound[path])
		}
		nodes[path] = node
	}
}

func uniqueNormalizedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(filepath.ToSlash(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
