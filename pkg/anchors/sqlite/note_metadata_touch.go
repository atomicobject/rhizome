package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// NoteSourceTouch refreshes the persisted filesystem freshness evidence of a
// note whose content identity is unchanged. It deliberately writes no derived
// row, so the graph revision stays put (the notes UPDATE trigger is narrowed
// to path/title).
type NoteSourceTouch struct {
	Path  string
	Mtime int64
	Size  int64
}

// applyNoteSourceTouches updates mtime/size in place for content-identical
// notes. A touch for a path the same delta rewrites or deletes is a contract
// violation; a touch for a path with no row is a benign race with deletion.
func applyNoteSourceTouches(ctx context.Context, tx *sql.Tx, touches []NoteSourceTouch, changedPaths, deletedPaths []string) error {
	if len(touches) == 0 {
		return nil
	}
	rewritten := make(map[string]struct{}, len(changedPaths)+len(deletedPaths))
	for _, path := range changedPaths {
		rewritten[path] = struct{}{}
	}
	for _, path := range deletedPaths {
		rewritten[path] = struct{}{}
	}
	stmt, err := tx.PrepareContext(ctx, `
		UPDATE notes SET mtime = ?, size = ?
		WHERE path = ? AND (mtime != ? OR size != ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, touch := range touches {
		path := strings.TrimSpace(touch.Path)
		if path == "" {
			continue
		}
		if _, conflict := rewritten[path]; conflict {
			return fmt.Errorf("note source touch for %q conflicts with a rewritten or deleted path in the same delta", path)
		}
		if _, err := stmt.ExecContext(ctx, touch.Mtime, touch.Size, path, touch.Mtime, touch.Size); err != nil {
			return err
		}
	}
	return nil
}
