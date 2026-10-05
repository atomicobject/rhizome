package notemeta

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// countingNoteReader observes reader traffic so no-op freshness checks can be
// held to an exact per-note read budget.
type countingNoteReader struct {
	fakeNoteReader
	lists atomic.Int64
	reads atomic.Int64
	stats atomic.Int64
}

func (c *countingNoteReader) GetNotesList(vaultDef obsidian.VaultDefinition) ([]string, error) {
	c.lists.Add(1)
	return c.fakeNoteReader.GetNotesList(vaultDef)
}

func (c *countingNoteReader) GetContents(vaultDef obsidian.VaultDefinition, notePath string) (string, error) {
	c.reads.Add(1)
	return c.fakeNoteReader.GetContents(vaultDef, notePath)
}

func (c *countingNoteReader) GetModTime(vaultDef obsidian.VaultDefinition, notePath string) (time.Time, error) {
	c.stats.Add(1)
	return c.fakeNoteReader.GetModTime(vaultDef, notePath)
}

func (c *countingNoteReader) reset() {
	c.lists.Store(0)
	c.reads.Store(0)
	c.stats.Store(0)
}

// BenchmarkNoopMetadataChecks measures the two whole-vault checks that run when
// nothing changed: graph CLI freshness verification and validation dirty scans.
func BenchmarkNoopMetadataChecks(b *testing.B) {
	ctx := context.Background()
	const noteCount = 1000
	notes := make(map[string]string, noteCount)
	for n := range noteCount {
		notes[fmt.Sprintf("n%04d.md", n)] = fmt.Sprintf("---\naliases: [alias-%d]\n---\n# Note %d\n#tag-%d\nSee [[n0000]].\n", n, n, n%16)
	}

	newFixture := func(b *testing.B) (Indexer, obsidian.VaultDefinition, *countingNoteReader, *semdb.Store) {
		b.Helper()
		root := b.TempDir()
		store, err := semdb.Open(filepath.Join(root, "intel.sqlite"))
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(func() { _ = store.Close() })
		reader := &countingNoteReader{fakeNoteReader: fakeNoteReader{notes: notes}}
		vault := obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth}
		indexer := testIndexer(b)
		if _, err := indexer.EnsureIndexed(ctx, vault, reader, store); err != nil {
			b.Fatal(err)
		}
		return indexer, vault, reader, store
	}

	b.Run("metadata_state_current", func(b *testing.B) {
		indexer, vault, reader, store := newFixture(b)
		b.ReportAllocs()
		reader.reset()
		for b.Loop() {
			current, err := indexer.MetadataStateCurrent(ctx, vault, reader, store)
			if err != nil {
				b.Fatal(err)
			}
			if !current {
				b.Fatal("fixture must stay current")
			}
		}
		b.ReportMetric(float64(reader.reads.Load())/float64(b.N), "reads/op")
		b.ReportMetric(float64(reader.lists.Load())/float64(b.N), "lists/op")
	})

	b.Run("discover_dirty_noop", func(b *testing.B) {
		indexer, vault, reader, store := newFixture(b)
		b.ReportAllocs()
		reader.reset()
		for b.Loop() {
			dirty, err := indexer.DiscoverDirtyPaths(ctx, vault, reader, store)
			if err != nil {
				b.Fatal(err)
			}
			if len(dirty.Changed)+len(dirty.Deleted) != 0 {
				b.Fatalf("fixture must stay clean: %d changed, %d deleted", len(dirty.Changed), len(dirty.Deleted))
			}
		}
		b.ReportMetric(float64(reader.reads.Load())/float64(b.N), "reads/op")
		b.ReportMetric(float64(reader.lists.Load())/float64(b.N), "lists/op")
	})
}

func BenchmarkBuildDeltaAliasCache(b *testing.B) {
	for _, size := range []int{1000, 5000} {
		b.Run(fmt.Sprintf("aliases_%d", size), func(b *testing.B) {
			aliases := make(map[string][]string, size)
			paths := make([]string, size)
			for n := range size {
				paths[n] = fmt.Sprintf("notes/n%04d.md", n)
				aliases[paths[n]] = []string{fmt.Sprintf("alias-%d", n)}
			}

			b.ReportAllocs()
			for b.Loop() {
				cache := obsidian.BuildNotePathCache(paths)
				cache = buildProjectionAliasCache(cache, aliases)
				if len(cache.Aliases) != size {
					b.Fatalf("got %d aliases, want %d", len(cache.Aliases), size)
				}
			}
		})
	}
}

func BenchmarkBuildPathDeltaAliases1000(b *testing.B) {
	ctx := context.Background()
	root := b.TempDir()
	store, err := semdb.Open(filepath.Join(root, "intel.sqlite"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = store.Close() })
	reader := fakeNoteReader{notes: make(map[string]string, 1000)}
	for n := range 1000 {
		reader.notes[fmt.Sprintf("n%04d.md", n)] = fmt.Sprintf("---\naliases: [alias-%d]\n---\nSee [[n0000]].\n", n)
	}
	indexer := testIndexer(b)
	vault := obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth}
	if _, err := indexer.EnsureIndexed(ctx, vault, reader, store); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := indexer.BuildPathDelta(ctx, vault, reader, store, []string{"n0001.md"}, nil); err != nil {
			b.Fatal(err)
		}
	}
}
