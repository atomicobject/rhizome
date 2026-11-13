package embedindex

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
	embsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	"github.com/stretchr/testify/require"
)

type cancelledProvider struct {
	embeddings.Provider
	started chan struct{}
}

func (p *cancelledProvider) EmbedTexts(ctx context.Context, _ []string) ([]embeddings.Embedding, error) {
	close(p.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestDerivedCancellationBoundsInFlightCalls(t *testing.T) {
	for _, route := range []string{"direct", "shared", "notes", "code"} {
		t.Run(route, func(t *testing.T) {
			t.Parallel()
			p := &cancelledProvider{started: make(chan struct{})}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			opts := EmbedPackerOptions{MinTexts: 1, MaxTexts: 1, MaxWait: time.Millisecond}
			var run func() error
			switch route {
			case "direct":
				run = func() error { _, err := embedProviderBatch(ctx, p, []string{"text"}, 1, nil); return err }
			case "shared":
				node := NewSharedEmbeddingNode(t.Context(), p, 1, nil, opts)
				defer node.Close()
				run = func() error { _, err := node.Submit(ctx, "test", []string{"text"}); return err }
			case "notes":
				s := &NoteSyncer{Provider: p, MaxConcurrent: 1, NoteEmbedPacker: &opts}
				plan := NotePlan{TotalWork: 1, tasks: []noteTask{{id: "note.md", payload: noteTaskPayload{embedChunks: []embeddings.ChunkInput{embeddings.NewChunkInput(0, "text", "T", "T")}, embedTexts: []string{"text"}}}}}
				run = func() error {
					return s.ComputeDerived(ctx, plan, func(context.Context, NoteDerivedResult) error { return nil })
				}
			case "code":
				index, err := embsqlite.OpenCode(t.Context(), embsqlite.File(filepath.Join(t.TempDir(), "code.db")), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: 8})
				require.NoError(t, err)
				defer index.Close()
				s := &Syncer{Index: index, Provider: p, MaxConcurrent: 1, CodeEmbedPacker: &opts}
				plan := SyncPlan{TotalWork: 1, state: codePlanState{reuseCache: newCodeReuseCache()}, tasks: []codeTask{{id: "owner", payload: codeTaskPayload{chunks: []SemanticChunk{{Input: codeindex.ChunkInput{Index: 0, Hash: "hash"}, Text: "text"}}}}}}
				run = func() error {
					return s.ComputeDerived(ctx, plan, func(context.Context, CodeDerivedResult) error { return nil })
				}
			}
			go func() { done <- run() }()
			<-p.started
			cancel()
			select {
			case err := <-done:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(8 * time.Second):
				t.Fatal("in-flight provider call did not cancel after its grace")
			}
		})
	}
}
