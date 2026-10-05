package notemeta

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// loadAuthoredSources inventories and reads every selected note exactly once
// and stops at authored source identity. Callers that only compare source
// freshness must not pay for a provider projection.
func (i Indexer) loadAuthoredSources(ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader) ([]noteformat.AuthoredSource, error) {
	if noteMgr == nil {
		return nil, fmt.Errorf("note reader is required")
	}
	if provider, ok := noteMgr.(cacheEntriesProvider); ok {
		entries, err := provider.EntriesSnapshot(ctx)
		if err == nil {
			return i.authoredSourcesFromCacheEntries(ctx, entries)
		}
	}
	pathsList, err := noteMgr.GetNotesList(vaultDef)
	if err != nil {
		return nil, err
	}
	pathsList = dedupeStrings(pathsList)
	sources, err := loadBounded(ctx, len(pathsList), func(index int) (noteformat.AuthoredSource, error) {
		return i.readAuthoredSource(vaultDef, noteMgr, pathsList[index])
	})
	if err != nil {
		return nil, err
	}
	sortAuthoredSources(sources)
	return sources, nil
}

func (i Indexer) authoredSourcesFromCacheEntries(ctx context.Context, entries []cache.Entry) ([]noteformat.AuthoredSource, error) {
	selected := make([]cache.Entry, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.Path) == "" {
			continue
		}
		selected = append(selected, entry)
	}
	sources, err := loadBounded(ctx, len(selected), func(index int) (noteformat.AuthoredSource, error) {
		entry := selected[index]
		return i.authoredSource(entry.Path, entry.Content, entry.ModTime.Unix())
	})
	if err != nil {
		return nil, err
	}
	sortAuthoredSources(sources)
	return sources, nil
}

func (i Indexer) readAuthoredSource(vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, notePath string) (noteformat.AuthoredSource, error) {
	content, err := noteMgr.GetContents(vaultDef, notePath)
	if err != nil {
		return noteformat.AuthoredSource{}, fmt.Errorf("read note %s: %w", notePath, err)
	}
	modTime, err := noteMgr.GetModTime(vaultDef, notePath)
	if err != nil {
		return noteformat.AuthoredSource{}, fmt.Errorf("stat note %s: %w", notePath, err)
	}
	return i.authoredSource(notePath, content, modTime.Unix())
}

// authoredSource canonicalizes the path, selects the claiming provider, and
// snapshots bytes plus filesystem facts. It never projects.
func (i Indexer) authoredSource(path, content string, mtime int64) (noteformat.AuthoredSource, error) {
	formats, err := i.runtime()
	if err != nil {
		return noteformat.AuthoredSource{}, err
	}
	notePath, err := paths.CleanNotePath(path)
	if err != nil {
		return noteformat.AuthoredSource{}, fmt.Errorf("canonical note path %q: %w", path, err)
	}
	provider, ok := formats.ProviderForPath(paths.RelPath(notePath))
	if !ok {
		return noteformat.AuthoredSource{}, fmt.Errorf("no note format provider claims %q", notePath)
	}
	source, err := noteformat.NewAuthoredSource(notePath, provider.Descriptor(), []byte(content), mtime)
	if err != nil {
		return noteformat.AuthoredSource{}, fmt.Errorf("create authored source %q: %w", notePath, err)
	}
	return source, nil
}

func sortAuthoredSources(sources []noteformat.AuthoredSource) {
	sort.SliceStable(sources, func(left, right int) bool {
		return sources[left].Path().String() < sources[right].Path().String()
	})
}

func sourcePaths(sources []noteformat.AuthoredSource) []string {
	out := make([]string, 0, len(sources))
	for _, source := range sources {
		if path := strings.TrimSpace(source.Path().String()); path != "" {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return dedupeStrings(out)
}

// notesHashFromSources must stay digest-for-digest identical to the note-entry
// and cache-entry hashes; freshness comparisons span all three.
func notesHashFromSources(sources []noteformat.AuthoredSource) string {
	var aggregate [sha256.Size]byte
	for _, source := range sources {
		path := strings.TrimSpace(source.Path().String())
		if path == "" {
			continue
		}
		xorDigest(&aggregate, noteStateDigest(path, source.ContentHash()))
	}
	return hex.EncodeToString(aggregate[:])
}

// loadBounded preserves input order while bounding concurrent filesystem and
// projector work to the current Go scheduler parallelism. The caller supplies
// authored paths in deterministic order; results keep those exact positions
// regardless of completion order.
func loadBounded[T any](ctx context.Context, count int, load func(int) (T, error)) ([]T, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	workerCount := runtime.GOMAXPROCS(0)
	if workerCount < 1 {
		workerCount = 1
	}
	if workerCount > count {
		workerCount = count
	}

	type loadJob struct{ index int }
	type loadResult struct {
		index int
		value T
		err   error
	}

	loadCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan loadJob)
	results := make(chan loadResult, workerCount)
	var workers sync.WaitGroup
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				select {
				case <-loadCtx.Done():
					return
				case job, ok := <-jobs:
					if !ok {
						return
					}
					value, err := load(job.index)
					select {
					case results <- loadResult{index: job.index, value: value, err: err}:
					case <-loadCtx.Done():
						return
					}
					if err != nil {
						return
					}
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for index := range count {
			select {
			case jobs <- loadJob{index: index}:
			case <-loadCtx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()

	out := make([]T, count)
	var firstErr error
	for result := range results {
		if result.err != nil {
			if firstErr == nil {
				firstErr = result.err
				cancel()
			}
			continue
		}
		out[result.index] = result.value
	}
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
