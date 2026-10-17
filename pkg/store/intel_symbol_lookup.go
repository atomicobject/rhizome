package store

import (
	"context"
	"strings"
)

// IntelAnchorIDsBySymbol returns anchor IDs whose symbol matches exactly (or contains) the provided value.
func (s *Store) IntelAnchorIDsBySymbol(ctx context.Context, symbol string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 5
	}
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT anchor_id
		FROM intel_code_anchors
		WHERE symbol = ? OR symbol LIKE ?
		ORDER BY path, fqn, anchor_id
		LIMIT ?
	`, symbol, "%"+symbol+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// IntelAnchorIDsByGoMethodNameInPkg resolves a Go symbol name to candidate anchors within a package import path.
//
// This handles two cases for same-package calls where the Go indexer cannot infer the full FQN:
//   - Methods: FQN matches "<pkgImportPath>.<ReceiverType>.<name>" (e.g., pkg.Service.Run)
//   - Functions: FQN equals "<pkgImportPath>.<name>" (e.g., pkg.DetectLanguage)
func (s *Store) IntelAnchorIDsByGoMethodNameInPkg(ctx context.Context, pkgImportPath string, methodName string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 10
	}
	pkgImportPath = strings.TrimSpace(pkgImportPath)
	methodName = strings.TrimSpace(methodName)
	if pkgImportPath == "" || methodName == "" {
		return nil, nil
	}
	methodLike := pkgImportPath + ".%." + methodName // pkg.Type.Method
	funcFQN := pkgImportPath + "." + methodName      // pkg.Function
	rows, err := s.db.QueryContext(ctx, `
		SELECT anchor_id
		FROM intel_code_anchors
		WHERE lang = 'go'
		  AND symbol = ?
		  AND (fqn LIKE ? OR fqn = ?)
		ORDER BY path, fqn, anchor_id
		LIMIT ?
	`, methodName, methodLike, funcFQN, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
