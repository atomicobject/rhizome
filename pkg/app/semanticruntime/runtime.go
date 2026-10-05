package semanticruntime

// Docs:
// - [[semantic-runtime-lane-policy#^spec-0044-compatibility-invariants]]
// - [[semantic-runtime-lane-policy#^spec-0044-lane-compatibility-key]]
// - [[indexing-pipeline-architecture#^spec-0012-us1-ac2]]
// - [[indexing-workflow#^spec-0036-us1-ac3]]

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	searchintent "github.com/atomicobject/rhizome/pkg/search/intent"
	"github.com/atomicobject/rhizome/pkg/search/intentstore"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
)

type LaneKind string

const (
	// LaneKindCode identifies code embedding work and owns intent metrics when
	// both code and note lanes exist.
	LaneKindCode LaneKind = "code"
	// LaneKindNote identifies note and ontology primary-chunk embedding
	// work.
	LaneKindNote LaneKind = "note"
)

// LaneKey is the compatibility identity for sharing one embedding node.
type LaneKey struct {
	Provider   string
	Model      string
	Endpoint   string
	Dimensions int
}

// LaneRequest describes a semantic embedding lane request from an indexing
// phase. Compatible requests coalesce; incompatible requests get separate nodes.
type LaneRequest struct {
	Kind          LaneKind
	Provider      embeddings.Provider
	ProviderInfo  embeddings.ProviderConfig
	MaxConcurrent int
	BatchSize     int
	EmbedGate     chan struct{}
	Packer        semantic.EmbedPackerOptions
}

// Lane is a configured provider execution lane with its shared embedding node.
type Lane struct {
	Kind          LaneKind
	Key           LaneKey
	Provider      embeddings.Provider
	ProviderInfo  embeddings.ProviderConfig
	MaxConcurrent int
	BatchSize     int
	EmbedGate     chan struct{}
	Packer        semantic.EmbedPackerOptions
	Node          *semantic.SharedEmbeddingNode
}

// Runtime owns all semantic embedding lanes for one indexing run.
type Runtime struct {
	mu         sync.Mutex
	lanes      map[LaneKey]*Lane
	codeLane   *Lane
	noteLane   *Lane
	closed     bool
	closeOrder []*Lane
}

// New constructs an empty semantic runtime.
func New() *Runtime {
	return &Runtime{lanes: make(map[LaneKey]*Lane)}
}

// KeyFor derives the provider compatibility key used to decide whether code,
// note, ontology, and intent work may share one node.
func KeyFor(provider embeddings.Provider, info embeddings.ProviderConfig) LaneKey {
	dim := info.Dimensions
	if provider != nil {
		dim = provider.Dimensions()
	}
	return LaneKey{
		Provider:   strings.ToLower(strings.TrimSpace(info.Provider)),
		Model:      strings.TrimSpace(info.Model),
		Endpoint:   strings.TrimRight(strings.TrimSpace(info.Endpoint), "/"),
		Dimensions: dim,
	}
}

// EnsureLane returns a compatible existing lane or creates a new shared node.
// Existing lanes are promoted to the largest compatible throughput policy while
// respecting any explicit batch-size cap.
func (r *Runtime) EnsureLane(ctx context.Context, req LaneRequest) (*Lane, error) {
	if r == nil {
		return nil, errors.New("semantic runtime is nil")
	}
	if req.Provider == nil {
		return nil, errors.New("semantic runtime lane requires provider")
	}
	key := KeyFor(req.Provider, req.ProviderInfo)
	packer := semantic.ApplyProviderPackerLimits(req.Provider, req.Packer)
	packer = applyExplicitBatchSize(packer, req.BatchSize)
	maxConcurrent := semantic.EffectiveMaxConcurrent(req.Provider, req.MaxConcurrent)
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errors.New("semantic runtime is closed")
	}
	if lane := r.lanes[key]; lane != nil {
		// WHY: compatible code/note/ontology work should converge on one
		// shared embedding node so provider packing and timings stay coherent;
		// see [[semantic-runtime-lane-policy#^spec-0044-compatibility-invariants]].
		if req.BatchSize > 0 && (lane.BatchSize == 0 || req.BatchSize < lane.BatchSize) {
			lane.BatchSize = req.BatchSize
		}
		lane.MaxConcurrent = max(lane.MaxConcurrent, maxConcurrent)
		lane.Packer = mergeLanePacker(lane.Packer, packer)
		lane.Packer = applyExplicitBatchSize(lane.Packer, lane.BatchSize)
		if lane.Node != nil {
			lane.Node.Configure(lane.MaxConcurrent, lane.Packer)
		}
		r.assignKind(req.Kind, lane)
		return lane, nil
	}

	lane := &Lane{
		Kind:          req.Kind,
		Key:           key,
		Provider:      req.Provider,
		ProviderInfo:  req.ProviderInfo,
		MaxConcurrent: maxConcurrent,
		BatchSize:     req.BatchSize,
		EmbedGate:     req.EmbedGate,
		Packer:        packer,
	}
	lane.Node = semantic.NewSharedEmbeddingNode(ctx, req.Provider, maxConcurrent, req.EmbedGate, packer)
	r.lanes[key] = lane
	r.closeOrder = append(r.closeOrder, lane)
	r.assignKind(req.Kind, lane)
	return lane, nil
}

func applyExplicitBatchSize(packer semantic.EmbedPackerOptions, batchSize int) semantic.EmbedPackerOptions {
	if batchSize <= 0 {
		return packer
	}
	if packer.MaxTexts <= 0 || batchSize < packer.MaxTexts {
		packer.MaxTexts = batchSize
	}
	if packer.MinTexts > packer.MaxTexts {
		packer.MinTexts = packer.MaxTexts
	}
	if packer.AdaptiveMinTexts > packer.MaxTexts {
		packer.AdaptiveMinTexts = packer.MaxTexts
	}
	return packer
}

func mergeLanePacker(existing, incoming semantic.EmbedPackerOptions) semantic.EmbedPackerOptions {
	if incoming.MinTexts > existing.MinTexts {
		existing.MinTexts = incoming.MinTexts
	}
	if incoming.AdaptiveMinTexts > existing.AdaptiveMinTexts {
		existing.AdaptiveMinTexts = incoming.AdaptiveMinTexts
	}
	if incoming.MaxTexts > existing.MaxTexts {
		existing.MaxTexts = incoming.MaxTexts
	}
	if incoming.MaxBytes > existing.MaxBytes {
		existing.MaxBytes = incoming.MaxBytes
	}
	if existing.MaxWait <= 0 || (incoming.MaxWait > 0 && incoming.MaxWait > existing.MaxWait) {
		existing.MaxWait = incoming.MaxWait
	}
	return existing
}

func (r *Runtime) assignKind(kind LaneKind, lane *Lane) {
	switch kind {
	case LaneKindCode:
		if r.codeLane == nil {
			r.codeLane = lane
		}
	case LaneKindNote:
		if r.noteLane == nil {
			r.noteLane = lane
		}
	}
}

// CodeLane returns the first lane registered for code work.
func (r *Runtime) CodeLane() *Lane {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.codeLane
}

// NoteLane returns the first lane registered for note work.
func (r *Runtime) NoteLane() *Lane {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.noteLane
}

// IntentLane selects the lane used for intent exemplar embeddings. Code wins
// because answer/search planning usually consumes code-intent signals alongside
// code embedding diagnostics.
func (r *Runtime) IntentLane() *Lane {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.codeLane != nil {
		return r.codeLane
	}
	return r.noteLane
}

// SyncIntentExemplars embeds the small built-in intent corpus through an
// existing shared lane so it does not create a one-off provider batcher.
func (r *Runtime) SyncIntentExemplars(ctx context.Context, store intentstore.Store) error {
	if store == nil {
		return nil
	}
	lane := r.IntentLane()
	if lane == nil || lane.Node == nil {
		return errors.New("semantic runtime has no shared lane for intent exemplars")
	}
	phase := r.intentMetricPhase(lane)
	laneCtx := indexingperf.WithPhase(ctx, phase)
	return searchintent.SyncEmbeddingsWithOptions(laneCtx, lane.Provider, store, searchintent.DefaultExemplars(), searchintent.SyncOptions{
		Node:         lane.Node,
		Phase:        phase,
		ProviderInfo: lane.ProviderInfo,
	})
}

func (r *Runtime) intentMetricPhase(lane *Lane) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if lane != nil && r.codeLane == lane {
		return "embed_code"
	}
	return "embed_notes"
}

func (r *Runtime) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	lanes := append([]*Lane(nil), r.closeOrder...)
	r.mu.Unlock()
	for i := len(lanes) - 1; i >= 0; i-- {
		if lanes[i] != nil && lanes[i].Node != nil {
			lanes[i].Node.Close()
		}
	}
}
