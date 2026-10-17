package embedindex

import (
	"context"
	"errors"

	"sync"
	"sync/atomic"

	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	codeidx "github.com/atomicobject/rhizome/pkg/embeddings/codeindex"

	semdb "github.com/atomicobject/rhizome/pkg/store"
)

type countingProvider struct {
	inner embeddings.Provider
	calls atomic.Int64
}

func (p *countingProvider) Dimensions() int { return p.inner.Dimensions() }

func (p *countingProvider) EmbedTexts(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
	p.calls.Add(1)
	return p.inner.EmbedTexts(ctx, texts)
}

func (p *countingProvider) Reset() { p.calls.Store(0) }

type batchRecordingProvider struct {
	inner      embeddings.Provider
	calls      atomic.Int64
	mu         sync.Mutex
	batchSizes []int
}

func (p *batchRecordingProvider) Dimensions() int { return p.inner.Dimensions() }

func (p *batchRecordingProvider) EmbedTexts(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
	p.calls.Add(1)
	p.mu.Lock()
	p.batchSizes = append(p.batchSizes, len(texts))
	p.mu.Unlock()
	return p.inner.EmbedTexts(ctx, texts)
}

func (p *batchRecordingProvider) BatchSizes() []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]int(nil), p.batchSizes...)
}

type failDeleteChunksIndex struct {
	codeidx.Index
}

func (i failDeleteChunksIndex) DeleteChunksNotIn(context.Context, codeidx.AnchorID, []int) error {
	return errors.New("delete chunks failed after writeback")
}

type stubIntelSource struct {
	anchors     []codeanchor.IntelAnchor
	chunkHashes map[string]map[int]string
	docLinks    map[string][]codeanchor.DocLink
}

type storedChunkIntelSource struct {
	*stubIntelSource
	store      *semdb.Store
	stateCalls atomic.Int64
}

func (s *storedChunkIntelSource) IntelChunkEmbeddingStates(ctx context.Context, ownerIDs []string) (map[string]map[int]string, error) {
	s.stateCalls.Add(1)
	return s.store.IntelChunkEmbeddingStates(ctx, ownerIDs)
}

type noopCallLookup struct{}

func (noopCallLookup) CalleesForOwnerFQN(ctx context.Context, lang, ownerFQN string, limit int) ([]string, error) {
	return nil, nil
}

func (noopCallLookup) CalleesForFile(ctx context.Context, path string, limit int) ([]string, error) {
	return nil, nil
}

func (noopCallLookup) CalleesForOwnerFQNs(ctx context.Context, lang string, ownerFQNs []string, limitPerOwner int) (map[string][]string, error) {
	return map[string][]string{}, nil
}

func (noopCallLookup) CalleesForFiles(ctx context.Context, paths []string, limitPerFile int) (map[string][]string, error) {
	return map[string][]string{}, nil
}

type countingBatchCallLookup struct {
	fileBatchCalls  atomic.Int64
	ownerBatchCalls atomic.Int64
	ownerBatchSizes atomic.Int64
}

func (c *countingBatchCallLookup) CalleesForOwnerFQN(context.Context, string, string, int) ([]string, error) {
	return nil, nil
}

func (c *countingBatchCallLookup) CalleesForFile(context.Context, string, int) ([]string, error) {
	return nil, nil
}

func (c *countingBatchCallLookup) CalleesForOwnerFQNs(_ context.Context, _ string, ownerFQNs []string, _ int) (map[string][]string, error) {
	c.ownerBatchCalls.Add(1)
	c.ownerBatchSizes.Add(int64(len(ownerFQNs)))
	out := make(map[string][]string, len(ownerFQNs))
	for _, owner := range ownerFQNs {
		out[owner] = []string{"callee:" + owner}
	}
	return out, nil
}

func (c *countingBatchCallLookup) CalleesForFiles(_ context.Context, paths []string, _ int) (map[string][]string, error) {
	c.fileBatchCalls.Add(1)
	out := make(map[string][]string, len(paths))
	for _, path := range paths {
		out[path] = []string{"file:" + path}
	}
	return out, nil
}

func (s *stubIntelSource) IntelAnchors(ctx context.Context) ([]codeanchor.IntelAnchor, error) {
	return s.anchors, nil
}

func (s *stubIntelSource) IntelAnchorsByPath(ctx context.Context, path string) ([]codeanchor.IntelAnchor, error) {
	var out []codeanchor.IntelAnchor
	for _, anchor := range s.anchors {
		if anchor.Path == path {
			out = append(out, anchor)
		}
	}
	return out, nil
}

func (s *stubIntelSource) IntelDocSections(ctx context.Context) ([]codeanchor.IntelDocSection, error) {
	return nil, nil
}

func (s *stubIntelSource) IntelEdges(ctx context.Context) ([]codeanchor.IntelEdge, error) {
	return nil, nil
}

func (s *stubIntelSource) IntelChunkEmbeddingStates(ctx context.Context, ownerIDs []string) (map[string]map[int]string, error) {
	out := make(map[string]map[int]string, len(ownerIDs))
	for _, id := range ownerIDs {
		if hashes, ok := s.chunkHashes[id]; ok {
			out[id] = hashes
		}
	}
	return out, nil
}

func (s *stubIntelSource) DocLinksForAnchors(ctx context.Context, anchorIDs []string, limitPerAnchor int) (map[string][]codeanchor.DocLink, error) {
	out := make(map[string][]codeanchor.DocLink, len(anchorIDs))
	for _, id := range anchorIDs {
		links := append([]codeanchor.DocLink(nil), s.docLinks[id]...)
		if limitPerAnchor > 0 && len(links) > limitPerAnchor {
			links = links[:limitPerAnchor]
		}
		out[id] = links
	}
	return out, nil
}

type singleChunkPolicy struct{}

func (singleChunkPolicy) BuildModuleChunks(path string, lang codeanchor.Lang, anchors []codeanchor.IntelAnchor, fileContent []byte) []SemanticChunk {
	return nil
}

func (singleChunkPolicy) BuildAnchorChunks(anchor codeanchor.IntelAnchor, fileContent []byte, relatedTitles []string, _ SymbolChunkContext) []SemanticChunk {
	txt := "anchor:" + anchor.AnchorID
	return []SemanticChunk{{
		Input: codeidx.ChunkInput{
			Index:       0,
			Granularity: "symbol",
			Breadcrumb:  anchor.Path,
			Heading:     anchor.Symbol,
			Hash:        "h-" + anchor.AnchorID,
		},
		Text: txt,
	}}
}

type multiChunkPolicy struct{}

func (multiChunkPolicy) BuildModuleChunks(path string, lang codeanchor.Lang, anchors []codeanchor.IntelAnchor, fileContent []byte) []SemanticChunk {
	return nil
}

func (multiChunkPolicy) BuildAnchorChunks(anchor codeanchor.IntelAnchor, fileContent []byte, relatedTitles []string, _ SymbolChunkContext) []SemanticChunk {
	return []SemanticChunk{
		{
			Input: codeidx.ChunkInput{
				Index:       0,
				Granularity: "symbol",
				Breadcrumb:  anchor.Path,
				Heading:     anchor.Symbol,
				Hash:        "h0-" + anchor.AnchorID,
			},
			Text: "anchor:" + anchor.AnchorID + ":0",
		},
		{
			Input: codeidx.ChunkInput{
				Index:       1,
				Granularity: "symbol",
				Breadcrumb:  anchor.Path,
				Heading:     anchor.Symbol,
				Hash:        "h1-" + anchor.AnchorID,
			},
			Text: "anchor:" + anchor.AnchorID + ":1",
		},
	}
}

type firstChunkOnlyPolicy struct{ multiChunkPolicy }

func (firstChunkOnlyPolicy) BuildAnchorChunks(anchor codeanchor.IntelAnchor, fileContent []byte, relatedTitles []string, ctx SymbolChunkContext) []SemanticChunk {
	return multiChunkPolicy{}.BuildAnchorChunks(anchor, fileContent, relatedTitles, ctx)[:1]
}

type sharedHashChunkPolicy struct {
	hash string
	text string
}

func (p sharedHashChunkPolicy) BuildModuleChunks(path string, lang codeanchor.Lang, anchors []codeanchor.IntelAnchor, fileContent []byte) []SemanticChunk {
	return nil
}

func (p sharedHashChunkPolicy) BuildAnchorChunks(anchor codeanchor.IntelAnchor, fileContent []byte, relatedTitles []string, _ SymbolChunkContext) []SemanticChunk {
	hash := p.hash
	if hash == "" {
		hash = "shared-hash"
	}
	text := p.text
	if text == "" {
		text = "shared-text"
	}
	return []SemanticChunk{{
		Input: codeidx.ChunkInput{
			Index:       0,
			Granularity: "symbol",
			Breadcrumb:  anchor.Path,
			Heading:     anchor.Symbol,
			Hash:        hash,
		},
		Text: text,
	}}
}

type blockingChunkPolicy struct {
	active  atomic.Int64
	max     atomic.Int64
	entered chan struct{}
	release <-chan struct{}
}

func (p *blockingChunkPolicy) BuildModuleChunks(path string, lang codeanchor.Lang, anchors []codeanchor.IntelAnchor, fileContent []byte) []SemanticChunk {
	return nil
}

func (p *blockingChunkPolicy) BuildAnchorChunks(anchor codeanchor.IntelAnchor, fileContent []byte, relatedTitles []string, _ SymbolChunkContext) []SemanticChunk {
	active := p.active.Add(1)
	for {
		max := p.max.Load()
		if active <= max || p.max.CompareAndSwap(max, active) {
			break
		}
	}
	if p.entered != nil {
		p.entered <- struct{}{}
	}
	if p.release != nil {
		<-p.release
	}
	p.active.Add(-1)
	return []SemanticChunk{{
		Input: codeidx.ChunkInput{
			Index:       0,
			Granularity: "symbol",
			Breadcrumb:  anchor.Path,
			Heading:     anchor.Symbol,
			Hash:        "h-" + anchor.AnchorID,
		},
		Text: "anchor:" + anchor.AnchorID,
	}}
}

type countingEmbeddingLookupIndex struct {
	codeidx.Index
	pointCalls  atomic.Int64
	batchCalls  atomic.Int64
	batchHashes atomic.Int64
}

func (i *countingEmbeddingLookupIndex) EmbeddingByHash(ctx context.Context, hash string) (embeddings.Embedding, bool, error) {
	i.pointCalls.Add(1)
	return i.Index.EmbeddingByHash(ctx, hash)
}

func (i *countingEmbeddingLookupIndex) EmbeddingByHashes(ctx context.Context, hashes []string) (map[string]embeddings.Embedding, error) {
	i.batchCalls.Add(1)
	i.batchHashes.Add(int64(len(hashes)))
	if batcher, ok := i.Index.(interface {
		EmbeddingByHashes(context.Context, []string) (map[string]embeddings.Embedding, error)
	}); ok {
		return batcher.EmbeddingByHashes(ctx, hashes)
	}
	out := make(map[string]embeddings.Embedding, len(hashes))
	for _, hash := range hashes {
		vec, ok, err := i.Index.EmbeddingByHash(ctx, hash)
		if err != nil {
			return nil, err
		}
		if ok && len(vec) > 0 {
			out[hash] = vec
		}
	}
	return out, nil
}

type noopEmbeddingWriter struct{}

func (noopEmbeddingWriter) UpsertEmbeddings(ctx context.Context, embeddings map[string]embeddings.Embedding) error {
	return nil
}

type noopChunkWriter struct{}

func (noopChunkWriter) ReplaceIntelChunks(context.Context, []string, []codeanchor.IntelChunk) error {
	return nil
}

type countingSyncWriteQueue struct {
	flushes          atomic.Int64
	singleItemCalls  atomic.Int64
	batchItemCalls   atomic.Int64
	singleChunkCalls atomic.Int64
	batchChunkCalls  atomic.Int64
	intelCalls       atomic.Int64
}

func (q *countingSyncWriteQueue) CodeWritebackBatchConfig() embeddings.CodeWritebackBatchConfig {
	return embeddings.CodeWritebackBatchConfig{
		QueueCapacity: 16,
		ItemEmbed:     embeddings.WritebackFlushPolicy{Rows: 4, Bytes: 1 << 20, Idle: 10 * time.Millisecond},
		CodeChunks:    embeddings.WritebackFlushPolicy{Rows: 4, Bytes: 1 << 20, Idle: 10 * time.Millisecond},
		IntelEmbed:    embeddings.WritebackFlushPolicy{Rows: 4, Bytes: 1 << 20, Idle: 10 * time.Millisecond},
	}
}

func (q *countingSyncWriteQueue) SubmitCodeItemEmbedding(context.Context, codeidx.ItemEmbeddingUpsert) error {
	q.singleItemCalls.Add(1)
	return nil
}

func (q *countingSyncWriteQueue) SubmitCodeItemEmbeddingBatch(context.Context, []codeidx.ItemEmbeddingUpsert) error {
	q.batchItemCalls.Add(1)
	return nil
}

func (q *countingSyncWriteQueue) SubmitCodeItemChunks(context.Context, codeidx.AnchorID, []codeidx.ChunkInput, []string, []embeddings.Embedding) error {
	q.singleChunkCalls.Add(1)
	return nil
}

func (q *countingSyncWriteQueue) SubmitCodeItemChunkBatch(context.Context, []codeidx.ItemChunksUpsert) error {
	q.batchChunkCalls.Add(1)
	return nil
}

func (q *countingSyncWriteQueue) SubmitIntelChunks(context.Context, []string, []codeanchor.IntelChunk) error {
	return nil
}

func (q *countingSyncWriteQueue) SubmitIntelChunksByFamily(context.Context, []string, string, []codeanchor.IntelChunk) error {
	return nil
}

func (q *countingSyncWriteQueue) SubmitIntelEmbeddings(context.Context, map[string]embeddings.Embedding) error {
	q.intelCalls.Add(1)
	return nil
}

func (q *countingSyncWriteQueue) FlushAndWait(context.Context) error {
	q.flushes.Add(1)
	return nil
}
