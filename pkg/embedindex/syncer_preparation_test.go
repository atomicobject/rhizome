package embedindex

import (
	"context"

	"os"
	"path/filepath"

	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	codeidx "github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
	sqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"

	"github.com/stretchr/testify/require"
)

func TestSyncerProgressTracksQueuedProviderCalls(t *testing.T) {
	tempDir := t.TempDir()

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := sqlite.OpenCode(context.Background(), sqlite.File(filepath.Join(tempDir, "code.db")), sqlite.Options{Access: sqlite.WriteIndex, Dimensions: prov.Dimensions()})
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
	store, err := sqlite.OpenCode(context.Background(), sqlite.File(filepath.Join(tempDir, "code.db")), sqlite.Options{Access: sqlite.WriteIndex, Dimensions: prov.Dimensions()})
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
	store, err := sqlite.OpenCode(context.Background(), sqlite.File(filepath.Join(tempDir, "code.db")), sqlite.Options{Access: sqlite.WriteIndex, Dimensions: prov.Dimensions()})
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
