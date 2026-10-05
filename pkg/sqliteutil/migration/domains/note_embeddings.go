package domains

import (
	"context"
	"database/sql"

	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
)

// VersionMigrator runs one version transition.
type VersionMigrator func(context.Context, *sql.Tx, int, int) error

// NoteEmbeddingsPlan builds a migration plan for note embeddings.
func NoteEmbeddingsPlan(target int, migrate VersionMigrator, validate func(context.Context, *sql.Tx) error) migration.DomainPlan {
	return migration.DomainPlan{
		Domain: migration.DomainNoteEmbeddings,
		Target: target,
		Steps:  sequentialVersionSteps(migration.DomainNoteEmbeddings, target, migrate),
		DiscoverLegacyVersion: func(ctx context.Context, tx *sql.Tx) (int, error) {
			return discoverLegacyVersionFromTable(ctx, tx, "emb_index_meta", "schema_version")
		},
		Validate: validate,
	}
}
