package store

import (
	"context"
	"database/sql"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration/domains"
)

const (
	intelBaselineVersion          = 56
	currentSchemaVersion          = 77
	intelVecTablePrefix           = "intel_embeddings_vec_d"
	currentIntelSchemaFingerprint = "intel-schema-v77-2026-10-17-section-source-identity"
)

// forwardSchemaMigrations contains only migrations added after the consolidated
// v56 baseline. Append one entry and increment currentSchemaVersion together.
var forwardSchemaMigrations = []domains.TxStep{
	func(ctx context.Context, tx *sql.Tx) error { // v56 -> v57
		return createNoteFragmentTargetsSchema(ctx, tx)
	},
	func(ctx context.Context, tx *sql.Tx) error { // v57 -> v58
		return addOntologyMaterializationVersionSchema(ctx, tx)
	},
	func(ctx context.Context, tx *sql.Tx) error { // v58 -> v59
		return createExternalTargetSchema(ctx, tx)
	},
	func(ctx context.Context, tx *sql.Tx) error { // v59 -> v60
		// v0.50.2 and main independently assigned different schemas to v59.
		// Reapply both idempotent definitions so either historical v59 shape
		// upgrades without losing indexed or session state.
		if err := createExternalTargetSchema(ctx, tx); err != nil {
			return err
		}
		return createSessionReservationSchema(ctx, tx)
	},
	func(ctx context.Context, tx *sql.Tx) error { // v60 -> v61
		return createNoteProjectionStateSchema(ctx, tx)
	},
	func(ctx context.Context, tx *sql.Tx) error { // v61 -> v62
		return createGraphRevisionSchema(ctx, tx)
	},
	func(ctx context.Context, tx *sql.Tx) error { // v62 -> v63
		// v62 bumped the graph revision on every `UPDATE notes`, including
		// indexing metadata the web graph never reads. Drop the unscoped
		// trigger so the column-scoped definition is recreated.
		if _, err := tx.ExecContext(ctx, `DROP TRIGGER IF EXISTS graph_web_revision_notes_update`); err != nil {
			return err
		}
		return createGraphRevisionSchema(ctx, tx)
	},
	func(ctx context.Context, tx *sql.Tx) error { // v63 -> v64
		return addOntologyAssessmentFlagsSchema(ctx, tx)
	},
	func(ctx context.Context, tx *sql.Tx) error { // v64 -> v65
		return migrateNotePublicationFactsSchema(ctx, tx)
	},
	func(ctx context.Context, tx *sql.Tx) error { // v65 -> v66
		return migrateValidationDiagnosticSchema(ctx, tx)
	},
	func(ctx context.Context, tx *sql.Tx) error { // v66 -> v67
		return migrateIntelVecOwnerPartitions(ctx, tx)
	},
	func(ctx context.Context, tx *sql.Tx) error { // v67 -> v68
		return createGoRelationshipSchema(ctx, tx)
	},
	func(ctx context.Context, tx *sql.Tx) error { // v68 -> v69
		return addSymbolRefTargetMemberSchema(ctx, tx)
	},
	func(ctx context.Context, tx *sql.Tx) error { // v69 -> v70
		return addValidationDiagnosticVariantSchema(ctx, tx)
	},
	createDerivedWorkSchema,       // v70 -> v71
	addGraphDocEdgeLinkTextSchema, // v71 -> v72
	createNoteLinkInputsSchema,    // v72 -> v73
	func(ctx context.Context, tx *sql.Tx) error { // v73 -> v74
		_, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS schema_version`)
		return err
	},
	addOntologyRelocationFingerprintSchema, // v74 -> v75
	func(ctx context.Context, tx *sql.Tx) error { // v75 -> v76
		return createOntologyReadinessSchema(ctx, tx)
	},
	addIntelSectionSourceIdentitySchema, // v76 -> v77
}

func (s *Store) ensureSchemaVersion(ctx context.Context, target int) error {
	plan, err := s.intelMigrationPlan(ctx, target)
	if err != nil {
		return err
	}
	return migration.EnsureDomain(ctx, s.db, plan, migration.EnsureOptions{})
}

func (s *Store) intelMigrationPlan(_ context.Context, target int) (migration.DomainPlan, error) {
	stepCount := target - intelBaselineVersion
	if stepCount < 0 || stepCount > len(forwardSchemaMigrations) {
		return migration.DomainPlan{}, &migration.ErrMissingStep{
			Domain: migration.DomainIntel,
			From:   intelBaselineVersion + len(forwardSchemaMigrations),
			To:     intelBaselineVersion + len(forwardSchemaMigrations) + 1,
		}
	}
	steps := make([]domains.TxStep, 0, stepCount)
	for _, migrate := range forwardSchemaMigrations[:stepCount] {
		migrateFn := migrate
		steps = append(steps, func(ctx context.Context, tx *sql.Tx) error {
			return migrateFn(ctx, tx)
		})
	}
	validate := s.validateIntelSchemaTx
	if target < currentSchemaVersion {
		validate = nil
	}
	plan := domains.IntelPlanFrom(intelBaselineVersion, target, steps, validate)
	if target == currentSchemaVersion {
		plan.SchemaFingerprint = currentIntelSchemaFingerprint
	}
	return plan, nil
}
