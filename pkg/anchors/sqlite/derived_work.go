package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	anchors "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/paths"
)

func createDerivedWorkSchema(ctx context.Context, tx *sql.Tx) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS derived_work_epoch(kind TEXT PRIMARY KEY,generation INTEGER NOT NULL,revision INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS derived_work_sequence(kind TEXT NOT NULL,path TEXT NOT NULL,generation INTEGER NOT NULL,PRIMARY KEY(kind,path))`,
		`CREATE TABLE IF NOT EXISTS derived_work(kind TEXT NOT NULL,path TEXT NOT NULL,generation INTEGER NOT NULL,attempt INTEGER NOT NULL DEFAULT 0,retry_at INTEGER NOT NULL DEFAULT 0,ready INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(kind,path))`,
		`CREATE INDEX IF NOT EXISTS derived_work_due ON derived_work(ready,retry_at)`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func cleanDerivedScope(scope anchors.DerivedScope) (anchors.DerivedScope, error) {
	switch scope.Kind {
	case anchors.DerivedNotes, anchors.DerivedOntology, anchors.DerivedCode, anchors.DerivedGraph:
	default:
		return scope, fmt.Errorf("unknown derived kind %q", scope.Kind)
	}
	if scope.Path != "" {
		p, err := paths.CleanRelPath(scope.Path)
		if err != nil {
			return scope, err
		}
		scope.Path = p.String()
		if scope.Kind == anchors.DerivedGraph {
			return scope, fmt.Errorf("graph work requires global scope")
		}
	}
	return scope, nil
}

// MarkDerivedDirty must commit before the corresponding structural mutation.
// Tickets are unavailable to workers until ActivateDerivedWork completes.
func (s *Store) MarkDerivedDirty(ctx context.Context, scopes []anchors.DerivedScope) ([]anchors.DerivedWork, error) {
	cleaned := make([]anchors.DerivedScope, 0, len(scopes))
	seen := map[anchors.DerivedScope]bool{}
	for _, scope := range scopes {
		scope, err := cleanDerivedScope(scope)
		if err != nil {
			return nil, err
		}
		if !seen[scope] {
			seen[scope] = true
			cleaned = append(cleaned, scope)
		}
	}
	var work []anchors.DerivedWork
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		kinds := map[anchors.DerivedKind]bool{}
		for _, scope := range cleaned {
			kinds[scope.Kind] = kinds[scope.Kind] || scope.Path == ""
		}
		for kind, global := range kinds {
			if _, err := tx.ExecContext(ctx, `INSERT INTO derived_work_epoch(kind,generation,revision) VALUES(?,?,1) ON CONFLICT(kind) DO UPDATE SET generation=generation+excluded.generation,revision=revision+1`, kind, global); err != nil {
				return err
			}
		}
		for _, scope := range cleaned {
			var gen int64
			if err := tx.QueryRowContext(ctx, `INSERT INTO derived_work_sequence(kind,path,generation) VALUES(?,?,1) ON CONFLICT(kind,path) DO UPDATE SET generation=generation+1 RETURNING generation`, scope.Kind, scope.Path).Scan(&gen); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO derived_work(kind,path,generation) VALUES(?,?,?) ON CONFLICT(kind,path) DO UPDATE SET generation=excluded.generation,attempt=0,retry_at=0,ready=0`, scope.Kind, scope.Path, gen); err != nil {
				return err
			}
			work = append(work, anchors.DerivedWork{DerivedScope: scope, Generation: gen})
		}
		for i := range work {
			if err := tx.QueryRowContext(ctx, `SELECT generation,revision FROM derived_work_epoch WHERE kind=?`, work[i].Kind).Scan(&work[i].Epoch, &work[i].Revision); err != nil {
				return err
			}
		}
		return nil
	})
	return work, err
}

func (s *Store) ActivateDerivedWork(ctx context.Context, work []anchors.DerivedWork) error {
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {

		for _, w := range work {
			if w.Path == "" {
				// A current global recovery covers older path obligations, including a
				// crash before their structural publication. Retire them only after the
				// exact full structural ticket becomes ready.
				if _, err := tx.ExecContext(ctx, `DELETE FROM derived_work WHERE kind=? AND path<>'' AND EXISTS(SELECT 1 FROM derived_work_epoch e WHERE e.kind=? AND e.generation=? AND e.revision=?)`, w.Kind, w.Kind, w.Epoch, w.Revision); err != nil {
					return err
				}
			}
			if _, err := tx.ExecContext(ctx, `UPDATE derived_work SET ready=1 WHERE kind=? AND path=? AND generation=?`, w.Kind, w.Path, w.Generation); err != nil {
				return err
			}
		}
		return nil
	})
}

// PendingDerivedWork bounds preparation and excludes unfinished structural work.
func (s *Store) PendingDerivedWork(ctx context.Context, now time.Time, limit int) ([]anchors.DerivedWork, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("derived work limit must be positive")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT w.kind,w.path,w.generation,w.attempt,w.retry_at,w.ready,COALESCE(g.generation,0),COALESCE(g.revision,0) FROM derived_work w LEFT JOIN derived_work_epoch g ON g.kind=w.kind WHERE w.ready=1 AND w.retry_at<=? AND NOT EXISTS(SELECT 1 FROM derived_work pending WHERE pending.kind=w.kind AND pending.ready=0) ORDER BY w.retry_at,w.kind,w.path LIMIT ?`, now.UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []anchors.DerivedWork
	for rows.Next() {
		var w anchors.DerivedWork
		var retry int64
		if err := rows.Scan(&w.Kind, &w.Path, &w.Generation, &w.Attempt, &retry, &w.Ready, &w.Epoch, &w.Revision); err != nil {
			return nil, err
		}
		if retry != 0 {
			w.RetryAt = time.UnixMilli(retry)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// CurrentDerivedWork is checked inside the publication writer lease before any
// destination write, including cleanup or freshness markers.
func (s *Store) CurrentDerivedWork(ctx context.Context, w anchors.DerivedWork) (bool, error) {
	var current bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM derived_work w WHERE kind=? AND path=? AND generation=? AND ready=1 AND COALESCE((SELECT generation FROM derived_work_epoch WHERE kind=w.kind),0)=? AND (path<>'' OR COALESCE((SELECT revision FROM derived_work_epoch WHERE kind=w.kind),0)=?))`, w.Kind, w.Path, w.Generation, w.Epoch, w.Revision).Scan(&current)
	return current, err
}

func (s *Store) AckDerivedWork(ctx context.Context, work []anchors.DerivedWork) error {
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		for _, w := range work {
			if _, err := tx.ExecContext(ctx, `DELETE FROM derived_work WHERE kind=? AND path=? AND generation=? AND COALESCE((SELECT generation FROM derived_work_epoch WHERE kind=?),0)=? AND (path<>'' OR COALESCE((SELECT revision FROM derived_work_epoch WHERE kind=?),0)=?)`, w.Kind, w.Path, w.Generation, w.Kind, w.Epoch, w.Kind, w.Revision); err != nil {
				return err
			}
		}
		return nil
	})
}

// RetryDerivedWork stores only an attempt count and deadline, never provider
// errors, source text or credentials. Old failures cannot postpone newer work.
func (s *Store) RetryDerivedWork(ctx context.Context, w anchors.DerivedWork, retryAt time.Time) error {
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE derived_work SET attempt=attempt+1,retry_at=? WHERE kind=? AND path=? AND generation=? AND COALESCE((SELECT generation FROM derived_work_epoch WHERE kind=?),0)=? AND (path<>'' OR COALESCE((SELECT revision FROM derived_work_epoch WHERE kind=?),0)=?)`, retryAt.UnixMilli(), w.Kind, w.Path, w.Generation, w.Kind, w.Epoch, w.Kind, w.Revision)
		return err
	})
}

// HasUnreadyDerivedWork requires structural recovery even when a crash happened
// after write-ahead debt and before the ownership reconciliation marker.
func (s *Store) HasUnreadyDerivedWork(ctx context.Context) (bool, error) {
	var pending bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM derived_work WHERE ready=0)`).Scan(&pending)
	return pending, err
}

// HasDerivedWork detects recovery obligations regardless of readiness or retry
// deadline, allowing batch discovery to preserve failed derived destinations.
func (s *Store) HasDerivedWork(ctx context.Context, kind anchors.DerivedKind) (bool, error) {
	if _, err := cleanDerivedScope(anchors.DerivedScope{Kind: kind}); err != nil {
		return false, err
	}
	var pending bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM derived_work WHERE kind=?)`, kind).Scan(&pending)
	return pending, err
}
