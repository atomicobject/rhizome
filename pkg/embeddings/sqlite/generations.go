package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

func (s *engine) SyncGeneration(ctx context.Context) (int64, error) {
	var gen int64
	err := s.db.QueryRowContext(ctx, `SELECT IFNULL(sync_generation,0) FROM `+s.spec.meta+` WHERE id=1`).Scan(&gen)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return gen, err
}
func (s *engine) ensureGenerationMetadata(ctx context.Context, db *sql.DB) error {
	columns, values := "", ""
	if s.spec.domain == Code {
		columns = ",fingerprint_version"
		values = ",0"
	}
	_, err := execWithRetry(ctx, db, `INSERT OR IGNORE INTO `+s.spec.meta+` (id,provider,model,dimensions,schema_version,created_at,last_sync,source_high_water,sync_generation,visible_generation`+columns+`) VALUES(1,'','',0,?,?,0,0,0,0`+values+`)`, s.spec.version, time.Now().Unix())
	return err
}
func (s *engine) IncrementSyncGeneration(ctx context.Context) (int64, error) {
	ctx = indexingperf.WithOp(ctx, s.spec.label+".increment_sync_generation")
	var gen int64
	err := s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		if err := s.ensureGenerationMetadata(ctx, db); err != nil {
			return err
		}
		if _, err := execWithRetry(ctx, db, `UPDATE `+s.spec.meta+` SET sync_generation=sync_generation+1 WHERE id=1`); err != nil {
			return err
		}
		return db.QueryRowContext(ctx, `SELECT sync_generation FROM `+s.spec.meta+` WHERE id=1`).Scan(&gen)
	})
	return gen, err
}
func (s *engine) CommitSyncGeneration(ctx context.Context) error {
	ctx = indexingperf.WithOp(ctx, s.spec.label+".commit_sync_generation")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		if err := s.ensureGenerationMetadata(ctx, db); err != nil {
			return err
		}
		_, err := execWithRetry(ctx, db, `UPDATE `+s.spec.meta+` SET visible_generation=sync_generation WHERE id=1`)
		return err
	})
}
func (s *engine) currentVisibleGeneration(ctx context.Context) (int64, bool, error) {
	var gen int64
	err := s.db.QueryRowContext(ctx, `SELECT IFNULL(visible_generation,0) FROM `+s.spec.meta+` WHERE id=1`).Scan(&gen)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return gen, gen > 0, nil
}
func (s *engine) VisibleGeneration(ctx context.Context) (int64, bool, error) {
	return s.currentVisibleGeneration(ctx)
}
