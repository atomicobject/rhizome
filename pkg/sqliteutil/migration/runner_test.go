package migration

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func validatedTestPlan(domain Domain, target int, validate func(context.Context, *sql.Tx) error) DomainPlan {
	return DomainPlan{
		Domain:            domain,
		Target:            target,
		SchemaFingerprint: "test-plan-v1",
		Validate:          validate,
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", t.TempDir()+"/migration.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestEnsureDomain_AppliesStepsInOrder(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	var seq []int

	err := EnsureDomain(ctx, db, DomainPlan{
		Domain: Domain("test"),
		Target: 3,
		DiscoverLegacyVersion: func(context.Context, *sql.Tx) (int, error) {
			return 0, nil
		},
		Steps: []Step{
			{To: 1, Name: "1", Run: func(context.Context, *sql.Tx) error { seq = append(seq, 1); return nil }},
			{To: 2, Name: "2", Run: func(context.Context, *sql.Tx) error { seq = append(seq, 2); return nil }},
			{To: 3, Name: "3", Run: func(context.Context, *sql.Tx) error { seq = append(seq, 3); return nil }},
		},
	}, EnsureOptions{})
	require.NoError(t, err)
	require.Equal(t, []int{1, 2, 3}, seq)
}

func TestEnsureDomain_ResumesAfterFailedStep(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	failOnce := true
	migrateErr := errors.New("step failed")

	plan := DomainPlan{
		Domain: Domain("resume"),
		Target: 2,
		DiscoverLegacyVersion: func(context.Context, *sql.Tx) (int, error) {
			return 0, nil
		},
		Steps: []Step{
			{To: 1, Run: func(context.Context, *sql.Tx) error { return nil }},
			{To: 2, Run: func(context.Context, *sql.Tx) error {
				if failOnce {
					failOnce = false
					return migrateErr
				}
				return nil
			}},
		},
	}

	err := EnsureDomain(ctx, db, plan, EnsureOptions{})
	require.ErrorContains(t, err, "step failed")

	err = EnsureDomain(ctx, db, plan, EnsureOptions{})
	require.NoError(t, err)

	var version int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT version FROM rzm_migration_state WHERE domain = 'resume'`).Scan(&version))
	require.Equal(t, 2, version)
}

func TestEnsureDomain_FutureSchema(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	require.NoError(t, ensureMetadataTables(ctx, db))
	_, err := db.ExecContext(ctx, `INSERT INTO rzm_migration_state(domain, version, dirty, updated_at) VALUES ('future', 9, 0, ?)`, time.Now().Unix())
	require.NoError(t, err)

	err = EnsureDomain(ctx, db, DomainPlan{
		Domain: Domain("future"),
		Target: 3,
	}, EnsureOptions{})
	require.Error(t, err)
	var future *ErrFutureSchema
	require.ErrorAs(t, err, &future)
	require.Equal(t, Domain("future"), future.Domain)
	require.Equal(t, 9, future.Current)
	require.Equal(t, 3, future.Supported)
}

func TestEnsureDomain_WritesLogAndDirty(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	err := EnsureDomain(ctx, db, DomainPlan{
		Domain: Domain("logs"),
		Target: 1,
		DiscoverLegacyVersion: func(context.Context, *sql.Tx) (int, error) {
			return 0, nil
		},
		Steps: []Step{
			{To: 1, Run: func(context.Context, *sql.Tx) error { return nil }},
		},
	}, EnsureOptions{})
	require.NoError(t, err)

	var dirty int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT dirty FROM rzm_migration_state WHERE domain = 'logs'`).Scan(&dirty))
	require.Equal(t, 0, dirty)

	var status string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT status FROM rzm_migration_log WHERE domain = 'logs' ORDER BY id DESC LIMIT 1`).Scan(&status))
	require.Equal(t, "applied", status)
}

func TestEnsureDomain_WarmCurrentSkipsRevalidation(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	validations := 0
	plan := validatedTestPlan(Domain("warm"), 0, func(context.Context, *sql.Tx) error {
		validations++
		return nil
	})

	require.NoError(t, EnsureDomain(ctx, db, plan, EnsureOptions{}))
	require.NoError(t, EnsureDomain(ctx, db, plan, EnsureOptions{}))
	require.Equal(t, 1, validations, "a matching proof must skip exhaustive validation")
}

func TestEnsureDomain_ProbeMismatchFallsBackAndRefreshesProof(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	validations := 0
	plan := validatedTestPlan(Domain("fallback"), 0, func(context.Context, *sql.Tx) error {
		validations++
		return nil
	})
	require.NoError(t, EnsureDomain(ctx, db, plan, EnsureOptions{}))

	_, err := db.ExecContext(ctx, `CREATE TABLE external_schema_change (id INTEGER PRIMARY KEY)`)
	require.NoError(t, err)
	require.NoError(t, EnsureDomain(ctx, db, plan, EnsureOptions{}))
	require.Equal(t, 2, validations, "an external schema_version change must invalidate proof")
	require.NoError(t, EnsureDomain(ctx, db, plan, EnsureOptions{}))
	require.Equal(t, 2, validations, "successful fallback must refresh proof")

	plan.SchemaFingerprint = "test-plan-v2"
	require.NoError(t, EnsureDomain(ctx, db, plan, EnsureOptions{}))
	require.Equal(t, 3, validations, "an explicit plan fingerprint change must invalidate proof")
}

func TestEnsureDomain_DirtyStateFallsBackAndClearsDirtyOnlyAfterValidation(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	plan := validatedTestPlan(Domain("dirty"), 0, func(context.Context, *sql.Tx) error { return nil })
	require.NoError(t, EnsureDomain(ctx, db, plan, EnsureOptions{}))
	_, err := db.ExecContext(ctx, `UPDATE rzm_migration_state SET dirty = 1 WHERE domain = 'dirty'`)
	require.NoError(t, err)
	require.NoError(t, EnsureDomain(ctx, db, plan, EnsureOptions{}))

	var dirty int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT dirty FROM rzm_migration_state WHERE domain = 'dirty'`).Scan(&dirty))
	require.Zero(t, dirty)
}

func TestEnsureDomain_FailedValidationDoesNotRefreshProof(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	fail := false
	validations := 0
	plan := validatedTestPlan(Domain("failed_validation"), 0, func(context.Context, *sql.Tx) error {
		validations++
		if fail {
			return errors.New("invalid structure")
		}
		return nil
	})
	require.NoError(t, EnsureDomain(ctx, db, plan, EnsureOptions{}))
	_, err := db.ExecContext(ctx, `CREATE TABLE invalidate_failed_proof (id INTEGER)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE rzm_migration_state SET dirty = 1 WHERE domain = 'failed_validation'`)
	require.NoError(t, err)
	fail = true
	require.Error(t, EnsureDomain(ctx, db, plan, EnsureOptions{}))
	var dirty int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT dirty FROM rzm_migration_state WHERE domain = 'failed_validation'`).Scan(&dirty))
	require.Equal(t, 1, dirty, "failed validation must not clear dirty state")
	fail = false
	require.NoError(t, EnsureDomain(ctx, db, plan, EnsureOptions{}))
	require.Equal(t, 3, validations, "failed validation must leave the prior proof invalid")
	require.NoError(t, db.QueryRowContext(ctx, `SELECT dirty FROM rzm_migration_state WHERE domain = 'failed_validation'`).Scan(&dirty))
	require.Zero(t, dirty)
}

func TestEnsureDomain_LegacyMetadataFallsBackToFullValidation(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	require.NoError(t, ensureMetadataTablesLegacy(ctx, db))
	_, err := db.ExecContext(ctx, `INSERT INTO rzm_migration_state(domain, version, dirty, updated_at) VALUES ('legacy', 0, 0, 1)`)
	require.NoError(t, err)
	validations := 0
	plan := validatedTestPlan(Domain("legacy"), 0, func(context.Context, *sql.Tx) error { validations++; return nil })
	require.NoError(t, EnsureDomain(ctx, db, plan, EnsureOptions{}))
	require.Equal(t, 1, validations)
	require.NoError(t, EnsureDomain(ctx, db, plan, EnsureOptions{}))
	require.Equal(t, 1, validations)
}

func TestEnsureDomain_ConcurrentWarmOpensUseValidatedProbe(t *testing.T) {
	db := openTestDB(t)
	db.SetMaxOpenConns(8)
	ctx := context.Background()
	var mu sync.Mutex
	validations := 0
	plan := validatedTestPlan(Domain("concurrent"), 0, func(context.Context, *sql.Tx) error {
		mu.Lock()
		validations++
		mu.Unlock()
		return nil
	})
	require.NoError(t, EnsureDomain(ctx, db, plan, EnsureOptions{}))

	const workers = 8
	errCh := make(chan error, workers)
	for range workers {
		go func() { errCh <- EnsureDomain(ctx, db, plan, EnsureOptions{}) }()
	}
	for range workers {
		require.NoError(t, <-errCh)
	}
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, validations)
}

func ensureMetadataTablesLegacy(ctx context.Context, db *sql.DB) error {
	for _, stmt := range []string{
		`CREATE TABLE rzm_migration_state (domain TEXT PRIMARY KEY, version INTEGER NOT NULL, dirty INTEGER NOT NULL DEFAULT 0, updated_at INTEGER NOT NULL)`,
		`CREATE TABLE rzm_migration_log (id INTEGER PRIMARY KEY, domain TEXT NOT NULL, from_version INTEGER NOT NULL, to_version INTEGER NOT NULL, status TEXT NOT NULL, started_at INTEGER NOT NULL, finished_at INTEGER, error TEXT)`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func TestEnsureDomain_ValidationErrorsPreserveClassification(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		drift bool
	}{
		{name: "operational", err: errors.New("read failed")},
		{name: "canceled", err: context.Canceled},
		{name: "busy exhausted", err: sqlite3.Error{Code: sqlite3.ErrBusy}},
		{name: "structural", err: &ErrSchemaDrift{Domain: DomainIntel, Err: errors.New("missing table")}, drift: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openTestDB(t)
			plan := validatedTestPlan(DomainIntel, 0, func(context.Context, *sql.Tx) error { return tc.err })
			err := EnsureDomain(context.Background(), db, plan, EnsureOptions{})
			require.ErrorIs(t, err, tc.err)
			var drift *ErrSchemaDrift
			require.Equal(t, tc.drift, errors.As(err, &drift))
			var proofs int
			require.NoError(t, db.QueryRow(`SELECT count(*) FROM rzm_migration_validation`).Scan(&proofs))
			require.Zero(t, proofs)
			plan.Validate = func(context.Context, *sql.Tx) error { return nil }
			require.NoError(t, EnsureDomain(context.Background(), db, plan, EnsureOptions{}))
		})
	}
}
