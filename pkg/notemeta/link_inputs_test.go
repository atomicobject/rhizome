package notemeta

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type sourceReadCounter struct {
	fakeNoteReader
	reads map[string]int
	mu    sync.Mutex
}

func (r *sourceReadCounter) GetContents(v obsidian.VaultDefinition, p string) (string, error) {
	r.mu.Lock()
	r.reads[p]++
	r.mu.Unlock()
	return r.fakeNoteReader.GetContents(v, p)
}

func TestNamespaceDeltaReadsOnlyChangedAndDependentSources(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "intel.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	reader := &sourceReadCounter{fakeNoteReader: fakeNoteReader{notes: map[string]string{"source.md": "[[Target]]", "other.md": "[[Unrelated]]"}}, reads: map[string]int{}}
	for i := 0; i < 200; i++ {
		reader.notes[fmt.Sprintf("untouched-%d.md", i)] = "# Unrelated"
	}
	vault := obsidian.VaultDefinition{Path: t.TempDir()}
	_, err = testIndexer(t).EnsureIndexed(ctx, vault, reader, store)
	require.NoError(t, err)
	reader.reads = map[string]int{}
	reader.notes["Target.md"] = "# Target"
	require.NoError(t, testIndexer(t).SyncPaths(ctx, vault, reader, store, []string{"Target.md"}, nil))
	t.Logf("add target read %d source files", len(reader.reads))
	require.Len(t, reader.reads, 2, "only Target.md and its unresolved inbound source require projection")
	require.Equal(t, 1, reader.reads["source.md"])
	require.Equal(t, 1, reader.reads["Target.md"])
	edges, err := store.GraphDocNoteEdges(ctx)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	require.Equal(t, "source.md", edges[0].SrcPath)
	require.Equal(t, "Target.md", edges[0].DstPath)
}
