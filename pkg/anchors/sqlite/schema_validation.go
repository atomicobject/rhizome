package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
)

func intelSchemaDrift(format string, args ...any) error {
	return &migration.ErrSchemaDrift{Domain: migration.DomainIntel, Err: fmt.Errorf(format, args...)}
}

func (s *Store) validateExistingSchema(ctx context.Context) error {
	plan, err := s.intelMigrationPlan(ctx, currentSchemaVersion)
	if err != nil {
		return err
	}
	valid, _, err := migration.ProbeValidatedDomain(ctx, s.db, plan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || isSchemaError(err) || sqliteutil.IsCorruptError(err) {
			return &migration.ErrSchemaDrift{Domain: migration.DomainIntel, Err: err}
		}
		// An unreadable proof does not establish schema drift. Preserve operational
		// errors (including cancellation) so callers do not prescribe a rebuild.
		return err
	}
	if !valid {
		return &migration.ErrSchemaDrift{
			Domain: migration.DomainIntel,
			Err:    errors.New("current validated schema proof is unavailable"),
		}
	}
	return nil
}
