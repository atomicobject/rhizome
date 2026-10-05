package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
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
		indexingperf.ObserveSample(ctx, "codepersist.batch.summaries", int64(len(batch.Summaries)))
		indexingperf.ObserveSample(ctx, "codepersist.batch.refs", int64(len(batch.SymbolRefBatches)+len(batch.ImportRefBatches)+len(batch.ModuleDefBatches)))
		indexingperf.ObserveSample(ctx, "codepersist.batch.external_evidence_paths", int64(len(batch.ExternalEvidenceBatches)))
		indexingperf.ObserveSample(ctx, "codepersist.batch.intel_paths", int64(len(batch.IntelReps)))
		indexingperf.ObserveSample(ctx, "codepersist.batch.rationale_paths", int64(len(batch.RationaleBatches)))

		started := time.Now()
		if err := s.replaceFileSummariesBatchTx(ctx, tx, batch.Summaries); err != nil {
			return err
		}
		if err := s.upsertFileMetasBatchTx(ctx, tx, batch.Metas); err != nil {
			return err
		}
		indexingperf.ObserveLatency(ctx, "codepersist.summary_meta", time.Since(started))

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
		indexingperf.ObserveLatency(ctx, "codepersist.refs", time.Since(started))

		chunks := chunkCodePersistenceIntel(batch.IntelReps, batch.RationaleBatches)
		indexingperf.ObserveSample(ctx, "codepersist.batch.intel_chunks", int64(len(chunks)))
		for _, chunk := range chunks {
			started = time.Now()
			if err := s.replaceIntelCodeFilesBatchTx(ctx, tx, chunk.intelReps); err != nil {
				return err
			}
			indexingperf.ObserveLatency(ctx, "codepersist.intel", time.Since(started))

			started = time.Now()
			if err := s.replaceRationaleForPathsBatchTx(ctx, tx, chunk.rationaleBatches); err != nil {
				return err
			}
			indexingperf.ObserveLatency(ctx, "codepersist.rationale", time.Since(started))
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

func (s *Store) replaceIntelSymbolRefsForPathsBatchTx(ctx context.Context, tx *sql.Tx, batches map[string][]codeanchor.SymbolRefRow) error {
	if len(batches) == 0 {
		return nil
	}
	stmts := []string{
		`DROP TABLE IF EXISTS temp_symbol_ref_batch;`,
		`DROP TABLE IF EXISTS temp_symbol_ref_paths;`,
		`CREATE TEMP TABLE temp_symbol_ref_batch (
			src_path TEXT NOT NULL,
			owner_fqn TEXT NOT NULL,
			ref_kind TEXT NOT NULL,
			dst_lang TEXT NOT NULL,
			dst_pkg TEXT NOT NULL,
			dst_name TEXT NOT NULL,
			dst_fqn TEXT NOT NULL,
			dst_member INTEGER NOT NULL
		);`,
		`CREATE TEMP TABLE temp_symbol_ref_paths (
			src_path TEXT PRIMARY KEY
		) WITHOUT ROWID;`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	defer func() {
		_, _ = tx.ExecContext(context.Background(), `DROP TABLE IF EXISTS temp_symbol_ref_batch`)
		_, _ = tx.ExecContext(context.Background(), `DROP TABLE IF EXISTS temp_symbol_ref_paths`)
	}()

	pathStmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_symbol_ref_paths(src_path) VALUES (?)`)
	if err != nil {
		return err
	}
	defer pathStmt.Close()

	batchStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO temp_symbol_ref_batch (
			src_path, owner_fqn, ref_kind, dst_lang, dst_pkg, dst_name, dst_fqn, dst_member
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer batchStmt.Close()

	for rawPath, rows := range batches {
		path := normalizeCodeLookupPath(rawPath)
		if path == "" {
			continue
		}
		if _, err := pathStmt.ExecContext(ctx, path); err != nil {
			return err
		}
		for _, row := range rows {
			if _, err := batchStmt.ExecContext(ctx,
				path,
				row.OwnerFQN,
				string(row.RefKind),
				string(row.DstLang),
				row.DstPkg,
				row.DstName,
				row.DstFQN,
				row.DstMember,
			); err != nil {
				return err
			}
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO intel_symbol_ref_files(path)
		SELECT src_path FROM temp_symbol_ref_paths
	`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO intel_symbol_ref_targets(dst_lang, dst_pkg, dst_name, dst_fqn, dst_member)
		SELECT DISTINCT dst_lang, dst_pkg, dst_name, dst_fqn, dst_member
		FROM temp_symbol_ref_batch
	`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM intel_symbol_refs
		WHERE src_file_id IN (
			SELECT sf.file_id
			FROM intel_symbol_ref_files sf
			JOIN temp_symbol_ref_paths p ON p.src_path = sf.path
		)
	`); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO intel_symbol_refs (
			src_file_id, owner_symbol_id, owner_fqn, ref_kind, dst_target_id
		)
		SELECT sf.file_id,
		       s.id,
		       b.owner_fqn,
		       b.ref_kind,
		       t.target_id
		FROM temp_symbol_ref_batch b
		JOIN intel_symbol_ref_files sf ON sf.path = b.src_path
		JOIN intel_symbol_ref_targets t
		  ON t.dst_lang = b.dst_lang
		 AND t.dst_pkg = b.dst_pkg
		 AND t.dst_name = b.dst_name
		 AND t.dst_fqn = b.dst_fqn
		 AND t.dst_member = b.dst_member
		LEFT JOIN symbols s ON s.fqn = b.owner_fqn
	`)
	return err
}

func (s *Store) replaceIntelImportRefsForPathsBatchTx(ctx context.Context, tx *sql.Tx, batches map[string][]codeanchor.ImportRefRow) error {
	if len(batches) == 0 {
		return nil
	}
	stmts := []string{
		`DROP TABLE IF EXISTS temp_intel_import_ref_paths;`,
		`DROP TABLE IF EXISTS temp_intel_import_ref_batch;`,
		`CREATE TEMP TABLE temp_intel_import_ref_paths (
			src_path TEXT PRIMARY KEY
		) WITHOUT ROWID;`,
		`CREATE TEMP TABLE temp_intel_import_ref_batch (
			src_path TEXT NOT NULL,
			module TEXT NOT NULL,
			PRIMARY KEY (src_path, module)
		) WITHOUT ROWID;`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	defer func() {
		_, _ = tx.ExecContext(context.Background(), `DROP TABLE IF EXISTS temp_intel_import_ref_paths`)
		_, _ = tx.ExecContext(context.Background(), `DROP TABLE IF EXISTS temp_intel_import_ref_batch`)
	}()
	pathStmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_intel_import_ref_paths(src_path) VALUES (?)`)
	if err != nil {
		return err
	}
	defer pathStmt.Close()
	batchStmt, err := tx.PrepareContext(ctx, `
		INSERT OR IGNORE INTO temp_intel_import_ref_batch (src_path, module)
		VALUES (?, ?)
	`)
	if err != nil {
		return err
	}
	defer batchStmt.Close()
	for rawPath, rows := range batches {
		path := normalizeCodeLookupPath(rawPath)
		if path == "" {
			continue
		}
		if _, err := pathStmt.ExecContext(ctx, path); err != nil {
			return err
		}
		for _, row := range rows {
			if strings.TrimSpace(row.Module) == "" {
				continue
			}
			if _, err := batchStmt.ExecContext(ctx, path, row.Module); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM intel_import_refs
		WHERE src_path IN (SELECT src_path FROM temp_intel_import_ref_paths)
	`); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO intel_import_refs (src_path, module)
		SELECT src_path, module
		FROM temp_intel_import_ref_batch
	`)
	return err
}

func (s *Store) replaceIntelModuleDefsForPathsBatchTx(ctx context.Context, tx *sql.Tx, batches map[string][]codeanchor.ModuleDefRow) error {
	if len(batches) == 0 {
		return nil
	}
	stmts := []string{
		`DROP TABLE IF EXISTS temp_intel_module_def_paths;`,
		`DROP TABLE IF EXISTS temp_intel_module_def_batch;`,
		`CREATE TEMP TABLE temp_intel_module_def_paths (
			src_path TEXT PRIMARY KEY
		) WITHOUT ROWID;`,
		`CREATE TEMP TABLE temp_intel_module_def_batch (
			src_path TEXT NOT NULL,
			lang TEXT NOT NULL,
			module TEXT NOT NULL,
			PRIMARY KEY (src_path, lang, module)
		) WITHOUT ROWID;`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	defer func() {
		_, _ = tx.ExecContext(context.Background(), `DROP TABLE IF EXISTS temp_intel_module_def_paths`)
		_, _ = tx.ExecContext(context.Background(), `DROP TABLE IF EXISTS temp_intel_module_def_batch`)
	}()
	pathStmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_intel_module_def_paths(src_path) VALUES (?)`)
	if err != nil {
		return err
	}
	defer pathStmt.Close()
	batchStmt, err := tx.PrepareContext(ctx, `
		INSERT OR IGNORE INTO temp_intel_module_def_batch (src_path, lang, module)
		VALUES (?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer batchStmt.Close()
	for rawPath, rows := range batches {
		path := normalizeCodeLookupPath(rawPath)
		if path == "" {
			continue
		}
		if _, err := pathStmt.ExecContext(ctx, path); err != nil {
			return err
		}
		for _, row := range rows {
			if strings.TrimSpace(row.Module) == "" {
				continue
			}
			if _, err := batchStmt.ExecContext(ctx, path, string(row.Lang), row.Module); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM intel_module_defs
		WHERE src_path IN (SELECT src_path FROM temp_intel_module_def_paths)
	`); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO intel_module_defs (src_path, lang, module)
		SELECT src_path, lang, module
		FROM temp_intel_module_def_batch
	`)
	return err
}
