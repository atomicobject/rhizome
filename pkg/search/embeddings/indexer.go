package embeddings

// Docs:
// - [Embeddings (Hub)](docs/hubs/Embeddings (Hub).md)
// - [Embeddings - indexing pipeline](docs/reference/analysis/Embeddings - indexing pipeline.md)

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
)

// FileDiscoveryFunc is a function that returns a list of relative file paths to index.
// This allows the indexer to support different file discovery strategies (e.g., glob-based).
type FileDiscoveryFunc func() ([]string, error)

// Indexer coordinates scanning a vault and keeping the embedding index fresh.
type Indexer struct {
	Index         Index
	Provider      Provider
	ProviderInfo  ProviderConfig
	Root          string
	Excludes      []string
	DiscoverFiles FileDiscoveryFunc // optional: custom file discovery function
	BatchSize     int
	MaxConcurrent int
	OnProgress    func(format string, args ...any)
	// OnEmbedProgress reports embedding progress as completed/total notes.
	OnEmbedProgress func(done, total int)
}

// NewIndexer constructs an indexer with sensible defaults for batching and concurrency.
func NewIndexer(idx Index, provider Provider, info ProviderConfig, root string) *Indexer {
	return &Indexer{
		Index:         idx,
		Provider:      provider,
		ProviderInfo:  info,
		Root:          root,
		BatchSize:     0, // prefer provider default
		MaxConcurrent: 0, // prefer provider default
	}
}

// NewIndexerWithDiscovery constructs an indexer that uses a custom file discovery function.
func NewIndexerWithDiscovery(idx Index, provider Provider, info ProviderConfig, root string, discoverFn FileDiscoveryFunc) *Indexer {
	return &Indexer{
		Index:         idx,
		Provider:      provider,
		ProviderInfo:  info,
		Root:          root,
		DiscoverFiles: discoverFn,
		BatchSize:     0, // prefer provider default
		MaxConcurrent: 0, // prefer provider default
	}
}

// ScanVault walks the vault root and returns markdown files with metadata.
// If DiscoverFiles is set, uses custom file discovery; otherwise walks the directory.
func (ix *Indexer) ScanVault() ([]NoteFileInfo, error) {
	if ix.Root == "" {
		return nil, errors.New("indexer root is required")
	}

	// Use custom discovery function if provided
	if ix.DiscoverFiles != nil {
		return ix.scanVaultByDiscovery()
	}

	return ix.scanVaultByWalk()
}

// scanVaultByDiscovery uses the custom DiscoverFiles function.
// DiscoverFiles is expected to already apply all ignore/exclude logic,
// so we trust its output and only perform basic validation here.
func (ix *Indexer) scanVaultByDiscovery() ([]NoteFileInfo, error) {
	files, err := ix.DiscoverFiles()
	if err != nil {
		return nil, fmt.Errorf("discover files: %w", err)
	}

	var notes []NoteFileInfo
	for _, relPath := range files {
		// Normalize path
		relPath = filepath.ToSlash(relPath)

		// Validate: skip absolute paths or malformed paths
		if filepath.IsAbs(relPath) || strings.HasPrefix(relPath, "../") {
			continue
		}

		absPath := filepath.Join(ix.Root, relPath)
		info, err := os.Stat(absPath)
		if err != nil {
			continue // skip files that can't be stat'd
		}

		notes = append(notes, NoteFileInfo{
			ID:    NoteID(relPath),
			Path:  relPath,
			Title: titleFromPath(relPath),
			Size:  info.Size(),
			Mtime: info.ModTime(),
		})
	}

	return notes, nil
}

// scanVaultByWalk is the narrow Markdown compatibility scanner used by the
// legacy constructor. Runtime composition uses DiscoverFiles, whose selected
// catalog already owns note-format authorization.
func (ix *Indexer) scanVaultByWalk() ([]NoteFileInfo, error) {
	matcher := ignore.LoadUnifiedMatcher(ix.Root, ix.Excludes)
	vaultPaths, _ := paths.NewVaultPaths(ix.Root)

	var notes []NoteFileInfo
	err := filepath.WalkDir(ix.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, relErr := vaultPaths.RelStrict(path)
		if relErr != nil {
			return relErr
		}
		rel := relPath.String()

		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != ix.Root {
				return filepath.SkipDir
			}
			if rel != "." && matcher != nil && matcher.IsIgnoredShallow(rel, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(d.Name()) != ".md" || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if matcher != nil && matcher.IsIgnoredShallow(rel, false) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		notes = append(notes, NoteFileInfo{
			ID:    NoteID(rel),
			Path:  rel,
			Title: titleFromPath(rel),
			Size:  info.Size(),
			Mtime: info.ModTime(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return notes, nil
}

// SyncVault refreshes the index for all notes under Root.
func (ix *Indexer) SyncVault(ctx context.Context) error {
	if ix.Index == nil || ix.Provider == nil {
		return errors.New("indexer is missing index or provider")
	}
	ix.progressf("Preparing index schema")
	if err := ix.Index.EnsureSchema(ctx); err != nil {
		return err
	}

	if err := ix.Index.ValidateOrInitMetadata(ctx, IndexMetadata{
		Provider:   ix.ProviderInfo.Provider,
		Model:      ix.ProviderInfo.Model,
		Dimensions: ix.Provider.Dimensions(),
	}); err != nil {
		return err
	}

	existing, err := ix.Index.ListNotes(ctx)
	if err != nil {
		return err
	}
	existingMap := make(map[NoteID]NoteFileInfo, len(existing))
	for _, n := range existing {
		existingMap[n.ID] = n
	}

	ix.progressf("Scanning vault for notes")
	files, err := ix.ScanVault()
	if err != nil {
		return err
	}
	ix.progressf("Found %d notes", len(files))

	ids := make([]NoteID, 0, len(files))
	var tasks []embedTask
	for _, info := range files {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		ids = append(ids, info.ID)
		if err := ix.Index.UpsertNoteMeta(ctx, info); err != nil {
			return fmt.Errorf("upsert meta %s: %w", info.Path, err)
		}

		prev, ok := existingMap[info.ID]
		metaChanged := !ok || !prev.Mtime.Equal(info.Mtime) || prev.Size != info.Size

		// Check chunk hashes to determine if we need to re-embed
		prevChunkHashes, err := ix.Index.ChunkHashes(ctx, info.ID)
		if err != nil {
			return fmt.Errorf("get chunk hashes %s: %w", info.Path, err)
		}

		// Skip only if metadata unchanged AND chunks exist.
		if !metaChanged && len(prevChunkHashes) > 0 {
			continue
		}

		tasks = append(tasks, embedTask{
			info:   info,
			hasEmb: len(prevChunkHashes) > 0,
		})
	}

	if len(tasks) > 0 {
		ix.progressf("Embedding %d changed notes (batch size %d, max concurrency %d)", len(tasks), ix.batchSize(), ix.maxConcurrent())
	} else {
		ix.progressf("No changed notes to embed")
	}
	if err := ix.processTasks(ctx, tasks); err != nil {
		return err
	}

	if err := ix.Index.DeleteNotesNotIn(ctx, ids); err != nil {
		return fmt.Errorf("delete removed notes: %w", err)
	}

	_ = ix.Index.UpdateLastSync(ctx, time.Now())

	if dimmer, ok := ix.Index.(interface{ Dimensions() int }); ok {
		if err := ix.Index.ValidateOrInitMetadata(ctx, IndexMetadata{
			Provider:   ix.ProviderInfo.Provider,
			Model:      ix.ProviderInfo.Model,
			Dimensions: dimmer.Dimensions(),
		}); err != nil {
			return err
		}
	}
	ix.progressf("Index sync complete")
	return nil
}

type embedTask struct {
	info   NoteFileInfo
	hasEmb bool
}

func (ix *Indexer) processTasks(ctx context.Context, tasks []embedTask) error {
	if len(tasks) == 0 {
		return nil
	}
	total := len(tasks)
	if ix.OnEmbedProgress != nil {
		ix.OnEmbedProgress(0, total)
	}
	workerCount := ix.maxConcurrent()
	if workerCount > len(tasks) {
		workerCount = len(tasks)
	}

	taskCh := make(chan embedTask)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	var completed int32

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskCh {
				select {
				case <-ctx.Done():
					return
				default:
				}

				hash, content, err := contentHashForFile(ix.absPath(task.info.Path))
				if err != nil {
					select {
					case errCh <- fmt.Errorf("hash file %s: %w", task.info.Path, err):
					default:
					}
					return
				}
				// Re-index if no existing chunks (hasEmb tracks chunk presence)
				if err := ix.indexNote(ctx, task.info, content, hash, task.hasEmb); err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
				if ix.OnEmbedProgress != nil {
					done := atomic.AddInt32(&completed, 1)
					ix.OnEmbedProgress(int(done), total)
				}
			}
		}()
	}

	for _, task := range tasks {
		select {
		case <-ctx.Done():
			close(taskCh)
			wg.Wait()
			return ctx.Err()
		case err := <-errCh:
			close(taskCh)
			wg.Wait()
			return err
		case taskCh <- task:
		}
	}
	close(taskCh)
	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

func (ix *Indexer) indexNote(ctx context.Context, info NoteFileInfo, content string, fileHash string, unchanged bool) error {
	chunks, err := ChunkNote(info.Path, info.Title, content)
	if err != nil {
		return fmt.Errorf("chunk note %s: %w", info.Path, err)
	}

	existingHashes, err := ix.Index.ChunkHashes(ctx, info.ID)
	if err != nil {
		return fmt.Errorf("load chunk hashes %s: %w", info.Path, err)
	}

	allIndices := make([]int, 0, len(chunks))
	var changedChunks []ChunkInput
	var changedTexts []string
	for _, ch := range chunks {
		allIndices = append(allIndices, ch.Index)
		if existingHashes[ch.Index] == ch.Hash {
			continue
		}
		changedChunks = append(changedChunks, ch)
		changedTexts = append(changedTexts, ch.Text)
	}

	if err := ix.Index.DeleteChunksNotIn(ctx, info.ID, allIndices); err != nil {
		return fmt.Errorf("delete stale chunks %s: %w", info.Path, err)
	}

	if len(changedChunks) > 0 {
		batchSize := ix.batchSize()
		for start := 0; start < len(changedChunks); start += batchSize {
			end := start + batchSize
			if end > len(changedChunks) {
				end = len(changedChunks)
			}
			batchChunks := changedChunks[start:end]
			batchTexts := changedTexts[start:end]
			vecs, err := ix.Provider.EmbedTexts(ctx, batchTexts)
			if err != nil {
				return fmt.Errorf("embed chunks %s: %w", info.Path, err)
			}
			if len(vecs) != len(batchChunks) {
				return fmt.Errorf("expected %d chunk embeddings, got %d", len(batchChunks), len(vecs))
			}
			if err := ix.Index.UpsertNoteChunks(ctx, info.ID, batchChunks, vecs); err != nil {
				return fmt.Errorf("upsert chunks %s: %w", info.Path, err)
			}
		}
	}

	if unchanged {
		return nil
	}
	return nil
}

// UpdateNote indexes a single note given its metadata and content (skips unchanged chunks).
func (ix *Indexer) UpdateNote(ctx context.Context, info NoteFileInfo, content string) error {
	if ix.Index == nil || ix.Provider == nil {
		return errors.New("indexer is missing index or provider")
	}
	if err := ix.Index.UpsertNoteMeta(ctx, info); err != nil {
		return fmt.Errorf("upsert meta %s: %w", info.Path, err)
	}
	prevChunkHashes, err := ix.Index.ChunkHashes(ctx, info.ID)
	if err != nil {
		return fmt.Errorf("get chunk hashes %s: %w", info.Path, err)
	}
	hash := hashContent(content)
	// Skip if chunks exist and content hash matches any previous chunk hash
	hasExistingChunks := len(prevChunkHashes) > 0
	return ix.indexNote(ctx, info, content, hash, hasExistingChunks)
}

func (ix *Indexer) absPath(path string) string {
	if path == "" || filepath.IsAbs(path) || ix.Root == "" {
		return path
	}
	return filepath.Join(ix.Root, filepath.FromSlash(path))
}

func contentHashForFile(path string) (string, string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), string(b), nil
}

func hashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func (ix *Indexer) batchSize() int {
	if ix.BatchSize > 0 {
		return ix.BatchSize
	}
	if d, ok := ix.Provider.(interface{ DefaultBatchSize() int }); ok {
		if v := d.DefaultBatchSize(); v > 0 {
			return v
		}
	}
	return DefaultBatchSize
}

func (ix *Indexer) maxConcurrent() int {
	if ix.MaxConcurrent > 0 {
		return ix.MaxConcurrent
	}
	if d, ok := ix.Provider.(interface{ DefaultMaxConcurrency() int }); ok {
		if v := d.DefaultMaxConcurrency(); v > 0 {
			return v
		}
	}
	return DefaultMaxConcurrent
}

func (ix *Indexer) progressf(format string, args ...any) {
	if ix.OnProgress != nil {
		ix.OnProgress(format, args...)
	}
}

func titleFromPath(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}
