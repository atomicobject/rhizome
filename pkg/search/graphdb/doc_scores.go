// Docs: [Indexing pipeline - Graph signals (doc scores + anchor PageRank)](docs/reference/analysis/Indexing pipeline - Graph signals (doc scores + anchor PageRank).md)
package graphdb

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search/graphalg"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type DocScoresOptions struct {
	WikilinkOptions obsidian.WikilinkOptions
	// VaultRoot converts absolute doc paths to vault-relative paths when possible.
	VaultRoot string
}

const (
	callWeightCap     = 4.0
	crossRefWeightCap = 3.0
)

// UpsertNoteWikilinkEdges is retained for compatibility with older watcher
// callers. Note metadata projection is now the sole writer for note-link
// edges, so this function must not reparse the supplied content.
func UpsertNoteWikilinkEdges(ctx context.Context, store *semdb.Store, notePath string, content string, opts DocScoresOptions) error {
	_ = ctx
	_ = store
	_ = notePath
	_ = content
	_ = opts
	return nil
}

// ComputeDocScores builds a lightweight doc graph from intel tables and computes:
// - HITS hub/authority scores (directed)
// - Label-propagation communities (undirected view)
//
// Nodes are doc paths (notes + code files). Edges include:
// - note -> note (provider-projected links persisted in graph_doc_edges)
// - note -> code (mentions edges: doc sections mentioning code anchors)
// - code -> note (coderefs stored in doc_links)
// - code -> code (call edges from intel_edges(kind='calls'))
//
// Edge selection:
//   - Note->note edges use persisted current provider facts from graph_doc_edges.
//   - Mention edges come from intel_edges(kind='mentions') joined to paths.
//   - Coderef edges come from doc_links(src_type='code', dst_kind='note').
//
// The resulting scores are persisted via Store.ReplaceGraphDocScores and used by graph-aware
// rankers (e.g., GraphDocScoreRanker) to boost important documents.
//
// See [Indexing pipeline - Graph signals (doc scores + anchor PageRank)](docs/reference/analysis/Indexing pipeline - Graph signals (doc scores + anchor PageRank).md) for the full
// design and update paths.
func ComputeDocScores(ctx context.Context, store *semdb.Store, opts DocScoresOptions) ([]semdb.GraphDocScore, error) {
	if store == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	vaultPaths, _ := paths.NewVaultPaths(opts.VaultRoot)
	normalizeDocPath := vaultPaths.NormalizeDocPath

	sections, err := store.IntelDocSections(ctx)
	if err != nil {
		return nil, err
	}
	metadataRows, err := store.CurrentNoteMetadataRows(ctx)
	if err != nil {
		return nil, err
	}
	currentNotes := make(map[string]struct{}, len(metadataRows))
	for _, row := range metadataRows {
		if !currentPersistedNoteProjection(row) {
			continue
		}
		path := normalizeDocPath(row.Path)
		if path != "" {
			currentNotes[path] = struct{}{}
		}
	}

	noteSet := make(map[string]struct{})
	docTypes := make(map[string]string)
	for _, s := range sections {
		path := normalizeDocPath(s.Path)
		if _, current := currentNotes[path]; !current {
			continue
		}
		noteSet[path] = struct{}{}
		docTypes[path] = "note"
	}
	notePaths := make([]string, 0, len(noteSet))
	for p := range noteSet {
		notePaths = append(notePaths, p)
	}
	sort.Strings(notePaths)

	adjacency := make(map[string]map[string]float64)
	// hitsAdjacency only includes doc-domain edges (wikilinks, mentions, coderefs).
	// Code→code edges are excluded because HITS hub/authority semantics apply to
	// knowledge curation (notes linking to notes), not structural code coupling.
	hitsAdjacency := make(map[string]map[string]struct{})
	ensureNode := func(node string) {
		if node == "" {
			return
		}
		if _, ok := adjacency[node]; !ok {
			adjacency[node] = make(map[string]float64)
		}
		if _, ok := hitsAdjacency[node]; !ok {
			hitsAdjacency[node] = make(map[string]struct{})
		}
	}
	addWeight := func(src, dst string, weight float64, isDocDomain bool) {
		if src == "" || dst == "" || src == dst || weight <= 0 {
			return
		}
		ensureNode(src)
		ensureNode(dst)
		adjacency[src][dst] += weight
		if isDocDomain {
			hitsAdjacency[src][dst] = struct{}{}
		}
	}

	for _, p := range notePaths {
		ensureNode(p)
	}
	codePaths, err := store.IndexedFilePaths(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range codePaths {
		path := normalizeDocPath(p)
		ensureNode(path)
		docTypes[path] = "code"
	}

	readScope := noderead.NewService(obsidian.VaultDefinition{Path: opts.VaultRoot}, &obsidian.Note{}, store, nil).NewScope(ctx, noderead.ScopeOptions{})
	facts, err := readScope.GraphFacts(ctx, noderead.GraphFactsRequest{
		IncludeOntology: true,
		IncludeDocLinks: true,
		IncludeCode:     true,
		IncludeCalls:    true,
		IncludeEmbedded: true,
		NodeLimit:       -1,
		EdgeLimit:       -1,
	})
	if err != nil {
		return nil, err
	}
	for _, fact := range facts.Edges {
		src := graphScoreDocNode(fact.SourceKind, fact.SourcePath, fact.SourceRef.NotePath, normalizeDocPath)
		dst := graphScoreDocNode(fact.TargetKind, fact.TargetPath, fact.TargetRef.NotePath, normalizeDocPath)
		if src == "" || dst == "" {
			continue
		}
		if graphScoreNoteEndpoint(fact.SourceKind) {
			if _, current := currentNotes[src]; !current {
				continue
			}
		}
		if graphScoreNoteEndpoint(fact.TargetKind) {
			if _, current := currentNotes[dst]; !current {
				continue
			}
		}
		isDocDomain := fact.Kind != "calls"
		weight := graphScoreFactWeight(fact)
		addWeight(src, dst, weight, isDocDomain)
		if docType := graphScoreDocType(fact.SourceKind); docType != "" {
			docTypes[src] = docType
		}
		if docType := graphScoreDocType(fact.TargetKind); docType != "" {
			docTypes[dst] = docType
		}
	}

	hits := graphalg.ComputeHITS(hitsAdjacency)
	communities := weightedLabelPropagation(adjacency)

	// Compute in/out degree from the full adjacency (including code edges) for diagnostics.
	inDeg := make(map[string]int, len(adjacency))
	outDeg := make(map[string]int, len(adjacency))
	for src, dsts := range adjacency {
		outDeg[src] = len(dsts)
		if _, ok := inDeg[src]; !ok {
			inDeg[src] = 0
		}
		for dst := range dsts {
			inDeg[dst]++
		}
	}

	ts := time.Now().Unix()
	out := make([]semdb.GraphDocScore, 0, len(adjacency))
	for node := range adjacency {
		docType := docTypes[node]
		if docType == "" {
			continue
		}
		comm := communities[node]
		if comm == "" {
			comm = node
		}
		out = append(out, semdb.GraphDocScore{
			DocPath:   node,
			DocType:   docType,
			Hub:       hits.Hubs[node],
			Authority: hits.Authorities[node],
			Community: comm,
			Inbound:   inDeg[node],
			Outbound:  outDeg[node],
			UpdatedAt: ts,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DocPath < out[j].DocPath })
	return out, nil
}

func currentPersistedNoteProjection(row semdb.NoteMetadataRow) bool {
	return row.Projection.Status == semdb.NoteProjectionStatusCurrent &&
		row.Projection.SourceContentHash != "" &&
		row.Projection.SourceContentHash == row.ContentHash
}

func graphScoreNoteEndpoint(kind noderead.GraphEndpointKind) bool {
	switch kind {
	case noderead.GraphEndpointNote, noderead.GraphEndpointEmbedded, noderead.GraphEndpointSection:
		return true
	default:
		return false
	}
}

func cappedLogWeight(count int, cap float64) float64 {
	if count <= 0 {
		return 0
	}
	weight := 1 + math.Log1p(float64(count))
	if cap > 0 && weight > cap {
		return cap
	}
	return weight
}

func graphScoreDocNode(kind noderead.GraphEndpointKind, path, notePath string, normalize func(string) string) string {
	switch kind {
	case noderead.GraphEndpointNote:
		return normalize(path)
	case noderead.GraphEndpointEmbedded, noderead.GraphEndpointSection:
		return normalize(notePath)
	case noderead.GraphEndpointCode:
		return normalize(path)
	default:
		return ""
	}
}

func graphScoreDocType(kind noderead.GraphEndpointKind) string {
	switch kind {
	case noderead.GraphEndpointNote, noderead.GraphEndpointEmbedded, noderead.GraphEndpointSection:
		return "note"
	case noderead.GraphEndpointCode:
		return "code"
	default:
		return ""
	}
}

func graphScoreFactWeight(fact noderead.GraphFactEdge) float64 {
	weight := fact.Weight
	if weight <= 0 {
		weight = 1
	}
	confidence := fact.Confidence
	if confidence <= 0 {
		confidence = 1
	}
	switch strings.ToLower(strings.TrimSpace(fact.Kind)) {
	case "mentions", "coderef":
		return cappedLogWeight(int(weight), crossRefWeightCap) * confidence
	case "calls":
		return cappedLogWeight(int(weight), callWeightCap) * confidence
	case string(noderead.GraphEdgeKindOntology):
		if fact.Structural {
			return 1.5 * confidence
		}
		return 1.0 * confidence
	case string(noderead.GraphEdgeKindEmbeds):
		return 0.5 * confidence
	default:
		return weight * confidence
	}
}

func weightedLabelPropagation(adjacency map[string]map[string]float64) map[string]string {
	if len(adjacency) == 0 {
		return map[string]string{}
	}
	nodes := make([]string, 0, len(adjacency))
	for node := range adjacency {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)

	labels := make(map[string]string, len(nodes))
	for _, node := range nodes {
		labels[node] = node
	}

	undirected := make(map[string]map[string]float64, len(adjacency))
	for _, node := range nodes {
		undirected[node] = make(map[string]float64)
	}
	for src, dsts := range adjacency {
		for dst, w := range dsts {
			if w <= 0 {
				continue
			}
			undirected[src][dst] += w
			undirected[dst][src] += w
		}
	}

	degree := make(map[string]float64, len(undirected))
	for node, dsts := range undirected {
		sum := 0.0
		for _, w := range dsts {
			sum += w
		}
		degree[node] = sum
	}

	const iterations = 20
	const epsilon = 1e-9
	for i := 0; i < iterations; i++ {
		changed := 0
		for _, node := range nodes {
			bestLabel := labels[node]
			bestWeight := -1.0
			labelWeights := make(map[string]float64)
			for neighbor, w := range undirected[node] {
				if w <= 0 {
					continue
				}
				denom := math.Sqrt(degree[node] * degree[neighbor])
				if denom > 0 {
					w = w / denom
				}
				label := labels[neighbor]
				labelWeights[label] += w
			}
			for label, weight := range labelWeights {
				if weight > bestWeight+epsilon || (math.Abs(weight-bestWeight) <= epsilon && label < bestLabel) {
					bestLabel = label
					bestWeight = weight
				}
			}
			if bestLabel != labels[node] {
				labels[node] = bestLabel
				changed++
			}
		}
		if changed == 0 {
			break
		}
	}
	return labels
}
