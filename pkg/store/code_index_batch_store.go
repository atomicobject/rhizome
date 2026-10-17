package store

import (
	"context"
	"database/sql"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"strings"
	"time"
)

const (
	codePersistenceIntelChunkMaxPaths = 128
	codePersistenceIntelChunkMaxBytes = 2 << 20
)

type codePersistenceIntelChunk struct {
	intelReps        []codeanchor.IntelCodeFileReplace
	rationaleBatches []codeanchor.RationaleBatch
}

func estimateIntelReplaceBytes(rep codeanchor.IntelCodeFileReplace) int {
	size := len(rep.Path) + 32
	for _, anchor := range rep.Anchors {
		size += len(anchor.AnchorID) + len(anchor.Kind) + len(anchor.Path) + len(anchor.Symbol) + len(anchor.FQN) + len(anchor.Fingerprint) + 96
	}
	for _, edge := range rep.Edges {
		size += len(edge.SrcID) + len(edge.DstID) + len(edge.Kind) + len(edge.MetaJSON) + 64
	}
	for _, row := range rep.FTSRows {
		size += len(row.ItemType) + len(row.ItemID) + len(row.Path) + len(row.Title) + len(row.Body) + 64
	}
	return size
}

func estimateRationaleBatchBytes(batch codeanchor.RationaleBatch) int {
	size := len(batch.Path) + 32
	for _, rationale := range batch.Rationales {
		size += len(rationale.ID) + len(rationale.Path) + len(rationale.SymbolFQN) + len(rationale.Content) + len(rationale.Fingerprint) + 80
	}
	return size
}

func chunkCodePersistenceIntel(intelReps []codeanchor.IntelCodeFileReplace, rationaleBatches []codeanchor.RationaleBatch) []codePersistenceIntelChunk {
	if len(intelReps) == 0 && len(rationaleBatches) == 0 {
		return nil
	}

	paths := make([]string, 0, len(intelReps)+len(rationaleBatches))
	seen := make(map[string]struct{}, len(intelReps)+len(rationaleBatches))
	repsByPath := make(map[string][]codeanchor.IntelCodeFileReplace, len(intelReps))
	rationaleByPath := make(map[string][]codeanchor.RationaleBatch, len(rationaleBatches))
	sizeByPath := make(map[string]int, len(intelReps)+len(rationaleBatches))

	for _, rep := range intelReps {
		path := normalizeCodeLookupPath(rep.Path)
		if path == "" {
			continue
		}
		rep.Path = path
		if _, ok := seen[path]; !ok {
			seen[path] = struct{}{}
			paths = append(paths, path)
		}
		repsByPath[path] = append(repsByPath[path], rep)
		sizeByPath[path] += estimateIntelReplaceBytes(rep)
	}
	for _, batch := range rationaleBatches {
		path := strings.TrimSpace(batch.Path)
		if path == "" {
			continue
		}
		batch.Path = path
		if _, ok := seen[path]; !ok {
			seen[path] = struct{}{}
			paths = append(paths, path)
		}
		rationaleByPath[path] = append(rationaleByPath[path], batch)
		sizeByPath[path] += estimateRationaleBatchBytes(batch)
	}

	chunks := make([]codePersistenceIntelChunk, 0, (len(paths)/codePersistenceIntelChunkMaxPaths)+1)
	current := codePersistenceIntelChunk{}
	currentPaths := 0
	currentBytes := 0
	flush := func() {
		if len(current.intelReps) == 0 && len(current.rationaleBatches) == 0 {
			return
		}
		chunks = append(chunks, current)
		current = codePersistenceIntelChunk{}
		currentPaths = 0
		currentBytes = 0
	}

	for _, path := range paths {
		pathBytes := sizeByPath[path]
		if currentPaths > 0 && (currentPaths >= codePersistenceIntelChunkMaxPaths || currentBytes+pathBytes > codePersistenceIntelChunkMaxBytes) {
			flush()
		}
		current.intelReps = append(current.intelReps, repsByPath[path]...)
		current.rationaleBatches = append(current.rationaleBatches, rationaleByPath[path]...)
		currentPaths++
		currentBytes += pathBytes
	}
	flush()
	return chunks
}

// ApplyCodePersistenceBatch atomically publishes freshness and all code artifacts.
// Queue bounds limit normal batches; statement chunks remain inside this transaction.
func (s *Store) ApplyCodePersistenceBatch(ctx context.Context, batch codeanchor.CodePersistenceBatch) error {
	if len(batch.Summaries) == 0 &&
		len(batch.Metas) == 0 &&
		len(batch.SymbolRefBatches) == 0 &&
		len(batch.ImportRefBatches) == 0 &&
		len(batch.ModuleDefBatches) == 0 &&
		len(batch.ExternalEvidenceBatches) == 0 &&
		len(batch.IntelReps) == 0 &&
		len(batch.RationaleBatches) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.replace_code_file")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		indexingperf.ObserveSample(ctx, indexingperf.CodepersistBatchSummaries, int64(len(batch.Summaries)))
		indexingperf.ObserveSample(ctx, indexingperf.CodepersistBatchRefs, int64(len(batch.SymbolRefBatches)+len(batch.ImportRefBatches)+len(batch.ModuleDefBatches)))
		indexingperf.ObserveSample(ctx, indexingperf.CodepersistBatchExternalEvidencePaths, int64(len(batch.ExternalEvidenceBatches)))
		indexingperf.ObserveSample(ctx, indexingperf.CodepersistBatchIntelPaths, int64(len(batch.IntelReps)))
		indexingperf.ObserveSample(ctx, indexingperf.CodepersistBatchRationalePaths, int64(len(batch.RationaleBatches)))

		started := time.Now()
		if err := s.replaceFileSummariesBatchTx(ctx, tx, batch.Summaries); err != nil {
			return err
		}
		if err := s.upsertFileMetasBatchTx(ctx, tx, batch.Metas); err != nil {
			return err
		}
		indexingperf.ObserveLatency(ctx, indexingperf.CodepersistSummaryMeta, time.Since(started))

		started = time.Now()
		if err := s.replaceIntelSymbolRefsForPathsBatchTx(ctx, tx, batch.SymbolRefBatches); err != nil {
			return err
		}
		if err := s.replaceIntelImportRefsForPathsBatchTx(ctx, tx, batch.ImportRefBatches); err != nil {
			return err
		}
		if err := s.replaceIntelModuleDefsForPathsBatchTx(ctx, tx, batch.ModuleDefBatches); err != nil {
			return err
		}
		if err := replaceExternalEvidenceForPathsBatchTx(ctx, tx, batch.ExternalEvidenceBatches); err != nil {
			return err
		}
		indexingperf.ObserveLatency(ctx, indexingperf.CodepersistRefs, time.Since(started))

		chunks := chunkCodePersistenceIntel(batch.IntelReps, batch.RationaleBatches)
		indexingperf.ObserveSample(ctx, indexingperf.CodepersistBatchIntelChunks, int64(len(chunks)))
		for _, chunk := range chunks {
			started = time.Now()
			if err := s.replaceIntelCodeFilesBatchTx(ctx, tx, chunk.intelReps); err != nil {
				return err
			}
			indexingperf.ObserveLatency(ctx, indexingperf.CodepersistIntel, time.Since(started))

			started = time.Now()
			if err := s.replaceRationaleForPathsBatchTx(ctx, tx, chunk.rationaleBatches); err != nil {
				return err
			}
			indexingperf.ObserveLatency(ctx, indexingperf.CodepersistRationale, time.Since(started))
		}
		return nil
	})
}

func (s *Store) upsertFileMetasBatchTx(ctx context.Context, tx *sql.Tx, metas []codeanchor.FileMeta) error {
	if len(metas) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO files(path, lang, hash, indexer_version, parse_status, mtime)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
			lang=excluded.lang,
			hash=excluded.hash,
			indexer_version=excluded.indexer_version,
			parse_status=excluded.parse_status,
			mtime=excluded.mtime
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := time.Now().Unix()
	for _, meta := range metas {
		path := strings.TrimSpace(meta.Path)
		if path == "" {
			continue
		}
		status := strings.TrimSpace(string(meta.ParseStatus))
		if status == "" {
			status = string(codeanchor.ParseOK)
		}
		if _, err := stmt.ExecContext(ctx, path, meta.Lang, meta.Hash, codeanchor.IndexerVersion, status, now); err != nil {
			return err
		}
	}
	return replaceAndInvalidateGoPackageMembershipTx(ctx, tx, nil, metas)
}

func (s *Store) replaceIntelCodeFilesBatchTx(ctx context.Context, tx *sql.Tx, reps []codeanchor.IntelCodeFileReplace) error {
	if len(reps) == 0 {
		return nil
	}
	stmts, err := s.prepareIntelCodeInsertStmts(ctx, tx)
	if err != nil {
		return err
	}
	defer stmts.anchor.Close()
	defer stmts.edge.Close()
	now := time.Now().Unix()
	var allFTS []codeanchor.IntelFTSRow
	for _, r := range reps {
		r.Path = normalizeCodeLookupPath(r.Path)
		if err := s.replaceIntelCodeFileTx(ctx, tx, stmts, r, now); err != nil {
			return err
		}
		if len(r.FTSRows) > 0 {
			allFTS = append(allFTS, r.FTSRows...)
		}
	}
	return s.upsertIntelFTSRowsTx(ctx, tx, allFTS)
}

func (s *Store) replaceRationaleForPathsBatchTx(ctx context.Context, tx *sql.Tx, batches []codeanchor.RationaleBatch) error {
	if len(batches) == 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE IF NOT EXISTS tmp_rationale_paths(path TEXT PRIMARY KEY)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tmp_rationale_paths`); err != nil {
		return err
	}
	pathStmt, err := tx.PrepareContext(ctx, `INSERT INTO tmp_rationale_paths(path) VALUES (?)`)
	if err != nil {
		return err
	}
	defer pathStmt.Close()
	for _, batch := range batches {
		path := strings.TrimSpace(batch.Path)
		if path == "" {
			continue
		}
		if _, err := pathStmt.ExecContext(ctx, path); err != nil {
			return err
		}
	}
	for _, batch := range batches {
		path := strings.TrimSpace(batch.Path)
		if path == "" {
			continue
		}
		if err := s.deleteRationaleFTSByPathTx(ctx, tx, path); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM intel_rationale WHERE path IN (SELECT path FROM tmp_rationale_paths)`); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO intel_rationale
			(rationale_id, path, symbol_fqn, kind, content, start_line, end_line, fingerprint, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := time.Now().Unix()
	for _, batch := range batches {
		for _, r := range batch.Rationales {
			symbolFQN := sql.NullString{String: r.SymbolFQN, Valid: r.SymbolFQN != ""}
			if _, err := stmt.ExecContext(ctx,
				r.ID, r.Path, symbolFQN, string(r.Kind), r.Content,
				r.StartLine, r.EndLine, r.Fingerprint, now,
			); err != nil {
				return err
			}
		}
	}
	for _, batch := range batches {
		path := strings.TrimSpace(batch.Path)
		if path == "" {
			continue
		}
		if err := s.replaceRationaleFTSForPathTx(ctx, tx, path, batch.Rationales); err != nil {
			return err
		}
	}
	return nil
}
