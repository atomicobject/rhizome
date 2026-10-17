package store

import (
	"context"
	"database/sql"
	"errors"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"sort"
	"strings"
	"time"
)

// ReferrerPathsBySymbolRefs returns caller paths keyed by symbolRefKey.
func (s *Store) ReferrerPathsBySymbolRefs(ctx context.Context, refs []codeanchor.SymbolRef) (map[string][]string, error) {
	if len(refs) == 0 {
		return map[string][]string{}, nil
	}
	result := make(map[string][]string, len(refs))

	return result, s.withWriteTx(ctx, func(tx *sql.Tx) error {
		_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_symbol_refs`)
		_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_symbol_ref_matches`)
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE temp_symbol_refs (lang TEXT, pkg TEXT, name TEXT, member INTEGER NOT NULL, fqn TEXT, key TEXT PRIMARY KEY)`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE temp_symbol_ref_matches (key TEXT NOT NULL, target_id INTEGER NOT NULL, PRIMARY KEY (key, target_id)) WITHOUT ROWID`); err != nil {
			return err
		}
		stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_symbol_refs(lang, pkg, name, member, fqn, key) VALUES (?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		for _, ref := range refs {
			key := symbolRefKey(ref)
			fqn := codeanchor.NormalizeSymbolRef(ref)
			member := ref.Lang == codeanchor.LangPhp && ref.Member
			if _, err := stmt.ExecContext(ctx, ref.Lang, ref.Pkg, ref.Name, member, fqn, key); err != nil {
				_ = stmt.Close()
				return err
			}
		}
		_ = stmt.Close()

		for _, query := range []string{
			`
				INSERT OR IGNORE INTO temp_symbol_ref_matches(key, target_id)
				SELECT t.key, dst.target_id
				FROM temp_symbol_refs t
				JOIN intel_symbol_ref_targets dst
				  ON t.pkg = ''
				 AND dst.dst_lang = t.lang
				 AND dst.dst_name = t.name
				 AND dst.dst_member = t.member
			`,
			`
				INSERT OR IGNORE INTO temp_symbol_ref_matches(key, target_id)
				SELECT t.key, dst.target_id
				FROM temp_symbol_refs t
				JOIN intel_symbol_ref_targets dst
				  ON t.pkg != ''
				 AND dst.dst_lang = t.lang
				 AND dst.dst_fqn = t.fqn
				 AND dst.dst_member = t.member
			`,
		} {
			if _, err := tx.ExecContext(ctx, query); err != nil {
				return err
			}
		}

		rows, err := tx.QueryContext(ctx, `
			SELECT m.key, sf.path
			FROM temp_symbol_ref_matches m
			JOIN intel_symbol_refs r ON r.dst_target_id = m.target_id
			JOIN intel_symbol_ref_files sf ON sf.file_id = r.src_file_id
			GROUP BY m.key, sf.path
			ORDER BY m.key, sf.path
		`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var key, path string
			if err := rows.Scan(&key, &path); err != nil {
				_ = rows.Close()
				return err
			}
			result[key] = append(result[key], path)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		_ = rows.Close()
		_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_symbol_refs`)
		_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_symbol_ref_matches`)
		return nil
	})
}

// ReferrerPathsByModules returns caller paths keyed by module string.
func (s *Store) ReferrerPathsByModules(ctx context.Context, modules []string) (map[string][]string, error) {
	unique := normalizeNonEmptyStrings(modules)
	if len(unique) == 0 {
		return map[string][]string{}, nil
	}
	result := make(map[string][]string, len(unique))

	return result, s.withWriteTx(ctx, func(tx *sql.Tx) error {
		_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_modules`)
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE temp_modules (module TEXT PRIMARY KEY)`); err != nil {
			return err
		}
		stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_modules(module) VALUES (?)`)
		if err != nil {
			return err
		}
		for _, mod := range unique {
			if _, err := stmt.ExecContext(ctx, mod); err != nil {
				_ = stmt.Close()
				return err
			}
		}
		_ = stmt.Close()

		rows, err := tx.QueryContext(ctx, `
			SELECT t.module, r.src_path
			FROM intel_import_refs r
			JOIN temp_modules t ON r.module = t.module
			GROUP BY t.module, r.src_path
			ORDER BY t.module, r.src_path
		`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var mod, path string
			if err := rows.Scan(&mod, &path); err != nil {
				_ = rows.Close()
				return err
			}
			result[mod] = append(result[mod], path)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		_ = rows.Close()
		_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_modules`)
		return nil
	})
}

type intelCodeInsertStmts struct {
	anchor *sql.Stmt
	edge   *sql.Stmt
}

func (s *Store) prepareIntelCodeInsertStmts(ctx context.Context, tx *sql.Tx) (*intelCodeInsertStmts, error) {
	anchorStmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO intel_code_anchors (
			anchor_id, lang, kind, path, symbol, fqn, signature, doc_comment,
			start_byte, end_byte, start_line, end_line, fingerprint, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return nil, err
	}
	edgeStmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO intel_edges (src_type, src_row_id, dst_type, dst_row_id, kind, meta_json)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		_ = anchorStmt.Close()
		return nil, err
	}
	return &intelCodeInsertStmts{anchor: anchorStmt, edge: edgeStmt}, nil
}

func (s *Store) replaceIntelCodeFileTx(ctx context.Context, tx *sql.Tx, stmts *intelCodeInsertStmts, r codeanchor.IntelCodeFileReplace, now int64) error {
	path := normalizeCodeLookupPath(r.Path)
	if path == "" {
		return nil
	}
	indexingperf.ObserveSample(ctx, indexingperf.CodepersistBatchAnchors, int64(len(r.Anchors)))
	indexingperf.ObserveSample(ctx, indexingperf.CodepersistBatchEdges, int64(len(r.Edges)))
	indexingperf.ObserveSample(ctx, indexingperf.CodepersistBatchFtsRows, int64(len(r.FTSRows)))
	if err := s.deleteIntelCodeByPathTx(ctx, tx, path); err != nil {
		return err
	}
	anchorIDs := make([]string, 0, len(r.Anchors))
	anchorRowIDs := make(map[string]int64, len(r.Anchors))
	anchorInsertStarted := time.Now()
	for _, a := range r.Anchors {
		if a.UpdatedAt == 0 {
			a.UpdatedAt = now
		}
		if _, err := stmts.anchor.ExecContext(ctx,
			a.AnchorID, a.Lang, a.Kind, a.Path, a.Symbol, a.FQN, a.Signature, a.DocComment,
			a.StartByte, a.EndByte, a.StartLine, a.EndLine, a.Fingerprint, a.UpdatedAt,
		); err != nil {
			return err
		}
		anchorIDs = append(anchorIDs, a.AnchorID)
	}
	indexingperf.ObserveLatency(ctx, indexingperf.CodepersistIntelAnchorInsert, time.Since(anchorInsertStarted))
	rowLookupStarted := time.Now()
	if len(anchorIDs) > 0 {
		rowIDs, err := selectIntelAnchorRowIDsByAnchorIDsTx(ctx, tx, anchorIDs)
		if err != nil {
			return err
		}
		for id, rowID := range rowIDs {
			anchorRowIDs[id] = rowID
		}
		indexingperf.AddCount(ctx, indexingperf.CodepersistIntelAnchorRowidLookupCount, int64(len(anchorIDs)))
	}
	indexingperf.ObserveLatency(ctx, indexingperf.CodepersistIntelAnchorRowidLookup, time.Since(rowLookupStarted))
	if len(anchorIDs) > 0 {
		ftsDeleteStarted := time.Now()
		if err := s.deleteIntelFTSByIDsTx(ctx, tx, "anchor", anchorIDs); err != nil {
			return err
		}
		indexingperf.ObserveLatency(ctx, indexingperf.CodepersistIntelFtsDelete, time.Since(ftsDeleteStarted))
	}
	missingIDs := make([]string, 0)
	for _, e := range r.Edges {
		if _, ok := anchorRowIDs[e.SrcID]; !ok && strings.TrimSpace(e.SrcID) != "" {
			missingIDs = append(missingIDs, e.SrcID)
		}
		if _, ok := anchorRowIDs[e.DstID]; !ok && strings.TrimSpace(e.DstID) != "" {
			missingIDs = append(missingIDs, e.DstID)
		}
	}
	if len(missingIDs) > 0 {
		rowLookupStarted = time.Now()
		uniqueMissing := dedupeNonEmptyStrings(missingIDs)
		resolved, err := selectIntelAnchorRowIDsByAnchorIDsTx(ctx, tx, uniqueMissing)
		if err != nil {
			return err
		}
		for id, rowID := range resolved {
			anchorRowIDs[id] = rowID
		}
		indexingperf.AddCount(ctx, indexingperf.CodepersistIntelAnchorRowidLookupCount, int64(len(uniqueMissing)))
		indexingperf.ObserveLatency(ctx, indexingperf.CodepersistIntelAnchorRowidLookup, time.Since(rowLookupStarted))
	}
	edgeInsertStarted := time.Now()
	for _, e := range r.Edges {
		srcRowID := anchorRowIDs[e.SrcID]
		dstRowID := anchorRowIDs[e.DstID]
		if srcRowID <= 0 || dstRowID <= 0 {
			continue
		}
		if _, err := stmts.edge.ExecContext(ctx, "anchor", srcRowID, "anchor", dstRowID, e.Kind, e.MetaJSON); err != nil {
			return err
		}
	}
	indexingperf.ObserveLatency(ctx, indexingperf.CodepersistIntelEdgeInsert, time.Since(edgeInsertStarted))
	return nil
}

type intelDocInsertStmts struct {
	section *sql.Stmt
	edge    *sql.Stmt
}

func (s *Store) prepareIntelDocInsertStmts(ctx context.Context, tx *sql.Tx) (*intelDocInsertStmts, error) {
	sectionStmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO intel_doc_sections (
			section_id, path, title, level, start_byte, end_byte, content, fingerprint, updated_at, source_node_id, ord
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return nil, err
	}
	edgeStmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO intel_edges (src_type, src_row_id, dst_type, dst_row_id, kind, meta_json)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		_ = sectionStmt.Close()
		return nil, err
	}
	return &intelDocInsertStmts{section: sectionStmt, edge: edgeStmt}, nil
}

func (s *Store) replaceIntelDocSectionsTx(ctx context.Context, tx *sql.Tx, stmts *intelDocInsertStmts, r codeanchor.IntelDocReplace, now int64) error {
	path := strings.TrimSpace(r.Path)
	if path == "" {
		return nil
	}
	if err := s.deleteIntelDocSectionsByPathTx(ctx, tx, path); err != nil {
		return err
	}
	sectionIDs := make([]string, 0, len(r.Sections))
	sectionRowIDs := make(map[string]int64, len(r.Sections))
	// Canonical section ordinals use the same ordering as source hydration.
	ordered := append([]codeanchor.IntelDocSection(nil), r.Sections...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].StartByte != ordered[j].StartByte {
			return ordered[i].StartByte < ordered[j].StartByte
		}
		return ordered[i].SectionID < ordered[j].SectionID
	})
	ordinals := make(map[string]int, len(ordered))
	for i, section := range ordered {
		ordinals[section.SectionID] = i
	}
	for _, sec := range r.Sections {
		if sec.UpdatedAt == 0 {
			sec.UpdatedAt = now
		}
		if _, err := stmts.section.ExecContext(ctx,
			sec.SectionID, sec.Path, sec.Title, sec.Level, sec.StartByte, sec.EndByte,
			sec.Content, sec.Fingerprint, sec.UpdatedAt, sec.SourceNodeID, ordinals[sec.SectionID],
		); err != nil {
			return err
		}
		sectionIDs = append(sectionIDs, sec.SectionID)
		rowID, err := lookupIntelSectionRowIDTx(ctx, tx, sec.SectionID)
		if err != nil {
			return err
		}
		sectionRowIDs[sec.SectionID] = rowID
	}
	if len(sectionIDs) > 0 {
		if err := s.deleteIntelFTSByIDsTx(ctx, tx, "doc_section", sectionIDs); err != nil {
			return err
		}
	}
	for _, e := range r.Mentions {
		srcRowID, ok := sectionRowIDs[e.SrcID]
		if !ok {
			var err error
			srcRowID, err = lookupIntelSectionRowIDTx(ctx, tx, e.SrcID)
			if err != nil {
				if !errors.Is(err, sql.ErrNoRows) {
					return err
				}
			}
			sectionRowIDs[e.SrcID] = srcRowID
		}
		dstRef, err := lookupIntelOwnerRefTx(ctx, tx, e.DstID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return err
		}
		if srcRowID <= 0 || dstRef.rowID <= 0 {
			continue
		}
		if _, err := stmts.edge.ExecContext(ctx, "doc_section", srcRowID, dstRef.ownerType, dstRef.rowID, e.Kind, e.MetaJSON); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ReplaceIntelCodeFile(ctx context.Context, path string, anchors []codeanchor.IntelAnchor, edges []codeanchor.IntelEdge, ftsRows []codeanchor.IntelFTSRow) error {
	return s.ReplaceIntelCodeFilesBatch(ctx, []codeanchor.IntelCodeFileReplace{{
		Path:    path,
		Anchors: anchors,
		Edges:   edges,
		FTSRows: ftsRows,
	}})
}

// ReplaceIntelCodeFilesBatch replaces intel data for multiple code files in a single transaction.
func (s *Store) ReplaceIntelCodeFilesBatch(ctx context.Context, reps []codeanchor.IntelCodeFileReplace) error {
	ctx = indexingperf.WithOp(ctx, "intel.replace_code_file")
	if len(reps) == 0 {
		return nil
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
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
		if err := s.upsertIntelFTSRowsTx(ctx, tx, allFTS); err != nil {
			return err
		}
		return nil
	})
}

// ReplaceIntelDocSections replaces all intel doc sections + mention edges for a note path.
func (s *Store) ReplaceIntelDocSections(ctx context.Context, path string, sections []codeanchor.IntelDocSection, mentions []codeanchor.IntelEdge, ftsRows []codeanchor.IntelFTSRow) error {
	return s.ReplaceIntelDocSectionsBatch(ctx, []codeanchor.IntelDocReplace{{
		Path:     path,
		Sections: sections,
		Mentions: mentions,
		FTSRows:  ftsRows,
	}})
}

// ReplaceIntelDocSectionsBatch replaces intel doc sections for multiple notes in a single transaction.
func (s *Store) ReplaceIntelDocSectionsBatch(ctx context.Context, reps []codeanchor.IntelDocReplace) error {
	ctx = indexingperf.WithOp(ctx, "intel.replace_doc_sections")
	if len(reps) == 0 {
		return nil
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		stmts, err := s.prepareIntelDocInsertStmts(ctx, tx)
		if err != nil {
			return err
		}
		defer stmts.section.Close()
		defer stmts.edge.Close()
		now := time.Now().Unix()
		var allFTS []codeanchor.IntelFTSRow
		for _, r := range reps {
			if err := s.replaceIntelDocSectionsTx(ctx, tx, stmts, r, now); err != nil {
				return err
			}
			if len(r.FTSRows) > 0 {
				allFTS = append(allFTS, r.FTSRows...)
			}
		}
		if err := s.upsertIntelFTSRowsTx(ctx, tx, allFTS); err != nil {
			return err
		}
		return nil
	})
}

// DeleteIntelByPath removes all intel data (anchors, sections, edges, FTS, rationale) for a given path.
func (s *Store) DeleteIntelByPath(ctx context.Context, path string) error {
	ctx = indexingperf.WithOp(ctx, "intel.delete_by_path")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if err := s.deleteIntelCodeByPathTx(ctx, tx, path); err != nil {
			return err
		}
		if err := s.deleteIntelDocSectionsByPathTx(ctx, tx, path); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM doc_links WHERE src_path = ?`, path); err != nil {
			return err
		}
		if err := s.deleteRationaleByPathTx(ctx, tx, path); err != nil {
			return err
		}
		return deleteIntelReverseIndexByPathsTx(ctx, tx, `SELECT ?`, normalizeCodeLookupPath(path))
	})
}
