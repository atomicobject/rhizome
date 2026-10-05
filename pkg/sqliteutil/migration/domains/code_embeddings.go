package domains

import (
	"context"
	"database/sql"

	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
)

// CodeEmbeddingsPlan builds a migration plan for code embeddings.
func CodeEmbeddingsPlan(target int, migrate VersionMigrator, validate func(context.Context, *sql.Tx) error) migration.DomainPlan {
	return migration.DomainPlan{
		Domain: migration.DomainCodeEmbeddings,
		Target: target,
		Steps:  sequentialVersionSteps(migration.DomainCodeEmbeddings, target, migrate),
		DiscoverLegacyVersion: func(ctx context.Context, tx *sql.Tx) (int, error) {
			return discoverLegacyVersionFromTable(ctx, tx, "code_index_meta", "schema_version")
		},
		Validate: validate,
	}
}
