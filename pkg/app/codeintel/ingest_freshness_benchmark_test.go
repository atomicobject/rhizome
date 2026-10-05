package codeintel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/indexingpipe"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
)

const warmNoopCodeFileBytes = 4 * 1024

var benchmarkFilesPattern = regexp.MustCompile(`\bfiles=(\d+)\b`)

func BenchmarkWarmNoopCodeIndex(b *testing.B) {
	for _, fileCount := range []int{1000, 5000} {
		for _, mode := range []string{"candidates", "root"} {
			b.Run(fmt.Sprintf("%s/%d", mode, fileCount), func(b *testing.B) {
				root := b.TempDir()
				codeRoot := filepath.Join(root, "src")
				if err := os.MkdirAll(codeRoot, 0o755); err != nil {
					b.Fatal(err)
				}
				candidates := make([]indexingpipe.FileCandidate, 0, fileCount)
				for n := range fileCount {
					content := benchmarkGoFile(warmNoopCodeFileBytes, n)
					relPath := filepath.ToSlash(filepath.Join("src", fmt.Sprintf("file%05d.go", n)))
					absPath := filepath.Join(root, filepath.FromSlash(relPath))
					if err := os.WriteFile(absPath, content, 0o644); err != nil {
						b.Fatal(err)
					}
					info, err := os.Stat(absPath)
					if err != nil {
						b.Fatal(err)
					}
					candidates = append(candidates, indexingpipe.FileCandidate{
						AbsPath: absPath,
						RelPath: relPath,
						ModTime: info.ModTime().Unix(),
						Kind:    indexingpipe.FileKindCode,
						Lang:    codeanchor.LangGo,
					})
				}

				store, err := semdb.Open(filepath.Join(root, "benchmark.sqlite"))
				if err != nil {
					b.Fatal(err)
				}
				b.Cleanup(func() { _ = store.Close() })
				countingStore := &benchmarkCodePersistenceStore{Store: store}
				indexer := &benchmarkLanguageIndexer{delegate: codeanchor.NewGoIndexer()}
				service := codeanchor.NewServiceWithOptions(
					countingStore,
					[]codeanchor.LanguageIndexer{indexer},
					codeanchor.WithBasePath(root),
					codeanchor.WithoutWarmCache(),
				)
				if result, err := IndexCandidates(context.Background(), service, root, candidates, nil); err != nil || result.Indexed != fileCount {
					b.Fatalf("seed index: indexed=%d err=%v", result.Indexed, err)
				}
				indexer.calls.Store(0)
				countingStore.batches.Store(0)

				var observedReads int64
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					collector := indexingperf.New()
					ctx := indexingperf.WithPhase(indexingperf.WithCollector(context.Background(), collector), "index_code")
					finish := indexingperf.StartSpan(ctx, "index_code")
					var result IndexResult
					var err error
					if mode == "candidates" {
						result, err = IndexCandidates(ctx, service, root, candidates, nil)
					} else {
						result, err = IndexRoot(ctx, service, root, codeRoot, ignore.NewMatcher(nil), nil)
					}
					finish(err)
					if err != nil {
						b.Fatal(err)
					}
					if result.Indexed != 0 || result.Unchanged != fileCount {
						b.Fatalf("indexed=%d unchanged=%d, want 0/%d", result.Indexed, result.Unchanged, fileCount)
					}
					b.StopTimer()
					observedReads += benchmarkReadCount(b, collector.RenderSummary())
					b.StartTimer()
				}
				b.StopTimer()
				if got := indexer.calls.Load(); got != 0 {
					b.Fatalf("warm no-op parsed %d files", got)
				}
				if got := countingStore.batches.Load(); got != 0 {
					b.Fatalf("warm no-op persisted %d batches", got)
				}
				b.ReportMetric(float64(observedReads)/float64(b.N), "reads/op")
				b.ReportMetric(float64(observedReads*warmNoopCodeFileBytes)/float64(b.N), "read-bytes/op")
				b.ReportMetric(float64(indexer.calls.Load())/float64(b.N), "parses/op")
				b.ReportMetric(float64(countingStore.batches.Load())/float64(b.N), "persist-batches/op")
			})
		}
	}
}

type benchmarkLanguageIndexer struct {
	delegate codeanchor.LanguageIndexer
	calls    atomic.Int64
}

func (i *benchmarkLanguageIndexer) IndexFile(content []byte, path paths.CodePathRef) (codeanchor.FileSummary, error) {
	i.calls.Add(1)
	return i.delegate.IndexFile(content, path)
}

func (i *benchmarkLanguageIndexer) Lang() codeanchor.Lang { return i.delegate.Lang() }

type benchmarkCodePersistenceStore struct {
	*semdb.Store
	batches atomic.Int64
}

func (s *benchmarkCodePersistenceStore) ApplyCodePersistenceBatch(ctx context.Context, batch codeanchor.CodePersistenceBatch) error {
	s.batches.Add(1)
	return s.Store.ApplyCodePersistenceBatch(ctx, batch)
}

func benchmarkGoFile(size, ordinal int) []byte {
	prefix := fmt.Sprintf("package bench\n\nfunc Representative%05d() int { return 42 }\n\n// ", ordinal)
	suffix := "\n"
	if size < len(prefix)+len(suffix) {
		panic("benchmark code file size is too small")
	}
	return []byte(prefix + strings.Repeat("x", size-len(prefix)-len(suffix)) + suffix)
}

func benchmarkReadCount(b *testing.B, summary string) int64 {
	b.Helper()
	match := benchmarkFilesPattern.FindStringSubmatch(summary)
	if len(match) != 2 {
		return 0
	}
	count, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		b.Fatalf("parse read count from %q: %v", match[0], err)
	}
	return count
}
