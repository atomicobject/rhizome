package userstate

import (
	"context"
	"database/sql"
	"fmt"
	"path"
	"strings"
	"time"
)

// MaxFinderOpens bounds the ⌘P frecency history; older entries are pruned.
const MaxFinderOpens = 2000

const maxFinderPathBytes = 4096

// FinderOpen is how often and how recently this checkout opened a path from
// the finder or other navigation.
type FinderOpen struct {
	Path       string
	Count      int
	LastOpened time.Time
}

// RecordFinderOpen counts one open of a vault-relative path.
func (s *Store) RecordFinderOpen(ctx context.Context, relPath string, at time.Time) error {
	if relPath == "" || len(relPath) > maxFinderPathBytes || strings.Contains(relPath, `\`) ||
		path.IsAbs(relPath) || path.Clean(relPath) != relPath || relPath == ".." || strings.HasPrefix(relPath, "../") {
		return fmt.Errorf("%w: finder path must be a clean vault-relative path", ErrInvalid)
	}
	return s.withWrite(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO finder_opens(path, open_count, last_opened_at) VALUES(?, 1, ?)
			ON CONFLICT(path) DO UPDATE SET open_count = open_count + 1, last_opened_at = excluded.last_opened_at`,
			relPath, at.UnixMilli()); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM finder_opens WHERE path NOT IN (
			SELECT path FROM finder_opens ORDER BY last_opened_at DESC, path LIMIT ?)`, MaxFinderOpens)
		return err
	})
}

// FinderOpens returns the recorded opens, most recent first.
func (s *Store) FinderOpens(ctx context.Context) ([]FinderOpen, error) {
	rows, err := s.reader.QueryContext(ctx, `SELECT path, open_count, last_opened_at FROM finder_opens ORDER BY last_opened_at DESC, path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FinderOpen
	for rows.Next() {
		var open FinderOpen
		var at int64
		if err := rows.Scan(&open.Path, &open.Count, &at); err != nil {
			return nil, err
		}
		open.LastOpened = time.UnixMilli(at)
		out = append(out, open)
	}
	return out, rows.Err()
}
