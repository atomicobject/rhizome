package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

func (s *engine) Metadata(ctx context.Context) (embeddings.IndexMetadata, bool, error) {
	fingerprint := "0"
	if s.spec.domain == Code {
		fingerprint = "fingerprint_version"
	}
	row := s.db.QueryRowContext(ctx, `SELECT provider,model,dimensions,schema_version,created_at,`+fingerprint+`,IFNULL(last_sync,0),IFNULL(source_high_water,0) FROM `+s.spec.meta+` WHERE id=1`)
	var m embeddings.IndexMetadata
	var created, last, water int64
	if err := row.Scan(&m.Provider, &m.Model, &m.Dimensions, &m.SchemaVersion, &created, &m.FingerprintVersion, &last, &water); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return embeddings.IndexMetadata{}, false, nil
		}
		return embeddings.IndexMetadata{}, false, err
	}
	m.CreatedAt = time.Unix(created, 0)
	if last > 0 {
		m.LastSync = time.Unix(last, 0)
	}
	if water > 0 {
		m.SourceHighWater = time.Unix(water, 0)
	}
	if s.spec.domain == Code && s.runtime.Dimensions.Load() == 0 && m.Dimensions > 0 {
		s.runtime.Dimensions.Store(int64(m.Dimensions))
	}
	return m, true, nil
}
func (s *engine) UpdateLastSync(ctx context.Context, ts time.Time) error {
	if s.spec.domain == Code && ts.IsZero() {
		ts = time.Now()
	}
	ctx = indexingperf.WithOp(ctx, s.spec.label+".update_last_sync")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		_, err := execWithRetry(ctx, db, `UPDATE `+s.spec.meta+` SET last_sync=? WHERE id=1`, ts.Unix())
		return err
	})
}
func (s *engine) UpdateSourceHighWater(ctx context.Context, ts time.Time) error {
	if err := s.requireWrite(); err != nil {
		return err
	}
	if ts.IsZero() {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, s.spec.label+".update_source_high_water")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		_, err := execWithRetry(ctx, db, `UPDATE `+s.spec.meta+` SET source_high_water=MAX(IFNULL(source_high_water,0),?) WHERE id=1`, ts.Unix())
		return err
	})
}
