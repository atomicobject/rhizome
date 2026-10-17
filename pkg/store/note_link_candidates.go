package store

import (
	"context"
	"path"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// StoredNoteLinkCandidatePaths returns complete path candidate sets. The
// shared note resolver owns exact spelling, alias precedence and ambiguity.
func (s *Store) StoredNoteLinkCandidatePaths(ctx context.Context, inputs []string) ([]string, error) {
	state, err := s.GetNoteMetadataState(ctx)
	if err != nil {
		return nil, err
	}
	if !state.Ready || state.LoadedAt == 0 {
		return nil, nil
	}
	keys := map[string]struct{}{}
	for _, input := range inputs {
		input, _, _ = strings.Cut(strings.TrimSpace(input), "#")
		canonical := paths.Normalize(input).String()
		key := paths.LinkKey(strings.TrimSuffix(canonical, path.Ext(canonical)))
		if key != "" {
			keys[key] = struct{}{}
			keys[path.Base(key)] = struct{}{}
		}
	}
	selected := make([]string, 0, len(keys))
	for key := range keys {
		selected = append(selected, key)
	}
	sort.Strings(selected)
	candidates := map[string]struct{}{}
	const batchLimit = sqliteValuesBatchMaxParams / 2
	for start := 0; start < len(selected); start += batchLimit {
		batch := selected[start:min(start+batchLimit, len(selected))]
		holders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		args := append(sliceAny(batch), sliceAny(batch)...)
		rows, err := s.db.QueryContext(ctx, `SELECT path FROM notes WHERE indexed_at>0 AND (note_key_full IN (`+holders+`) OR note_key_base IN (`+holders+`))`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var path string
			if err := rows.Scan(&path); err != nil {
				rows.Close()
				return nil, err
			}
			candidates[path] = struct{}{}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	out := make([]string, 0, len(candidates))
	for path := range candidates {
		out = append(out, path)
	}
	sort.Strings(out)
	return out, nil
}
