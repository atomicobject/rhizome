package sqlite

import (
	"context"
	"database/sql"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

// VacuumStats summarizes reclaimable free pages in the main SQLite database.
type VacuumStats struct {
	PageCount        int64
	FreelistCount    int64
	PageSize         int64
	ReclaimableBytes int64
}

// FreeRatio returns the fraction of database pages currently on the freelist.
func (s VacuumStats) FreeRatio() float64 {
	if s.PageCount <= 0 {
		return 0
	}
	return float64(s.FreelistCount) / float64(s.PageCount)
}

// Analyze refreshes SQLite query planner statistics after large writes.
//
// Docs:
// - [[indexing-observability-maintenance-policy#^spec-0047-us3-ac1]]
//
// This helps SQLite choose better query plans for traversal-heavy intel queries.
func (s *Store) Analyze(ctx context.Context) error {
	ctx = indexingperf.WithOp(ctx, "intel.analyze")
	return s.runMaintenance(ctx, `ANALYZE;`)
}

// Optimize asks SQLite to run built-in optimizations (typically small, incremental work).
//
// Docs:
// - [[indexing-observability-maintenance-policy#^spec-0047-us3-ac1]]
func (s *Store) Optimize(ctx context.Context) error {
	ctx = indexingperf.WithOp(ctx, "intel.optimize")
	return s.runMaintenance(ctx, `PRAGMA optimize;`)
}

// VacuumStats returns SQLite freelist/page-size metrics used to decide whether
// a full VACUUM would actually reclaim meaningful disk space.
//
// Docs:
// - [[indexing-observability-maintenance-policy#^spec-0047-us3-ac2]]
func (s *Store) VacuumStats(ctx context.Context) (VacuumStats, error) {
	var stats VacuumStats
	if err := s.db.QueryRowContext(ctx, `PRAGMA page_count;`).Scan(&stats.PageCount); err != nil {
		return stats, err
	}
	if err := s.db.QueryRowContext(ctx, `PRAGMA freelist_count;`).Scan(&stats.FreelistCount); err != nil {
		return stats, err
	}
	if err := s.db.QueryRowContext(ctx, `PRAGMA page_size;`).Scan(&stats.PageSize); err != nil {
		return stats, err
	}
	stats.ReclaimableBytes = stats.FreelistCount * stats.PageSize
	return stats, nil
}

// Vacuum rewrites the database to reclaim free space.
//
// Docs:
// - [[indexing-observability-maintenance-policy#^spec-0047-us3]]
//
// This can be expensive and requires an exclusive lock; prefer running it manually or
// behind an explicit flag rather than on every index run.
func (s *Store) Vacuum(ctx context.Context) error {
	ctx = indexingperf.WithOp(ctx, "intel.vacuum")
	return s.runMaintenance(ctx, `VACUUM;`)
}

// SQLite maintenance can change schema_version (for example, the first ANALYZE
// creates sqlite_stat1). Validate the existing schema before publishing its new
// proof so managed readers can open the completed index without a writer reopen.
func (s *Store) runMaintenance(ctx context.Context, statement string) error {
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
		return s.ensureSchemaVersion(ctx, currentSchemaVersion)
	})
}
