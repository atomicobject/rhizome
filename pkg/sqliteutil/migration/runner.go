package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/sqliteutil"
)

// Domain identifies an independently versioned schema domain.
type Domain string

const (
	DomainIntel          Domain = "intel"
	DomainNoteEmbeddings Domain = "note_embeddings"
	DomainCodeEmbeddings Domain = "code_embeddings"
	DomainUserState      Domain = "user_state"
)

// Step migrates a domain to version To.
// Steps are expected to run in a single SQL transaction.
type Step struct {
	To   int
	Name string
	Run  func(context.Context, *sql.Tx) error
}

// DomainPlan describes how to migrate one domain.
type DomainPlan struct {
	Domain Domain
	Target int
	Steps  []Step
	// SchemaFingerprint is an explicit, deterministic identifier for the full
	// structural schema plan. A non-empty value enables the validated warm-open
	// probe when Validate is also present. Callers must change it whenever the
	// structures checked by Validate change, even if Target does not.
	SchemaFingerprint string
	// DiscoverLegacyVersion resolves an initial version when the migration state
	// table has no row for this domain.
	DiscoverLegacyVersion func(context.Context, *sql.Tx) (int, error)
	// Validate runs after all migrations. Return ErrSchemaDrift for structural
	// mismatches that should trigger caller-defined recovery behavior. Other errors
	// propagate without authorizing recovery. Validation may repeat after SQLite
	// contention; keep effects inside the transaction.
	Validate func(context.Context, *sql.Tx) error
}

// EnsureOptions controls migration runner behavior.
type EnsureOptions struct {
	Now func() time.Time
}

// ProbeObservation reports the bounded SQL cost and outcome of a current-schema
// validation-proof probe. Begin/commit are not SQL statements.
type ProbeObservation struct {
	Attempted  bool
	Hit        bool
	Statements int
}

// ErrFutureSchema indicates the database schema is newer than supported.
type ErrFutureSchema struct {
	Domain    Domain
	Current   int
	Supported int
}

func (e *ErrFutureSchema) Error() string {
	if e.Domain == DomainUserState {
		return fmt.Sprintf("personal-state database newer than this rhizome build (have %d, support %d); upgrade rhizome", e.Current, e.Supported)
	}
	return fmt.Sprintf(
		"database newer than this rhizome build for domain %q (have %d, support %d); upgrade rhizome or run `rzm index --rebuild`",
		e.Domain, e.Current, e.Supported,
	)
}

// ErrMissingStep indicates the migration plan is incomplete for the discovered
// current version.
type ErrMissingStep struct {
	Domain Domain
	From   int
	To     int
}

func (e *ErrMissingStep) Error() string {
	return fmt.Sprintf("missing migration step for domain %q: %d -> %d", e.Domain, e.From, e.To)
}

// ErrSchemaDrift indicates schema structures do not match expected layout.
type ErrSchemaDrift struct {
	Domain Domain
	Err    error
}

func (e *ErrSchemaDrift) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("schema drift detected for domain %q", e.Domain)
	}
	return fmt.Sprintf("schema drift detected for domain %q: %v", e.Domain, e.Err)
}

func (e *ErrSchemaDrift) Unwrap() error { return e.Err }

const (
	migrationStateTable      = "rzm_migration_state"
	migrationLogTable        = "rzm_migration_log"
	migrationValidationTable = "rzm_migration_validation"
)

func EnsureAll(ctx context.Context, db *sql.DB, plans []DomainPlan, opts EnsureOptions) error {
	for _, plan := range plans {
		if err := EnsureDomain(ctx, db, plan, opts); err != nil {
			return err
		}
	}
	return nil
}

func EnsureDomain(ctx context.Context, db *sql.DB, plan DomainPlan, opts EnsureOptions) error {
	if db == nil {
		return errors.New("migration db is required")
	}
	if strings.TrimSpace(string(plan.Domain)) == "" {
		return errors.New("migration domain is required")
	}
	if plan.Target < 0 {
		return fmt.Errorf("migration target must be >= 0 for domain %q", plan.Domain)
	}
	if strings.TrimSpace(plan.SchemaFingerprint) != "" && plan.Validate != nil {
		valid, _, probeErr := ProbeValidatedDomain(ctx, db, plan)
		var future *ErrFutureSchema
		if errors.As(probeErr, &future) {
			return probeErr
		}
		if valid {
			return nil
		}
		// Missing/legacy metadata, mismatches, and read errors all fail safely to
		// the authoritative ensure + structural validation path below.
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	if err := ensureMetadataTables(ctx, db); err != nil {
		return err
	}

	stepByTo := make(map[int]Step, len(plan.Steps))
	for _, step := range plan.Steps {
		if step.To <= 0 {
			return fmt.Errorf("invalid migration step target %d for domain %q", step.To, plan.Domain)
		}
		if _, exists := stepByTo[step.To]; exists {
			return fmt.Errorf("duplicate migration step target %d for domain %q", step.To, plan.Domain)
		}
		stepByTo[step.To] = step
	}

	current, err := loadOrInitializeState(ctx, db, plan, now)
	if err != nil {
		return err
	}
	if current > plan.Target {
		return &ErrFutureSchema{
			Domain:    plan.Domain,
			Current:   current,
			Supported: plan.Target,
		}
	}

	for current < plan.Target {
		next := current + 1
		step, ok := stepByTo[next]
		if !ok {
			return &ErrMissingStep{Domain: plan.Domain, From: current, To: next}
		}
		if err := runStep(ctx, db, plan.Domain, current, step, now); err != nil {
			return err
		}
		current = next
	}

	if plan.Validate != nil {
		return sqliteutil.ExecTxWithRetry(ctx, db, func(tx *sql.Tx) error {
			if err := plan.Validate(ctx, tx); err != nil {
				return err
			}
			if strings.TrimSpace(plan.SchemaFingerprint) != "" {
				var schemaVersion int
				if err := tx.QueryRowContext(ctx, `PRAGMA schema_version`).Scan(&schemaVersion); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `
				INSERT INTO `+migrationValidationTable+` (
					domain, target_version, schema_fingerprint, sqlite_schema_version, validated_at
				) VALUES (?, ?, ?, ?, ?)
				ON CONFLICT(domain) DO UPDATE SET
					target_version = excluded.target_version,
					schema_fingerprint = excluded.schema_fingerprint,
					sqlite_schema_version = excluded.sqlite_schema_version,
					validated_at = excluded.validated_at
			`, string(plan.Domain), plan.Target, plan.SchemaFingerprint, schemaVersion, now().Unix()); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `
				UPDATE `+migrationStateTable+` SET dirty = 0, updated_at = ? WHERE domain = ?
			`, now().Unix(), string(plan.Domain)); err != nil {
					return err
				}
			}
			return nil
		})
	}
	return nil
}

func ensureMetadataTables(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS ` + migrationStateTable + ` (
			domain TEXT PRIMARY KEY,
			version INTEGER NOT NULL,
			dirty INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS ` + migrationLogTable + ` (
			id INTEGER PRIMARY KEY,
			domain TEXT NOT NULL,
			from_version INTEGER NOT NULL,
			to_version INTEGER NOT NULL,
			status TEXT NOT NULL,
			started_at INTEGER NOT NULL,
			finished_at INTEGER,
			error TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_rzm_migration_log_domain_started ON ` + migrationLogTable + `(domain, started_at DESC);`,
		`CREATE TABLE IF NOT EXISTS ` + migrationValidationTable + ` (
			domain TEXT PRIMARY KEY,
			target_version INTEGER NOT NULL,
			schema_fingerprint TEXT NOT NULL CHECK (schema_fingerprint != ''),
			sqlite_schema_version INTEGER NOT NULL,
			validated_at INTEGER NOT NULL,
			FOREIGN KEY(domain) REFERENCES ` + migrationStateTable + `(domain) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT;`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

// ProbeValidatedDomain checks whether a domain has a current structural
// validation proof. SQL/read failures are returned so callers can deliberately
// choose the authoritative full ensure path; they must never be treated as a
// cache hit. The probe uses exactly two SQL statements on a readable proof.
func ProbeValidatedDomain(ctx context.Context, db *sql.DB, plan DomainPlan) (bool, ProbeObservation, error) {
	observation := ProbeObservation{Attempted: true}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return false, observation, err
	}
	defer func() { _ = tx.Rollback() }()

	var current, dirty, proofTarget, proofSchemaVersion int
	var proofFingerprint string
	observation.Statements++
	err = tx.QueryRowContext(ctx, `
		SELECT s.version, s.dirty, v.target_version, v.schema_fingerprint, v.sqlite_schema_version
		FROM `+migrationStateTable+` AS s
		JOIN `+migrationValidationTable+` AS v ON v.domain = s.domain
		WHERE s.domain = ?
	`, string(plan.Domain)).Scan(&current, &dirty, &proofTarget, &proofFingerprint, &proofSchemaVersion)
	if err != nil {
		return false, observation, err
	}
	if current > plan.Target {
		return false, observation, &ErrFutureSchema{Domain: plan.Domain, Current: current, Supported: plan.Target}
	}

	var sqliteSchemaVersion int
	observation.Statements++
	if err := tx.QueryRowContext(ctx, `PRAGMA schema_version`).Scan(&sqliteSchemaVersion); err != nil {
		return false, observation, err
	}
	if err := tx.Commit(); err != nil {
		return false, observation, err
	}
	valid := current == plan.Target &&
		dirty == 0 &&
		proofTarget == plan.Target &&
		proofFingerprint == plan.SchemaFingerprint &&
		proofSchemaVersion == sqliteSchemaVersion
	observation.Hit = valid
	return valid, observation, nil
}

func loadOrInitializeState(ctx context.Context, db *sql.DB, plan DomainPlan, now func() time.Time) (int, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	current, exists, err := selectStateVersion(ctx, tx, plan.Domain)
	if err != nil {
		return 0, err
	}
	if exists {
		if err := tx.Commit(); err != nil {
			return 0, err
		}
		return current, nil
	}

	if plan.DiscoverLegacyVersion != nil {
		current, err = plan.DiscoverLegacyVersion(ctx, tx)
		if err != nil {
			return 0, err
		}
	}
	if current < 0 {
		current = 0
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO `+migrationStateTable+`(domain, version, dirty, updated_at) VALUES (?, ?, 0, ?)`,
		string(plan.Domain),
		current,
		now().Unix(),
	); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return current, nil
}

func selectStateVersion(ctx context.Context, tx *sql.Tx, domain Domain) (int, bool, error) {
	var current int
	err := tx.QueryRowContext(
		ctx,
		`SELECT version FROM `+migrationStateTable+` WHERE domain = ?`,
		string(domain),
	).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return current, true, nil
}

func runStep(ctx context.Context, db *sql.DB, domain Domain, from int, step Step, now func() time.Time) error {
	name := strings.TrimSpace(step.Name)
	if name == "" {
		name = fmt.Sprintf("v%d_to_v%d", from, step.To)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	started := now().Unix()
	var logID int64
	rollbackWithFailureLog := func(stepErr error) error {
		_ = tx.Rollback()
		_ = insertFailureLog(ctx, db, domain, from, step.To, started, now().Unix(), stepErr)
		if stepErr != nil {
			return fmt.Errorf("migrate %q %s: %w", domain, name, stepErr)
		}
		return fmt.Errorf("migrate %q %s failed", domain, name)
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE `+migrationStateTable+` SET dirty = 1, updated_at = ? WHERE domain = ?`,
		started,
		string(domain),
	); err != nil {
		return rollbackWithFailureLog(err)
	}

	res, err := tx.ExecContext(
		ctx,
		`INSERT INTO `+migrationLogTable+`(domain, from_version, to_version, status, started_at) VALUES (?, ?, ?, 'running', ?)`,
		string(domain), from, step.To, started,
	)
	if err != nil {
		return rollbackWithFailureLog(err)
	}
	if id, idErr := res.LastInsertId(); idErr == nil {
		logID = id
	}

	if step.Run != nil {
		if err := step.Run(ctx, tx); err != nil {
			return rollbackWithFailureLog(err)
		}
	}

	finished := now().Unix()
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE `+migrationStateTable+` SET version = ?, dirty = 0, updated_at = ? WHERE domain = ?`,
		step.To,
		finished,
		string(domain),
	); err != nil {
		return rollbackWithFailureLog(err)
	}

	if logID > 0 {
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE `+migrationLogTable+` SET status = 'applied', finished_at = ?, error = NULL WHERE id = ?`,
			finished,
			logID,
		); err != nil {
			return rollbackWithFailureLog(err)
		}
	} else {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO `+migrationLogTable+`(domain, from_version, to_version, status, started_at, finished_at) VALUES (?, ?, ?, 'applied', ?, ?)`,
			string(domain), from, step.To, started, finished,
		); err != nil {
			return rollbackWithFailureLog(err)
		}
	}

	if err := tx.Commit(); err != nil {
		return rollbackWithFailureLog(err)
	}
	return nil
}

func insertFailureLog(ctx context.Context, db *sql.DB, domain Domain, from, to int, started, finished int64, stepErr error) error {
	errText := ""
	if stepErr != nil {
		errText = stepErr.Error()
	}
	_, err := db.ExecContext(
		ctx,
		`INSERT INTO `+migrationLogTable+`(domain, from_version, to_version, status, started_at, finished_at, error) VALUES (?, ?, ?, 'failed', ?, ?, ?)`,
		string(domain), from, to, started, finished, errText,
	)
	return err
}
