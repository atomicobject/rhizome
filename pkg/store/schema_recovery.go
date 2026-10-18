package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
)

// bootstrapIntelBaseline creates the consolidated v56 Intel schema for a fresh
// or reset domain. Historical schema steps are schema assembly details here;
// they are no longer replayed as independently committed upgrade migrations.
func (s *Store) bootstrapIntelBaseline(ctx context.Context) error {
	version, _, exists, err := s.intelSchemaState(ctx)
	if err != nil {
		return err
	}
	if exists && version >= intelBaselineVersion {
		return nil
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		for _, build := range schemaMigrations {
			if err := build(ctx, tx); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM schema_version`); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO schema_version(version) VALUES (?)`, intelBaselineVersion)
		return err
	})
}

func (s *Store) ensureSchemaWithRecovery(ctx context.Context, schemaVersion int) error {
	if plan, err := s.intelMigrationPlan(ctx, schemaVersion); err == nil {
		valid, observation, _ := migration.ProbeValidatedDomain(ctx, s.db, plan)
		if observation.Statements > 0 {
			indexingperf.AddCount(ctx, indexingperf.AgentStartOpTransactions, 1)
		}
		if valid {
			indexingperf.AddCount(ctx, indexingperf.AgentStartOpSchemaStatements, int64(observation.Statements))
			return nil
		}
	}
	if reset, err := s.classifyIntelBaseline(ctx, schemaVersion); err != nil {
		return err
	} else if reset {
		if err := s.ResetDomain(ctx); err != nil {
			return err
		}
	}
	if err := s.ensureSchema(ctx, schemaVersion); err == nil {
		return nil
	} else {
		var drift *migration.ErrSchemaDrift
		if errors.As(err, &drift) {
			// Drift means declared version and actual schema shape diverged.
			// Intel tables are derived, so reset and rebuild them deterministically.
			if resetErr := s.ResetDomain(ctx); resetErr != nil {
				return resetErr
			}
			return s.ensureSchema(ctx, schemaVersion)
		}
		if !isSchemaError(err) {
			return err
		}
	}

	// Self-heal: drop and recreate code-intel/code-anchor tables, but intentionally preserve
	// embeddings tables (so unchanged code chunks can keep their existing embeddings).
	if err := s.ResetDomain(ctx); err != nil {
		return err
	}
	return s.ensureSchema(ctx, schemaVersion)
}

// classifyIntelBaseline inspects schema state before ensureSchema performs any
// writes. Intel is a disposable derived domain: pre-baseline and dirty schemas
// reset in place, while future schemas fail closed without mutation.
func (s *Store) classifyIntelBaseline(ctx context.Context, supportedVersion int) (bool, error) {
	version, dirty, exists, err := s.intelSchemaState(ctx)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	if version > supportedVersion {
		return false, &migration.ErrFutureSchema{
			Domain:    migration.DomainIntel,
			Current:   version,
			Supported: supportedVersion,
		}
	}
	return dirty || version < intelBaselineVersion, nil
}

func (s *Store) intelSchemaState(ctx context.Context) (version int, dirty bool, exists bool, err error) {
	var metadataExists int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name = 'rzm_migration_state'
	`).Scan(&metadataExists); err != nil {
		return 0, false, false, err
	}
	if metadataExists > 0 {
		var dirtyInt int
		err := s.db.QueryRowContext(ctx, `
			SELECT version, dirty FROM rzm_migration_state WHERE domain = ?
		`, string(migration.DomainIntel)).Scan(&version, &dirtyInt)
		if err == nil {
			return version, dirtyInt != 0, true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, false, false, err
		}
	}

	var legacyExists int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name = 'schema_version'
	`).Scan(&legacyExists); err != nil {
		return 0, false, false, err
	}
	if legacyExists == 0 {
		return 0, false, false, nil
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&version); err != nil {
		return 0, false, false, err
	}
	return version, false, true, nil
}
