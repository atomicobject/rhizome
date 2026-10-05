package validate

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/validate/identifierreconcile"
)

const (
	issueCodeIdentifierStrategyMigrationRequired = "identifier_strategy_migration_required"
	issueCodeIdentifierStrategyMigrationBlocked  = "identifier_strategy_migration_blocked"
)

type identifierMigrationAssessment struct {
	pool *identifierRuntimePool
	plan *identifierreconcile.MigrationPlan
	err  *identifierreconcile.MigrationError
}

func assessIdentifierMigrations(inventory identifierRuntimeInventory) []identifierMigrationAssessment {
	keys := make([]identifierreconcile.PoolKey, 0, len(inventory.poolsByKey))
	for key := range inventory.poolsByKey {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })

	assessments := make([]identifierMigrationAssessment, 0, len(keys))
	for _, key := range keys {
		pool := inventory.poolsByKey[key]
		alreadyTarget := identifierPoolAlreadyUsesTargetStrategy(pool)
		if len(pool.blockers) > 0 && (!alreadyTarget || identifierPoolHasUnconditionalBlocker(pool)) {
			sort.Slice(pool.blockers, func(i, j int) bool { return pool.blockers[i].node.String() < pool.blockers[j].node.String() })
			blocker := pool.blockers[0]
			assessments = append(assessments, identifierMigrationAssessment{pool: pool, err: &identifierreconcile.MigrationError{
				Code: identifierreconcile.MigrationErrorMixedStrategy, Node: blocker.node, Value: blocker.value,
				Message: "complete pool recovery is blocked: " + blocker.reason,
			}})
			continue
		}
		if alreadyTarget {
			continue
		}
		members, reservations := identifierMigrationSourceMembers(pool)
		plan, err := identifierreconcile.BuildMigrationPlan(identifierreconcile.MigrationInput{
			TargetFormat: pool.targetFormat, Members: members, TargetReservations: reservations,
		})
		if err == nil {
			if len(plan.Rewrites) > 0 {
				assessments = append(assessments, identifierMigrationAssessment{pool: pool, plan: plan})
			}
			continue
		}
		var migrationErr *identifierreconcile.MigrationError
		if !errors.As(err, &migrationErr) || migrationErr.Code == identifierreconcile.MigrationErrorNotRequired {
			continue
		}
		assessments = append(assessments, identifierMigrationAssessment{pool: pool, err: migrationErr})
	}
	return assessments
}

// identifierMigrationSourceMembers keeps already-migrated members in the
// complete pool as target reservations. This lets an interrupted migration or
// branch merge resume without rewriting target-strategy identifiers.
func identifierMigrationSourceMembers(pool *identifierRuntimePool) ([]identifierreconcile.MigrationMember, []ontology.IdentifierReservation) {
	members := make([]identifierreconcile.MigrationMember, 0, len(pool.members))
	reservations := append([]ontology.IdentifierReservation(nil), pool.targetReservations...)
	target, err := pool.targetFormat.StrategyContract()
	if err != nil {
		return append(members, pool.members...), reservations
	}
	for _, member := range pool.members {
		if _, ok := target.Parse(member.PreferredValue); ok {
			reservations = append(reservations, ontology.IdentifierReservation{
				Value: member.PreferredValue,
				Owner: member.Node.String(),
			})
			continue
		}
		members = append(members, member)
	}
	return members, reservations
}

func identifierPoolHasUnconditionalBlocker(pool *identifierRuntimePool) bool {
	for _, blocker := range pool.blockers {
		if blocker.always {
			return true
		}
	}
	return false
}

func identifierPoolAlreadyUsesTargetStrategy(pool *identifierRuntimePool) bool {
	if pool == nil || len(pool.members) == 0 {
		return true
	}
	strategy, err := pool.targetFormat.StrategyContract()
	if err != nil {
		return false
	}
	for _, member := range pool.members {
		if _, ok := strategy.Parse(member.PreferredValue); !ok {
			return false
		}
	}
	return true
}

func identifierMigrationOutput(assessments []identifierMigrationAssessment) ([]Issue, []FixAction, []IdentifierRepairActionBinding, error) {
	var issues []Issue
	var actions []FixAction
	var bindings []IdentifierRepairActionBinding
	for _, assessment := range assessments {
		if assessment.plan != nil {
			planIssues, action, binding, err := plannedIdentifierMigrationOutput(assessment)
			if err != nil {
				return nil, nil, nil, err
			}
			issues = append(issues, planIssues...)
			actions = append(actions, action)
			bindings = append(bindings, binding)
			continue
		}
		issue, action, err := blockedIdentifierMigrationOutput(assessment)
		if err != nil {
			return nil, nil, nil, err
		}
		issues = append(issues, issue)
		actions = append(actions, action)
	}
	return issues, actions, bindings, nil
}

func plannedIdentifierMigrationOutput(assessment identifierMigrationAssessment) ([]Issue, FixAction, IdentifierRepairActionBinding, error) {
	plan := assessment.plan
	data := IdentifierStrategyMigrationData{
		SourcePool: plan.SourcePool.String(), TargetPool: plan.TargetPool.String(),
		MemberCount: len(plan.Rewrites), Materializable: true, PlanFingerprint: plan.Fingerprint,
	}
	// One reviewed action migrates the whole pool, so the pool is the variant.
	variant := newIssueVariant(plan.TargetPool.String(), fmt.Sprintf("%s%s (%s → %s)",
		plan.TargetPool.Prefix, plan.TargetPool.Separator, plan.SourcePool.Strategy, plan.TargetPool.Strategy))
	issues := make([]Issue, 0, len(plan.Rewrites))
	issueKeys := make([]string, 0, len(plan.Rewrites))
	paths := make([]string, 0, len(plan.Rewrites))
	for _, rewrite := range plan.Rewrites {
		memberData := data
		memberData.OldIdentifier = rewrite.OldIdentifier
		memberData.NewIdentifier = rewrite.NewIdentifier
		issue := Issue{
			Code: issueCodeIdentifierStrategyMigrationRequired, Path: rewrite.Node.NotePath,
			Type: rewrite.Node.TypeName, Field: rewrite.Node.IdentifierField, Target: rewrite.OldIdentifier,
			Message: fmt.Sprintf("preferred identifier %q uses %s while the schema declares %s; migrate it to %q as part of the complete pool", rewrite.OldIdentifier, plan.SourcePool.Strategy, plan.TargetPool.Strategy, rewrite.NewIdentifier),
			Data:    mustMarshal(memberData),
			Variant: variant,
		}
		key, err := StableIssueKey(CheckIdentifiers, issue)
		if err != nil {
			return nil, FixAction{}, IdentifierRepairActionBinding{}, err
		}
		issue.Key = key
		issues = append(issues, issue)
		issueKeys = append(issueKeys, key)
		paths = append(paths, rewrite.Node.NotePath)
	}
	action := FixAction{
		ID:    "identifier-migrate:" + strings.TrimPrefix(plan.Key, "identifier-migration:v1:"),
		Check: CheckIdentifiers, IssueCode: issueCodeIdentifierStrategyMigrationRequired, Kind: "migrate_identifier_strategy",
		Safety: FixSafetyConfirm, Title: fmt.Sprintf("Migrate identifier pool from %s to %s", plan.SourcePool.Strategy, plan.TargetPool.Strategy),
		Summary:  fmt.Sprintf("rekey all %d pool member(s) and their structured identity graph", len(plan.Rewrites)),
		Question: "Apply the reviewed whole-pool identifier strategy migration?", InstanceCount: len(plan.Rewrites),
		IssueKeys: sortedUnique(issueKeys), AffectedPaths: sortedUnique(paths),
	}
	return issues, action, IdentifierRepairActionBinding{MembershipKey: plan.Key, Action: action}, nil
}

func blockedIdentifierMigrationOutput(assessment identifierMigrationAssessment) (Issue, FixAction, error) {
	migrationErr := assessment.err
	path, typeName, fieldName, target := "", "", "", migrationErr.Value
	if migrationErr.Node.NotePath != "" {
		path, typeName, fieldName = migrationErr.Node.NotePath, migrationErr.Node.TypeName, migrationErr.Node.IdentifierField
	} else if len(assessment.pool.members) > 0 {
		member := assessment.pool.members[0]
		path, typeName, fieldName = member.Node.NotePath, member.Node.TypeName, member.Node.IdentifierField
		if target == "" {
			target = member.PreferredValue
		}
	}
	data := IdentifierStrategyMigrationData{
		TargetPool: assessment.pool.key.String(), MemberCount: len(assessment.pool.members),
		Materializable: false, BlockReason: string(migrationErr.Code), Guidance: migrationErr.Message,
	}
	issueCode := issueCodeIdentifierStrategyMigrationBlocked
	kind := "review_identifier_strategy_migration"
	title := "Review blocked identifier strategy migration"
	message := "identifier strategy migration is not safely materializable: " + migrationErr.Message
	if migrationErr.Code == identifierreconcile.MigrationErrorInvalidSourceValue {
		if assessment.pool.key.Strategy == "DATETIME" {
			issueCode = issueCodeIdentifierDateTimeFormatMismatch
			kind = "review_identifier_datetime_format"
			title = "Repair noncanonical datetime identifier"
			message = fmt.Sprintf("preferred datetime identifier %q is not canonical; use <prefix><separator>YYYY-MM-DD-HH-MM or append an unpadded collision suffix -N where N is at least 2", target)
		} else {
			issueCode = issueCodeIdentifierSequentialFormatMismatch
			kind = "review_identifier_sequential_format"
			title = "Repair noncanonical sequential identifier"
			message = fmt.Sprintf("preferred sequential identifier %q is not canonical; use the schema-declared prefix, separator, and decimal ordinal", target)
		}
	}
	issue := Issue{
		Code: issueCode, Path: path, Type: typeName, Field: fieldName, Target: target,
		Message: message,
		Data:    mustMarshal(data),
	}
	key, err := StableIssueKey(CheckIdentifiers, issue)
	if err != nil {
		return Issue{}, FixAction{}, err
	}
	issue.Key = key
	action := FixAction{
		ID: "identifier-migration-blocked:" + assessment.pool.key.String(), Check: CheckIdentifiers,
		IssueCode: issue.Code, Kind: kind, Safety: FixSafetyAgent,
		Title: title, Summary: migrationErr.Message,
		InstanceCount: len(assessment.pool.members), IssueKeys: []string{key}, AffectedPaths: identifierMigrationMemberPaths(assessment.pool.members),
	}
	return issue, action, nil
}

func identifierMigrationMemberPaths(members []identifierreconcile.MigrationMember) []string {
	paths := make([]string, 0, len(members))
	for _, member := range members {
		paths = append(paths, member.Node.NotePath)
	}
	return sortedUnique(paths)
}
