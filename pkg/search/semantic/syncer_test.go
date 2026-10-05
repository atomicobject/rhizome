package semantic

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	codeidx "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
	"github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex/sqlite"
	"github.com/stretchr/testify/require"
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

func TestFileSystemLoaderRejectsEscapedPaths(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "inside.go"), []byte("inside"), 0o644))

	sibling := filepath.Join(parent, "root-sibling")
	require.NoError(t, os.MkdirAll(sibling, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sibling, "secret.go"), []byte("outside"), 0o644))

	loader := FileSystemLoader(root)
	got, err := loader("inside.go")
	require.NoError(t, err)
	require.Equal(t, []byte("inside"), got)

	_, err = loader("../" + filepath.Base(sibling) + "/secret.go")
	require.Error(t, err)

	_, err = loader(filepath.Join(sibling, "secret.go"))
	require.Error(t, err)
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

type recordingChunkWriter struct {
	ownerIDs []string
	chunks   []codeanchor.IntelChunk
}

func (w *recordingChunkWriter) ReplaceIntelChunks(_ context.Context, ownerIDs []string, chunks []codeanchor.IntelChunk) error {
	w.ownerIDs = append([]string(nil), ownerIDs...)
	w.chunks = append([]codeanchor.IntelChunk(nil), chunks...)
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

func (q *countingSyncWriteQueue) CodeWritebackBatchConfig() CodeWritebackBatchConfig {
	return CodeWritebackBatchConfig{
		QueueCapacity: 16,
		ItemEmbed:     WritebackFlushPolicy{Rows: 4, Bytes: 1 << 20, Idle: 10 * time.Millisecond},
		CodeChunks:    WritebackFlushPolicy{Rows: 4, Bytes: 1 << 20, Idle: 10 * time.Millisecond},
		IntelEmbed:    WritebackFlushPolicy{Rows: 4, Bytes: 1 << 20, Idle: 10 * time.Millisecond},
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

func TestSyncerBatchesIntelEmbeddingWritesAcrossItems(t *testing.T) {
	tempDir := t.TempDir()

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := sqlite.Open(filepath.Join(tempDir, "code.db"), prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().Unix()
	anchors := make([]codeanchor.IntelAnchor, 0, 48)
	for i := 0; i < 48; i++ {
		anchors = append(anchors, codeanchor.IntelAnchor{
			AnchorID:    fmt.Sprintf("a%d", i),
			Lang:        codeanchor.Lang("go"),
			Kind:        "func",
			Path:        fmt.Sprintf("pkg/file%d.go", i),
			Symbol:      fmt.Sprintf("Fn%d", i),
			FQN:         fmt.Sprintf("pkg.Fn%d", i),
			Fingerprint: fmt.Sprintf("fp-%d", i),
			UpdatedAt:   now,
		})
	}
	writer := &recordingEmbeddingWriter{}
	syncer := Syncer{
		Index:           store,
		Provider:        prov,
		ProviderInfo:    embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:           &stubIntelSource{anchors: anchors},
		PlanConcurrent:  1,
		Policy:          singleChunkPolicy{},
		EmbeddingWriter: writer,
	}

	require.NoError(t, syncer.Sync(context.Background()))
	require.Less(t, writer.CallCount(), len(anchors), "intel writes should batch across items")
	require.Equal(t, len(anchors), writer.StoredCount())
}

func TestSyncerEmbedWithoutSyncMarkFlushesQueuedWritesOnce(t *testing.T) {
	tempDir := t.TempDir()

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := sqlite.Open(filepath.Join(tempDir, "code.db"), prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().Unix()
	anchors := []codeanchor.IntelAnchor{{
		AnchorID:    "a1",
		Lang:        codeanchor.Lang("go"),
		Kind:        "func",
		Path:        "pkg/file.go",
		Symbol:      "Fn",
		FQN:         "pkg.Fn",
		Fingerprint: "fp-1",
		UpdatedAt:   now,
	}}
	queue := &countingSyncWriteQueue{}
	syncer := Syncer{
		Index:           store,
		Provider:        prov,
		ProviderInfo:    embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:           &stubIntelSource{anchors: anchors},
		Root:            tempDir,
		Policy:          singleChunkPolicy{},
		ChunkWriter:     noopChunkWriter{},
		EmbeddingWriter: noopEmbeddingWriter{},
		WriteQueue:      queue,
	}

	plan, err := syncer.Plan(context.Background())
	require.NoError(t, err)
	require.NoError(t, syncer.EmbedWithoutSyncMark(context.Background(), plan))
	require.EqualValues(t, 1, queue.flushes.Load())
	require.Zero(t, queue.singleItemCalls.Load())
	require.Greater(t, queue.batchItemCalls.Load(), int64(0))
	require.Zero(t, queue.singleChunkCalls.Load())
	require.Greater(t, queue.intelCalls.Load(), int64(0))
}

func TestSyncerDrainsQueuedWritebackOnFinalizeError(t *testing.T) {
	tempDir := t.TempDir()

	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store, err := sqlite.Open(filepath.Join(tempDir, "code.db"), prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().Unix()
	anchors := []codeanchor.IntelAnchor{{
		AnchorID:    "a1",
		Lang:        codeanchor.Lang("go"),
		Kind:        "func",
		Path:        "pkg/file.go",
		Symbol:      "A",
		FQN:         "pkg.A",
		Fingerprint: "fp-1",
		UpdatedAt:   now,
	}}
	queue := &countingSyncWriteQueue{}
	syncer := Syncer{
		Index:          failDeleteChunksIndex{Index: store},
		Provider:       prov,
		ProviderInfo:   embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:          &stubIntelSource{anchors: anchors},
		Root:           tempDir,
		BatchSize:      1,
		MaxConcurrent:  1,
		PlanConcurrent: 1,
		Policy:         singleChunkPolicy{},
		WriteQueue:     queue,
	}

	err = syncer.Sync(context.Background())
	require.Error(t, err)
	require.ErrorContains(t, err, "delete chunks failed after writeback")
	require.EqualValues(t, 1, queue.flushes.Load())
	require.Greater(t, queue.batchChunkCalls.Load(), int64(0))
	require.Greater(t, queue.batchItemCalls.Load(), int64(0))
}

func TestSyncerReturnsIntelEmbeddingFlushError(t *testing.T) {
	tempDir := t.TempDir()

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := sqlite.Open(filepath.Join(tempDir, "code.db"), prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	writer := &recordingEmbeddingWriter{failCall: 1, failErr: errors.New("boom")}
	syncer := Syncer{
		Index:           store,
		Provider:        prov,
		ProviderInfo:    embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:           &stubIntelSource{anchors: []codeanchor.IntelAnchor{{AnchorID: "a1", Lang: codeanchor.Lang("go"), Kind: "func", Path: "pkg/file.go", Symbol: "Fn", FQN: "pkg.Fn", Fingerprint: "fp-1", UpdatedAt: time.Now().Unix()}}},
		PlanConcurrent:  1,
		Policy:          singleChunkPolicy{},
		EmbeddingWriter: writer,
	}

	err = syncer.Sync(context.Background())
	require.Error(t, err)
	require.ErrorContains(t, err, "boom")
}

func TestSyncerProgressTracksQueuedProviderCalls(t *testing.T) {
	tempDir := t.TempDir()

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := sqlite.Open(filepath.Join(tempDir, "code.db"), prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	var updates [][2]int
	syncer := Syncer{
		Index:          store,
		Provider:       prov,
		ProviderInfo:   embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:          &stubIntelSource{anchors: []codeanchor.IntelAnchor{{AnchorID: "a1", Lang: codeanchor.Lang("go"), Kind: "func", Path: "pkg/file.go", Symbol: "Fn", FQN: "pkg.Fn", Fingerprint: "fp-1", UpdatedAt: time.Now().Unix()}}},
		PlanConcurrent: 1,
		Policy:         multiChunkPolicy{},
		OnEmbedProgress: func(done, total int) {
			updates = append(updates, [2]int{done, total})
		},
	}

	require.NoError(t, syncer.Sync(context.Background()))
	require.NotEmpty(t, updates)
	require.Equal(t, [2]int{0, 1}, updates[0])
	require.Equal(t, [2]int{2, 2}, updates[len(updates)-1])
	for _, update := range updates {
		require.LessOrEqual(t, update[0], update[1], "progress should stay within queued provider-call count")
	}
}

func TestBuildTasksForPreparedPathsParallelizesPathWork(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	policy := &blockingChunkPolicy{
		entered: make(chan struct{}, 8),
		release: release,
	}
	syncer := Syncer{
		PlanConcurrent: 4,
		Policy:         policy,
		Budget:         DefaultChunkBudget(),
	}
	prepared := PreparedCodePaths{
		paths: []string{"pkg/a.go", "pkg/b.go", "pkg/c.go", "pkg/d.go"},
		moduleByPath: map[string][]codeanchor.IntelAnchor{
			"pkg/a.go": {{AnchorID: "a", Lang: codeanchor.Lang("go"), Kind: "func", Path: "pkg/a.go", Symbol: "A", Fingerprint: "fa"}},
			"pkg/b.go": {{AnchorID: "b", Lang: codeanchor.Lang("go"), Kind: "func", Path: "pkg/b.go", Symbol: "B", Fingerprint: "fb"}},
			"pkg/c.go": {{AnchorID: "c", Lang: codeanchor.Lang("go"), Kind: "func", Path: "pkg/c.go", Symbol: "C", Fingerprint: "fc"}},
			"pkg/d.go": {{AnchorID: "d", Lang: codeanchor.Lang("go"), Kind: "func", Path: "pkg/d.go", Symbol: "D", Fingerprint: "fd"}},
		},
		fileContentByPath: map[string][]byte{},
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := syncer.buildTasksForPreparedPaths(context.Background(), prepared)
		errCh <- err
	}()

	for range 2 {
		select {
		case <-policy.entered:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for parallel chunk builders")
		}
	}
	close(release)

	require.NoError(t, <-errCh)
	require.GreaterOrEqual(t, policy.max.Load(), int64(2))
}

func TestBuildTasksForPreparedPathsBatchesOwnerLookupsAcrossPaths(t *testing.T) {
	t.Parallel()

	lookup := &countingBatchCallLookup{}
	syncer := Syncer{
		PlanConcurrent: 4,
		Policy:         singleChunkPolicy{},
		Budget:         DefaultChunkBudget(),
		Calls:          lookup,
	}
	prepared := PreparedCodePaths{
		paths: []string{"pkg/a.go", "pkg/b.go", "pkg/c.go"},
		moduleByPath: map[string][]codeanchor.IntelAnchor{
			"pkg/a.go": {{AnchorID: "a", Lang: codeanchor.Lang("go"), Kind: "func", Path: "pkg/a.go", Symbol: "A", FQN: "pkg.A", Fingerprint: "fa"}},
			"pkg/b.go": {{AnchorID: "b", Lang: codeanchor.Lang("go"), Kind: "func", Path: "pkg/b.go", Symbol: "B", FQN: "pkg.B", Fingerprint: "fb"}},
			"pkg/c.go": {{AnchorID: "c", Lang: codeanchor.Lang("go"), Kind: "func", Path: "pkg/c.go", Symbol: "C", FQN: "pkg.C", Fingerprint: "fc"}},
		},
		fileContentByPath: map[string][]byte{},
	}

	tasks, err := syncer.buildTasksForPreparedPaths(context.Background(), prepared)
	require.NoError(t, err)
	require.Len(t, tasks, 3)
	require.Equal(t, int64(1), lookup.fileBatchCalls.Load())
	require.Equal(t, int64(1), lookup.ownerBatchCalls.Load())
	require.Equal(t, int64(3), lookup.ownerBatchSizes.Load())
}

func TestSyncerIndexesAnchorsAndChunks(t *testing.T) {
	t.Run("default policy removes stored field owner without fallback", func(t *testing.T) {
		ctx := context.Background()
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "model.go"), []byte("package pkg\ntype Model struct { Count int }\n"), 0o644))
		prov := &countingProvider{inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})}
		store, err := sqlite.Open(filepath.Join(root, "code.db"), prov.Dimensions())
		require.NoError(t, err)
		t.Cleanup(func() { _ = store.Close() })
		intelStore, err := semdb.Open(filepath.Join(root, "intel.db"))
		require.NoError(t, err)
		t.Cleanup(func() { _ = intelStore.Close() })
		anchor := codeanchor.IntelAnchor{AnchorID: "field-1", Lang: codeanchor.LangGo, Kind: "field", Path: "pkg/model.go", Symbol: "Count", FQN: "pkg.Model.Count", Signature: "Count int", Fingerprint: "fp-field", UpdatedAt: time.Now().Unix()}
		intel := &storedChunkIntelSource{stubIntelSource: &stubIntelSource{anchors: []codeanchor.IntelAnchor{anchor}}, store: intelStore}
		syncer := Syncer{Index: store, Provider: prov, ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"}, Intel: intel, Root: root, Policy: singleChunkPolicy{}, ChunkWriter: intelStore, EmbeddingWriter: intelStore}
		require.NoError(t, syncer.SyncPaths(ctx, []string{anchor.Path}))
		stale, err := intelStore.IntelChunksByOwners(ctx, []string{"field-1"})
		require.NoError(t, err)
		require.Len(t, stale, 1)
		syncer.Policy = DefaultSynthesisPolicy(PolicyOptions{Budget: DefaultChunkBudget()})
		meta, ok, err := store.Metadata(ctx)
		require.NoError(t, err)
		require.True(t, ok)
		intel.anchors[0].UpdatedAt = meta.SourceHighWater.Unix() + 1
		prov.Reset()
		require.NoError(t, syncer.SyncPaths(ctx, []string{anchor.Path}))
		chunks, err := store.ItemChunks(ctx, "field-1")
		require.NoError(t, err)
		require.Empty(t, chunks, "suppressed field must not use fallback chunk")
		chunksIntel, err := intelStore.IntelChunksByOwners(ctx, []string{"field-1"})
		require.NoError(t, err)
		require.Empty(t, chunksIntel)
		require.Zero(t, prov.calls.Load(), "cleanup should not embed a field")
	})
	t.Run("unchanged prefix removes stale extra chunk", func(t *testing.T) {
		ctx := context.Background()
		prov := &countingProvider{inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})}
		store, err := sqlite.Open(filepath.Join(t.TempDir(), "code.db"), prov.Dimensions())
		require.NoError(t, err)
		t.Cleanup(func() { _ = store.Close() })
		anchor := codeanchor.IntelAnchor{AnchorID: "a1", Lang: codeanchor.LangGo, Kind: "func", Path: "pkg/foo.go", Symbol: "Foo", FQN: "pkg.Foo", Fingerprint: "fp", UpdatedAt: time.Now().Unix()}
		syncer := Syncer{Index: store, Provider: prov, ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"}, Intel: &stubIntelSource{anchors: []codeanchor.IntelAnchor{anchor}}, Policy: multiChunkPolicy{}}
		require.NoError(t, syncer.SyncPaths(ctx, []string{anchor.Path}))
		before, err := store.ItemChunks(ctx, "a1")
		require.NoError(t, err)
		require.Len(t, before, 2)
		syncer.Policy = firstChunkOnlyPolicy{}
		prov.Reset()
		require.NoError(t, syncer.SyncPaths(ctx, []string{anchor.Path}))
		after, err := store.ItemChunks(ctx, "a1")
		require.NoError(t, err)
		require.Len(t, after, 1)
		require.Equal(t, 0, after[0].Index)
		require.Equal(t, before[0].Embedding, after[0].Embedding)
		require.Zero(t, prov.calls.Load())
	})

	for _, unified := range []bool{false, true} {
		name := "legacy"
		if unified {
			name = "unified"
		}
		t.Run(name+" reuses primary and embeds secondary", func(t *testing.T) {
			ctx := context.Background()
			prov := &countingProvider{inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})}
			store, err := sqlite.Open(filepath.Join(t.TempDir(), "code.db"), prov.Dimensions())
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })
			reused := embeddings.Embedding{9, 8, 7, 6, 5, 4, 3, 2}
			require.NoError(t, store.CacheEmbedding(ctx, "h0-a1", reused))
			anchor := codeanchor.IntelAnchor{AnchorID: "a1", Lang: codeanchor.LangGo, Kind: "func", Path: "pkg/foo.go", Symbol: "Foo", FQN: "pkg.Foo", Fingerprint: "fp", UpdatedAt: time.Now().Unix()}
			syncer := Syncer{Index: store, Provider: prov, ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"}, Intel: &stubIntelSource{anchors: []codeanchor.IntelAnchor{anchor}}, Policy: multiChunkPolicy{}}
			var intelStore *semdb.Store
			if unified {
				intelStore, err = semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
				require.NoError(t, err)
				t.Cleanup(func() { _ = intelStore.Close() })
				syncer.ChunkWriter = intelStore
				syncer.EmbeddingWriter = intelStore
			}
			require.NoError(t, syncer.Sync(ctx))
			require.Equal(t, int64(1), prov.calls.Load())
			newVecs, err := prov.inner.EmbedTexts(ctx, []string{"anchor:a1:1"})
			require.NoError(t, err)
			if unified {
				chunks, err := intelStore.IntelChunksByOwners(ctx, []string{"a1"})
				require.NoError(t, err)
				require.Len(t, chunks, 2)
				ids := []string{chunks[0].ChunkID, chunks[1].ChunkID}
				vectors, err := intelStore.EmbeddingsByChunkIDs(ctx, ids)
				require.NoError(t, err)
				require.Equal(t, []int{0, 1}, []int{chunks[0].Ord, chunks[1].Ord})
				require.Equal(t, reused, vectors[ids[0]])
				require.Equal(t, newVecs[0], vectors[ids[1]])
			} else {
				chunks, err := store.ItemChunks(ctx, "a1")
				require.NoError(t, err)
				require.Len(t, chunks, 2)
				require.Equal(t, []int{0, 1}, []int{chunks[0].Index, chunks[1].Index})
				require.Equal(t, reused, chunks[0].Embedding)
				require.Equal(t, newVecs[0], chunks[1].Embedding)
				itemVec, _, ok, err := store.GetItemEmbedding(ctx, "a1")
				require.NoError(t, err)
				require.True(t, ok)
				require.Equal(t, reused, itemVec)
			}
		})
	}

	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "pkg", "foo.go")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "package foo\n\n// test doc\nfunc DoThing() string {\n    return \"ok\"\n}\n"
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	module := codeanchor.IntelAnchor{
		AnchorID:    "m1",
		Lang:        codeanchor.Lang("go"),
		Kind:        "module",
		Path:        "pkg/foo.go",
		Symbol:      "foo.go",
		Fingerprint: "fp-file",
		StartLine:   1,
		EndLine:     1,
		UpdatedAt:   time.Now().Unix(),
	}
	anchor := codeanchor.IntelAnchor{
		AnchorID:   "a1",
		Lang:       codeanchor.Lang("go"),
		Kind:       "func",
		Path:       "pkg/foo.go",
		Symbol:     "DoThing",
		FQN:        "foo.DoThing",
		Signature:  "func DoThing() string",
		DocComment: "test doc",
		StartLine:  3,
		EndLine:    5,
		UpdatedAt:  time.Now().Unix(),
	}

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	storePath := filepath.Join(tempDir, "code.db")
	store, err := sqlite.Open(storePath, prov.Dimensions())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	syncer := Syncer{
		Index:        store,
		Provider:     prov,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:        &stubIntelSource{anchors: []codeanchor.IntelAnchor{module, anchor}},
		Root:         tempDir,
	}

	if err := syncer.Sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if prov.calls.Load() == 0 {
		t.Fatalf("expected provider to be called on first sync")
	}

	items, chunks, err := store.Stats(context.Background())
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if items == 0 || chunks == 0 {
		t.Fatalf("expected items and chunks to be indexed, got %d items %d chunks", items, chunks)
	}

	storedChunks, err := store.ItemChunks(context.Background(), codeidx.AnchorID(anchor.AnchorID))
	if err != nil {
		t.Fatalf("fetch chunks: %v", err)
	}
	if len(storedChunks) < 1 {
		t.Fatalf("expected at least one chunk, got %d", len(storedChunks))
	}

	// Second pass should detect unchanged anchors/chunks and avoid re-embedding.
	prov.Reset()
	if err := syncer.Sync(context.Background()); err != nil {
		t.Fatalf("sync second: %v", err)
	}
	if prov.calls.Load() != 0 {
		t.Fatalf("expected no provider calls on second sync, got %d", prov.calls.Load())
	}
}

func TestSyncerPlanDoesNotDeadlock(t *testing.T) {
	tempDir := t.TempDir()

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := sqlite.Open(filepath.Join(tempDir, "code.db"), prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().Unix()
	anchors := make([]codeanchor.IntelAnchor, 0, 4)
	for i := 0; i < 4; i++ {
		anchors = append(anchors, codeanchor.IntelAnchor{
			AnchorID:    fmt.Sprintf("a%d", i),
			Lang:        codeanchor.Lang("go"),
			Kind:        "func",
			Path:        fmt.Sprintf("pkg/file%d.go", i),
			Symbol:      fmt.Sprintf("Fn%d", i),
			FQN:         fmt.Sprintf("pkg.Fn%d", i),
			Fingerprint: fmt.Sprintf("fp-%d", i),
			UpdatedAt:   now,
		})
	}

	syncer := Syncer{
		Index:          store,
		Provider:       prov,
		ProviderInfo:   embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:          &stubIntelSource{anchors: anchors},
		PlanConcurrent: 1,
		Policy:         singleChunkPolicy{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = syncer.Plan(ctx)
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("plan hung waiting for workers")
	}
	require.NoError(t, err)
}

func TestSyncerPlanPassesRelatedDocsIntoAnchorChunks(t *testing.T) {
	tempDir := t.TempDir()
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store, err := sqlite.Open(filepath.Join(tempDir, "code.db"), prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	anchor := codeanchor.IntelAnchor{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "func",
		Path:        "pkg/story.go",
		Symbol:      "ResolveStory",
		FQN:         "pkg.ResolveStory",
		Signature:   "func ResolveStory()",
		DocComment:  "Resolves a user story into structured answer context.",
		Fingerprint: "fp-1",
		UpdatedAt:   time.Now().Unix(),
	}
	intel := &stubIntelSource{
		anchors: []codeanchor.IntelAnchor{anchor},
		docLinks: map[string][]codeanchor.DocLink{
			"a1": {{SrcPath: "docs/specs/user-story-retrieval.md", Label: "User Story Retrieval"}, {SrcPath: "docs/specs/user-story-retrieval.md", Label: "User Story Retrieval"}, {SrcPath: "docs/specs/process.md"}},
		},
	}
	syncer := Syncer{
		Index:        store,
		Provider:     prov,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:        intel,
	}

	plan, err := syncer.Plan(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, plan.tasks)

	var text string
	for _, task := range plan.tasks {
		if task.id == codeidx.AnchorID("a1") && len(task.payload.chunks) > 0 {
			text = task.payload.chunks[0].Text
			break
		}
	}
	require.Contains(t, text, "Related docs:")
	require.Contains(t, text, "User Story Retrieval (docs/specs/user-story-retrieval.md)")
	require.Contains(t, text, "process (docs/specs/process.md)")
	require.Equal(t, 1, strings.Count(text, "User Story Retrieval (docs/specs/user-story-retrieval.md)"))
	require.Less(t, strings.Index(text, "User Story Retrieval (docs/specs/user-story-retrieval.md)"), strings.Index(text, "process (docs/specs/process.md)"))
}

func TestSyncerReembedsOnFingerprintVersionMismatch(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "pkg", "foo.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0o755))
	require.NoError(t, os.WriteFile(filePath, []byte("package foo\n\nfunc DoThing() {}\n"), 0o644))

	anchor := codeanchor.IntelAnchor{
		AnchorID:    "a1",
		Lang:        codeanchor.Lang("go"),
		Kind:        "func",
		Path:        "pkg/foo.go",
		Symbol:      "DoThing",
		FQN:         "foo.DoThing",
		Fingerprint: "fp1",
		UpdatedAt:   time.Now().Unix(),
	}

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := sqlite.Open(filepath.Join(tempDir, "code.db"), prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	syncer := Syncer{
		Index:        store,
		Provider:     prov,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:        &stubIntelSource{anchors: []codeanchor.IntelAnchor{anchor}},
		Root:         tempDir,
	}

	require.NoError(t, syncer.Sync(context.Background()))
	prov.Reset()

	// Simulate an older index fingerprint version after a release. Use a fresh
	// Syncer so ensureReady runs again, matching a new process after upgrade.
	require.NoError(t, store.SetFingerprintVersion(context.Background(), 0))

	// Clear the embedding cache so we can verify the provider is actually called.
	// (Without this, the embedding would be found in cache and no API call needed.)
	require.NoError(t, store.ClearEmbeddingCache(context.Background()))

	upgradedSyncer := Syncer{
		Index:        store,
		Provider:     prov,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:        &stubIntelSource{anchors: []codeanchor.IntelAnchor{anchor}},
		Root:         tempDir,
	}

	require.NoError(t, upgradedSyncer.Sync(context.Background()))
	require.Greater(t, prov.calls.Load(), int64(0), "expected re-embed when fingerprint version changes")

	meta, ok, err := store.Metadata(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, codeFingerprintVersion, meta.FingerprintVersion)
}

func TestSyncerCodeEmbedPackerBatchesAcrossItems(t *testing.T) {
	for _, tc := range []struct {
		name                                           string
		owners, chunksPerOwner, batchSize, concurrency int
		packer                                         *EmbedPackerOptions
		wantBatches                                    []int
	}{
		{"default packer", 31, 1, 100, 250, nil, nil},
		{"explicit single chunks", 6, 1, 0, 0, &EmbedPackerOptions{MaxTexts: 3, MaxBytes: 1 << 20, MinTexts: 3, MaxWait: 200 * time.Millisecond}, []int{3, 3}},
		{"explicit multiple chunks", 6, 2, 0, 0, &EmbedPackerOptions{MaxTexts: 6, MaxBytes: 1 << 20, MinTexts: 4, MaxWait: 200 * time.Millisecond}, []int{4, 4, 4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			filePath := filepath.Join(root, "pkg", "foo.go")
			require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0o755))
			require.NoError(t, os.WriteFile(filePath, []byte("package foo\n"), 0o644))
			anchorsList := make([]codeanchor.IntelAnchor, 0, tc.owners)
			for i := 0; i < tc.owners; i++ {
				anchorsList = append(anchorsList, codeanchor.IntelAnchor{AnchorID: fmt.Sprintf("a%d", i), Lang: codeanchor.LangGo, Kind: "func", Path: "pkg/foo.go", Symbol: fmt.Sprintf("Fn%d", i), FQN: fmt.Sprintf("foo.Fn%d", i), Fingerprint: fmt.Sprintf("fp%d", i), UpdatedAt: time.Now().Unix()})
			}
			prov := &batchRecordingProvider{inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})}
			store, err := sqlite.Open(filepath.Join(root, "code.db"), prov.Dimensions())
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })
			var policy SynthesisPolicy = singleChunkPolicy{}
			if tc.chunksPerOwner == 2 {
				policy = multiChunkPolicy{}
			}
			syncer := Syncer{Index: store, Provider: prov, ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"}, Intel: &stubIntelSource{anchors: anchorsList}, Root: root, BatchSize: tc.batchSize, MaxConcurrent: tc.concurrency, Policy: policy, CodeEmbedPacker: tc.packer}
			require.NoError(t, syncer.Sync(ctx))
			require.Positive(t, prov.calls.Load())
			if tc.wantBatches != nil {
				require.Equal(t, tc.wantBatches, prov.BatchSizes())
			} else {
				require.Less(t, prov.calls.Load(), int64(tc.owners))
			}
			for _, anchor := range anchorsList {
				chunks, err := store.ItemChunks(ctx, codeidx.AnchorID(anchor.AnchorID))
				require.NoError(t, err)
				require.Len(t, chunks, tc.chunksPerOwner, anchor.AnchorID)
				for j, chunk := range chunks {
					text := "anchor:" + anchor.AnchorID
					if tc.chunksPerOwner == 2 {
						text += ":" + strconv.Itoa(j)
					}
					want, err := prov.inner.EmbedTexts(ctx, []string{text})
					require.NoError(t, err)
					require.Equal(t, want[0], chunk.Embedding, "owner=%s chunk=%d", anchor.AnchorID, j)
				}
			}
		})
	}
}

func TestSyncerPiggybacksItemEmbeddingOnChunkBatch(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "pkg", "foo.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0o755))
	require.NoError(t, os.WriteFile(filePath, []byte("package foo\n"), 0o644))

	prov := &batchRecordingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := sqlite.Open(filepath.Join(tempDir, "code.db"), prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	syncer := Syncer{
		Index:        store,
		Provider:     prov,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel: &stubIntelSource{anchors: []codeanchor.IntelAnchor{{
			AnchorID:    "a1",
			Lang:        codeanchor.Lang("go"),
			Kind:        "func",
			Path:        "pkg/foo.go",
			Symbol:      "DoThing",
			FQN:         "foo.DoThing",
			Fingerprint: "fp1",
			UpdatedAt:   time.Now().Unix(),
		}}},
		Root:      tempDir,
		Policy:    multiChunkPolicy{},
		BatchSize: 16,
	}

	require.NoError(t, syncer.Sync(context.Background()))
	require.Equal(t, int64(1), prov.calls.Load(), "expected one provider call when item embedding reuses chunk 0")
	require.Equal(t, []int{2}, prov.BatchSizes())

	itemVec, _, ok, err := store.GetItemEmbedding(context.Background(), codeidx.AnchorID("a1"))
	require.NoError(t, err)
	require.True(t, ok)
	chunks, err := store.ItemChunks(context.Background(), "a1")
	require.NoError(t, err)
	require.Len(t, chunks, 2)
	require.Equal(t, chunks[0].Embedding, itemVec)
}

func TestSyncerUsesIntelChunkStatesWhenEmbeddingWriterEnabled(t *testing.T) {
	tempDir := t.TempDir()

	anchor := codeanchor.IntelAnchor{
		AnchorID:    "a1",
		Lang:        codeanchor.Lang("go"),
		Kind:        "func",
		Path:        "pkg/foo.go",
		Symbol:      "DoThing",
		FQN:         "foo.DoThing",
		Fingerprint: "fp1",
		UpdatedAt:   time.Now().Unix(),
	}

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := sqlite.Open(filepath.Join(tempDir, "code.db"), prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	intelStore, err := semdb.Open(filepath.Join(tempDir, "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	intel := &storedChunkIntelSource{
		stubIntelSource: &stubIntelSource{anchors: []codeanchor.IntelAnchor{anchor}},
		store:           intelStore,
	}
	syncer := Syncer{
		Index:           store,
		Provider:        prov,
		ProviderInfo:    embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:           intel,
		Policy:          singleChunkPolicy{},
		ChunkWriter:     intelStore,
		EmbeddingWriter: intelStore,
	}

	require.NoError(t, syncer.Sync(context.Background()))
	require.Greater(t, prov.calls.Load(), int64(0), "expected provider to be called on first sync")

	hashes, err := store.ChunkHashes(context.Background(), codeidx.AnchorID(anchor.AnchorID))
	require.NoError(t, err)
	require.Len(t, hashes, 0, "expected no legacy chunk hashes when EmbeddingWriter is set")
	chunks, err := intelStore.IntelChunksByOwners(context.Background(), []string{anchor.AnchorID})
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	vectors, err := intelStore.EmbeddingsByChunkIDs(context.Background(), []string{chunks[0].ChunkID})
	require.NoError(t, err)
	require.Len(t, vectors[chunks[0].ChunkID], prov.Dimensions())
	states, err := intel.IntelChunkEmbeddingStates(context.Background(), []string{anchor.AnchorID})
	require.NoError(t, err)
	require.Equal(t, chunks[0].ContentHash, states[anchor.AnchorID][chunks[0].Ord])

	prov.Reset()
	meta, ok, err := store.Metadata(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	intel.anchors[0].UpdatedAt = meta.SourceHighWater.Unix() + 1
	require.Greater(t, intel.anchors[0].UpdatedAt, meta.SourceHighWater.Unix())
	plan, err := syncer.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, plan.TotalWork)
	// TotalWork excludes cache hits, so discarded chunk states would still plan zero work.
	require.Equal(t, 0, plan.CacheHits, "unchanged Intel chunk states must leave no task planned")
	stateCallsBefore := intel.stateCalls.Load()

	require.NoError(t, syncer.Sync(context.Background()))
	require.Greater(t, intel.stateCalls.Load(), stateCallsBefore, "second sync must read persisted Intel chunk states")
	require.Equal(t, int64(0), prov.calls.Load(), "expected intel chunk hashes to skip re-embedding")
	retained, err := intelStore.EmbeddingsByChunkIDs(context.Background(), []string{chunks[0].ChunkID})
	require.NoError(t, err)
	require.Equal(t, vectors[chunks[0].ChunkID], retained[chunks[0].ChunkID])
}

func TestSyncerSyncPathsPrunesOnlyTargetPath(t *testing.T) {
	tempDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "pkg", "foo.go"), []byte("package foo\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "pkg", "bar.go"), []byte("package bar\n"), 0o644))

	intel := &stubIntelSource{anchors: []codeanchor.IntelAnchor{
		{AnchorID: "a1", Lang: codeanchor.Lang("go"), Kind: "func", Path: "pkg/foo.go", Symbol: "Foo", FQN: "pkg.Foo", Fingerprint: "fp-a1", UpdatedAt: time.Now().Unix()},
		{AnchorID: "b1", Lang: codeanchor.Lang("go"), Kind: "func", Path: "pkg/bar.go", Symbol: "Bar", FQN: "pkg.Bar", Fingerprint: "fp-b1", UpdatedAt: time.Now().Unix()},
	}}
	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := sqlite.Open(filepath.Join(tempDir, "code.db"), prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	syncer := Syncer{
		Index:        store,
		Provider:     prov,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:        intel,
		Root:         tempDir,
		Policy:       singleChunkPolicy{},
	}
	require.NoError(t, syncer.Sync(context.Background()))

	intel.anchors = []codeanchor.IntelAnchor{
		{AnchorID: "b1", Lang: codeanchor.Lang("go"), Kind: "func", Path: "pkg/bar.go", Symbol: "Bar", FQN: "pkg.Bar", Fingerprint: "fp-b1", UpdatedAt: time.Now().Unix()},
	}
	require.NoError(t, syncer.SyncPaths(context.Background(), []string{"pkg/foo.go"}))

	items, err := store.ListItems(context.Background())
	require.NoError(t, err)
	var have []string
	for _, item := range items {
		have = append(have, string(item.AnchorID))
	}
	require.Equal(t, []string{"b1"}, have)
}

func TestSyncerSyncPathsBatchesAcrossPaths(t *testing.T) {
	tempDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "pkg", "foo.go"), []byte("package foo\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "pkg", "bar.go"), []byte("package bar\n"), 0o644))

	var anchorsList []codeanchor.IntelAnchor
	for i := 0; i < 3; i++ {
		anchorsList = append(anchorsList,
			codeanchor.IntelAnchor{
				AnchorID:    fmt.Sprintf("fa%d", i),
				Lang:        codeanchor.Lang("go"),
				Kind:        "func",
				Path:        "pkg/foo.go",
				Symbol:      fmt.Sprintf("Foo%d", i),
				FQN:         fmt.Sprintf("pkg.Foo%d", i),
				Fingerprint: fmt.Sprintf("fp-f-%d", i),
				UpdatedAt:   time.Now().Unix(),
			},
			codeanchor.IntelAnchor{
				AnchorID:    fmt.Sprintf("ba%d", i),
				Lang:        codeanchor.Lang("go"),
				Kind:        "func",
				Path:        "pkg/bar.go",
				Symbol:      fmt.Sprintf("Bar%d", i),
				FQN:         fmt.Sprintf("pkg.Bar%d", i),
				Fingerprint: fmt.Sprintf("fp-b-%d", i),
				UpdatedAt:   time.Now().Unix(),
			},
		)
	}

	prov := &batchRecordingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := sqlite.Open(filepath.Join(tempDir, "code.db"), prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	syncer := Syncer{
		Index:        store,
		Provider:     prov,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:        &stubIntelSource{anchors: anchorsList},
		Root:         tempDir,
		Policy:       singleChunkPolicy{},
		CodeEmbedPacker: &EmbedPackerOptions{
			MaxTexts: 6,
			MaxBytes: 1 << 20,
			MinTexts: 4,
			MaxWait:  200 * time.Millisecond,
		},
	}

	require.NoError(t, syncer.SyncPaths(context.Background(), []string{"pkg/foo.go", "pkg/bar.go"}))
	require.Equal(t, int64(2), prov.calls.Load())
	require.ElementsMatch(t, []int{2, 4}, prov.BatchSizes())
}

func TestSyncerPlanPrefetchesReuseCacheAndPrepUsesMemory(t *testing.T) {
	tempDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "pkg", "foo.go"), []byte("package foo\n"), 0o644))

	intel := &stubIntelSource{anchors: []codeanchor.IntelAnchor{{
		AnchorID:    "fa",
		Lang:        codeanchor.Lang("go"),
		Kind:        "func",
		Path:        "pkg/foo.go",
		Symbol:      "Foo",
		FQN:         "pkg.Foo",
		Fingerprint: "fp-fa",
		UpdatedAt:   time.Now().Unix(),
	}}}
	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := sqlite.Open(filepath.Join(tempDir, "code.db"), prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	seedVec, err := prov.inner.EmbedTexts(context.Background(), []string{"shared-text"})
	require.NoError(t, err)
	require.NoError(t, store.CacheEmbedding(context.Background(), "shared-hash", seedVec[0]))

	tracked := &countingEmbeddingLookupIndex{Index: store}
	syncer := Syncer{
		Index:        tracked,
		Provider:     prov,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:        intel,
		Root:         tempDir,
		Policy:       sharedHashChunkPolicy{hash: "shared-hash", text: "shared-text"},
	}

	prepared, err := syncer.PreparePathsEarly(context.Background(), []string{"pkg/foo.go"})
	require.NoError(t, err)
	plan, err := syncer.PlanPreparedPaths(context.Background(), prepared)
	require.NoError(t, err)
	require.Positive(t, tracked.batchCalls.Load())
	require.Zero(t, tracked.pointCalls.Load())
	require.NoError(t, syncer.EmbedWithoutSyncMark(context.Background(), plan))
	require.Zero(t, tracked.pointCalls.Load())
	require.Zero(t, prov.calls.Load())
	chunks, err := store.ItemChunks(context.Background(), "fa")
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	require.Equal(t, seedVec[0], chunks[0].Embedding)
	t.Run("multiple owners use one batch lookup", func(t *testing.T) {
		ctx := context.Background()
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "foo.go"), []byte("package foo\n"), 0o644))
		prov := &countingProvider{inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})}
		store, err := sqlite.Open(filepath.Join(root, "code.db"), prov.Dimensions())
		require.NoError(t, err)
		t.Cleanup(func() { _ = store.Close() })
		var anchors []codeanchor.IntelAnchor
		want := map[codeidx.AnchorID]embeddings.Embedding{}
		for i := 0; i < 4; i++ {
			id := codeidx.AnchorID("a" + strconv.Itoa(i))
			anchors = append(anchors, codeanchor.IntelAnchor{AnchorID: string(id), Lang: codeanchor.LangGo, Kind: "func", Path: "pkg/foo.go", Symbol: "Fn" + strconv.Itoa(i), FQN: "pkg.Fn" + strconv.Itoa(i), Fingerprint: "fp-" + strconv.Itoa(i), UpdatedAt: time.Now().Unix()})
			vecs, err := prov.inner.EmbedTexts(ctx, []string{"anchor:" + string(id)})
			require.NoError(t, err)
			want[id] = vecs[0]
			require.NoError(t, store.CacheEmbedding(ctx, "h-"+string(id), vecs[0]))
		}
		tracked := &countingEmbeddingLookupIndex{Index: store}
		syncer := Syncer{Index: tracked, Provider: prov, ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"}, Intel: &stubIntelSource{anchors: anchors}, Root: root, Policy: singleChunkPolicy{}}
		plan, err := syncer.Plan(ctx)
		require.NoError(t, err)
		require.Positive(t, tracked.batchCalls.Load())
		require.GreaterOrEqual(t, tracked.batchHashes.Load(), int64(4))
		require.NoError(t, syncer.EmbedWithoutSyncMark(ctx, plan))
		require.Zero(t, tracked.pointCalls.Load())
		require.Zero(t, prov.calls.Load())
		for id, vector := range want {
			chunks, err := store.ItemChunks(ctx, id)
			require.NoError(t, err)
			require.Len(t, chunks, 1)
			require.Equal(t, vector, chunks[0].Embedding)
		}
	})

	t.Run("completed owner warms later owner", func(t *testing.T) {
		tempDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "pkg"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(tempDir, "pkg", "foo.go"), []byte("package foo\n"), 0o644))

		intel := &stubIntelSource{anchors: []codeanchor.IntelAnchor{
			{AnchorID: "a1", Lang: codeanchor.Lang("go"), Kind: "func", Path: "pkg/foo.go", Symbol: "Foo", FQN: "pkg.Foo", Fingerprint: "fp-a1", UpdatedAt: time.Now().Unix()},
			{AnchorID: "a2", Lang: codeanchor.Lang("go"), Kind: "func", Path: "pkg/foo.go", Symbol: "Bar", FQN: "pkg.Bar", Fingerprint: "fp-a2", UpdatedAt: time.Now().Unix()},
		}}
		prov := &countingProvider{
			inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
		}
		store, err := sqlite.Open(filepath.Join(tempDir, "code.db"), prov.Dimensions())
		require.NoError(t, err)
		t.Cleanup(func() { _ = store.Close() })

		tracked := &countingEmbeddingLookupIndex{Index: store}
		syncer := Syncer{
			Index:        tracked,
			Provider:     prov,
			ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
			Intel:        intel,
			Root:         tempDir,
			Policy:       sharedHashChunkPolicy{hash: "shared-hash", text: "shared-text"},
		}

		syncer.MaxConcurrent = 1
		ctx := context.Background()
		plan, err := syncer.Plan(ctx)
		require.NoError(t, err)
		require.Len(t, plan.tasks, 2)
		first, second := plan, plan
		first.tasks = plan.tasks[:1]
		second.tasks = plan.tasks[1:]
		require.NoError(t, syncer.EmbedWithoutSyncMark(ctx, first))
		require.EqualValues(t, 1, prov.calls.Load(), "first owner must complete its provider request")
		require.NoError(t, syncer.EmbedWithoutSyncMark(ctx, second))
		require.EqualValues(t, 1, prov.calls.Load(), "second owner should reuse first completed embedding")
		require.Zero(t, tracked.pointCalls.Load())
		want, err := prov.inner.EmbedTexts(ctx, []string{"shared-text"})
		require.NoError(t, err)
		for _, id := range []codeidx.AnchorID{"a1", "a2"} {
			chunks, err := store.ItemChunks(ctx, id)
			require.NoError(t, err)
			require.Len(t, chunks, 1)
			require.Equal(t, want[0], chunks[0].Embedding)
		}
	})
}

func TestPreparePathsEarlyKeepsPrimaryTasksCallInsensitive(t *testing.T) {
	tempDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "pkg", "foo.go"), []byte("package foo\ntype Foo struct{}\n// Bar does work.\nfunc Bar() {}\n"), 0o644))

	intel := &stubIntelSource{anchors: []codeanchor.IntelAnchor{
		{AnchorID: "type1", Lang: codeanchor.Lang("go"), Kind: "type", Path: "pkg/foo.go", Symbol: "Foo", FQN: "pkg.Foo", Fingerprint: "fp-type", UpdatedAt: time.Now().Unix()},
		{AnchorID: "func1", Lang: codeanchor.Lang("go"), Kind: "func", Path: "pkg/foo.go", Symbol: "Bar", FQN: "pkg.Bar", Fingerprint: "fp-func", DocComment: "Bar does work.", UpdatedAt: time.Now().Unix()},
	}}
	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := sqlite.Open(filepath.Join(tempDir, "code.db"), prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	syncer := Syncer{
		Index:        store,
		Provider:     prov,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:        intel,
		Calls:        noopCallLookup{},
		Root:         tempDir,
		Policy:       DefaultSynthesisPolicy(PolicyOptions{Budget: DefaultChunkBudget()}),
	}
	capabilities, ok := syncer.Policy.(CallSensitiveSynthesisPolicy)
	require.True(t, ok)
	require.False(t, capabilities.ModuleChunkUsesCalls("pkg/foo.go", codeanchor.LangGo, intel.anchors, nil))
	require.False(t, capabilities.AnchorChunkUsesCalls(intel.anchors[1], nil, nil))

	prepared, err := syncer.PreparePathsEarly(context.Background(), []string{"pkg/foo.go"})
	require.NoError(t, err)
	require.False(t, prepared.Empty())
	require.False(t, prepared.HasDeferredPaths())

	earlyPlan, err := syncer.PlanPreparedPathsEarly(context.Background(), prepared)
	require.NoError(t, err)
	require.Len(t, earlyPlan.tasks, 2)
	beforeCalls := make(map[codeidx.AnchorID][]string)
	for _, task := range earlyPlan.tasks {
		for _, chunk := range task.payload.chunks {
			beforeCalls[task.id] = append(beforeCalls[task.id], chunk.Text)
		}
	}
	intel.anchors[0].Calls = []string{"service.Search"}
	intel.anchors[1].Calls = []string{"service.Search", "store.Find"}
	withCalls, err := syncer.PreparePathsEarly(context.Background(), []string{"pkg/foo.go"})
	require.NoError(t, err)
	withCallsPlan, err := syncer.PlanPreparedPathsEarly(context.Background(), withCalls)
	require.NoError(t, err)
	for _, task := range withCallsPlan.tasks {
		var text []string
		for _, chunk := range task.payload.chunks {
			text = append(text, chunk.Text)
		}
		require.Equal(t, beforeCalls[task.id], text, "primary chunk changed with calls for %s", task.id)
	}

	combinedPlan, err := syncer.PlanPreparedPaths(context.Background(), prepared)
	require.NoError(t, err)
	require.Len(t, combinedPlan.tasks, 2)

	require.NoError(t, syncer.Embed(context.Background(), earlyPlan))

	deferredPlan, err := syncer.PlanPreparedPathsDeferred(context.Background(), prepared)
	require.NoError(t, err)
	require.True(t, deferredPlan.ShouldSkipEmbed())
	require.Empty(t, deferredPlan.tasks)
}

func TestPrepareCodeIndexWorksEarlyBuildsPreparedFromFreshWork(t *testing.T) {
	tempDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "pkg", "foo.go"), []byte("package foo\nfunc Bar() {}\n"), 0o644))

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := sqlite.Open(filepath.Join(tempDir, "code.db"), prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	syncer := Syncer{
		Index:        store,
		Provider:     prov,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:        &stubIntelSource{},
		Root:         tempDir,
		Policy:       singleChunkPolicy{},
	}

	prepared, err := syncer.PrepareCodeIndexWorksEarly(context.Background(), []codeanchor.CodeIndexWork{
		{
			Path:         "pkg/foo.go",
			ReplaceIndex: true,
			IntelAnchors: []codeanchor.IntelAnchor{
				{AnchorID: "func1", Lang: codeanchor.LangGo, Kind: "func", Path: "pkg/foo.go", Symbol: "Bar", FQN: "pkg.Bar", Fingerprint: "fp-func"},
			},
		},
	})
	require.NoError(t, err)
	require.False(t, prepared.Empty())
	require.Equal(t, []string{"pkg/foo.go"}, prepared.Paths())

	plan, err := syncer.PlanPreparedPathsEarly(context.Background(), prepared)
	require.NoError(t, err)
	require.Len(t, plan.tasks, 1)
	require.Equal(t, codeidx.AnchorID("func1"), plan.tasks[0].id)
}

func TestSyncerEmbedWithoutSyncMarkLeavesLastSyncUnsetUntilMarked(t *testing.T) {
	tempDir := t.TempDir()

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := sqlite.Open(filepath.Join(tempDir, "code.db"), prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	syncer := Syncer{
		Index:           store,
		Provider:        prov,
		ProviderInfo:    embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:           &stubIntelSource{anchors: []codeanchor.IntelAnchor{{AnchorID: "a1", Lang: codeanchor.Lang("go"), Kind: "func", Path: "pkg/file.go", Symbol: "Fn", FQN: "pkg.Fn", Fingerprint: "fp-1", UpdatedAt: time.Now().Unix()}}},
		PlanConcurrent:  1,
		Policy:          singleChunkPolicy{},
		EmbeddingWriter: noopEmbeddingWriter{},
	}

	plan, err := syncer.Plan(context.Background())
	require.NoError(t, err)
	require.NotZero(t, plan.TaskCount())

	require.NoError(t, syncer.EmbedWithoutSyncMark(context.Background(), plan))

	meta, ok, err := store.Metadata(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, meta.LastSync.IsZero())
	require.True(t, meta.SourceHighWater.IsZero())

	require.NoError(t, syncer.MarkLastSync(context.Background()))

	meta, ok, err = store.Metadata(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	require.False(t, meta.LastSync.IsZero())
	require.False(t, meta.SourceHighWater.IsZero())
}

func TestSyncerFullSyncUsesSourceHighWaterForSkip(t *testing.T) {
	tempDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "pkg", "file.go"), []byte("package pkg\n"), 0o644))
	ctx := context.Background()

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := sqlite.Open(filepath.Join(tempDir, "code.db"), prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	anchor := codeanchor.IntelAnchor{AnchorID: "a1", Lang: codeanchor.Lang("go"), Kind: "func", Path: "pkg/file.go", Symbol: "Fn", FQN: "pkg.Fn", Fingerprint: "fp-1", UpdatedAt: 100}
	syncer := Syncer{
		Index:          store,
		Provider:       prov,
		ProviderInfo:   embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:          &stubIntelSource{anchors: []codeanchor.IntelAnchor{anchor}},
		Root:           tempDir,
		PlanConcurrent: 1,
		Policy:         singleChunkPolicy{},
	}
	require.NoError(t, syncer.Sync(ctx))
	prov.Reset()

	require.NoError(t, store.UpdateLastSync(ctx, time.Unix(10_000, 0)))
	anchor.Fingerprint = "fp-2"
	anchor.UpdatedAt = 150
	syncer.Intel = &stubIntelSource{anchors: []codeanchor.IntelAnchor{anchor}}

	plan, err := syncer.Plan(ctx)
	require.NoError(t, err)
	require.False(t, plan.ShouldSkipEmbed(), "future diagnostic last_sync must not hide newer source rows")
	require.NoError(t, syncer.Embed(ctx, plan))

	meta, ok, err := store.Metadata(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, int64(150), meta.SourceHighWater.Unix())
}

func TestSyncerSyncPathsDoesNotMarkLastSync(t *testing.T) {
	tempDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "pkg", "file.go"), []byte("package pkg\n"), 0o644))

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := sqlite.Open(filepath.Join(tempDir, "code.db"), prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	syncer := Syncer{
		Index:        store,
		Provider:     prov,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel: &stubIntelSource{anchors: []codeanchor.IntelAnchor{{
			AnchorID:    "a1",
			Lang:        codeanchor.Lang("go"),
			Kind:        "func",
			Path:        "pkg/file.go",
			Symbol:      "Fn",
			FQN:         "pkg.Fn",
			Fingerprint: "fp-1",
			UpdatedAt:   time.Now().Unix(),
		}}},
		Root:   tempDir,
		Policy: singleChunkPolicy{},
	}

	require.NoError(t, syncer.SyncPaths(context.Background(), []string{"pkg/file.go"}))

	meta, ok, err := store.Metadata(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, meta.LastSync.IsZero())
}
