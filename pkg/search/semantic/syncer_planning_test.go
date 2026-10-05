package semantic

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	codeidx "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
	"github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex/sqlite"
	"github.com/stretchr/testify/require"
)

type planningRationaleSource struct {
	stubIntelSource
	mu    sync.Mutex
	loads map[string]int
}

func (s *planningRationaleSource) RationaleForPath(_ context.Context, path string) ([]codeanchor.Rationale, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loads[path]++
	return []codeanchor.Rationale{{Kind: "why", Content: "original rationale", StartLine: 2, EndLine: 2}}, nil
}

type fallbackPlanningPolicy struct{}

func (fallbackPlanningPolicy) BuildModuleChunks(string, codeanchor.Lang, []codeanchor.IntelAnchor, []byte) []SemanticChunk {
	return nil
}
func (fallbackPlanningPolicy) BuildAnchorChunks(_ codeanchor.IntelAnchor, _ []byte, _ []string, context SymbolChunkContext) []SemanticChunk {
	if len(context.Rationale) > 0 {
		context.Rationale[0].Content = "policy mutated its input"
	}
	return nil
}
func (fallbackPlanningPolicy) ShouldIndexAnchor(anchor codeanchor.IntelAnchor) bool {
	return anchor.Kind != "field"
}
func (fallbackPlanningPolicy) ModuleChunkUsesCalls(string, codeanchor.Lang, []codeanchor.IntelAnchor, []byte) bool {
	return false
}
func (fallbackPlanningPolicy) AnchorChunkUsesCalls(codeanchor.IntelAnchor, []byte, []string) bool {
	return false
}

func planningFixture(t *testing.T, policy SynthesisPolicy) (*Syncer, *planningRationaleSource) {
	t.Helper()
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "code.db"), provider.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.UpsertItemMeta(context.Background(), codeidx.Item{AnchorID: "suppressed", Path: "pkg/b.go", Kind: "field", Fingerprint: "suppressed-fp"}))
	require.NoError(t, store.UpsertItemChunks(context.Background(), "suppressed", []codeidx.ChunkInput{{Index: 0, Hash: "stale"}}, []string{"stale"}, []embeddings.Embedding{make(embeddings.Embedding, 8)}))
	source := &planningRationaleSource{stubIntelSource: stubIntelSource{anchors: []codeanchor.IntelAnchor{
		{AnchorID: "module", Lang: codeanchor.LangGo, Kind: "module", Path: "pkg/a.go", Fingerprint: "module-fp", DocComment: "Package planning owns tasks."},
		{AnchorID: "first", Lang: codeanchor.LangGo, Kind: "func", Path: "pkg/a.go", Symbol: "Now", FQN: "pkg.Now", Fingerprint: "first-fp", Signature: "func Now()", StartLine: 1, EndLine: 3},
		{AnchorID: "second", Lang: codeanchor.LangGo, Kind: "func", Path: "pkg/a.go", Symbol: "Later", FQN: "pkg.Later", Fingerprint: "second-fp", Signature: "func Later()", StartLine: 1, EndLine: 3},
		{AnchorID: "suppressed", Lang: codeanchor.LangGo, Kind: "field", Path: "pkg/b.go", Symbol: "Field", FQN: "pkg.Field", Fingerprint: "suppressed-fp"},
	}}, loads: make(map[string]int)}
	return &Syncer{
		Index: store, Provider: provider, ProviderInfo: embeddings.ProviderConfig{Provider: "test"}, Intel: source,
		Policy: policy, Calls: noopCallLookup{}, Loader: func(string) ([]byte, error) { return []byte("package planning\n"), nil },
	}, source
}

func TestSyncerPlanningRoutesPreserveFallbackChunksAndLazyRationale(t *testing.T) {
	fullSyncer, fullSource := planningFixture(t, fallbackPlanningPolicy{})
	fullPlan, err := fullSyncer.Plan(context.Background())
	require.NoError(t, err)
	for _, route := range []string{"full", "prepared", "early"} {
		t.Run(route, func(t *testing.T) {
			plan, source := fullPlan, fullSource
			if route != "full" {
				syncer, preparedSource := planningFixture(t, fallbackPlanningPolicy{})
				source = preparedSource
				ctx := context.Background()
				var err error
				prepared, prepErr := syncer.PreparePathsEarly(ctx, []string{"pkg/b.go", "pkg/a.go", "pkg/a.go"})
				require.NoError(t, prepErr)
				require.False(t, prepared.HasDeferredPaths())
				if route == "early" {
					plan, err = syncer.PlanPreparedPathsEarly(ctx, prepared)
				} else {
					plan, err = syncer.PlanPreparedPaths(ctx, prepared)
				}
				require.NoError(t, err)
			}
			require.Len(t, plan.tasks, 4)
			ids := []codeidx.AnchorID{"module", "first", "second", "suppressed"}
			if route == "full" {
				ids = []codeidx.AnchorID{"module", "second", "first", "suppressed"}
			}
			for i, id := range ids {
				task := plan.tasks[i]
				require.Equal(t, id, task.id)
				require.Equal(t, string(id)+"-fp", task.fingerprint)
				if id == "suppressed" {
					require.Equal(t, "field", task.payload.ownerKind)
					require.Empty(t, task.payload.chunks, "suppressed anchors must remain prune-only")
					continue
				}
				require.Len(t, task.payload.chunks, 1)
				chunk := task.payload.chunks[0]
				require.Zero(t, chunk.Input.Index)
				require.NotEmpty(t, chunk.Input.Hash)
				require.Contains(t, chunk.Input.Breadcrumb, "pkg/a.go")
				if id == "module" {
					require.Equal(t, "module", chunk.Input.Granularity)
					require.Equal(t, "a.go", chunk.Input.Heading)
					require.Contains(t, chunk.Text, "Package planning owns tasks.")
				} else {
					require.Equal(t, "symbol", chunk.Input.Granularity)
					require.Contains(t, chunk.Text, "original rationale")
					require.NotContains(t, chunk.Text, "policy mutated")
				}
			}
			require.Equal(t, map[string]int{"pkg/a.go": 1}, source.loads, "rationale is loaded once per used file, never for suppressed-only files")
			require.ElementsMatch(t, fullPlan.tasks, plan.tasks, "preserve complete chunk text, hashes, metadata and task ownership")
		})
	}
}

type changingPlanningCalls struct{ value string }

func (c *changingPlanningCalls) CalleesForOwnerFQN(context.Context, string, string, int) ([]string, error) {
	return []string{c.value}, nil
}
func (c *changingPlanningCalls) CalleesForFile(context.Context, string, int) ([]string, error) {
	return []string{c.value}, nil
}

type deferredPlanningPolicy struct{ fallbackPlanningPolicy }

func (deferredPlanningPolicy) ModuleChunkUsesCalls(string, codeanchor.Lang, []codeanchor.IntelAnchor, []byte) bool {
	return true
}
func (deferredPlanningPolicy) AnchorChunkUsesCalls(a codeanchor.IntelAnchor, _ []byte, _ []string) bool {
	return a.Symbol == "Later"
}
func (deferredPlanningPolicy) BuildModuleChunks(_ string, _ codeanchor.Lang, anchors []codeanchor.IntelAnchor, _ []byte) []SemanticChunk {
	for _, anchor := range anchors {
		if anchor.Kind == "module" {
			return planningCallChunk("module", anchor.Calls)
		}
	}
	return nil
}
func (deferredPlanningPolicy) BuildAnchorChunks(a codeanchor.IntelAnchor, _ []byte, _ []string, _ SymbolChunkContext) []SemanticChunk {
	if a.Symbol == "Later" {
		return planningCallChunk("anchor", a.Calls)
	}
	return planningCallChunk("early", nil)
}
func planningCallChunk(kind string, calls []string) []SemanticChunk {
	text := kind + ":" + strings.Join(calls, ",")
	return []SemanticChunk{{Input: codeidx.ChunkInput{Index: 0, Hash: text, Granularity: kind}, Text: text}}
}

func TestSyncerPlanningDefersCallSensitiveChunksUntilCallsAreCurrent(t *testing.T) {
	syncer, source := planningFixture(t, deferredPlanningPolicy{})
	calls := &changingPlanningCalls{value: "before-barrier"}
	syncer.Calls = calls
	ctx := context.Background()
	prepared, err := syncer.PreparePathsEarly(ctx, []string{"pkg/a.go", "pkg/b.go"})
	require.NoError(t, err)
	require.Equal(t, []string{"pkg/a.go"}, prepared.deferredPaths)
	require.Len(t, prepared.earlyTasks, 2)
	require.Equal(t, codeidx.AnchorID("first"), prepared.earlyTasks[0].id)
	require.Equal(t, "early:", prepared.earlyTasks[0].payload.chunks[0].Text)
	require.Equal(t, codeidx.AnchorID("suppressed"), prepared.earlyTasks[1].id)
	require.Equal(t, map[string]int{"pkg/a.go": 1}, source.loads)

	calls.value = "after-barrier"
	plan, err := syncer.PlanPreparedPaths(ctx, prepared)
	require.NoError(t, err)
	require.Len(t, plan.tasks, 4)
	require.Equal(t, prepared.earlyTasks, plan.tasks[:2])
	require.Equal(t, codeidx.AnchorID("module"), plan.tasks[2].id)
	require.Equal(t, "module:after-barrier", plan.tasks[2].payload.chunks[0].Text)
	require.Equal(t, codeidx.AnchorID("second"), plan.tasks[3].id)
	require.Equal(t, "anchor:after-barrier", plan.tasks[3].payload.chunks[0].Text)
	require.Equal(t, map[string]int{"pkg/a.go": 2}, source.loads)

	full, _ := planningFixture(t, deferredPlanningPolicy{})
	full.Calls = calls
	fullPlan, err := full.Plan(ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, fullPlan.tasks, plan.tasks)
}

type planningFileLookup struct{ paths []string }

func (*planningFileLookup) CalleesForOwnerFQN(context.Context, string, string, int) ([]string, error) {
	return nil, nil
}
func (p *planningFileLookup) CalleesForFile(_ context.Context, path string, limit int) ([]string, error) {
	p.paths = append(p.paths, path)
	if path == "bad.go" {
		return nil, errors.New("lookup failed")
	}
	if path == "empty.go" {
		return nil, nil
	}
	return []string{fmt.Sprintf("callee-limit-%d", limit)}, nil
}

type planningFileBatchLookup struct {
	planningFileLookup
	batches [][]string
	fail    bool
}

func (*planningFileBatchLookup) CalleesForOwnerFQNs(context.Context, string, []string, int) (map[string][]string, error) {
	return nil, nil
}
func (p *planningFileBatchLookup) CalleesForFiles(_ context.Context, paths []string, limit int) (map[string][]string, error) {
	p.batches = append(p.batches, append([]string(nil), paths...))
	rows := map[string][]string{"good.go": {fmt.Sprintf("callee-limit-%d", limit)}}
	if p.fail {
		return rows, errors.New("batch lookup failed")
	}
	return rows, nil
}

func TestPreparedPlanningFileCallsRemainBestEffortAndBatched(t *testing.T) {
	for _, mode := range []string{"point", "batch", "batch-error"} {
		for _, empty := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/empty=%t", mode, empty), func(t *testing.T) {
				point := &planningFileLookup{}
				batch := &planningFileBatchLookup{fail: mode == "batch-error"}
				var lookup CallLookup = point
				if mode != "point" {
					lookup = batch
				}
				prepared := PreparedCodePaths{moduleByPath: map[string][]codeanchor.IntelAnchor{}}
				if !empty {
					prepared.paths = []string{"bad.go", "empty.go", "good.go"}
					for _, path := range prepared.paths {
						prepared.moduleByPath[path] = []codeanchor.IntelAnchor{{AnchorID: path, Path: path, Kind: "module", Fingerprint: "fp", Lang: codeanchor.LangGo}}
					}
				}
				syncer := &Syncer{Policy: deferredPlanningPolicy{}, Calls: lookup}
				tasks, err := syncer.buildTasksForPreparedPaths(context.Background(), prepared)
				require.NoError(t, err, "call lookup failures do not fail semantic planning")
				require.Len(t, tasks, len(prepared.paths))
				for _, task := range tasks {
					want := "module:"
					if task.id == "good.go" && mode != "batch-error" {
						want += "callee-limit-8"
					}
					require.Equal(t, want, task.payload.chunks[0].Text)
				}
				if mode == "point" {
					require.Equal(t, prepared.paths, point.paths)
				} else {
					require.Equal(t, [][]string{prepared.paths}, batch.batches, "even an empty path list reaches the batch lookup")
					require.Empty(t, batch.paths, "batch failure must not trigger per-file fallback")
				}
			})
		}
	}
}
