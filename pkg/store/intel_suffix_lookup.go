package store

import (
	"context"
	"fmt"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"strings"
)

func (s *Store) CallEdgeSuffixSeedsByLangs(ctx context.Context, langs []codeanchor.Lang) ([]codeanchor.CallEdgeSuffixSeed, error) {
	uniqueLangs := make([]string, 0, len(langs))
	seen := make(map[string]struct{}, len(langs))
	for _, lang := range langs {
		key := strings.TrimSpace(string(lang))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		uniqueLangs = append(uniqueLangs, key)
	}
	if len(uniqueLangs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, 0, len(uniqueLangs))
	args := make([]any, 0, len(uniqueLangs))
	for _, lang := range uniqueLangs {
		placeholders = append(placeholders, "?")
		args = append(args, lang)
	}

	query := fmt.Sprintf(`
		SELECT DISTINCT s.lang, s.fqn, a.anchor_id
		FROM symbols s
		JOIN intel_code_anchors a
		  ON a.lang = s.lang
		 AND a.fqn = s.fqn
		 AND a.path = s.file
		WHERE s.lang IN (%s)
		ORDER BY s.lang, s.fqn, a.anchor_id
	`, strings.Join(placeholders, ","))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seeds := make([]codeanchor.CallEdgeSuffixSeed, 0)
	for rows.Next() {
		var seed codeanchor.CallEdgeSuffixSeed
		if err := rows.Scan(&seed.Lang, &seed.FQN, &seed.AnchorID); err != nil {
			return nil, err
		}
		seeds = append(seeds, seed)
	}
	return seeds, rows.Err()
}

func intelAnchorIDsByFQNsAndLangExactQuery(lang string, batch []string) (string, []any) {
	values := make([]string, 0, len(batch))
	args := make([]any, 0, len(batch)+1)
	for _, fqn := range batch {
		values = append(values, "(?)")
		args = append(args, fqn)
	}
	query := fmt.Sprintf(`
		WITH input(fqn) AS (VALUES %s)
		SELECT i.fqn, a.anchor_id
		FROM input i
		JOIN intel_code_anchors a ON a.fqn = i.fqn
	`, strings.Join(values, ","))
	if lang != "" {
		query += ` WHERE a.lang = ?`
		args = append(args, lang)
	}
	query += ` ORDER BY i.fqn, a.path, a.anchor_id`
	return query, args
}

func intelAnchorIDsByFQNsAndLangSuffixQuery(lang string, batch []string) (string, []any) {
	values := make([]string, 0, len(batch))
	args := make([]any, 0, len(batch)*2+1)
	for _, fqn := range batch {
		values = append(values, "(?, ?)")
		args = append(args, fqn, codeanchor.ReverseString(fqn))
	}
	query := fmt.Sprintf(`
		WITH input(fqn, rev) AS (VALUES %s)
		SELECT i.fqn, a.anchor_id
		FROM input i
		JOIN symbols s ON s.fqn_reversed LIKE i.rev || '.%%'
		JOIN intel_code_anchors a
		  ON a.lang = s.lang
		 AND a.fqn = s.fqn
		 AND a.path = s.file
	`, strings.Join(values, ","))
	if lang != "" {
		query += ` WHERE s.lang = ?`
		args = append(args, lang)
	}
	query += ` ORDER BY i.fqn, LENGTH(s.fqn), a.path, a.anchor_id`
	return query, args
}
