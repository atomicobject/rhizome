package notemeta

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func BenchmarkEnsureIndexedCapture(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		for _, cached := range []bool{false, true} {
			readerName := "direct"
			if cached {
				readerName = "cached"
			}
			for _, rebuild := range []bool{false, true} {
				mode := "noop"
				if rebuild {
					mode = "full"
				}
				b.Run(fmt.Sprintf("%d/%s/%s", count, readerName, mode), func(b *testing.B) {
					ctx := context.Background()
					root := b.TempDir()
					store, err := sqlitefixture.Open(filepath.Join(root, "intel.sqlite"))
					if err != nil {
						b.Fatal(err)
					}
					b.Cleanup(func() { _ = store.Close() })
					content := "# Note\n" + strings.Repeat("A paragraph of authored content.\n", 64)
					reader := &countingNoteReader{fakeNoteReader: fakeNoteReader{notes: make(map[string]string, count)}}
					snapshotReader := &countingSnapshotReader{}
					for n := range count {
						path := fmt.Sprintf("n%05d.md", n)
						reader.notes[path] = content
						snapshotReader.entries = append(snapshotReader.entries, cache.Entry{Path: path, Content: content, ModTime: time.Unix(1, 0)})
					}
					var notes obsidian.NoteReader = reader
					if cached {
						notes = snapshotReader
					}
					vault := obsidian.VaultDefinition{Path: root}
					indexer := testIndexer(b)
					if _, err := indexer.EnsureIndexed(ctx, vault, notes, store); err != nil {
						b.Fatal(err)
					}
					reader.reset()
					snapshotReader.reads.Store(0)
					snapshotReader.snapshots.Store(0)
					b.ReportAllocs()
					iteration := 0
					for b.Loop() {
						if rebuild {
							changed := content + fmt.Sprintf("iteration %d\n", iteration)
							reader.notes["n00000.md"] = changed
							snapshotReader.entries[0].Content = changed
							iteration++
						}
						result, err := indexer.EnsureIndexed(ctx, vault, notes, store)
						if err != nil {
							b.Fatal(err)
						}
						if result.Dirty != rebuild {
							b.Fatalf("dirty = %v, want %v", result.Dirty, rebuild)
						}
					}
					reads := reader.reads.Load() + snapshotReader.reads.Load()
					b.ReportMetric(float64(reads)/float64(b.N), "source-reads/op")
					b.ReportMetric(float64(reader.stats.Load())/float64(b.N), "stats/op")
					b.ReportMetric(float64(reader.lists.Load())/float64(b.N), "lists/op")
					b.ReportMetric(float64(snapshotReader.snapshots.Load())/float64(b.N), "snapshots/op")
				})
			}
		}
	}
}

type countingSnapshotReader struct {
	fakeCachedMetadataReader
	reads     atomic.Int64
	snapshots atomic.Int64
}

func (r *countingSnapshotReader) EntriesSnapshot(ctx context.Context) ([]cache.Entry, error) {
	entries, err := r.fakeCachedMetadataReader.EntriesSnapshot(ctx)
	r.snapshots.Add(1)
	r.reads.Add(int64(len(entries)))
	return entries, err
}
