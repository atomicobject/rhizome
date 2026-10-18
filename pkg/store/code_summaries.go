package store

import (
	"context"
	"database/sql"
	"encoding/json"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"strings"
	"time"
)

// ReplaceFileSummary replaces all stored data for the file with the provided summary.
func (s *Store) ReplaceFileSummary(ctx context.Context, summary codeanchor.FileSummary) error {
	ctx = indexingperf.WithOp(ctx, "intel.replace_file_summary")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		file := summary.FilePath
		status := strings.TrimSpace(string(summary.ParseStatus))
		if status == "" {
			status = string(codeanchor.ParseOK)
		}

		// Delete previous rows.
		if _, err := tx.ExecContext(ctx, `DELETE FROM super_edges WHERE child_fqn IN (SELECT fqn FROM symbols WHERE file = ?) OR parent_fqn IN (SELECT fqn FROM symbols WHERE file = ?)`, file, file); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM annotations WHERE owner_fqn IN (SELECT fqn FROM symbols WHERE file = ?)`, file); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM symbols WHERE file = ?`, file); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM files WHERE path = ?`, file); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `INSERT INTO files(path, lang, hash, indexer_version, parse_status, mtime) VALUES (?, ?, ?, ?, ?, ?)`, file, summary.Lang, summary.Hash, codeanchor.IndexerVersion, status, time.Now().Unix()); err != nil {
			return err
		}

		symbolStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO symbols (lang, kind, file, pkg, name, fqn, fqn_reversed)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(fqn) DO UPDATE SET
			lang=excluded.lang,
			kind=excluded.kind,
			file=excluded.file,
			pkg=excluded.pkg,
			name=excluded.name,
			fqn_reversed=excluded.fqn_reversed
	`)
		if err != nil {
			return err
		}
		defer symbolStmt.Close()

		superStmt, err := tx.PrepareContext(ctx, `INSERT INTO super_edges(child_fqn, parent_fqn, kind) VALUES (?, ?, ?)`)
		if err != nil {
			return err
		}
		defer superStmt.Close()

		annotationStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO annotations(owner_fqn, ann_lang, ann_pkg, ann_name, args_json, confidence)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
		if err != nil {
			return err
		}
		defer annotationStmt.Close()

		for _, sym := range summary.Symbols {
			fqn := sym.NormalizeFQN()
			fqnReversed := codeanchor.ReverseString(fqn)
			if _, err := symbolStmt.ExecContext(ctx, sym.Lang, sym.Kind, sym.File, sym.Pkg, sym.Name, fqn, fqnReversed); err != nil {
				return err
			}
		}

		for _, edge := range summary.Supers {
			if edge.ChildFQN == "" || edge.ParentFQN == "" {
				continue
			}
			if _, err := superStmt.ExecContext(ctx, edge.ChildFQN, edge.ParentFQN, "extends"); err != nil {
				return err
			}
		}

		for _, ann := range summary.Annotations {
			blob, _ := json.Marshal(ann.Args)
			if _, err := annotationStmt.ExecContext(ctx, ann.OwnerFQN, ann.AnnSymbol.Lang, ann.AnnSymbol.Pkg, ann.AnnSymbol.Name, string(blob), 1.0); err != nil {
				return err
			}
		}

		return replaceAndInvalidateGoPackageMembershipTx(ctx, tx, []codeanchor.FileSummary{summary}, nil)
	})
}

// ReplaceFileSummariesBatch replaces stored data for multiple files in a single transaction.
func (s *Store) ReplaceFileSummariesBatch(ctx context.Context, summaries []codeanchor.FileSummary) error {
	summaries = dedupeFileSummariesByPath(summaries)
	if len(summaries) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.replace_file_summary")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		return s.replaceFileSummariesBatchTx(ctx, tx, summaries)
	})
}

func (s *Store) replaceFileSummariesBatchTx(ctx context.Context, tx *sql.Tx, summaries []codeanchor.FileSummary) error {
	summaries = dedupeFileSummariesByPath(summaries)
	if len(summaries) == 0 {
		return nil
	}
	return func() error {
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE IF NOT EXISTS tmp_batch_files(path TEXT PRIMARY KEY)`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM tmp_batch_files`); err != nil {
			return err
		}
		tmpFileStmt, err := tx.PrepareContext(ctx, `INSERT INTO tmp_batch_files(path) VALUES (?)`)
		if err != nil {
			return err
		}
		for _, summary := range summaries {
			if _, err := tmpFileStmt.ExecContext(ctx, summary.FilePath); err != nil {
				_ = tmpFileStmt.Close()
				return err
			}
		}
		if err := tmpFileStmt.Close(); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `
			DELETE FROM super_edges
			WHERE child_fqn IN (SELECT fqn FROM symbols WHERE file IN (SELECT path FROM tmp_batch_files))
			   OR parent_fqn IN (SELECT fqn FROM symbols WHERE file IN (SELECT path FROM tmp_batch_files))
		`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM annotations
			WHERE owner_fqn IN (SELECT fqn FROM symbols WHERE file IN (SELECT path FROM tmp_batch_files))
		`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM symbols WHERE file IN (SELECT path FROM tmp_batch_files)`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM files WHERE path IN (SELECT path FROM tmp_batch_files)`); err != nil {
			return err
		}

		// Prepare statements once for the entire batch.
		symbolStmt, err := tx.PrepareContext(ctx, `
			INSERT INTO symbols (lang, kind, file, pkg, name, fqn, fqn_reversed)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(fqn) DO UPDATE SET
				lang=excluded.lang,
				kind=excluded.kind,
				file=excluded.file,
				pkg=excluded.pkg,
				name=excluded.name,
				fqn_reversed=excluded.fqn_reversed
		`)
		if err != nil {
			return err
		}
		defer symbolStmt.Close()

		superStmt, err := tx.PrepareContext(ctx, `INSERT INTO super_edges(child_fqn, parent_fqn, kind) VALUES (?, ?, ?)`)
		if err != nil {
			return err
		}
		defer superStmt.Close()

		annotationStmt, err := tx.PrepareContext(ctx, `
			INSERT INTO annotations(owner_fqn, ann_lang, ann_pkg, ann_name, args_json, confidence)
			VALUES (?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer annotationStmt.Close()

		for _, summary := range summaries {
			file := summary.FilePath
			status := strings.TrimSpace(string(summary.ParseStatus))
			if status == "" {
				status = string(codeanchor.ParseOK)
			}

			if _, err := tx.ExecContext(ctx, `INSERT INTO files(path, lang, hash, indexer_version, parse_status, mtime) VALUES (?, ?, ?, ?, ?, ?)`, file, summary.Lang, summary.Hash, codeanchor.IndexerVersion, status, time.Now().Unix()); err != nil {
				return err
			}

			for _, sym := range summary.Symbols {
				fqn := sym.NormalizeFQN()
				fqnReversed := codeanchor.ReverseString(fqn)
				if _, err := symbolStmt.ExecContext(ctx, sym.Lang, sym.Kind, sym.File, sym.Pkg, sym.Name, fqn, fqnReversed); err != nil {
					return err
				}
			}

			for _, edge := range summary.Supers {
				if edge.ChildFQN == "" || edge.ParentFQN == "" {
					continue
				}
				if _, err := superStmt.ExecContext(ctx, edge.ChildFQN, edge.ParentFQN, "extends"); err != nil {
					return err
				}
			}

			for _, ann := range summary.Annotations {
				blob, _ := json.Marshal(ann.Args)
				if _, err := annotationStmt.ExecContext(ctx, ann.OwnerFQN, ann.AnnSymbol.Lang, ann.AnnSymbol.Pkg, ann.AnnSymbol.Name, string(blob), 1.0); err != nil {
					return err
				}
			}
		}

		return replaceAndInvalidateGoPackageMembershipTx(ctx, tx, summaries, nil)
	}()
}

func dedupeFileSummariesByPath(summaries []codeanchor.FileSummary) []codeanchor.FileSummary {
	if len(summaries) < 2 {
		return summaries
	}
	indexByPath := make(map[string]int, len(summaries))
	deduped := make([]codeanchor.FileSummary, 0, len(summaries))
	for _, summary := range summaries {
		if idx, ok := indexByPath[summary.FilePath]; ok {
			deduped[idx] = summary
			continue
		}
		indexByPath[summary.FilePath] = len(deduped)
		deduped = append(deduped, summary)
	}
	return deduped
}
