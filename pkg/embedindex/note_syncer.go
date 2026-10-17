package embedindex

import (
	"context"
	"errors"

	"sync"

	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// NoteIntelSource provides access to intel doc sections for semantic note embedding sync.
type NoteIntelSource interface {
	IntelDocSections(ctx context.Context) ([]codeanchor.IntelDocSection, error)
}

// NoteIntelMetaSource provides lightweight intel doc section metadata for fast planning.
type NoteIntelMetaSource interface {
	IntelDocSectionMetas(ctx context.Context) ([]codeanchor.IntelDocSectionMeta, error)
}

type noteIntelByPath interface {
	IntelDocSectionsByPath(ctx context.Context, path string) ([]codeanchor.IntelDocSection, error)
}

type noteChunkHashesBatch interface {
	ChunkHashesBatch(ctx context.Context, ids []embeddings.NoteID) (map[embeddings.NoteID]map[int]string, error)
}

// RawNoteEmbeddingEligibilitySource decides whether paths should receive raw
// authored-section embeddings. In ontology-ready repos this should return
// false for all ontology-owned notes so ontology-node chunks become the
// primary semantic note surface for typed and untyped notes.
type RawNoteEmbeddingEligibilitySource interface {
	RawNoteEmbeddingEligibility(ctx context.Context, paths []string) (RawNoteEmbeddingEligibility, error)
}

type RawNoteEmbeddingEligibility struct {
	RawEligible map[string]bool
	TypedPaths  []string
}

// NoteMetaWriteQueue is the note-side subset of the indexing queued writer.
type NoteMetaWriteQueue interface {
	SubmitNoteMeta(context.Context, embeddings.NoteFileInfo) error
	SubmitNoteChunkSync(context.Context, embeddings.NoteChunkSync) error
	SubmitIntelChunks(context.Context, []string, []codeanchor.IntelChunk) error
	SubmitIntelChunksByFamily(context.Context, []string, string, []codeanchor.IntelChunk) error
	SubmitIntelEmbeddings(context.Context, map[string]embeddings.Embedding) error
	FlushAndWait(context.Context) error
}

// NoteSyncer keeps the note embeddings index in sync with intel doc sections.
//
// This uses the markdown intel parser as the single source of raw chunk
// boundaries. In ontology-ready runs, RawEligibility normally suppresses raw
// doc_section embeddings and ontology-node sync owns the primary note surface.
type NoteSyncer struct {
	Index          embeddings.Index
	Provider       embeddings.Provider
	ProviderInfo   embeddings.ProviderConfig
	Intel          NoteIntelSource
	NoteReader     obsidian.NoteReader
	BatchSize      int
	MaxConcurrent  int
	PlanConcurrent int
	// EmbedGate optionally limits aggregate EmbedTexts concurrency across syncers.
	EmbedGate     chan struct{}
	EmbeddingNode *SharedEmbeddingNode
	// NoteEmbedPacker configures staged cross-note request packing.
	NoteEmbedPacker *EmbedPackerOptions
	// MaxSectionBytes caps per-section chunk embedding text (0 uses default).
	MaxSectionBytes int
	OnProgress      func(format string, args ...any)
	// OnEmbedProgress reports embedding progress as completed/total chunks.
	OnEmbedProgress func(done, total int)

	// StaleNoteThreshold controls lazy pruning of notes not seen in current sync.
	// If the fraction of stale notes is below this threshold, they're preserved
	// to avoid re-embedding costs when switching branches. Default is 0.3 (30%).
	// Set to 0 for immediate pruning (old behavior).
	StaleNoteThreshold float64

	// ChunkWriter optionally persists intel chunk records prior to embedding.
	// When set, chunk ordering is recorded in the intel spine for determinism.
	ChunkWriter ChunkWriter

	// EmbeddingWriter optionally persists embeddings to the intel spine.
	// When set, embeddings are written keyed by chunk_id after embedding.
	EmbeddingWriter EmbeddingWriter

	// WriteQueue optionally routes note/index intel writes through a single writer lane.
	WriteQueue NoteMetaWriteQueue

	// RawEligibility controls which notes receive raw authored-section
	// embeddings. Nil means all notes remain raw-eligible, which is reserved
	// for ontology-unavailable indexing paths.
	RawEligibility RawNoteEmbeddingEligibilitySource

	setupMu                sync.Mutex
	schemaReady            bool
	metaDimsKnown          bool
	setupErr               error
	freshnessMu            sync.Mutex
	pendingSourceHighWater time.Time
}

const (
	defaultSectionMaxBytes = 12000
)

func observeNoteMetric[T any](ctx context.Context, name indexingperf.MetricName, fn func() (T, error)) (T, error) {
	started := time.Now()
	value, err := fn()
	indexingperf.ObserveLatency(ctx, name, time.Since(started))
	return value, err
}

func observeNoteMetricErr(ctx context.Context, name indexingperf.MetricName, fn func() error) error {
	started := time.Now()
	err := fn()
	indexingperf.ObserveLatency(ctx, name, time.Since(started))
	return err
}

// SyncPath incrementally syncs a single note's embeddings using the current intel doc sections.
// path must be repository/vault-root relative (slash-separated), matching intel_doc_sections.path.
func (s *NoteSyncer) SyncPath(ctx context.Context, path string) error {
	return s.SyncPaths(ctx, []string{path})
}

// Sync plans and embeds the raw note surface. In full indexing this is a
// compatibility path; ontology-ready note bodies are synced separately as
// ontology-node chunks.
func (s *NoteSyncer) Sync(ctx context.Context) error {
	plan, err := s.Plan(ctx)
	if err != nil {
		return err
	}
	return s.Embed(ctx, plan)
}

func (s *NoteSyncer) ensureReady(ctx context.Context) error {
	if s.Index == nil || s.Provider == nil {
		return errors.New("note syncer requires index and provider")
	}
	s.applyProviderCaps(ctx)
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	if s.setupErr != nil {
		return s.setupErr
	}
	if !s.schemaReady {
		if err := s.Index.EnsureSchema(ctx); err != nil {
			s.setupErr = err
			return err
		}
		s.schemaReady = true
	}
	// Re-validate each call until the provider has reported its real dimensions.
	// Voyage learns dims from its first embedding response, so the first call may
	// pass Dimensions=0; we follow up once the value is known.
	if !s.metaDimsKnown {
		dims := s.Provider.Dimensions()
		if err := s.Index.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{
			Provider:   s.ProviderInfo.Provider,
			Model:      s.ProviderInfo.Model,
			Dimensions: dims,
		}); err != nil {
			s.setupErr = err
			return err
		}
		if dims > 0 {
			s.metaDimsKnown = true
		}
	}
	return nil
}

func (s *NoteSyncer) sectionMaxBytes() int {
	if s.MaxSectionBytes > 0 {
		return s.MaxSectionBytes
	}
	return defaultSectionMaxBytes
}

func (s *NoteSyncer) planConcurrent() int {
	if s.PlanConcurrent > 0 {
		return s.PlanConcurrent
	}
	return defaultPlanConcurrency()
}

func (s *NoteSyncer) maxConcurrent() int {
	if s.MaxConcurrent > 0 {
		return s.MaxConcurrent
	}
	return EffectiveMaxConcurrent(s.Provider, 0)
}

func (s *NoteSyncer) progressf(format string, args ...any) {
	if s.OnProgress != nil {
		s.OnProgress(format, args...)
	}
}

func (s *NoteSyncer) applyProviderCaps(ctx context.Context) {
	if s.Provider == nil {
		return
	}
	if s.MaxSectionBytes > 0 {
		return
	}
	ctxProvider, ok := s.Provider.(embeddings.ContextTokenProvider)
	if !ok {
		return
	}
	tokens, err := ctxProvider.ContextTokens(ctx)
	if err != nil && tokens == 0 {
		s.progressf("Ollama context probe failed: %v", err)
		return
	}
	if tokens <= 0 {
		return
	}
	capBytes := tokensToBytes(tokens)
	sectionMax := min(defaultSectionMaxBytes, capBytes)
	if s.MaxSectionBytes == 0 {
		s.MaxSectionBytes = sectionMax
	}
	if err != nil {
		s.progressf("Ollama context probe failed (%v); using fallback %d tokens", err, tokens)
	}
}

func tokensToBytes(tokens int) int {
	if tokens <= 0 {
		return 0
	}
	const tokenByteMultiplier = 3
	if tokens > (int(^uint(0)>>1))/tokenByteMultiplier {
		return int(^uint(0) >> 1)
	}
	return tokens * tokenByteMultiplier
}
