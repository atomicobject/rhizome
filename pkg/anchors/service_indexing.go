package codeanchor

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/codefile"
	"github.com/atomicobject/rhizome/pkg/paths"
)

const dirtyAnchorLimit = 10000 // prevent unbounded dirty tracking; falls back to full recompute when exceeded

// InvalidateModuleMetadata notifies language indexers that filesystem-backed
// module metadata changed. Callers are reindexed on the next source event or rebuild.
func (s *Service) InvalidateModuleMetadata(path string) {
	if s == nil {
		return
	}
	for _, idx := range s.indexers {
		if invalidator, ok := idx.(ModuleMetadataInvalidator); ok {
			invalidator.InvalidateModuleMetadata(path)
		}
	}
}

// IndexCodeFile parses and stores code file structure.
func (s *Service) IndexCodeFile(ctx context.Context, lang Lang, path string, content []byte) error {
	ref, err := s.codePathRef(path)
	if err != nil {
		return err
	}
	return s.IndexCodeFileRef(ctx, lang, ref, content)
}

// IndexCodeFileRef parses and stores code file structure using a pre-resolved path reference.
func (s *Service) IndexCodeFileRef(ctx context.Context, lang Lang, ref paths.CodePathRef, content []byte) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
	}
	work, err := s.buildCodeIndexWork(ctx, lang, ref, content, false)
	if err != nil || work == nil {
		return err
	}
	batchIndexing := isBatchIndexing(ctx)
	var oldSignals indexSignals
	if !batchIndexing {
		sigCtx, cancelSig := context.WithTimeout(ctx, 750*time.Millisecond)
		oldSignals = s.collectIndexSignalsBestEffort(sigCtx, work.Path)
		cancelSig()
		if errors.Is(sigCtx.Err(), context.DeadlineExceeded) {
			s.dirtyMu.Lock()
			s.dirtyOverflow = true
			s.dirtyMu.Unlock()
		}
	}
	if err := s.ApplyCodeIndexBatch(ctx, []CodeIndexWork{*work}); err != nil {
		return fmt.Errorf("code publication failed for %s: %w", work.Path, err)
	}
	if !batchIndexing && work.ReplaceIndex {
		newSignals := s.indexSignalsFromSummary(work.Summary)
		sigCtx, cancelSig := context.WithTimeout(ctx, 750*time.Millisecond)
		newSignals.Merge(s.collectIndexSignalsBestEffort(sigCtx, work.Path))
		cancelSig()
		if errors.Is(sigCtx.Err(), context.DeadlineExceeded) {
			s.dirtyMu.Lock()
			s.dirtyOverflow = true
			s.dirtyMu.Unlock()
		}
		newSignals.Merge(oldSignals)
		s.markAnchorsDirtyForSignals(ctx, work.Summary.Lang, newSignals)
	}
	return nil
}

const defaultIndexFileTimeout = 30 * time.Second

func (s *Service) indexFileWithTimeout(ctx context.Context, idx LanguageIndexer, content []byte, ref paths.CodePathRef) (FileSummary, error) {
	if idx == nil {
		return FileSummary{}, ErrUnsupportedLanguage
	}
	timeout := defaultIndexFileTimeout
	if timeout <= 0 {
		return idx.IndexFile(content, ref)
	}

	type result struct {
		summary FileSummary
		err     error
	}
	ch := make(chan result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				path := ref.Rel.String()
				if path == "" {
					path = ref.Abs.String()
				}
				log.Printf("codeanchor: panic indexing %s (%s): %v\n%s", path, idx.Lang(), r, debug.Stack())
				ch <- result{summary: FileSummary{FilePath: path, Lang: idx.Lang(), ParseStatus: ParseErrored}, err: nil}
			}
		}()
		summary, err := idx.IndexFile(content, ref)
		ch <- result{summary: summary, err: err}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return FileSummary{}, ctx.Err()
	case res := <-ch:
		return res.summary, res.err
	case <-timer.C:
		// If an indexer hangs (e.g., a tree-sitter grammar bug), treat it as best-effort by
		// returning an empty summary. Callers should avoid destructive writes in this case
		// (preserve prior symbols/edges) while recording parse status for observability.
		path := ref.Rel.String()
		if path == "" {
			path = ref.Abs.String()
		}
		fallback := FileSummary{FilePath: path, Lang: langFromPath(path), ParseStatus: ParseTimeout}
		return fallback, nil
	}
}

func langFromPath(path string) Lang {
	ext := strings.ToLower(filepath.Ext(path))
	if codefile.IsTypeScriptJavaScriptExtension(ext) {
		return LangTS
	}
	switch ext {
	case ".py":
		return LangPy
	case ".go":
		return LangGo
	case ".cs":
		return LangCs
	default:
		return ""
	}
}

// IngestNoteSource persists anchor declarations and code-doc bridge data from
// canonical note source facts.
func (s *Service) IngestNoteSource(ctx context.Context, src NoteSource) (Note, error) {
	if src == nil {
		return Note{}, fmt.Errorf("note source is required")
	}
	ref, err := s.notePathRef(src.NotePathString())
	if err != nil {
		return Note{}, err
	}
	relPath := ref.Rel.String()
	content := src.NoteContentString()
	contentHash := strings.TrimSpace(src.NoteContentHashString())
	if contentHash == "" {
		contentHash = hashBytes([]byte(content))
	}
	noteMtime := src.NoteMtimeUnix()
	declarations, err := s.ExtractAnchorDeclarations(ctx, src)
	if err != nil {
		return Note{}, err
	}
	note := declarations.Note
	// Anchor parsing consumes source bytes, while durable note identity must use
	// the canonical authored path resolved at the service boundary. This avoids
	// persisting an absolute source path or inferring a Markdown suffix.
	note.Path = relPath

	upsertResult, err := s.store.UpsertNoteWithCleanup(ctx, note, declarations.KeepLabels)
	if err != nil {
		return Note{}, err
	}

	// Best-effort intel persistence (optional).
	_ = s.updateIntelForMarkdownSource(ctx, relPath, content, noteMtime)

	// Doc links for this note (best-effort via injected linker).
	if s.linker != nil {
		_ = s.store.ReplaceDocLinksForPath(ctx, relPath, s.linker.LinksForNote(note))
	}
	if metaStore, ok := s.store.(interface {
		UpsertNoteMetadataBatch(context.Context, map[string]NoteIndexMeta) error
	}); ok {
		_ = metaStore.UpsertNoteMetadataBatch(ctx, map[string]NoteIndexMeta{
			relPath: {
				ContentHash:    contentHash,
				IndexerVersion: NoteIndexerVersion,
				Mtime:          noteMtime,
			},
		})
	} else if metaWriter, ok := s.store.(noteMetaWriter); ok {
		_ = metaWriter.UpsertNoteMeta(ctx, relPath, contentHash, NoteIndexerVersion, noteMtime)
	}

	// Invalidate all cached file contexts after any note ingest.
	s.invalidateAllFiles()
	// Only mark new/changed anchors dirty (not unchanged ones).
	s.markAnchorsDirty(upsertResult.NeedsScopeRecompute())
	s.invalidateFile(relPath)
	return note, nil
}

// NotePaths returns currently stored note file paths (as stored by the indexer).
// Callers can use this to purge stale note intel when running full re-ingest passes.
func (s *Service) NotePaths(ctx context.Context) ([]string, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	if l, ok := s.store.(notePathLister); ok {
		return l.NotePaths(ctx)
	}
	return nil, nil
}

// NoteMtimes returns stored note mtimes (unix seconds) per indexed note path.
// Used for mtime-based change detection during incremental indexing.
func (s *Service) NoteMtimes(ctx context.Context) (map[string]int64, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	if l, ok := s.store.(noteMtimeLister); ok {
		return l.IntelNoteMtimes(ctx)
	}
	return nil, nil
}

// NoteIndexMeta returns stored note metadata (content hash, indexer version, mtime) per indexed note path.
func (s *Service) NoteIndexMeta(ctx context.Context) (map[string]NoteIndexMeta, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	if l, ok := s.store.(noteMetaLister); ok {
		return l.IntelNoteIndexMeta(ctx)
	}
	return nil, nil
}

// TouchNotePaths refreshes stored note mtimes for the given note paths.
func (s *Service) TouchNotePaths(ctx context.Context, pathMtimes map[string]int64) error {
	if s == nil || s.store == nil || len(pathMtimes) == 0 {
		return nil
	}
	if t, ok := s.store.(noteMtimeToucher); ok {
		return t.TouchIntelNotePaths(ctx, pathMtimes)
	}
	return nil
}

// TouchNoteMtimes refreshes stored note mtimes for the given paths.
func (s *Service) TouchNoteMtimes(ctx context.Context, updates map[string]int64) error {
	if s == nil || s.store == nil || len(updates) == 0 {
		return nil
	}
	if t, ok := s.store.(noteMtimeToucher); ok {
		return t.TouchNoteMtimes(ctx, updates)
	}
	return nil
}

// DeleteNote removes a note and its associated anchors from the store.
func (s *Service) DeleteNote(ctx context.Context, path string) error {
	ref, err := s.notePathRef(path)
	if err != nil {
		return err
	}
	relPath := ref.Rel.String()
	affectedAnchors, err := s.store.DeleteNote(ctx, relPath)
	if err != nil {
		return err
	}
	_ = s.deleteIntelByPath(ctx, relPath, true)
	for _, id := range affectedAnchors {
		s.markAnchorsDirty([]int64{id})
	}
	if _, gcErr := s.store.GarbageCollectOrphanedAnchors(ctx); gcErr != nil {
		log.Printf("codeanchor: gc orphan anchors after delete %s: %v", relPath, gcErr)
	}
	s.invalidateFile(relPath)
	if len(affectedAnchors) > 0 {
		s.invalidateAllFiles()
	}
	return nil
}

// GarbageCollect removes orphaned anchors from the store.
func (s *Service) GarbageCollect(ctx context.Context) (int, error) {
	return s.store.GarbageCollectOrphanedAnchors(ctx)
}

// DeleteFile removes a file from the store and invalidates caches.
func (s *Service) DeleteFile(ctx context.Context, path string) error {
	ref, err := s.codePathRef(path)
	if err != nil {
		return err
	}
	relPath := ref.Rel.String()
	s.invalidateFile(relPath)
	if s.tailIdx != nil {
		s.tailIdx.Remove(relPath)
	}
	if err := s.store.DeleteFile(ctx, relPath); err != nil {
		return err
	}
	return nil
}

func normalizeSummaryPaths(summary *FileSummary, relPath string) {
	if summary == nil {
		return
	}
	summary.FilePath = relPath
	for i := range summary.Symbols {
		summary.Symbols[i].File = relPath
	}
	for i := range summary.Calls {
		summary.Calls[i].File = relPath
	}
	for i := range summary.TypeRefs {
		summary.TypeRefs[i].File = relPath
	}
	for i := range summary.MemberRefs {
		summary.MemberRefs[i].File = relPath
	}
}

func (s *Service) collectIndexSignalsBestEffort(ctx context.Context, path string) indexSignals {
	out := indexSignals{
		symPairs: make(map[string]struct{}),
		annTypes: make(map[string]SymbolRef),
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// Existing calls.
	if calls, err := s.store.CallsFromFile(ctx, path); err == nil {
		for _, c := range calls {
			if c.CalleeSymbol.Pkg == "" || c.CalleeSymbol.Name == "" {
				continue
			}
			out.AddPair(c.CalleeSymbol.Pkg, c.CalleeSymbol.Name)
		}
	}
	if refStore, ok := s.store.(intelRefStore); ok {
		if refsByPath, err := refStore.SymbolRefsByPaths(ctx, []string{path}); err == nil {
			for _, row := range refsByPath[path] {
				if row.DstPkg == "" || row.DstName == "" {
					continue
				}
				out.AddPair(row.DstPkg, row.DstName)
			}
		}
	}

	// Existing symbols and their ancestors (for baseClass anchors).
	if fqns, err := s.store.SymbolsByFile(ctx, path); err == nil && len(fqns) > 0 {
		for _, fqn := range fqns {
			pkg, name := splitFQN(fqn)
			if pkg != "" && name != "" {
				out.AddPair(pkg, name)
			}
			if anc, err := s.store.Ancestors(ctx, fqn); err == nil {
				for _, a := range anc {
					pkg, name := splitFQN(a)
					if pkg != "" && name != "" {
						out.AddPair(pkg, name)
					}
				}
			}
		}
		// Existing annotation uses on owners.
		if anns, err := s.store.AnnotationsOnSymbols(ctx, fqns); err == nil {
			for _, use := range anns {
				ref := use.AnnSymbol
				if ref.Name == "" {
					continue
				}
				key := string(ref.Lang) + "\x00" + ref.Pkg + "\x00" + ref.Name
				out.annTypes[key] = ref
			}
		}
	}
	return out
}

func (s *Service) indexSignalsFromSummary(summary FileSummary) indexSignals {
	out := indexSignals{
		symPairs: make(map[string]struct{}),
		annTypes: make(map[string]SymbolRef),
	}
	for _, sym := range summary.Symbols {
		if sym.Pkg == "" || sym.Name == "" {
			continue
		}
		out.AddPair(sym.Pkg, sym.Name)
	}
	for _, call := range summary.Calls {
		if call.CalleeSymbol.Pkg == "" || call.CalleeSymbol.Name == "" {
			continue
		}
		out.AddPair(call.CalleeSymbol.Pkg, call.CalleeSymbol.Name)
	}
	for _, member := range summary.MemberRefs {
		if member.Sym.Pkg == "" || member.Sym.Name == "" {
			continue
		}
		out.AddPair(member.Sym.Pkg, member.Sym.Name)
	}
	for _, ann := range summary.Annotations {
		ref := ann.AnnSymbol
		if ref.Name == "" {
			continue
		}
		key := string(ref.Lang) + "\x00" + ref.Pkg + "\x00" + ref.Name
		out.annTypes[key] = ref
	}
	return out
}

func (s *Service) markAnchorsDirtyForSignals(ctx context.Context, lang Lang, sig indexSignals) {
	if lang == "" {
		return
	}
	if len(sig.symPairs) > 0 {
		names := make([]string, 0, len(sig.symPairs))
		pkgs := make([]string, 0, len(sig.symPairs))
		for k := range sig.symPairs {
			parts := strings.SplitN(k, "\x00", 2)
			if len(parts) != 2 {
				continue
			}
			pkgs = append(pkgs, parts[0])
			names = append(names, parts[1])
		}
		if ids, err := s.store.AnchorsMatchingSymbols(ctx, names, pkgs, lang); err == nil {
			s.markAnchorsDirty(ids)
		}
	}
	if len(sig.annTypes) > 0 {
		refs := make([]SymbolRef, 0, len(sig.annTypes))
		for _, ref := range sig.annTypes {
			refs = append(refs, ref)
		}
		if ids, err := s.store.AnchorsMatchingAnnotations(ctx, refs); err == nil {
			s.markAnchorsDirty(ids)
		}
	}
}

func resolvePathPrefix(prefix string, vaultPaths paths.VaultPaths) (string, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "", nil
	}
	if vaultPaths.Root() == "" {
		return "", fmt.Errorf("missing vault root for path anchor")
	}
	rel, err := vaultPaths.RelCodeStrict(prefix)
	if err != nil {
		return "", err
	}
	return rel.String(), nil
}
