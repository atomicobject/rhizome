package presentation

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/stretchr/testify/require"
)

func TestPackHydratesPathOnlyNoteOnceAcrossMultipleResults(t *testing.T) {
	root := t.TempDir()
	path := "notes/guide.md"
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte("# First\nfirst body\n\n## Second\nsecond body\n"), 0o644))

	collector := indexingperf.NewSemanticQueryCollector()
	ctx := indexingperf.WithCollector(context.Background(), collector)
	packed, err := (&DefaultPacker{VaultPath: root}).Pack(ctx, search.QuerySpec{Text: "guide", Budget: search.Budget{Chars: 2_000}}, []search.RankedResult{
		{Candidate: search.Candidate{Type: "note", Path: path, ChunkIndex: 0}, FinalScore: 1},
		{Candidate: search.Candidate{Type: "note", Path: path, ChunkIndex: 1}, FinalScore: 0.9},
	})
	require.NoError(t, err)
	require.Contains(t, packed.Text, "first body")
	require.Contains(t, packed.Text, "second body")

	operations := map[string]int64{}
	for _, operation := range collector.SemanticQueryDiagnostics().Operations {
		operations[operation.Label] = operation.Count
	}
	require.Equal(t, int64(1), operations[indexingperf.SemanticQueryOpBodyReads])
	require.Positive(t, operations[indexingperf.SemanticQueryOpBodyReadBytes])
}

func TestPackRejectsPathOnlyNoteOutsideVault(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "vault")
	require.NoError(t, os.Mkdir(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(parent, "secret.md"), []byte("secret body marker"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "allowed.md"), []byte("allowed body marker"), 0o644))
	packed, err := (&DefaultPacker{VaultPath: root}).Pack(context.Background(), search.QuerySpec{Text: "guide", Budget: search.Budget{Chars: 2_000}}, []search.RankedResult{
		{Candidate: search.Candidate{Type: "note", Path: "../secret.md", ChunkIndex: 0}, FinalScore: 1},
		{Candidate: search.Candidate{Type: "note", Path: "allowed.md", ChunkIndex: 0}, FinalScore: 0.9},
	})
	require.NoError(t, err)
	require.NotContains(t, packed.Text, "secret body marker")
	require.Contains(t, packed.Text, "allowed body marker")
}

type cancelingChunkReader struct {
	cancel context.CancelFunc
}

func (r cancelingChunkReader) GetChunkBody(ctx context.Context, _ string, _ int) (string, error) {
	r.cancel()
	return "", ctx.Err()
}

func TestPackPreservesCancellationDuringLastBodyRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	packer := &DefaultPacker{UseFTSBody: true, CodeIndex: cancelingChunkReader{cancel: cancel}}
	packed, err := packer.Pack(ctx, search.QuerySpec{Text: "example"}, []search.RankedResult{
		{Candidate: search.Candidate{Type: "code", AnchorID: "example", ChunkIndex: 0}},
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, packed.Text)
}
