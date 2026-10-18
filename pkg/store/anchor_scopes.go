package store

import (
	"context"
	"database/sql"
	"errors"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

// SetAnchorScope replaces scope rows for an anchor.
func (s *Store) SetAnchorScope(ctx context.Context, anchorID int64, symbols []string, callFiles []string) error {
	ctx = indexingperf.WithOp(ctx, "intel.set_anchor_scope")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		return s.setAnchorScopesTx(ctx, tx, []codeanchor.AnchorScopeUpdate{{
			ID:        anchorID,
			Symbols:   symbols,
			CallFiles: callFiles,
		}})
	})
}

// SetAnchorScopesBatch updates multiple anchor scopes in a single transaction.
func (s *Store) SetAnchorScopesBatch(ctx context.Context, updates []codeanchor.AnchorScopeUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.set_anchor_scopes_batch")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		return s.setAnchorScopesTx(ctx, tx, updates)
	})
}

func (s *Store) setAnchorScopesTx(ctx context.Context, tx *sql.Tx, updates []codeanchor.AnchorScopeUpdate) error {
	for _, upd := range updates {
		if _, err := tx.ExecContext(ctx, `DELETE FROM anchor_scopes WHERE anchor_id = ?`, upd.ID); err != nil {
			return err
		}
		for _, fqn := range upd.Symbols {
			if fqn == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO anchor_scopes(anchor_id, symbol_fqn) VALUES (?, ?)`, upd.ID, fqn); err != nil {
				return err
			}
		}
		for _, file := range upd.CallFiles {
			if file == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO anchor_scopes(anchor_id, call_file) VALUES (?, ?)`, upd.ID, file); err != nil {
				return err
			}
		}
	}
	return nil
}

// AnchorScope returns symbols and call files for the anchor.
func (s *Store) AnchorScope(ctx context.Context, anchorID int64) ([]string, []string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT symbol_fqn, call_file FROM anchor_scopes WHERE anchor_id = ?
	`, anchorID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var symbols, calls []string
	for rows.Next() {
		var sym, callFile sql.NullString
		if err := rows.Scan(&sym, &callFile); err != nil {
			return nil, nil, err
		}
		if sym.Valid && sym.String != "" {
			symbols = append(symbols, sym.String)
		}
		if callFile.Valid && callFile.String != "" {
			calls = append(calls, callFile.String)
		}
	}
	return symbols, calls, rows.Err()
}

func (s *Store) HasAnyAnchorScopes(ctx context.Context) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM anchor_scopes LIMIT 1`).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// AllAnchorScopes returns scopes for all anchors keyed by anchor_id.
func (s *Store) AllAnchorScopes(ctx context.Context) (map[int64]codeanchor.AnchorScope, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT anchor_id, symbol_fqn, call_file
		FROM anchor_scopes
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]codeanchor.AnchorScope)
	for rows.Next() {
		var anchorID int64
		var sym, call sql.NullString
		if err := rows.Scan(&anchorID, &sym, &call); err != nil {
			return nil, err
		}
		entry := out[anchorID]
		if sym.Valid && sym.String != "" {
			entry.Symbols = append(entry.Symbols, sym.String)
		}
		if call.Valid && call.String != "" {
			entry.Calls = append(entry.Calls, call.String)
		}
		out[anchorID] = entry
	}
	return out, rows.Err()
}
