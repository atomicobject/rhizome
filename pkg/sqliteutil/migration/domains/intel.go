package domains

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
)

// TxStep is a transactional migration function.
type TxStep func(context.Context, *sql.Tx) error

// IntelPlan builds a migration plan for the intel domain, using the provided
// per-version step functions.
func IntelPlan(target int, steps []TxStep, validate func(context.Context, *sql.Tx) error) migration.DomainPlan {
	return IntelPlanFrom(0, target, steps, validate)
}

// IntelPlanFrom builds a migration plan whose first forward step follows a
// consolidated baseline. Baseline construction is owned by the Intel store;
// only post-baseline changes are represented as migration steps here.
func IntelPlanFrom(baseline, target int, steps []TxStep, validate func(context.Context, *sql.Tx) error) migration.DomainPlan {
	planSteps := make([]migration.Step, 0, len(steps))
	for i, step := range steps {
		from := baseline + i
		to := from + 1
		run := step
		planSteps = append(planSteps, migration.Step{
			To:   to,
			Name: fmt.Sprintf("intel_v%d_to_v%d", from, to),
			Run:  run,
		})
	}

	return migration.DomainPlan{
		Domain: migration.DomainIntel,
		Target: target,
		Steps:  planSteps,
		DiscoverLegacyVersion: func(ctx context.Context, tx *sql.Tx) (int, error) {
			return discoverLegacyVersionFromTable(ctx, tx, "schema_version", "version")
		},
		Validate: validate,
	}
}
