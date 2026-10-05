package sqlite

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

func (s *Store) CalleesForOwnerFQNs(ctx context.Context, lang string, ownerFQNs []string, limitPerOwner int) (map[string][]string, error) {
	lang = strings.TrimSpace(lang)
	ownerFQNs = uniqueStrings(ownerFQNs)
	if len(ownerFQNs) == 0 {
		return nil, nil
	}
	if limitPerOwner <= 0 {
		limitPerOwner = 8
	}

	holders := make([]string, 0, len(ownerFQNs))
	args := make([]any, 0, len(ownerFQNs)+2)
	for _, o := range ownerFQNs {
		holders = append(holders, "?")
		args = append(args, o)
	}
	args = append(args, lang, lang)

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT caller.fqn, callee.fqn, callee.symbol
		FROM intel_edges e
		JOIN intel_code_anchors caller ON caller.id = e.src_row_id
		JOIN intel_code_anchors callee ON callee.id = e.dst_row_id
		WHERE e.kind = 'calls'
		  AND e.src_type = 'anchor'
		  AND e.dst_type = 'anchor'
		  AND caller.fqn IN (%s)
		  AND (? = '' OR caller.lang = ?)
		  AND callee.symbol IS NOT NULL AND callee.symbol != ''
		ORDER BY caller.fqn, callee.fqn, callee.symbol
	`, strings.Join(holders, ",")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]map[string]int, len(ownerFQNs))
	for rows.Next() {
		var owner, fqn, symbol string
		if err := rows.Scan(&owner, &fqn, &symbol); err != nil {
			return nil, err
		}
		if strings.TrimSpace(owner) == "" {
			continue
		}
		callee := formatCalleeFromAnchor(fqn, symbol)
		if callee == "" {
			continue
		}
		if counts[owner] == nil {
			counts[owner] = make(map[string]int)
		}
		counts[owner][callee]++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return limitCalleeResults(counts, limitPerOwner), nil
}

func (s *Store) CalleesForFiles(ctx context.Context, paths []string, limitPerFile int) (map[string][]string, error) {
	paths = uniqueStrings(paths)
	if len(paths) == 0 {
		return nil, nil
	}
	if limitPerFile <= 0 {
		limitPerFile = 8
	}

	holders := make([]string, 0, len(paths))
	args := make([]any, 0, len(paths))
	for _, p := range paths {
		holders = append(holders, "?")
		args = append(args, p)
	}

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT caller.path, callee.fqn, callee.symbol
		FROM intel_edges e
		JOIN intel_code_anchors caller ON caller.id = e.src_row_id
		JOIN intel_code_anchors callee ON callee.id = e.dst_row_id
		WHERE e.kind = 'calls'
		  AND e.src_type = 'anchor'
		  AND e.dst_type = 'anchor'
		  AND caller.path IN (%s)
		  AND callee.symbol IS NOT NULL AND callee.symbol != ''
		ORDER BY caller.path, callee.fqn, callee.symbol
	`, strings.Join(holders, ",")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]map[string]int, len(paths))
	for rows.Next() {
		var file, fqn, symbol string
		if err := rows.Scan(&file, &fqn, &symbol); err != nil {
			return nil, err
		}
		if strings.TrimSpace(file) == "" {
			continue
		}
		callee := formatCalleeFromAnchor(fqn, symbol)
		if callee == "" {
			continue
		}
		if counts[file] == nil {
			counts[file] = make(map[string]int)
		}
		counts[file][callee]++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return limitCalleeResults(counts, limitPerFile), nil
}

func (s *Store) CalleesForOwnerFQN(ctx context.Context, lang, ownerFQN string, limit int) ([]string, error) {
	ownerFQN = strings.TrimSpace(ownerFQN)
	if ownerFQN == "" {
		return nil, nil
	}
	out, err := s.CalleesForOwnerFQNs(ctx, lang, []string{ownerFQN}, limit)
	if err != nil {
		return nil, err
	}
	return out[ownerFQN], nil
}

func (s *Store) CalleesForFile(ctx context.Context, file string, limit int) ([]string, error) {
	file = strings.TrimSpace(file)
	if file == "" {
		return nil, nil
	}
	out, err := s.CalleesForFiles(ctx, []string{file}, limit)
	if err != nil {
		return nil, err
	}
	return out[file], nil
}

func formatCalleeFromAnchor(fqn, symbol string) string {
	fqn = strings.TrimSpace(fqn)
	if fqn != "" {
		return fqn
	}
	return strings.TrimSpace(symbol)
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func limitCalleeResults(counts map[string]map[string]int, limit int) map[string][]string {
	if len(counts) == 0 {
		return map[string][]string{}
	}
	if limit <= 0 {
		limit = 8
	}
	out := make(map[string][]string, len(counts))
	for owner, calleeCounts := range counts {
		if len(calleeCounts) == 0 {
			continue
		}
		type pair struct {
			callee string
			count  int
		}
		list := make([]pair, 0, len(calleeCounts))
		for callee, count := range calleeCounts {
			list = append(list, pair{callee: callee, count: count})
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].count != list[j].count {
				return list[i].count > list[j].count
			}
			return list[i].callee < list[j].callee
		})
		if len(list) > limit {
			list = list[:limit]
		}
		callees := make([]string, 0, len(list))
		for _, item := range list {
			callees = append(callees, item.callee)
		}
		out[owner] = callees
	}
	return out
}
