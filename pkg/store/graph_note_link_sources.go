package store

import (
	"context"
	"sort"
	"strings"
)

// GraphDocNoteLinkSourcesByTargets returns only sources with authored links to
// requested note paths. URI provider links share the same inbound boundary.
func (s *Store) GraphDocNoteLinkSourcesByTargets(ctx context.Context, paths []string) ([]string, error) {
	paths = normalizeNonEmptyStrings(paths)
	result := map[string]struct{}{}
	for start := 0; start < len(paths); start += sqliteValuesBatchMaxParams {
		batch := paths[start:min(start+sqliteValuesBatchMaxParams, len(paths))]
		holders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT src_path FROM graph_doc_edges WHERE dst_path IN (`+holders+`) AND (kind IN ('wikilink','mdlink') OR kind GLOB 'note_link:*')`, sliceAny(batch)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var source string
			if err := rows.Scan(&source); err != nil {
				rows.Close()
				return nil, err
			}
			result[source] = struct{}{}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	out := make([]string, 0, len(result))
	for source := range result {
		out = append(out, source)
	}
	sort.Strings(out)
	return out, nil
}
