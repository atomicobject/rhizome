package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/sqliteutil"
)

// CodeStatsOptions controls diagnostic presentation without changing indexed data.
type CodeStatsOptions struct {
	UnresolvedTop int
	ExcludeNames  map[string]bool
}

// ReadCodeStats reads an existing index without migration, repair, or writes.
// Legacy indexes retain the local/unknown reference-classification fallback.
func ReadCodeStats(ctx context.Context, path string, opts CodeStatsOptions) (CodeStats, error) {
	db, err := sqliteutil.OpenDSN(sqliteutil.DSNWithOptions(path, sqliteutil.DSNOptions{ReadOnly: true}), sqliteutil.Options{})
	if err != nil {
		return CodeStats{}, fmt.Errorf("open code index: %w", err)
	}
	defer db.Close()
	return (&Store{db: db, readOnly: true}).CodeStats(ctx, opts)
}

// CodeStats returns code-coverage analytics from the store's current rows.
func (s *Store) CodeStats(ctx context.Context, opts CodeStatsOptions) (CodeStats, error) {
	stats := CodeStats{
		Languages:                map[string]LanguageCounts{},
		Edges:                    map[string]EdgeCounts{},
		ReferenceClassifications: map[string]ReferenceClassificationStats{},
		UnresolvedCallees:        map[string][]UnresolvedCallee{},
		ByDirectory:              map[string][]DirectoryDistribution{},
	}
	for _, query := range []func(context.Context, *sql.DB, *CodeStats) error{
		queryLanguageCounts, queryEdgeCategories, queryReferenceClassifications,
	} {
		if err := query(ctx, s.db, &stats); err != nil {
			return CodeStats{}, err
		}
	}
	if err := queryTopUnresolvedCallees(ctx, s.db, &stats, opts.UnresolvedTop, opts.ExcludeNames); err != nil {
		return CodeStats{}, err
	}
	if err := queryByDirectory(ctx, s.db, &stats); err != nil {
		return CodeStats{}, err
	}
	return stats, nil
}
