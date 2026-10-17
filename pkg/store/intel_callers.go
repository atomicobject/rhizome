package store

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/anchors"
	"path/filepath"
	"strings"
)

// CallerAnchorsByCalleeIDs returns caller anchors for the provided callee anchor IDs.
// Results are keyed by callee anchor ID and capped by perCalleeLimit.
func (s *Store) CallerAnchorsByCalleeIDs(ctx context.Context, calleeIDs []string, perCalleeLimit int) (map[string][]codeanchor.IntelAnchor, error) {
	return s.callerAnchorsByCalleeIDs(ctx, calleeIDs, nil, perCalleeLimit, false)
}

// CallerTestAnchorsByCalleeIDs returns caller anchors from recognized test
// paths. Filtering happens before the per-target cap so production callers
// cannot crowd test relationship evidence out of a bounded result.
func (s *Store) CallerTestAnchorsByCalleeIDs(ctx context.Context, calleeIDs, pathPrefixes []string, perCalleeLimit int) (map[string][]codeanchor.IntelAnchor, error) {
	return s.callerAnchorsByCalleeIDs(ctx, calleeIDs, pathPrefixes, perCalleeLimit, true)
}

func (s *Store) callerAnchorsByCalleeIDs(ctx context.Context, calleeIDs, pathPrefixes []string, perCalleeLimit int, testsOnly bool) (map[string][]codeanchor.IntelAnchor, error) {
	if perCalleeLimit <= 0 {
		perCalleeLimit = 20
	}
	normalized := normalizeNonEmptyStrings(calleeIDs)
	if len(normalized) == 0 {
		return map[string][]codeanchor.IntelAnchor{}, nil
	}

	holders := make([]string, len(normalized))
	args := make([]any, 0, len(normalized))
	for i, id := range normalized {
		holders[i] = "?"
		args = append(args, id)
	}

	query := fmt.Sprintf(`
		WITH requested AS (
			SELECT anchor_id, fqn FROM intel_code_anchors WHERE anchor_id IN (%s)
		), candidates AS (
		SELECT DISTINCT
			callee.anchor_id AS callee_id,
			caller.anchor_id, caller.lang, caller.kind, caller.path, caller.symbol, caller.fqn,
			caller.signature, caller.doc_comment, caller.start_byte, caller.end_byte,
			caller.start_line, caller.end_line, caller.fingerprint, caller.updated_at
		FROM intel_edges e
		JOIN intel_code_anchors caller ON caller.id = e.src_row_id
		JOIN intel_code_anchors callee ON callee.id = e.dst_row_id
		WHERE e.kind = 'calls' AND e.src_type = 'anchor' AND e.dst_type = 'anchor' AND callee.anchor_id IN (SELECT anchor_id FROM requested)
		UNION
		SELECT requested.anchor_id,
			caller.anchor_id, caller.lang, caller.kind, caller.path, caller.symbol, caller.fqn,
			caller.signature, caller.doc_comment, caller.start_byte, caller.end_byte,
			caller.start_line, caller.end_line, caller.fingerprint, caller.updated_at
		FROM requested
		JOIN intel_go_derived_relationships r ON r.kind='calls' AND r.target_fqn=requested.fqn
		JOIN intel_go_package_relationship_state st ON st.package_key=r.package_key AND st.valid=1
		JOIN intel_code_anchors caller ON caller.fqn=r.source_fqn AND caller.path=r.source_path
		)
		SELECT * FROM candidates
		ORDER BY callee_id, fqn, anchor_id
	`, strings.Join(holders, ","))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string][]codeanchor.IntelAnchor, len(normalized))
	counts := make(map[string]int, len(normalized))
	for rows.Next() {
		var calleeID string
		var anchor codeanchor.IntelAnchor
		if err := rows.Scan(
			&calleeID,
			&anchor.AnchorID,
			&anchor.Lang,
			&anchor.Kind,
			&anchor.Path,
			&anchor.Symbol,
			&anchor.FQN,
			&anchor.Signature,
			&anchor.DocComment,
			&anchor.StartByte,
			&anchor.EndByte,
			&anchor.StartLine,
			&anchor.EndLine,
			&anchor.Fingerprint,
			&anchor.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if testsOnly && (!codeanchor.IsTestPath(anchor.Path) || !intelPathAllowed(anchor.Path, pathPrefixes)) {
			continue
		}
		if counts[calleeID] >= perCalleeLimit {
			continue
		}
		out[calleeID] = append(out[calleeID], anchor)
		counts[calleeID]++
	}
	return out, rows.Err()
}

func intelPathAllowed(value string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	value = filepath.ToSlash(filepath.Clean(strings.TrimSpace(value)))
	for _, prefix := range prefixes {
		prefix = strings.TrimSuffix(filepath.ToSlash(filepath.Clean(strings.TrimSpace(prefix))), "/")
		if prefix == "" || prefix == "." || prefix == "/" || value == prefix || strings.HasPrefix(value, prefix+"/") {
			return true
		}
	}
	return false
}
