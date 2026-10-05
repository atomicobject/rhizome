package sqlite

import (
	"context"
	"database/sql"
	"time"
)

// CodeIndexPrerequisiteSnapshot is the storage-owned persisted fact set used
// to assess a code-index prerequisite. Presence flags distinguish missing
// metadata from persisted empty values without inventing defaults.
type CodeIndexPrerequisiteSnapshot struct {
	// CodeRowsPresent means the code domain contains at least one indexed anchor
	// or raw symbol reference. File metadata alone does not satisfy this signal.
	CodeRowsPresent bool

	IndexedFileCount int

	IndexerVersion        string
	IndexerVersionPresent bool

	ScopeConfigHash        string
	ScopeConfigHashPresent bool

	// IndexedAt is the maximum positive files.mtime persisted during code-file
	// indexing. It describes stored snapshot metadata and does not prove that the
	// live worktree is fresh.
	IndexedAt        time.Time
	IndexedAtPresent bool
}

// CodeIndexPrerequisiteSnapshot reads one aggregate from an already-open
// store. It performs no schema migration, writes, filesystem discovery, index
// build, or provider work.
func (s *Store) CodeIndexPrerequisiteSnapshot(ctx context.Context) (CodeIndexPrerequisiteSnapshot, error) {
	var (
		codeRowsPresent  int
		indexedFileCount int
		indexerVersion   sql.NullString
		scopeConfigHash  sql.NullString
		indexedAtUnix    sql.NullInt64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT
			CASE WHEN EXISTS (SELECT 1 FROM intel_code_anchors LIMIT 1)
			       OR EXISTS (SELECT 1 FROM intel_symbol_refs LIMIT 1)
			     THEN 1 ELSE 0 END,
			(SELECT COUNT(*) FROM files),
			(SELECT value FROM index_metadata WHERE key = ?),
			(SELECT value FROM index_metadata WHERE key = ?),
			(SELECT MAX(mtime) FROM files WHERE mtime > 0)
	`, MetaKeyIndexerVersion, MetaKeyScopeConfigHash).Scan(
		&codeRowsPresent,
		&indexedFileCount,
		&indexerVersion,
		&scopeConfigHash,
		&indexedAtUnix,
	)
	if err != nil {
		return CodeIndexPrerequisiteSnapshot{}, err
	}

	snapshot := CodeIndexPrerequisiteSnapshot{
		CodeRowsPresent:        codeRowsPresent != 0,
		IndexedFileCount:       indexedFileCount,
		IndexerVersion:         indexerVersion.String,
		IndexerVersionPresent:  indexerVersion.Valid,
		ScopeConfigHash:        scopeConfigHash.String,
		ScopeConfigHashPresent: scopeConfigHash.Valid,
	}
	if indexedAtUnix.Valid {
		snapshot.IndexedAt = time.Unix(indexedAtUnix.Int64, 0).UTC()
		snapshot.IndexedAtPresent = true
	}
	return snapshot, nil
}
