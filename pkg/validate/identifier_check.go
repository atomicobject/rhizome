package validate

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/validate/identifierreconcile"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type identifierRepairPayload struct {
	Assembly *identifierreconcile.RepairAssembly
	Bindings []IdentifierRepairActionBinding
}

type manualIdentifierCollision struct {
	value     string
	kind      identifierreconcile.CollisionKind
	claimants []identifierreconcile.Claim
}

const issueCodeIdentifierDateTimeFormatMismatch = "identifier_datetime_format_mismatch"
const issueCodeIdentifierSequentialFormatMismatch = "identifier_sequential_format_mismatch"

// RunIdentifiers refreshes one runtime for non-product callers. Prepared
// product execution enters through RunIdentifiersWithRuntime instead.
func RunIdentifiers(ctx context.Context, runCtx RunContext) CheckResult {
	result := CheckResult{Name: CheckIdentifiers, OK: true}
	if err := runCtx.NoteMetadata.Validate(); err != nil {
		result.OK = false
		result.Error = fmt.Sprintf("note metadata indexer: %v", err)
		return result
	}
	runtime, cleanup, err := ontology.EnsureFreshRuntime(ctx, runCtx.NoteMetadata, runCtx.VaultDef, runCtx.NoteReader)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil && (runtime == nil || runtime.Schema == nil) {
		result.OK = false
		result.Error = err.Error()
		return result
	}
	return RunIdentifiersWithRuntime(ctx, runCtx, runtime)
}

// RunIdentifiersWithStore refreshes an existing caller-owned store.
func RunIdentifiersWithStore(ctx context.Context, runCtx RunContext, store *semdb.Store) CheckResult {
	if err := runCtx.NoteMetadata.Validate(); err != nil {
		return CheckResult{Name: CheckIdentifiers, Error: fmt.Sprintf("note metadata indexer: %v", err)}
	}
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, runCtx.NoteMetadata, runCtx.VaultDef, runCtx.NoteReader, store)
	if err != nil && (runtime == nil || runtime.Schema == nil) {
		return CheckResult{Name: CheckIdentifiers, Error: err.Error()}
	}
	return RunIdentifiersWithRuntime(ctx, runCtx, runtime)
}

// RunIdentifiersWithRuntime validates the complete prepared ontology node
// catalog. It never reacquires a runtime or opens a replacement store.
func RunIdentifiersWithRuntime(ctx context.Context, runCtx RunContext, runtime *ontology.Runtime) CheckResult {
	if runtime == nil || runtime.Schema == nil {
		return CheckResult{Name: CheckIdentifiers, Skipped: true, OK: true, Summary: "no ontology schema"}
	}
	if runtime.Store == nil {
		return CheckResult{Name: CheckIdentifiers, Skipped: true, OK: true, Summary: "no intel store"}
	}

	// Preserve the identifier/alias mirror and missing-required-field behavior,
	// then replace its legacy note-only collision pass with the pool inventory.
	result := runAliasesWithRuntime(ctx, runCtx, runtime, CheckResult{Name: CheckIdentifiers, OK: true})
	if result.Error != "" || result.Skipped {
		return result
	}
	inventoryStarted := time.Now()
	runtimeInventory, err := buildIdentifierRuntimeInventory(ctx, runtime)
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	}
	timings := identifierreconcile.StageTimings{}

	var reconciliationPlan *identifierreconcile.Plan
	diagnostics := identifierreconcile.RunDiagnostics{HistoryComplete: true}
	if len(runtimeInventory.claims) > 0 {
		inventory, buildErr := identifierreconcile.BuildInventory(runtimeInventory.claims)
		if buildErr != nil {
			result.OK = false
			result.Error = buildErr.Error()
			return result
		}
		timings.Inventory = nonZeroDuration(time.Since(inventoryStarted))
		var provenance identifierreconcile.GitProvenanceResult
		if collisionClaims := inventory.CollisionClaims(); len(collisionClaims) > 0 {
			gitStarted := time.Now()
			var provenanceErr error
			provenance, provenanceErr = identifierreconcile.ResolveGitProvenanceWithSchema(ctx, runCtx.VaultDef.BasePath(), runtime.Schema, collisionClaims)
			timings.GitProvenance = nonZeroDuration(time.Since(gitStarted))
			if provenanceErr != nil {
				result.OK = false
				result.Error = provenanceErr.Error()
				return result
			}
		}
		diagnostics.Git = provenance.Stats
		reconciliationPlan, err = identifierreconcile.BuildPlan(inventory, provenance.Evidence)
		if err != nil {
			result.OK = false
			result.Error = err.Error()
			return result
		}
		for _, collision := range reconciliationPlan.Collisions {
			if collision.KeeperBasis == identifierreconcile.KeeperByCanonicalKey {
				diagnostics.FallbackCollisions++
				diagnostics.HistoryComplete = false
			}
		}
	} else {
		timings.Inventory = nonZeroDuration(time.Since(inventoryStarted))
	}
	diagnostics.Timings = timings
	result.identifierReconciliation = &identifierreconcile.ReconciliationResult{Plan: reconciliationPlan, Diagnostics: diagnostics}
	migrationAssessments := assessIdentifierMigrations(runtimeInventory)

	plannedIssues, plannedActions, bindings, outputErr := plannedIdentifierCollisionOutput(reconciliationPlan, runtimeInventory)
	if outputErr != nil {
		result.OK = false
		result.Error = fmt.Sprintf("identify planned identifier collision issue: %v", outputErr)
		result.IssueCount = 0
		result.Summary = ""
		result.Issues = nil
		result.Fixes = nil
		return result
	}
	manualIssues, manualActions, outputErr := manualIdentifierCollisionOutput(runtimeInventory.manualClaims)
	if outputErr != nil {
		result.OK = false
		result.Error = fmt.Sprintf("identify manual identifier collision issue: %v", outputErr)
		result.IssueCount = 0
		result.Summary = ""
		result.Issues = nil
		result.Fixes = nil
		return result
	}
	migrationIssues, migrationActions, migrationBindings, outputErr := identifierMigrationOutput(migrationAssessments)
	if outputErr != nil {
		result.OK = false
		result.Error = fmt.Sprintf("identify identifier strategy migration issue: %v", outputErr)
		result.IssueCount = 0
		result.Summary = ""
		result.Issues = nil
		result.Fixes = nil
		return result
	}
	result.Issues = append(result.Issues, plannedIssues...)
	result.Issues = append(result.Issues, manualIssues...)
	result.Issues = append(result.Issues, migrationIssues...)
	result.Fixes = append(result.Fixes, plannedActions...)
	result.Fixes = append(result.Fixes, manualActions...)
	result.Fixes = append(result.Fixes, migrationActions...)
	bindings = append(bindings, migrationBindings...)
	sortIdentifierCheckOutput(&result)

	if identifierRepairRequired(reconciliationPlan, migrationAssessments) {
		payload, referenceDuration, transactionDuration, payloadErr := buildIdentifierRepairPayload(ctx, runCtx, reconciliationPlan, migrationAssessments, runtimeInventory, bindings)
		result.identifierReconciliation.Diagnostics.Timings.ReferenceDiscovery = referenceDuration
		result.identifierReconciliation.Diagnostics.Timings.TransactionConstruction = transactionDuration
		if payloadErr != nil {
			result.Notes = append(result.Notes, "identifier repair planning unavailable: "+payloadErr.Error())
		} else {
			result.identifierRepair = payload
		}
	}

	result.IssueCount = len(result.Issues)
	if result.IssueCount == 0 {
		result.Summary = fmt.Sprintf("%d identifier nodes checked", len(runtimeInventory.nodesByKey))
	} else {
		result.Summary = fmt.Sprintf("%d identifier issues", result.IssueCount)
	}
	return result
}

func plannedIdentifierCollisionOutput(plan *identifierreconcile.Plan, inventory identifierRuntimeInventory) ([]Issue, []FixAction, []IdentifierRepairActionBinding, error) {
	if plan == nil {
		return nil, nil, nil, nil
	}
	var issues []Issue
	var actions []FixAction
	var bindings []IdentifierRepairActionBinding
	for _, collision := range plan.Collisions {
		materializable := identifierCollisionMaterializable(collision, inventory)
		data := plannedIdentifierCollisionData(collision, plan.Fingerprint, materializable)
		code := identifierCollisionIssueCode(collision.Kind)
		var issueKeys []string
		for _, claimant := range append([]identifierreconcile.Claim{collision.Keeper}, collisionLoserClaims(collision.Losers)...) {
			claimantData := data
			claimantData.Claimant = identifierCollisionClaimant(claimant, identifierCollisionReplacement(collision, claimant))
			issue := Issue{
				Code: code, Path: claimant.Node.NotePath, Type: claimant.Node.TypeName,
				Field: claimant.Node.IdentifierField, Target: collision.Value,
				Message: identifierCollisionMessage(collision), Data: mustMarshal(claimantData),
			}
			key, err := StableIssueKey(CheckIdentifiers, issue)
			if err != nil {
				return nil, nil, nil, err
			}
			issue.Key = key
			issues = append(issues, issue)
			issueKeys = append(issueKeys, key)
		}
		affected := identifierCollisionPaths(collision)
		action := FixAction{
			ID:    "identifier-reconcile:" + strings.TrimPrefix(collision.Key, "identifier-collision:v1:"),
			Check: CheckIdentifiers, IssueCode: code, Kind: "reconcile_identifier_collision",
			Safety: FixSafetyConfirm, Title: fmt.Sprintf("Reconcile identifier collision %q", collision.Value),
			Summary:       fmt.Sprintf("keep %s and reconcile %d conflicting claimant(s)", collision.Keeper.Node.String(), len(collision.Losers)),
			Question:      fmt.Sprintf("Apply the reviewed identifier reconciliation for %q?", collision.Value),
			InstanceCount: len(collision.Losers), IssueKeys: sortedUnique(issueKeys), AffectedPaths: affected,
		}
		if !materializable {
			action.Safety = FixSafetyAgent
			action.Question = ""
			action.Summary = data.Guidance
		}
		actions = append(actions, action)
		bindings = append(bindings, IdentifierRepairActionBinding{MembershipKey: collision.Key, Action: action})
	}
	return issues, actions, bindings, nil
}

func plannedIdentifierCollisionData(collision identifierreconcile.PlannedCollision, fingerprint string, materializable bool) IdentifierCollisionData {
	claimants := []IdentifierCollisionClaimant{identifierCollisionClaimant(collision.Keeper, "")}
	for _, loser := range collision.Losers {
		claimants = append(claimants, identifierCollisionClaimant(loser.Claim, loser.Replacement))
	}
	sort.Slice(claimants, func(i, j int) bool {
		left := claimants[i].NodePath + "#" + claimants[i].Fragment + ":" + claimants[i].Kind
		right := claimants[j].NodePath + "#" + claimants[j].Fragment + ":" + claimants[j].Kind
		return left < right
	})
	data := IdentifierCollisionData{
		Identifier: collision.Value, Kind: string(collision.Kind), Pool: collision.Pool.String(), Claimants: claimants,
		Keeper: identifierCollisionClaimant(collision.Keeper, ""), KeeperBasis: string(collision.KeeperBasis),
		FallbackReason: collision.FallbackReason, Materializable: materializable, PlanFingerprint: fingerprint,
	}
	if !materializable {
		data.Guidance = "collision includes an identifier claim that cannot be materialized deterministically; review the listed canonical claimants and configure an allocatable preferred identifier before replanning"
	}
	return data
}

func identifierCollisionClaimant(claim identifierreconcile.Claim, replacement string) IdentifierCollisionClaimant {
	return IdentifierCollisionClaimant{
		NodePath: claim.Node.NotePath, Fragment: claim.Node.Fragment, TypeName: claim.Node.TypeName,
		FieldName: claim.Node.IdentifierField, Kind: string(claim.Kind), Value: claim.Value, Replacement: replacement,
	}
}

func manualIdentifierCollisionOutput(claims []identifierreconcile.Claim) ([]Issue, []FixAction, error) {
	var issues []Issue
	var actions []FixAction
	for _, collision := range buildManualIdentifierCollisions(claims) {
		claimants := make([]IdentifierCollisionClaimant, 0, len(collision.claimants))
		owners := make([]string, 0, len(collision.claimants))
		for _, claim := range collision.claimants {
			claimants = append(claimants, identifierCollisionClaimant(claim, ""))
			owners = append(owners, claim.Node.NotePath)
		}
		data := IdentifierCollisionData{
			Identifier: collision.value, Kind: string(collision.kind), Claimants: claimants, Keeper: claimants[0],
			KeeperBasis: string(identifierreconcile.KeeperByCanonicalKey), Materializable: false,
			Guidance: "author-supplied identifier pool has no allocation strategy; choose a replacement explicitly, then rerun `rzm validate fix identifiers`",
		}
		code := identifierCollisionIssueCode(collision.kind)
		var keys []string
		for _, claim := range collision.claimants {
			claimantData := data
			claimantData.Claimant = identifierCollisionClaimant(claim, "")
			issue := Issue{Code: code, Path: claim.Node.NotePath, Type: claim.Node.TypeName, Field: claim.Node.IdentifierField, Target: collision.value, Message: identifierManualCollisionMessage(collision), Data: mustMarshal(claimantData)}
			key, err := StableIssueKey(CheckIdentifiers, issue)
			if err != nil {
				return nil, nil, err
			}
			issue.Key = key
			issues = append(issues, issue)
			keys = append(keys, key)
		}
		actions = append(actions, FixAction{
			ID: manualIdentifierReviewActionID(collision), Check: CheckIdentifiers, IssueCode: code,
			Kind: "review_identifier_collision", Safety: FixSafetyAgent, Title: fmt.Sprintf("Review identifier collision %q", collision.value),
			Summary: data.Guidance, InstanceCount: len(collision.claimants), IssueKeys: sortedUnique(keys), AffectedPaths: sortedUnique(owners),
		})
	}
	return issues, actions, nil
}

func identifierCollisionReplacement(collision identifierreconcile.PlannedCollision, claimant identifierreconcile.Claim) string {
	for _, loser := range collision.Losers {
		if loser.Claim.ID() == claimant.ID() {
			return loser.Replacement
		}
	}
	return ""
}

func manualIdentifierReviewActionID(collision manualIdentifierCollision) string {
	identity := []string{collision.value, string(collision.kind)}
	for _, claim := range collision.claimants {
		identity = append(identity, claim.Node.String()+":"+string(claim.Kind)+":"+identifierreconcile.IdentifierComparisonKey(claim.Value))
	}
	return "identifier-review:" + strings.TrimPrefix(SourceHash([]byte(strings.Join(identity, "\x00"))), "sha256:")
}

func buildManualIdentifierCollisions(claims []identifierreconcile.Claim) []manualIdentifierCollision {
	groups := make(map[string][]identifierreconcile.Claim)
	for _, claim := range claims {
		family := claim.Node.TypeName + "\x00" + claim.Node.IdentifierField
		key := family + "\x00" + identifierreconcile.IdentifierComparisonKey(claim.Value)
		groups[key] = append(groups[key], claim)
	}
	var collisions []manualIdentifierCollision
	for _, group := range groups {
		byNode := make(map[string]identifierreconcile.Claim)
		for _, claim := range group {
			key := claim.Node.String()
			current, found := byNode[key]
			if !found || (current.Kind == identifierreconcile.ClaimAlias && claim.Kind == identifierreconcile.ClaimPreferred) {
				byNode[key] = claim
			}
		}
		if len(byNode) < 2 {
			continue
		}
		claimants := make([]identifierreconcile.Claim, 0, len(byNode))
		for _, claim := range byNode {
			claimants = append(claimants, claim)
		}
		sort.Slice(claimants, func(i, j int) bool { return claimants[i].Node.String() < claimants[j].Node.String() })
		collisions = append(collisions, manualIdentifierCollision{value: identifierreconcile.IdentifierComparisonKey(claimants[0].Value), kind: identifierClaimsCollisionKind(claimants), claimants: claimants})
	}
	sort.Slice(collisions, func(i, j int) bool { return collisions[i].value < collisions[j].value })
	return collisions
}

func identifierClaimsCollisionKind(claims []identifierreconcile.Claim) identifierreconcile.CollisionKind {
	preferred, aliases := 0, 0
	for _, claim := range claims {
		if claim.Kind == identifierreconcile.ClaimPreferred {
			preferred++
		} else {
			aliases++
		}
	}
	if preferred > 0 && aliases > 0 {
		return identifierreconcile.CollisionPreferredAlias
	}
	if preferred > 0 {
		return identifierreconcile.CollisionPreferredPreferred
	}
	return identifierreconcile.CollisionAliasAlias
}

func identifierRepairRequired(plan *identifierreconcile.Plan, migrations []identifierMigrationAssessment) bool {
	if plan != nil && len(plan.Collisions) > 0 {
		return true
	}
	for _, migration := range migrations {
		if migration.plan != nil {
			return true
		}
	}
	return false
}

func buildIdentifierRepairPayload(ctx context.Context, runCtx RunContext, plan *identifierreconcile.Plan, migrations []identifierMigrationAssessment, inventory identifierRuntimeInventory, bindings []IdentifierRepairActionBinding) (*identifierRepairPayload, time.Duration, time.Duration, error) {
	var roots []reference.IdentifierRewrite
	var collisionInputs []identifierreconcile.CollisionRepairInput
	if plan != nil {
		for _, collision := range plan.Collisions {
			input := identifierreconcile.CollisionRepairInput{CollisionKey: collision.Key}
			for _, loser := range collision.Losers {
				node := inventory.nodesByKey[loser.Claim.Node.String()]
				oldRef := ontology.NodeRef{NotePath: loser.Claim.Node.NotePath, Fragment: loser.Claim.Node.Fragment, TypeName: loser.Claim.Node.TypeName, Kind: ontology.NodeKind(node.row.NodeKind)}
				newRef := oldRef
				newIdentifier := loser.Replacement
				mode := reference.IdentifierRewritePreferredRekey
				if loser.Claim.Kind == identifierreconcile.ClaimAlias {
					mode = reference.IdentifierRewriteAliasRemoval
					newIdentifier = inventory.preferredByNode[loser.Claim.Node.String()]
				}
				aliasField, additionalAliasesFields := identifierRewriteAliasFields(node, loser.Claim.Value, mode == reference.IdentifierRewriteAliasRemoval)
				rewrite := reference.IdentifierRewrite{Mode: mode, OldRef: oldRef, NewRef: newRef, OldIdentifier: loser.Claim.Value, NewIdentifier: newIdentifier, PreferredField: loser.Claim.Node.IdentifierField, AliasesField: aliasField, AdditionalAliasesFields: additionalAliasesFields}
				if loser.Claim.Kind == identifierreconcile.ClaimPreferred {
					request := obsidian.GovernedMoveRequest{SourcePath: oldRef.NotePath, OldID: loser.Claim.Value, NewID: newIdentifier, SiblingPaths: inventory.notePaths}
					move, conflict, err := obsidian.PlanGovernedIdentifierMove(request)
					if err != nil {
						return nil, 0, 0, err
					}
					newRef.NotePath = move.DestinationPath
					rewrite.NewRef = newRef
					input.Moves = append(input.Moves, identifierreconcile.GovernedMoveResult{Request: request, Plan: move, Conflict: conflict})
				}
				input.Rewrites = append(input.Rewrites, rewrite)
				roots = append(roots, rewrite)
			}
			collisionInputs = append(collisionInputs, input)
		}
	}
	var migrationInputs []identifierreconcile.MigrationRepairInput
	for _, assessment := range migrations {
		if assessment.plan == nil {
			continue
		}
		input := identifierreconcile.MigrationRepairInput{Plan: assessment.plan}
		for _, planned := range assessment.plan.Rewrites {
			node, ok := inventory.nodesByKey[planned.Node.String()]
			if !ok {
				return nil, 0, 0, fmt.Errorf("migration node %s is missing from identifier inventory", planned.Node.String())
			}
			oldRef := ontology.NodeRef{NotePath: planned.Node.NotePath, Fragment: planned.Node.Fragment, TypeName: planned.Node.TypeName, Kind: ontology.NodeKind(node.row.NodeKind)}
			newRef := oldRef
			aliasField, additionalAliasesFields := identifierRewriteAliasFields(node, planned.OldIdentifier, false)
			rewrite := reference.IdentifierRewrite{
				Mode: reference.IdentifierRewritePreferredRekey, OldRef: oldRef, NewRef: newRef,
				OldIdentifier: planned.OldIdentifier, NewIdentifier: planned.NewIdentifier,
				PreferredField: planned.Node.IdentifierField, AliasesField: aliasField, AdditionalAliasesFields: additionalAliasesFields,
			}
			if oldRef.Kind == ontology.NodeKindNote {
				request := obsidian.GovernedMoveRequest{SourcePath: oldRef.NotePath, OldID: planned.OldIdentifier, NewID: planned.NewIdentifier, SiblingPaths: inventory.notePaths}
				move, conflict, err := obsidian.PlanGovernedIdentifierMove(request)
				if err != nil {
					return nil, 0, 0, err
				}
				newRef.NotePath = move.DestinationPath
				rewrite.NewRef = newRef
				input.Moves = append(input.Moves, identifierreconcile.GovernedMoveResult{Request: request, Plan: move, Conflict: conflict})
			}
			input.Rewrites = append(input.Rewrites, rewrite)
			roots = append(roots, rewrite)
		}
		migrationInputs = append(migrationInputs, input)
	}
	referenceStarted := time.Now()
	fields, err := identifierreconcile.DiscoverIdentifierFields(ctx, identifierreconcile.IdentifierFieldDiscoveryRequest{VaultDef: runCtx.VaultDef, RootRewrites: roots})
	if err != nil {
		return nil, nonZeroDuration(time.Since(referenceStarted)), 0, err
	}
	links, err := identifierreconcile.DiscoverIdentifierLinks(ctx, identifierreconcile.IdentifierLinkDiscoveryRequest{FieldDiscovery: fields})
	referenceDuration := nonZeroDuration(time.Since(referenceStarted))
	if err != nil {
		return nil, referenceDuration, 0, err
	}
	transactionStarted := time.Now()
	assemblyPlan := plan
	if assemblyPlan != nil && len(assemblyPlan.Collisions) == 0 {
		assemblyPlan = nil
	}
	assembly, err := identifierreconcile.AssembleRekeyRepairIntents(identifierreconcile.RekeyRepairAssemblyInput{
		Plan: assemblyPlan, Collisions: collisionInputs, Migrations: migrationInputs,
		FieldDiscovery: fields, LinkDiscovery: links,
	})
	if err != nil {
		return nil, referenceDuration, nonZeroDuration(time.Since(transactionStarted)), err
	}
	transactionDuration := nonZeroDuration(time.Since(transactionStarted))
	return &identifierRepairPayload{Assembly: assembly, Bindings: bindings}, referenceDuration, transactionDuration, nil
}

func identifierCollisionMaterializable(collision identifierreconcile.PlannedCollision, inventory identifierRuntimeInventory) bool {
	for _, loser := range collision.Losers {
		node, ok := inventory.nodesByKey[loser.Claim.Node.String()]
		if !ok {
			return false
		}
		if loser.Claim.Kind == identifierreconcile.ClaimPreferred {
			if ontology.NodeKind(node.row.NodeKind) != ontology.NodeKindNote || loser.Replacement == "" {
				return false
			}
			_, conflict, err := obsidian.PlanGovernedIdentifierMove(obsidian.GovernedMoveRequest{
				SourcePath: loser.Claim.Node.NotePath, OldID: loser.Claim.Value,
				NewID: loser.Replacement, SiblingPaths: inventory.notePaths,
			})
			if err != nil || conflict != nil {
				return false
			}
			continue
		}
		preferred := inventory.preferredByNode[loser.Claim.Node.String()]
		if preferred == "" || identifierreconcile.IdentifierComparisonKey(preferred) == identifierreconcile.IdentifierComparisonKey(loser.Claim.Value) {
			return false
		}
	}
	return true
}

func collisionLoserClaims(losers []identifierreconcile.PlannedLoser) []identifierreconcile.Claim {
	out := make([]identifierreconcile.Claim, 0, len(losers))
	for _, loser := range losers {
		out = append(out, loser.Claim)
	}
	return out
}

func identifierCollisionPaths(collision identifierreconcile.PlannedCollision) []string {
	paths := []string{collision.Keeper.Node.NotePath}
	for _, loser := range collision.Losers {
		paths = append(paths, loser.Claim.Node.NotePath)
	}
	return sortedUnique(paths)
}

func identifierCollisionIssueCode(kind identifierreconcile.CollisionKind) string {
	switch kind {
	case identifierreconcile.CollisionPreferredPreferred:
		return "duplicate_preferred_identifier"
	case identifierreconcile.CollisionPreferredAlias:
		return "identifier_preferred_alias_collision"
	default:
		return "duplicate_identifier_alias"
	}
}

func identifierCollisionMessage(collision identifierreconcile.PlannedCollision) string {
	return fmt.Sprintf("identifier %q has %d canonical claimants in one shared pool; keeper %s selected by %s", collision.Value, len(collision.Losers)+1, collision.Keeper.Node.String(), collision.KeeperBasis)
}

func identifierManualCollisionMessage(collision manualIdentifierCollision) string {
	return fmt.Sprintf("author-supplied identifier %q has %d canonical claimants and requires an explicit replacement", collision.value, len(collision.claimants))
}

func sortIdentifierCheckOutput(result *CheckResult) {
	sort.SliceStable(result.Issues, func(i, j int) bool {
		if result.Issues[i].Target != result.Issues[j].Target {
			return result.Issues[i].Target < result.Issues[j].Target
		}
		if result.Issues[i].Path != result.Issues[j].Path {
			return result.Issues[i].Path < result.Issues[j].Path
		}
		return result.Issues[i].Code < result.Issues[j].Code
	})
	sort.SliceStable(result.Fixes, func(i, j int) bool { return result.Fixes[i].ID < result.Fixes[j].ID })
}

func nonZeroDuration(value time.Duration) time.Duration {
	if value <= 0 {
		return time.Nanosecond
	}
	return value
}
