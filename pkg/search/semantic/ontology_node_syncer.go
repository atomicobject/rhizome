package semantic

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type OntologyNodeStore interface {
	ReplaceIntelChunks(context.Context, []string, []codeanchor.IntelChunk) error
	UpsertEmbeddings(context.Context, map[string]embeddings.Embedding) error
	UpsertOntologyNodeEmbeddingStates(context.Context, []codeanchor.IntelOntologyNodeEmbeddingState) error
	OntologyNodeEmbeddingStatesByChunkIDs(context.Context, []string) (map[string]codeanchor.IntelOntologyNodeEmbeddingState, error)
	IntelChunksByOwners(context.Context, []string) ([]codeanchor.IntelChunk, error)
	OntologyNodesByPaths(context.Context, []string) ([]codeanchor.IntelOntologyNode, error)
}

// OntologyNodeSyncer projects typed notes into ontology-node chunks, embeds only
// changed primary body chunks, and writes node/chunk/embedding state back through
// the supplied store.
type OntologyNodeSyncer struct {
	Store         OntologyNodeStore
	Provider      embeddings.Provider
	ProviderInfo  embeddings.ProviderConfig
	Schema        *ontology.Schema
	BatchSize     int
	MaxConcurrent int
	EmbedGate     chan struct{}
	EmbeddingNode *SharedEmbeddingNode
}

// SyncProjections persists ontology node projections and embeds chunks whose
// provider/schema/content fingerprint has changed.
func (s OntologyNodeSyncer) SyncProjections(ctx context.Context, projections ...*ontology.NodeProjection) error {
	plan, err := s.PrepareProjections(ctx, projections...)
	if err != nil {
		return err
	}
	if plan.chunksChanged {
		if err := runOntologyNodeWritePhase(ctx, "writeback_ontology_nodes", func(ctx context.Context) error {
			return s.Store.ReplaceIntelChunks(ctx, plan.owners, plan.chunks)
		}); err != nil {
			return err
		}
		plan.chunksChanged = false
	}
	result, err := s.ComputeDerived(ctx, plan)
	if err != nil {
		return err
	}
	return s.PublishDerived(ctx, result)
}

func (s OntologyNodeSyncer) PrepareProjections(ctx context.Context, projections ...*ontology.NodeProjection) (OntologyDerivedPlan, error) {
	if s.Store == nil {
		return OntologyDerivedPlan{}, errors.New("ontology node syncer requires store")
	}
	if s.Provider == nil {
		return OntologyDerivedPlan{}, errors.New("ontology node syncer requires provider")
	}
	if s.Schema == nil {
		return OntologyDerivedPlan{}, errors.New("ontology node syncer requires schema")
	}
	combined := OntologyNodeChunkSet{Texts: map[string]string{}}
	now := int64(0)
	for _, projection := range projections {
		if fallback, ok := ontology.AsFallbackNoteProjection(projection); ok {
			// IMPORTANT: fallback projections are internal semantic coverage for
			// genuinely untyped notes. They should behave like ontology-node
			// chunks for retrieval without creating a public ontology type.
			projection = fallback
		}
		set, err := s.chunkSetForProjection(projection, now)
		if err != nil {
			return OntologyDerivedPlan{}, err
		}
		combined.NotePaths = append(combined.NotePaths, set.NotePaths...)
		combined.Nodes = append(combined.Nodes, set.Nodes...)
		combined.FieldValues = append(combined.FieldValues, set.FieldValues...)
		combined.Chunks = append(combined.Chunks, set.Chunks...)
		combined.States = append(combined.States, set.States...)
		for id, text := range set.Texts {
			combined.Texts[id] = text
		}
	}
	combined.NotePaths = uniqueSortedNonEmpty(combined.NotePaths)
	if len(combined.NotePaths) == 0 {
		return OntologyDerivedPlan{}, nil
	}
	ownerIDs := make([]string, 0, len(combined.Nodes))
	for _, node := range combined.Nodes {
		ownerIDs = append(ownerIDs, node.NodeID)
	}
	ownerIDs = uniqueSortedNonEmpty(ownerIDs)
	// WHY: ontology_nodes / field rows are owned by the catalog sync
	// (BuildIntelOntologyNodeReadModel). Writing them here would re-issue
	// per-path destructive Replace calls and wipe rows the catalog already
	// installed (notably embedded children of untyped notes that match a
	// global @source).
	existingChunks, err := s.Store.IntelChunksByOwners(ctx, ownerIDs)
	if err != nil {
		return OntologyDerivedPlan{}, fmt.Errorf("load ontology node chunks: %w", err)
	}
	chunksChanged := !sameOntologyNodeChunks(combined.Chunks, existingChunks)
	indexingperf.AddCount(ctx, "ontology_nodes.body_chunks_planned", int64(countOntologyNodeChunksByGranularity(combined.Chunks, GranularityOntologyNodeBody)))
	indexingperf.AddCount(ctx, "ontology_body.chunks_planned", int64(countOntologyNodeChunksByGranularity(combined.Chunks, GranularityOntologyNodeBody)))

	chunkIDs := make([]string, 0, len(combined.States))
	for _, state := range combined.States {
		chunkIDs = append(chunkIDs, state.ChunkID)
	}
	prev, err := s.Store.OntologyNodeEmbeddingStatesByChunkIDs(ctx, chunkIDs)
	if err != nil {
		return OntologyDerivedPlan{}, fmt.Errorf("load ontology node embedding states: %w", err)
	}
	toEmbed := make([]string, 0)
	var nextStates []codeanchor.IntelOntologyNodeEmbeddingState
	for _, state := range combined.States {
		if !sameOntologyNodeEmbeddingState(prev[state.ChunkID], state) {
			toEmbed = append(toEmbed, state.ChunkID)
			nextStates = append(nextStates, state)
		}
	}
	indexingperf.AddCount(ctx, "ontology_nodes.chunks_reused", int64(len(combined.States)-len(nextStates)))
	indexingperf.AddCount(ctx, "ontology_body.chunks_reused", int64(len(combined.States)-len(nextStates)))
	sort.Strings(toEmbed)
	texts := make([]string, 0, len(toEmbed))
	for _, id := range toEmbed {
		texts = append(texts, combined.Texts[id])
	}
	return OntologyDerivedPlan{owners: ownerIDs, chunks: combined.Chunks, chunksChanged: chunksChanged, ids: toEmbed, texts: texts, states: nextStates}, nil
}

func (s OntologyNodeSyncer) chunkSetForProjection(projection *ontology.NodeProjection, now int64) (OntologyNodeChunkSet, error) {
	if projection == nil || projection.RootSnapshot == nil || projection.Snapshot != nil {
		return BuildOntologyNodeChunks(s.Schema, projection, s.ProviderInfo, now)
	}
	if strings.TrimSpace(projection.ResolvedType) == "" {
		return OntologyNodeChunkSet{Texts: map[string]string{}}, nil
	}
	owner, err := ontology.IntelOntologyNodeForProjection(s.Schema, projection, nil, now)
	if err != nil {
		return OntologyNodeChunkSet{}, err
	}
	set := BuildRootEvidenceChunks(projection.RootSnapshot, owner, s.ProviderInfo, defaultSectionMaxBytes, now)
	// Keep the owner in the replacement set when the provider emits no usable
	// regions so a later empty projection removes stale evidence.
	set.NotePaths = uniqueSortedNonEmpty(append(set.NotePaths, projection.Ref.NotePath))
	if len(set.Nodes) == 0 {
		set.Nodes = []codeanchor.IntelOntologyNode{owner}
	}
	return set, nil
}

func countOntologyNodeChunksByGranularity(chunks []codeanchor.IntelChunk, granularity string) int {
	count := 0
	for _, chunk := range chunks {
		if chunk.Granularity == granularity {
			count++
		}
	}
	return count
}

func runOntologyNodeWritePhase(ctx context.Context, phase string, fn func(context.Context) error) error {
	return fn(indexingperf.WithPhase(ctx, phase))
}

func sameOntologyNodeChunks(expected, actual []codeanchor.IntelChunk) bool {
	expected = sortedOntologyChunks(expected)
	actual = sortedOntologyChunks(actual)
	if len(expected) != len(actual) {
		return false
	}
	for i := range expected {
		if !sameOntologyNodeChunk(expected[i], actual[i]) {
			return false
		}
	}
	return true
}

func sortedOntologyChunks(chunks []codeanchor.IntelChunk) []codeanchor.IntelChunk {
	out := append([]codeanchor.IntelChunk(nil), chunks...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].OwnerID != out[j].OwnerID {
			return out[i].OwnerID < out[j].OwnerID
		}
		if out[i].Ord != out[j].Ord {
			return out[i].Ord < out[j].Ord
		}
		return out[i].ChunkID < out[j].ChunkID
	})
	return out
}

func sameOntologyNodeChunk(a, b codeanchor.IntelChunk) bool {
	return a.ChunkID == b.ChunkID &&
		a.OwnerID == b.OwnerID &&
		a.OwnerType == b.OwnerType &&
		normalizeOntologyNodeChunkFamily(a.ChunkFamily) == normalizeOntologyNodeChunkFamily(b.ChunkFamily) &&
		a.Ord == b.Ord &&
		a.Granularity == b.Granularity &&
		a.Breadcrumb == b.Breadcrumb &&
		a.Heading == b.Heading &&
		a.ContentHash == b.ContentHash &&
		a.StartByte == b.StartByte &&
		a.EndByte == b.EndByte
}

func normalizeOntologyNodeChunkFamily(family string) string {
	if strings.TrimSpace(family) == "" {
		return codeanchor.IntelChunkFamilyDefault
	}
	return strings.TrimSpace(family)
}

func (s OntologyNodeSyncer) SyncNotePaths(ctx context.Context, vaultDef obsidian.VaultDefinition, noteReader obsidian.NoteReader, paths []string, deletedPaths []string) error {
	if s.Store == nil {
		return errors.New("ontology node syncer requires store")
	}
	if s.Provider == nil {
		return errors.New("ontology node syncer requires provider")
	}
	if s.Schema == nil {
		return errors.New("ontology node syncer requires schema")
	}
	if len(deletedPaths) > 0 {
		if err := s.pruneChunksForPaths(ctx, deletedPaths); err != nil {
			return fmt.Errorf("prune deleted ontology node chunks: %w", err)
		}
	}
	paths = uniqueSortedNonEmpty(paths)
	if len(paths) == 0 {
		return nil
	}
	workerCount := runtime.GOMAXPROCS(0)
	if workerCount < 1 {
		workerCount = 1
	}
	if workerCount > len(paths) {
		workerCount = len(paths)
	}
	pathCh := make(chan string, workerCount)
	projCh := make(chan *ontology.NodeProjection, len(paths))
	pruneCh := make(chan string, len(paths))
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range pathCh {
				projection, err := ontology.ProjectNote(ctx, vaultDef, noteReader, s.Schema, path)
				if err != nil {
					select {
					case errCh <- err:
					default:
					}
					continue
				}
				if strings.TrimSpace(projection.ResolvedType) == "" {
					if fallback, ok := ontology.AsFallbackNoteProjection(projection); ok {
						indexingperf.AddCount(ctx, "ontology_nodes.fallback_projections_planned", 1)
						indexingperf.AddCount(ctx, "ontology_body.fallback_paths", 1)
						projCh <- fallback
					} else {
						pruneCh <- path
					}
					continue
				}
				indexingperf.AddCount(ctx, "ontology_nodes.typed_projections_planned", 1)
				indexingperf.AddCount(ctx, "ontology_body.typed_paths", 1)
				projCh <- projection
			}
		}()
	}
	for _, path := range paths {
		select {
		case err := <-errCh:
			close(pathCh)
			wg.Wait()
			close(projCh)
			return err
		case pathCh <- path:
		}
	}
	close(pathCh)
	wg.Wait()
	close(projCh)
	close(pruneCh)
	select {
	case err := <-errCh:
		return err
	default:
	}
	var prunePaths []string
	for path := range pruneCh {
		prunePaths = append(prunePaths, path)
	}
	if len(prunePaths) > 0 {
		if err := s.pruneChunksForPaths(ctx, prunePaths); err != nil {
			return fmt.Errorf("prune unresolved ontology node chunks: %w", err)
		}
	}
	projections := make([]*ontology.NodeProjection, 0, len(projCh))
	for projection := range projCh {
		projections = append(projections, projection)
	}
	return s.SyncProjections(ctx, projections...)
}

func (s OntologyNodeSyncer) pruneChunksForPaths(ctx context.Context, paths []string) error {
	paths = uniqueSortedNonEmpty(paths)
	if len(paths) == 0 {
		return nil
	}
	nodes, err := s.Store.OntologyNodesByPaths(ctx, paths)
	if err != nil {
		return err
	}
	ownerIDs := make([]string, 0, len(nodes))
	for _, node := range nodes {
		ownerIDs = append(ownerIDs, node.NodeID)
	}
	ownerIDs = uniqueSortedNonEmpty(ownerIDs)
	if len(ownerIDs) == 0 {
		return nil
	}
	return s.Store.ReplaceIntelChunks(ctx, ownerIDs, nil)
}

func sameOntologyNodeEmbeddingState(a, b codeanchor.IntelOntologyNodeEmbeddingState) bool {
	return strings.TrimSpace(a.ChunkID) != "" &&
		a.NodeID == b.NodeID &&
		a.NotePath == b.NotePath &&
		a.TypeName == b.TypeName &&
		a.NodeKind == b.NodeKind &&
		a.EmbeddingSchemaSignature == b.EmbeddingSchemaSignature &&
		a.NodeStructureFingerprint == b.NodeStructureFingerprint &&
		a.SourceContentHash == b.SourceContentHash &&
		a.ChunkTextHash == b.ChunkTextHash &&
		a.ChunkGranularity == b.ChunkGranularity &&
		a.Provider == b.Provider &&
		a.Model == b.Model
}
