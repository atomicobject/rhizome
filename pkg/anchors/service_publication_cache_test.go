package codeanchor_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/stretchr/testify/require"
)

type codePublicationGate struct {
	armed            atomic.Bool
	entered, release chan struct{}
	once             sync.Once
}

func (g *codePublicationGate) pause(ctx context.Context) error {
	if !g.armed.Swap(false) {
		return nil
	}
	close(g.entered)
	select {
	case <-g.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *codePublicationGate) resume() { g.once.Do(func() { close(g.release) }) }

type codePublicationPausedBatchStore struct {
	*semdb.Store
	gate *codePublicationGate
}

func (s *codePublicationPausedBatchStore) ApplyCodePersistenceBatch(ctx context.Context, batch codeanchor.CodePersistenceBatch) error {
	if err := s.gate.pause(ctx); err != nil {
		return err
	}
	return s.Store.ApplyCodePersistenceBatch(ctx, batch)
}

type codePublicationPausedGenericStore struct {
	codeanchor.Store
	gate *codePublicationGate
	err  error
}

func (s *codePublicationPausedGenericStore) ReplaceFileSummary(ctx context.Context, summary codeanchor.FileSummary) error {
	if summary.FilePath == "failed.go" {
		return s.err
	}
	if err := s.gate.pause(ctx); err != nil {
		return err
	}
	return s.Store.ReplaceFileSummary(ctx, summary)
}

func TestCodePublicationInvalidatesContextAfterWrite(t *testing.T) {
	for _, mode := range []string{"live", "batch", "generic partial failure"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, root, db, _ := codePublicationFixture(t, codeanchor.LangGo)
			gate := &codePublicationGate{entered: make(chan struct{}), release: make(chan struct{})}
			defer gate.resume()
			injected := errors.New("second summary failed")
			var store codeanchor.Store = &codePublicationPausedBatchStore{Store: db, gate: gate}
			if mode == "generic partial failure" {
				store = &codePublicationPausedGenericStore{Store: db, gate: gate, err: injected}
			}
			svc := codeanchor.NewServiceWithOptions(store, []codeanchor.LanguageIndexer{codeanchor.NewGoIndexer()}, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache())
			path := filepath.Join(root, "source.go")
			before := []byte("package audit\nfunc Before() {}\n")
			after := []byte("package audit\nfunc After() {}\n")
			require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangGo, path, before))
			gate.armed.Store(true)
			done := make(chan error, 1)
			go func() {
				if mode == "live" {
					done <- svc.IndexCodeFile(ctx, codeanchor.LangGo, path, after)
					return
				}
				work, err := svc.BuildCodeIndexWork(codeanchor.WithBatchIndexing(ctx), codeanchor.LangGo, path, after)
				if err != nil || work == nil {
					done <- fmt.Errorf("prepare updated work: %v", err)
					return
				}
				works := []codeanchor.CodeIndexWork{*work}
				if mode == "generic partial failure" {
					works = append(works, codeanchor.CodeIndexWork{Path: "failed.go", ReplaceIndex: true,
						Summary: codeanchor.FileSummary{FilePath: "failed.go", Lang: codeanchor.LangGo, Hash: "failed", ParseStatus: codeanchor.ParseOK}})
				}
				done <- svc.ApplyCodeIndexBatch(codeanchor.WithBatchIndexing(ctx), works)
			}()
			select {
			case <-gate.entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			during, err := svc.NotesForFile(ctx, path)
			require.NoError(t, err)
			require.Contains(t, during.Symbols, "example.com/audit.Before")
			gate.resume()
			err = <-done
			if mode == "generic partial failure" {
				require.ErrorIs(t, err, injected)
			} else {
				require.NoError(t, err)
			}
			stored, err := db.SymbolsByFile(ctx, "source.go")
			require.NoError(t, err)
			require.Contains(t, stored, "example.com/audit.After")
			final, err := svc.NotesForFile(ctx, path)
			require.NoError(t, err)
			require.Equal(t, stored, final.Symbols)
			require.NotContains(t, final.Symbols, "example.com/audit.Before")
		})
	}
}

type codePublicationPausedReadStore struct {
	*semdb.Store
	gate *codePublicationGate
}

func (s *codePublicationPausedReadStore) SymbolsByFile(ctx context.Context, path string) ([]string, error) {
	values, err := s.Store.SymbolsByFile(ctx, path)
	if err != nil {
		return nil, err
	}
	if err := s.gate.pause(ctx); err != nil {
		return nil, err
	}
	return values, nil
}

func TestCodeContextDoesNotCacheReadAcrossInvalidation(t *testing.T) {
	for _, invalidation := range []string{"code publication", "note ingestion"} {
		t.Run(invalidation, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, root, db, _ := codePublicationFixture(t, codeanchor.LangGo)
			gate := &codePublicationGate{entered: make(chan struct{}), release: make(chan struct{})}
			defer gate.resume()
			store := &codePublicationPausedReadStore{Store: db, gate: gate}
			svc := codeanchor.NewServiceWithOptions(store, []codeanchor.LanguageIndexer{codeanchor.NewGoIndexer()}, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache())
			path := filepath.Join(root, "source.go")
			before := []byte("package audit\nfunc Before() {}\n")
			after := []byte("package audit\nfunc After() {}\n")
			require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangGo, path, before))
			work, err := svc.BuildCodeIndexWork(ctx, codeanchor.LangGo, path, after)
			require.NoError(t, err)
			require.NotNil(t, work)
			gate.armed.Store(true)
			type readResult struct {
				context codeanchor.FileContext
				err     error
			}
			done := make(chan readResult, 1)
			go func() {
				value, err := svc.NotesForFile(ctx, path)
				done <- readResult{context: value, err: err}
			}()
			select {
			case <-gate.entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if invalidation == "code publication" {
				require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangGo, path, after))
			} else {
				// Publish through the store to isolate whole-cache invalidation
				// from the per-file invalidation in ApplyCodeIndexBatch.
				require.NoError(t, db.ReplaceFileSummary(ctx, work.Summary))
				_, err := svc.IngestNoteSource(ctx, testNoteSource{path: "note.md", content: "# Note\n"})
				require.NoError(t, err)
			}
			gate.resume()
			result := <-done
			require.NoError(t, result.err)
			require.Contains(t, result.context.Symbols, "example.com/audit.Before", "an in-flight read retains its observation")
			for range 2 {
				final, err := svc.NotesForFile(ctx, path)
				require.NoError(t, err)
				require.Contains(t, final.Symbols, "example.com/audit.After")
				require.NotContains(t, final.Symbols, "example.com/audit.Before")
			}
			hits, misses := svc.CacheStats()
			require.EqualValues(t, 1, hits)
			require.EqualValues(t, 2, misses)
		})
	}
}
