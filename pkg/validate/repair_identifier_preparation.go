package validate

import (
	"context"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/validate/identifierreconcile"
)

// Identifier diagnostics retain semantic intent, never a source-backed assembly.
type identifierRepairPreparation struct {
	plan       *identifierreconcile.Plan
	migrations []identifierMigrationAssessment
	inventory  identifierRuntimeInventory
}

func prepareIdentifierRepairChecks(ctx context.Context, runCtx RunContext, checks []CheckResult) ([]CheckResult, error) {
	prepared := append([]CheckResult(nil), checks...)
	for i := range prepared {
		check := &prepared[i]
		payload := check.identifierRepair
		if payload == nil || payload.preparation == nil {
			continue
		}
		intent := payload.preparation
		materialized, references, construction, err := buildIdentifierRepairPayload(ctx, runCtx, intent.plan, intent.migrations, intent.inventory, payload.Bindings)
		if err != nil {
			return nil, fmt.Errorf("prepare identifier repairs: %w", err)
		}
		check.identifierRepair = materialized
		if check.identifierReconciliation != nil {
			reconciliation := *check.identifierReconciliation
			reconciliation.Diagnostics.Timings.ReferenceDiscovery = references
			reconciliation.Diagnostics.Timings.TransactionConstruction = construction
			check.identifierReconciliation = &reconciliation
		}
	}
	return prepared, nil
}

func (p *identifierRepairPreparation) aliasAppendMemberships(edit FixEdit) []string {
	var memberships []string
	matches := func(path, value string) bool {
		return path == edit.NotePath && identifierreconcile.IdentifierComparisonKey(value) == identifierreconcile.IdentifierComparisonKey(edit.Value)
	}
	if p.plan != nil {
		for _, collision := range p.plan.Collisions {
			for _, loser := range collision.Losers {
				if loser.Claim.Kind == identifierreconcile.ClaimPreferred && matches(loser.Claim.Node.NotePath, loser.Claim.Value) {
					memberships = append(memberships, collision.Key)
				}
			}
		}
	}
	for _, migration := range p.migrations {
		if migration.plan == nil {
			continue
		}
		for _, rewrite := range migration.plan.Rewrites {
			if matches(rewrite.Node.NotePath, rewrite.OldIdentifier) {
				memberships = append(memberships, migration.plan.Key)
			}
		}
	}
	return sortedUnique(memberships)
}
