package store

import (
	"context"
	"fmt"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"strings"
	"time"
)

// IntelAnchorIDsByFQN returns anchor IDs whose FQN matches exactly (or suffix-matches) the provided value.
func (s *Store) IntelAnchorIDsByFQN(ctx context.Context, fqn string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 5
	}
	fqn = strings.TrimSpace(fqn)
	if fqn == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT anchor_id
		FROM intel_code_anchors
		WHERE fqn = ? OR fqn LIKE ?
		ORDER BY path, fqn, anchor_id
		LIMIT ?
	`, fqn, "%."+fqn, limit)
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

// IntelAnchorsByFQNsLimited returns anchors keyed by FQN (exact or suffix match),
// capped to a maximum number of anchors per FQN.
func (s *Store) IntelAnchorsByFQNsLimited(ctx context.Context, fqns []string, limitPerFQN int) (map[string][]codeanchor.IntelAnchor, error) {
	if limitPerFQN <= 0 {
		limitPerFQN = 5
	}
	unique := normalizeNonEmptyStrings(fqns)
	if len(unique) == 0 {
		return map[string][]codeanchor.IntelAnchor{}, nil
	}

	where := make([]string, 0, len(unique))
	args := make([]any, 0, len(unique)*2)
	for _, f := range unique {
		where = append(where, "(fqn = ? OR fqn LIKE ?)")
		args = append(args, f, "%."+f)
	}

	query := fmt.Sprintf(`
		SELECT
			anchor_id, lang, kind, path, symbol, fqn, signature, doc_comment,
			start_byte, end_byte, start_line, end_line, fingerprint, updated_at
		FROM intel_code_anchors
		WHERE %s
		ORDER BY fqn, path, anchor_id
	`, strings.Join(where, " OR "))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string][]codeanchor.IntelAnchor, len(unique))
	for rows.Next() {
		var anchor codeanchor.IntelAnchor
		if err := rows.Scan(
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
		for _, input := range unique {
			if anchor.FQN == input || strings.HasSuffix(anchor.FQN, "."+input) {
				if len(out[input]) >= limitPerFQN {
					continue
				}
				out[input] = append(out[input], anchor)
			}
		}
	}
	return out, rows.Err()
}

// IntelAnchorIDsByFQNsAndLang returns anchor IDs keyed by FQN (exact or suffix match) for a given language.
// When lang is empty, it returns matches across all languages.
//
// Suffix matching allows imports like `charm.services.X` to resolve to anchors with FQNs like
// `backend.src.charm.services.X`, making edge creation robust to source-root configuration.
func (s *Store) IntelAnchorIDsByExactFQNsAndLang(ctx context.Context, lang string, fqns []string) (map[string][]string, error) {
	if len(fqns) == 0 {
		return map[string][]string{}, nil
	}
	lang = strings.TrimSpace(lang)
	indexingperf.AddCount(ctx, indexingperf.CalledgeCacheBuildFqnLookupInputCount, int64(len(fqns)))

	seen := make(map[string]struct{}, len(fqns))
	unique := make([]string, 0, len(fqns))
	for _, f := range fqns {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		unique = append(unique, f)
	}
	if len(unique) == 0 {
		return map[string][]string{}, nil
	}
	indexingperf.AddCount(ctx, indexingperf.CalledgeCacheBuildFqnLookupUniqueCount, int64(len(unique)))

	out := make(map[string][]string, len(unique))
	const exactBatchSize = 400
	exactStarted := time.Now()
	exactChunks := 0
	for start := 0; start < len(unique); start += exactBatchSize {
		end := start + exactBatchSize
		if end > len(unique) {
			end = len(unique)
		}
		batch := unique[start:end]
		exactChunks++
		query, args := intelAnchorIDsByFQNsAndLangExactQuery(lang, batch)
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var inputFQN, id string
			if err := rows.Scan(&inputFQN, &id); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out[inputFQN] = append(out[inputFQN], id)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	indexingperf.ObserveLatency(ctx, indexingperf.CalledgeCacheBuildFqnLookupExact, time.Since(exactStarted))

	exactResolved := 0
	for _, fqn := range unique {
		if len(out[fqn]) > 0 {
			exactResolved++
		}
	}
	indexingperf.AddCount(ctx, indexingperf.CalledgeCacheBuildFqnLookupExactResolvedCount, int64(exactResolved))
	indexingperf.AddCount(ctx, indexingperf.CalledgeCacheBuildFqnLookupChunkCount, int64(exactChunks))
	return out, nil
}

func (s *Store) IntelAnchorIDsByFQNsAndLang(ctx context.Context, lang string, fqns []string) (map[string][]string, error) {
	out, err := s.IntelAnchorIDsByExactFQNsAndLang(ctx, lang, fqns)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(fqns))
	unique := make([]string, 0, len(fqns))
	for _, f := range fqns {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		unique = append(unique, f)
	}

	unresolved := make([]string, 0, len(unique))
	for _, fqn := range unique {
		if len(out[fqn]) > 0 {
			continue
		}
		unresolved = append(unresolved, fqn)
	}
	indexingperf.AddCount(ctx, indexingperf.CalledgeCacheBuildFqnLookupSuffixRequestedCount, int64(len(unresolved)))

	if len(unresolved) == 0 {
		return out, nil
	}

	const suffixBatchSize = 200
	suffixStarted := time.Now()
	suffixChunks := 0
	suffixResolved := 0
	for start := 0; start < len(unresolved); start += suffixBatchSize {
		end := start + suffixBatchSize
		if end > len(unresolved) {
			end = len(unresolved)
		}
		batch := unresolved[start:end]
		suffixChunks++
		query, args := intelAnchorIDsByFQNsAndLangSuffixQuery(lang, batch)
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		batchResolved := make(map[string]struct{}, len(batch))
		for rows.Next() {
			var inputFQN, id string
			if err := rows.Scan(&inputFQN, &id); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out[inputFQN] = append(out[inputFQN], id)
			batchResolved[inputFQN] = struct{}{}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
		suffixResolved += len(batchResolved)
	}
	indexingperf.ObserveLatency(ctx, indexingperf.CalledgeCacheBuildFqnLookupSuffix, time.Since(suffixStarted))
	indexingperf.AddCount(ctx, indexingperf.CalledgeCacheBuildFqnLookupSuffixResolvedCount, int64(suffixResolved))
	indexingperf.AddCount(ctx, indexingperf.CalledgeCacheBuildFqnLookupChunkCount, int64(suffixChunks))
	return out, nil
}
