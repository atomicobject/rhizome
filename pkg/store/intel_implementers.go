package store

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/anchors"
	"path/filepath"
	"strings"
)

// ImplementerAnchorsByTargetIDs returns exact local Go implementers whose
// package proof is still valid. Results are keyed by the requested interface
// anchor ID and capped after deterministic ordering.
func (s *Store) ImplementerAnchorsByTargetIDs(ctx context.Context, targetIDs []string, perTargetLimit int) (map[string][]codeanchor.IntelAnchor, error) {
	return s.implementerAnchorsByTargetIDs(ctx, targetIDs, nil, perTargetLimit)
}

// ImplementerAnchorsByTargetIDsFiltered applies path eligibility before the
// per-target cap so an excluded implementer cannot crowd out an eligible one.
func (s *Store) ImplementerAnchorsByTargetIDsFiltered(ctx context.Context, targetIDs, pathPrefixes []string, perTargetLimit int) (map[string][]codeanchor.IntelAnchor, error) {
	return s.implementerAnchorsByTargetIDs(ctx, targetIDs, pathPrefixes, perTargetLimit)
}

func (s *Store) implementerAnchorsByTargetIDs(ctx context.Context, targetIDs, pathPrefixes []string, perTargetLimit int) (map[string][]codeanchor.IntelAnchor, error) {
	if perTargetLimit <= 0 {
		perTargetLimit = 20
	}
	targetIDs = normalizeNonEmptyStrings(targetIDs)
	if len(targetIDs) == 0 {
		return map[string][]codeanchor.IntelAnchor{}, nil
	}
	holders := make([]string, len(targetIDs))
	args := make([]any, len(targetIDs))
	for i, id := range targetIDs {
		holders[i], args[i] = "?", id
	}
	pathPredicate := ""
	for _, prefix := range normalizeNonEmptyStrings(pathPrefixes) {
		prefix = strings.TrimSuffix(filepath.ToSlash(filepath.Clean(prefix)), "/")
		if prefix == "" || prefix == "." || prefix == "/" {
			pathPredicate = ""
			args = args[:len(targetIDs)]
			break
		}
		if pathPredicate != "" {
			pathPredicate += " OR "
		}
		pathPredicate += `(impl.path=? OR impl.path LIKE ? ESCAPE '\')`
		args = append(args, prefix, escapeLike(prefix)+"/%")
	}
	if pathPredicate != "" {
		pathPredicate = " AND (" + pathPredicate + ")"
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT target.anchor_id,
			impl.anchor_id, impl.lang, impl.kind, impl.path, impl.symbol, impl.fqn,
			impl.signature, impl.doc_comment, impl.start_byte, impl.end_byte,
			impl.start_line, impl.end_line, impl.fingerprint, impl.updated_at
		FROM intel_code_anchors target
		JOIN intel_go_derived_relationships r ON r.kind='implements' AND r.target_fqn=target.fqn
		JOIN intel_go_package_relationship_state st ON st.package_key=r.package_key AND st.valid=1
		JOIN intel_code_anchors impl ON impl.fqn=r.source_fqn AND impl.path=r.source_path
		WHERE target.anchor_id IN (%s)%s
		ORDER BY target.anchor_id, impl.fqn, impl.anchor_id
	`, strings.Join(holders, ","), pathPredicate), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string][]codeanchor.IntelAnchor, len(targetIDs))
	counts := make(map[string]int, len(targetIDs))
	for rows.Next() {
		var targetID string
		var anchor codeanchor.IntelAnchor
		if err := rows.Scan(
			&targetID, &anchor.AnchorID, &anchor.Lang, &anchor.Kind, &anchor.Path,
			&anchor.Symbol, &anchor.FQN, &anchor.Signature, &anchor.DocComment,
			&anchor.StartByte, &anchor.EndByte, &anchor.StartLine, &anchor.EndLine,
			&anchor.Fingerprint, &anchor.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if counts[targetID] >= perTargetLimit {
			continue
		}
		out[targetID] = append(out[targetID], anchor)
		counts[targetID]++
	}
	return out, rows.Err()
}
