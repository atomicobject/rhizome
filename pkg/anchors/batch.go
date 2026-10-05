package codeanchor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
)

// CodeIndexWork captures parsed code + intel data for a single file.
//
// Work objects are designed to cross goroutine boundaries and be persisted by a
// single writer lane. Path, Summary.FilePath, intel rows, doc links, rationale,
// and reverse-index rows must all describe the same normalized vault-relative
// file; otherwise later search/file_context joins will silently miss each other.
type CodeIndexWork struct {
	Lang                  Lang
	Path                  string
	Summary               FileSummary
	IntelAnchors          []IntelAnchor
	IntelEdges            []IntelEdge
	IntelFTSRows          []IntelFTSRow
	SymbolRefs            []SymbolRefRow
	ImportRefs            []ImportRefRow
	ModuleDefs            []ModuleDefRow
	ExternalEvidence      ExternalEvidenceBatch
	ExternalEvidenceReady bool
	RefFootprint          DurableRefFootprint
	ScopeReady            ScopeReadyFootprint
	DocLinks              []DocLink
	Rationale             []Rationale
	ReplaceIndex          bool
	HasRefSignals         bool
	DefDeltas             DefDeltas
}

// IntelCodeFileReplace batches intel data for a single code file.
type IntelCodeFileReplace struct {
	Path    string
	Anchors []IntelAnchor
	Edges   []IntelEdge
	FTSRows []IntelFTSRow
}

// DocLinksBatch groups doc links by source path for batch operations.
type DocLinksBatch struct {
	SrcPath string
	Links   []DocLink
}

// NoteIndexWork captures parsed note + intel data for a single note.
type NoteIndexWork struct {
	Path           string
	Note           Note
	KeepLabels     []string
	IntelSections  []IntelDocSection
	IntelMentions  []IntelEdge
	IntelFTSRows   []IntelFTSRow
	DocLinks       []DocLink
	ContentHash    string
	IndexerVersion string
	Mtime          int64
}

type noteIndexMetaSource interface {
	NoteIndexMeta(ctx context.Context, path string) (hash string, indexerVersion string, mtime int64, ok bool, err error)
}

// IntelDocReplace batches intel data for a single note.
type IntelDocReplace struct {
	Path     string
	Sections []IntelDocSection
	Mentions []IntelEdge
	FTSRows  []IntelFTSRow
}

// BuildCodeIndexWork parses a code file and returns the data needed for batched writes.
// Returns (nil, nil) when the file hash is unchanged.
//
// The function intentionally builds durable rows without committing them. That
// lets callers overlap parsing, semantic preparation, and SQLite writes while
// preserving one writer lane. In batch mode it skips resolver-backed intel edges;
// reverse-index and call-edge rebuilds consume SymbolRefs/ImportRefs/ModuleDefs
// after the batch is durable, which avoids N per-file store lookups during ingest.
func (s *Service) BuildCodeIndexWork(ctx context.Context, lang Lang, path string, content []byte) (*CodeIndexWork, error) {
	ref, err := s.codePathRef(path)
	if err != nil {
		return nil, err
	}
	work, err := s.buildCodeIndexWork(ctx, lang, ref, content, true)
	if work != nil && s.tailIdx != nil {
		// Queued parsers share path hints before the writer receives this work.
		s.tailIdx.Add(work.Path)
	}
	return work, err
}

func (s *Service) buildCodeIndexWork(ctx context.Context, lang Lang, ref paths.CodePathRef, content []byte, collectDefDeltas bool) (*CodeIndexWork, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
	}

	relPath := ref.Rel.String()
	idx := s.indexers[lang]
	if idx == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedLanguage, lang)
	}
	newHash := hashBytes(content)
	batchIndexing := isBatchIndexing(ctx)
	if !isForceReindex(ctx) {
		hashCheckStarted := time.Now()
		if prevHash, prevVersion, prevStatus, ok, _ := s.store.FileHash(ctx, relPath); ok && prevHash == newHash && prevVersion == IndexerVersion && prevStatus.TrustedForIndexing() {
			indexingperf.ObserveLatency(ctx, "codeindex.hashcheck", time.Since(hashCheckStarted))
			return nil, nil
		}
		indexingperf.ObserveLatency(ctx, "codeindex.hashcheck", time.Since(hashCheckStarted))
	}

	parseStarted := time.Now()
	summary, err := s.indexFileWithTimeout(ctx, idx, content, ref)
	indexingperf.ObserveLatency(ctx, "codeindex.parse", time.Since(parseStarted))
	if err != nil {
		return nil, err
	}
	normalizeStarted := time.Now()
	normalizeSummaryPaths(&summary, relPath)
	if summary.Lang == "" {
		summary.Lang = lang
	}
	if summary.ParseStatus == "" {
		summary.ParseStatus = ParseOK
	}
	summary.Hash = newHash
	indexingperf.ObserveLatency(ctx, "codeindex.normalize", time.Since(normalizeStarted))

	// Parse failures/timeouts may still carry file metadata, but they are not
	// trusted enough to destructively replace existing symbol/scope rows.
	replaceSummary := summary.ParseStatus.TrustedForIndexing() || len(summary.Symbols) > 0 || len(summary.Supers) > 0 || len(summary.Annotations) > 0
	hasRefSignals := hasAnyCodeRefSignals(summary)
	intelPath := string(paths.NormalizeCode(relPath))
	intelStarted := time.Now()
	anchors, edges, ftsRows := extractIntelCodeFromSummary(intelPath, lang, summary)
	if !batchIndexing {
		// Non-batch/live indexing can afford resolver reads because it needs fresh
		// per-file context immediately. Full batch indexing defers these edges to
		// rebuild passes so worker throughput is not bounded by SQLite lookups.
		edges = append(edges, s.buildIntelCallEdges(ctx, lang, intelPath, summary, anchors)...)
		edges = append(edges, s.buildIntelImportEdges(ctx, lang, intelPath, summary, anchors)...)
		edges = append(edges, s.buildIntelTypeRefEdges(ctx, lang, intelPath, summary, anchors)...)
		edges = append(edges, s.buildIntelMemberRefEdges(ctx, lang, intelPath, summary, anchors)...)
	}

	rationaleStarted := time.Now()
	rationale := ExtractRationale(content, intelPath, lang, summary.Symbols)
	indexingperf.ObserveLatency(ctx, "codeindex.rationale", time.Since(rationaleStarted))

	var docLinks []DocLink
	if s.linker != nil {
		docLinksStarted := time.Now()
		docLinks = s.linker.LinksForCode(relPath, content, FileContext{})
		indexingperf.ObserveLatency(ctx, "codeindex.doclinks", time.Since(docLinksStarted))
	}
	indexingperf.ObserveLatency(ctx, "codeindex.intel", time.Since(intelStarted))

	var symbolRefs []SymbolRefRow
	var importRefs []ImportRefRow
	var moduleDefs []ModuleDefRow
	refFootprint := DurableRefFootprint{Path: relPath}
	scopeReady := ScopeReadyFootprint{Path: relPath}
	if summary.ParseStatus.TrustedForIndexing() {
		// Rebuild paths consume these rows directly, so recovered parses need the
		// same reverse-index materialization as clean parses.
		refsStarted := time.Now()
		symbolRefs = BuildSymbolRefRows(relPath, summary)
		importRefs = BuildImportRefRows(relPath, summary, s.vault)
		moduleDefs = BuildModuleDefRows(relPath, summary, idx)
		refFootprint = BuildDurableRefFootprint(relPath, symbolRefs, importRefs)
		scopeReady = BuildScopeReadyFootprint(relPath, summary)
		indexingperf.ObserveLatency(ctx, "codeindex.refs", time.Since(refsStarted))
	}

	defDeltas := DefDeltas{}
	if replaceSummary && collectDefDeltas {
		deltasStarted := time.Now()
		// Def deltas are intentionally computed from normalized refs, not raw FQN
		// strings, so module/symbol rebuild fallback can stay language-neutral.
		oldSymbolRefs := SymbolRefsFromFQNs(s.symbolFQNsForPath(ctx, relPath), summary.Lang)
		newSymbolRefs := SymbolRefsFromSymbols(summary.Symbols)
		addedSyms, removedSyms := DiffSymbolRefs(oldSymbolRefs, newSymbolRefs)
		defDeltas.AddedSymbols = addedSyms
		defDeltas.RemovedSymbols = removedSyms

		if summary.ParseStatus.TrustedForIndexing() {
			oldModules := s.moduleDefsForPath(ctx, relPath)
			newModules := ModulesFromDefs(moduleDefs)
			addedMods, removedMods := DiffModules(oldModules, newModules)
			defDeltas.AddedModules = addedMods
			defDeltas.RemovedModules = removedMods
		}
		indexingperf.ObserveLatency(ctx, "codeindex.defdeltas", time.Since(deltasStarted))
	}

	return &CodeIndexWork{
		Lang:                  lang,
		Path:                  relPath,
		Summary:               summary,
		IntelAnchors:          anchors,
		IntelEdges:            edges,
		IntelFTSRows:          ftsRows,
		SymbolRefs:            symbolRefs,
		ImportRefs:            importRefs,
		ModuleDefs:            moduleDefs,
		ExternalEvidence:      summary.ExternalEvidence,
		ExternalEvidenceReady: summary.ExternalEvidenceReady,
		RefFootprint:          refFootprint,
		ScopeReady:            scopeReady,
		DocLinks:              docLinks,
		Rationale:             rationale,
		ReplaceIndex:          replaceSummary,
		HasRefSignals:         hasRefSignals,
		DefDeltas:             defDeltas,
	}, nil
}

func (s *Service) symbolFQNsForPath(ctx context.Context, relPath string) []string {
	if s == nil || s.store == nil || strings.TrimSpace(relPath) == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	fqns, err := s.store.SymbolsByFile(ctx, relPath)
	if err != nil {
		return nil
	}
	return fqns
}

func (s *Service) moduleDefsForPath(ctx context.Context, relPath string) []string {
	if s == nil || s.store == nil || strings.TrimSpace(relPath) == "" {
		return nil
	}
	refStore, ok := s.store.(intelRefStore)
	if !ok {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	defsByPath, err := refStore.ModuleDefsByPaths(ctx, []string{relPath})
	if err != nil {
		return nil
	}
	return ModulesFromDefs(defsByPath[relPath])
}

// BuildNoteIndexWorkFromSource parses canonical note source facts and returns
// the data needed for batched anchor/code-doc writes.
func (s *Service) BuildNoteIndexWorkFromSource(ctx context.Context, src NoteSource) (*NoteIndexWork, error) {
	if src == nil {
		return nil, fmt.Errorf("note source is required")
	}
	if src.NoteFormatID() != noteformat.FormatID("markdown") {
		return nil, fmt.Errorf("note index work requires Markdown source, got %q", src.NoteFormatID())
	}
	ref, err := s.notePathRef(src.NotePathString())
	if err != nil {
		return nil, err
	}
	relPath := ref.Rel.String()
	content := src.NoteContentString()
	newHash := strings.TrimSpace(src.NoteContentHashString())
	if newHash == "" {
		newHash = hashBytes([]byte(content))
	}
	if !isForceReindex(ctx) {
		if metaSrc, ok := s.store.(noteIndexMetaSource); ok {
			if prevHash, prevVersion, _, ok, _ := metaSrc.NoteIndexMeta(ctx, relPath); ok && prevHash == newHash && prevVersion == NoteIndexerVersion {
				return nil, nil
			}
		}
	}
	// relPath was resolved from the canonical source above. It is an authored
	// note identity, not Markdown user input, so preserve its extension/casing.
	intelPath := string(paths.NormalizeNotePath(relPath))
	sections, _, ftsRows := extractMarkdownIntelDocSections(intelPath, content)
	noteMtime := src.NoteMtimeUnix()
	for i := range sections {
		sections[i].UpdatedAt = noteMtime
	}
	declarations, err := s.ExtractAnchorDeclarations(ctx, src)
	if err != nil {
		return nil, err
	}
	note := declarations.Note
	// Anchor parsing receives source text, but persistence must use the
	// vault-relative authored identity resolved above rather than its original
	// (possibly absolute) input spelling.
	note.Path = relPath
	mentions := s.resolveNoteMentions(ctx, intelPath, sections, content)

	var docLinks []DocLink
	if s.linker != nil {
		docLinks = s.linker.LinksForNote(note)
	}

	return &NoteIndexWork{
		Path:           relPath,
		Note:           note,
		KeepLabels:     declarations.KeepLabels,
		IntelSections:  sections,
		IntelMentions:  mentions,
		IntelFTSRows:   ftsRows,
		DocLinks:       docLinks,
		ContentHash:    newHash,
		IndexerVersion: NoteIndexerVersion,
		Mtime:          noteMtime,
	}, nil
}

type fileSummaryBatchStore interface {
	ReplaceFileSummariesBatch(ctx context.Context, summaries []FileSummary) error
}

type intelCodeBatchStore interface {
	ReplaceIntelCodeFilesBatch(ctx context.Context, reps []IntelCodeFileReplace) error
}

type intelDocBatchStore interface {
	ReplaceIntelDocSectionsBatch(ctx context.Context, reps []IntelDocReplace) error
}

type docLinksBatchStore interface {
	ReplaceDocLinksForPathsBatch(ctx context.Context, batches []DocLinksBatch) error
}

type rationaleStore interface {
	ReplaceRationaleForPath(ctx context.Context, path string, rationales []Rationale) error
}

type RationaleBatch struct {
	Path       string
	Rationales []Rationale
}

type CodePersistenceBatch struct {
	Summaries               []FileSummary
	Metas                   []FileMeta
	SymbolRefBatches        map[string][]SymbolRefRow
	ImportRefBatches        map[string][]ImportRefRow
	ModuleDefBatches        map[string][]ModuleDefRow
	ExternalEvidenceBatches map[string]ExternalEvidenceBatch
	IntelReps               []IntelCodeFileReplace
	RationaleBatches        []RationaleBatch
}

type codeIndexPersistenceBatchStore interface {
	ApplyCodePersistenceBatch(ctx context.Context, batch CodePersistenceBatch) error
}

type externalEvidenceBatchStore interface {
	ReplaceExternalEvidenceForPathsBatch(ctx context.Context, batches map[string]ExternalEvidenceBatch) error
}

// NoteWithKeepLabels pairs a note with its keep labels for batch operations.
type NoteWithKeepLabels struct {
	Note           Note
	KeepLabels     []string
	ContentHash    string
	IndexerVersion string
	Mtime          int64
}

type noteBatchStore interface {
	UpsertNotesWithCleanupBatch(ctx context.Context, notes []NoteWithKeepLabels) (AnchorUpsertResult, error)
}

type noteMetaBatchStore interface {
	UpsertNoteMetadataBatch(ctx context.Context, metadata map[string]NoteIndexMeta) error
}

// ApplyCodeIndexBatch persists prepared code for live calls and batch writer lanes.
//
// This is the write boundary for code ingest. Preserve the batch-store fast path
// as the owner of summary/meta/ref/intel/rationale durability; pre-writing those
// tables outside it reintroduces duplicate writes to the hottest SQLite tables.
func (s *Service) ApplyCodeIndexBatch(ctx context.Context, works []CodeIndexWork) error {
	if s == nil || s.store == nil || len(works) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	applyStarted := time.Now()
	defer func() {
		indexingperf.ObserveLatency(ctx, "codepersist.apply", time.Since(applyStarted))
	}()
	// Readers can cache old rows during publication, including partial failures.
	defer func() {
		for _, work := range works {
			s.invalidateFile(work.Path)
		}
	}()
	batchIndexing := isBatchIndexing(ctx)
	batchStore, hasBatchStore := s.store.(codeIndexPersistenceBatchStore)

	// Collect summaries and non-destructive parse metadata for publication.
	summaries := make([]FileSummary, 0, len(works))
	metas := make([]FileMeta, 0, len(works))
	stalePaths := make([]string, 0, len(works))
	for _, work := range works {
		if work.ReplaceIndex {
			summaries = append(summaries, work.Summary)
		} else {
			metas = append(metas, FileMeta{
				Path:        work.Path,
				Lang:        work.Summary.Lang,
				Hash:        work.Summary.Hash,
				ParseStatus: work.Summary.ParseStatus,
			})
		}
		if batchIndexing && hasAnyCodeRefSignals(work.Summary) {
			stalePaths = append(stalePaths, work.Path)
		}
	}
	if batchIndexing && len(stalePaths) > 0 {
		if marker, ok := s.store.(callEdgeStaleMarker); ok {
			_ = marker.MarkCallEdgesStale(ctx, stalePaths)
		}
	}

	// Process auxiliary data (tail index, doc links, intel).
	intelReps := make([]IntelCodeFileReplace, 0, len(works))
	var docLinkBatches []DocLinksBatch
	if s.linker != nil {
		docLinkBatches = make([]DocLinksBatch, 0, len(works))
	}
	rationaleBatches := make([]RationaleBatch, 0, len(works))
	refStore, hasRefStore := s.store.(intelRefStore)
	symbolRefBatches := make(map[string][]SymbolRefRow)
	importRefBatches := make(map[string][]ImportRefRow)
	moduleDefBatches := make(map[string][]ModuleDefRow)
	externalEvidenceBatches := make(map[string]ExternalEvidenceBatch)
	for _, work := range works {
		if s.tailIdx != nil {
			s.tailIdx.Add(work.Path)
		}
		if s.linker != nil {
			docLinkBatches = append(docLinkBatches, DocLinksBatch{SrcPath: work.Path, Links: work.DocLinks})
		}
		intelPath := string(paths.NormalizeCode(work.Path))
		rationaleBatches = append(rationaleBatches, RationaleBatch{
			Path:       intelPath,
			Rationales: append([]Rationale(nil), work.Rationale...),
		})

		// Parse failures may refresh metadata/rationale, but must not wipe existing
		// anchors, refs, or external evidence. Successful import-only files still
		// replace refs/evidence even when they have no intel anchor rows.
		if !work.ReplaceIndex {
			s.updateIntelCallEdgesOnly(ctx, work.Lang, work.Path, work.Summary)
			continue
		}
		// Successful empty/import-only parses are authoritative replacements too:
		// the empty row set clears stale ambient anchors, edges, and FTS rows while
		// external import evidence persists through its dedicated family.
		intelReps = append(intelReps, IntelCodeFileReplace{
			Path:    intelPath,
			Anchors: work.IntelAnchors,
			Edges:   work.IntelEdges,
			FTSRows: work.IntelFTSRows,
		})
		if work.Summary.ParseStatus.TrustedForIndexing() {
			if hasRefStore {
				symbolRefBatches[work.Path] = append([]SymbolRefRow(nil), work.SymbolRefs...)
				importRefBatches[work.Path] = append([]ImportRefRow(nil), work.ImportRefs...)
				moduleDefBatches[work.Path] = append([]ModuleDefRow(nil), work.ModuleDefs...)
			}
			if work.ExternalEvidenceReady {
				externalEvidenceBatches[work.Path] = cloneExternalEvidenceBatch(work.ExternalEvidence)
			}
		}
	}

	// Batch doc links into a single transaction.
	if len(docLinkBatches) > 0 {
		docLinksStarted := time.Now()
		if batch, ok := s.store.(docLinksBatchStore); ok {
			if err := batch.ReplaceDocLinksForPathsBatch(ctx, docLinkBatches); err != nil {
				return err
			}
		} else {
			for _, b := range docLinkBatches {
				if err := s.store.ReplaceDocLinksForPath(ctx, b.SrcPath, b.Links); err != nil {
					return err
				}
			}
		}
		indexingperf.ObserveLatency(ctx, "codepersist.doclinks", time.Since(docLinksStarted))
	}

	if hasBatchStore {
		sqliteBatchStarted := time.Now()
		if err := batchStore.ApplyCodePersistenceBatch(ctx, CodePersistenceBatch{
			Summaries:               summaries,
			Metas:                   metas,
			SymbolRefBatches:        symbolRefBatches,
			ImportRefBatches:        importRefBatches,
			ModuleDefBatches:        moduleDefBatches,
			ExternalEvidenceBatches: externalEvidenceBatches,
			IntelReps:               intelReps,
			RationaleBatches:        rationaleBatches,
		}); err != nil {
			return err
		}
		indexingperf.ObserveLatency(ctx, "codepersist.sqlite_batch", time.Since(sqliteBatchStarted))
		return nil
	}

	if hasRefStore {
		refsStarted := time.Now()
		var refErr error
		if err := refStore.ReplaceIntelSymbolRefsForPathsBatch(ctx, symbolRefBatches); err != nil {
			refErr = errors.Join(refErr, fmt.Errorf("symbol refs: %w", err))
		}
		if err := refStore.ReplaceIntelImportRefsForPathsBatch(ctx, importRefBatches); err != nil {
			refErr = errors.Join(refErr, fmt.Errorf("import refs: %w", err))
		}
		if err := refStore.ReplaceIntelModuleDefsForPathsBatch(ctx, moduleDefBatches); err != nil {
			refErr = errors.Join(refErr, fmt.Errorf("module defs: %w", err))
		}
		indexingperf.ObserveLatency(ctx, "codepersist.refs", time.Since(refsStarted))
		if refErr != nil {
			return fmt.Errorf("reverse-index write failed: %w", refErr)
		}
	}
	if externalStore, ok := s.store.(externalEvidenceBatchStore); ok && len(externalEvidenceBatches) > 0 {
		started := time.Now()
		if err := externalStore.ReplaceExternalEvidenceForPathsBatch(ctx, externalEvidenceBatches); err != nil {
			return err
		}
		indexingperf.ObserveLatency(ctx, "codepersist.external_evidence", time.Since(started))
	}

	if len(intelReps) > 0 {
		intelStarted := time.Now()
		if batch, ok := s.store.(intelCodeBatchStore); ok {
			if err := batch.ReplaceIntelCodeFilesBatch(ctx, intelReps); err != nil {
				return err
			}
		} else if intel, ok := s.store.(intelStore); ok {
			for _, rep := range intelReps {
				if err := intel.ReplaceIntelCodeFile(ctx, rep.Path, rep.Anchors, rep.Edges, rep.FTSRows); err != nil {
					return err
				}
			}
		}
		indexingperf.ObserveLatency(ctx, "codepersist.intel", time.Since(intelStarted))
	}

	// Persist rationale for files that were (re)indexed.
	if rStore, ok := s.store.(rationaleStore); ok {
		rationaleStarted := time.Now()
		for _, batch := range rationaleBatches {
			if err := rStore.ReplaceRationaleForPath(ctx, batch.Path, batch.Rationales); err != nil {
				return err
			}
		}
		indexingperf.ObserveLatency(ctx, "codepersist.rationale", time.Since(rationaleStarted))
	}

	// Generic stores cannot roll back partial writes. Publish freshness only
	// after every supported artifact family succeeds so failures remain retryable.
	summaryStarted := time.Now()
	if batch, ok := s.store.(fileSummaryBatchStore); ok {
		if err := batch.ReplaceFileSummariesBatch(ctx, summaries); err != nil {
			return err
		}
	} else {
		for _, summary := range summaries {
			if err := s.store.ReplaceFileSummary(ctx, summary); err != nil {
				return err
			}
		}
	}
	for _, meta := range metas {
		if err := s.store.UpsertFileMeta(ctx, meta); err != nil {
			return err
		}
	}
	indexingperf.ObserveLatency(ctx, "codepersist.summary_meta", time.Since(summaryStarted))

	return nil
}

// ApplyNoteIndexBatch persists parsed note data in a single writer lane.
// Intended for batch indexing paths that already use BuildNoteIndexWork.
func (s *Service) ApplyNoteIndexBatch(ctx context.Context, works []NoteIndexWork) error {
	if s == nil || s.store == nil || len(works) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// Batch note upserts into a single transaction.
	noteBatches := make([]NoteWithKeepLabels, 0, len(works))
	for _, work := range works {
		noteBatches = append(noteBatches, NoteWithKeepLabels{
			Note:           work.Note,
			KeepLabels:     work.KeepLabels,
			ContentHash:    work.ContentHash,
			IndexerVersion: work.IndexerVersion,
			Mtime:          work.Mtime,
		})
	}
	var upsertResult AnchorUpsertResult
	if batch, ok := s.store.(noteBatchStore); ok {
		var err error
		upsertResult, err = batch.UpsertNotesWithCleanupBatch(ctx, noteBatches)
		if err != nil {
			return err
		}
	} else {
		for _, nb := range noteBatches {
			res, err := s.store.UpsertNoteWithCleanup(ctx, nb.Note, nb.KeepLabels)
			if err != nil {
				return err
			}
			// Aggregate results.
			upsertResult.NewIDs = append(upsertResult.NewIDs, res.NewIDs...)
			upsertResult.ChangedIDs = append(upsertResult.ChangedIDs, res.ChangedIDs...)
			upsertResult.UnchangedIDs = append(upsertResult.UnchangedIDs, res.UnchangedIDs...)
			upsertResult.DeletedIDs = append(upsertResult.DeletedIDs, res.DeletedIDs...)
		}
	}

	// Mark only new/changed anchors dirty for scope recomputation.
	s.markAnchorsDirty(upsertResult.NeedsScopeRecompute())

	// Collect auxiliary data for batched writes.
	intelReps := make([]IntelDocReplace, 0, len(works))
	var docLinkBatches []DocLinksBatch
	if s.linker != nil {
		docLinkBatches = make([]DocLinksBatch, 0, len(works))
	}
	for _, work := range works {
		if s.linker != nil {
			docLinkBatches = append(docLinkBatches, DocLinksBatch{SrcPath: work.Path, Links: work.DocLinks})
		}
		// NoteIndexWork paths are canonical authored note paths produced by
		// BuildNoteIndexWorkFromSource. Do not infer a Markdown suffix here.
		intelPath := string(paths.NormalizeNotePath(work.Path))
		intelReps = append(intelReps, IntelDocReplace{
			Path:     intelPath,
			Sections: work.IntelSections,
			Mentions: work.IntelMentions,
			FTSRows:  work.IntelFTSRows,
		})
		s.invalidateFile(work.Path)
	}

	// Batch doc links into a single transaction.
	if len(docLinkBatches) > 0 {
		if batch, ok := s.store.(docLinksBatchStore); ok {
			if err := batch.ReplaceDocLinksForPathsBatch(ctx, docLinkBatches); err != nil {
				return err
			}
		} else {
			for _, b := range docLinkBatches {
				_ = s.store.ReplaceDocLinksForPath(ctx, b.SrcPath, b.Links)
			}
		}
	}

	if len(intelReps) > 0 {
		if batch, ok := s.store.(intelDocBatchStore); ok {
			if err := batch.ReplaceIntelDocSectionsBatch(ctx, intelReps); err != nil {
				return err
			}
		} else if intel, ok := s.store.(intelStore); ok {
			for _, rep := range intelReps {
				if err := intel.ReplaceIntelDocSections(ctx, rep.Path, rep.Sections, rep.Mentions, rep.FTSRows); err != nil {
					return err
				}
			}
		}
	}

	noteMeta := make(map[string]NoteIndexMeta, len(works))
	for _, work := range works {
		indexerVersion := strings.TrimSpace(work.IndexerVersion)
		if indexerVersion == "" {
			indexerVersion = NoteIndexerVersion
		}
		if strings.TrimSpace(work.ContentHash) == "" {
			continue
		}
		noteMeta[work.Path] = NoteIndexMeta{
			ContentHash:    work.ContentHash,
			IndexerVersion: indexerVersion,
			Mtime:          work.Mtime,
		}
	}
	if len(noteMeta) > 0 {
		if batch, ok := s.store.(noteMetaBatchStore); ok {
			if err := batch.UpsertNoteMetadataBatch(ctx, noteMeta); err != nil {
				return err
			}
		}
	}

	if len(works) > 0 {
		s.invalidateAllFiles()
	}

	return nil
}
