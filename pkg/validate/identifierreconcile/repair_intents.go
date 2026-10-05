package identifierreconcile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// RepairPostcheck names semantic validation required after applying one
// rekey component. The EFF-0047 adapter maps these names to stable check IDs.
type RepairPostcheck string

const (
	PostcheckIdentifiers     RepairPostcheck = "identifiers"
	PostcheckOntology        RepairPostcheck = "ontology"
	PostcheckBrokenLinks     RepairPostcheck = "broken-links"
	PostcheckFragileExternal RepairPostcheck = "fragile-external"
)

// GovernedMoveResult preserves the pure move plan and any portable collision
// found while constructing it.
type GovernedMoveResult struct {
	Request  obsidian.GovernedMoveRequest `json:"request"`
	Plan     obsidian.GovernedMovePlan    `json:"plan"`
	Conflict *obsidian.MoveConflict       `json:"conflict,omitempty"`
}

// CollisionRepairInput contains already-planned reference and move evidence
// for exactly one PlannedCollision.
type CollisionRepairInput struct {
	CollisionKey string                        `json:"collisionKey"`
	Rewrites     []reference.IdentifierRewrite `json:"rewrites"`
	Moves        []GovernedMoveResult          `json:"moves,omitempty"`
}

// MigrationRepairInput binds one sealed whole-pool migration mapping to the
// root reference rewrites and governed note moves that materialize it.
// Descendant rewrites are supplied only by the shared sealed discovery.
type MigrationRepairInput struct {
	Plan     *MigrationPlan                `json:"plan"`
	Rewrites []reference.IdentifierRewrite `json:"rewrites"`
	Moves    []GovernedMoveResult          `json:"moves,omitempty"`
}

// RekeyRepairAssemblyInput composes collision and whole-pool migration
// components over one field/link discovery and complete source snapshot.
type RekeyRepairAssemblyInput struct {
	Plan           *Plan                     `json:"plan,omitempty"`
	Collisions     []CollisionRepairInput    `json:"collisions,omitempty"`
	Migrations     []MigrationRepairInput    `json:"migrations,omitempty"`
	FieldDiscovery *IdentifierFieldDiscovery `json:"-"`
	LinkDiscovery  *IdentifierLinkDiscovery  `json:"-"`
}

type IdentifierRepairIntent struct {
	MembershipKeys []string                    `json:"membershipKeys"`
	Rewrite        reference.IdentifierRewrite `json:"rewrite"`
}

type FieldRepairIntent struct {
	MembershipKeys []string                      `json:"membershipKeys"`
	SourceHash     string                        `json:"sourceHash"`
	Edit           reference.StructuredFieldEdit `json:"edit"`
}

type LinkRepairIntent struct {
	MembershipKeys []string                     `json:"membershipKeys"`
	SourceHash     string                       `json:"sourceHash"`
	Edit           reference.StructuredLinkEdit `json:"edit"`
}

type MoveRepairIntent struct {
	MembershipKeys     []string                           `json:"membershipKeys"`
	SourceHash         string                             `json:"sourceHash"`
	Move               obsidian.GovernedMovePlan          `json:"move"`
	DestinationVacancy MoveDestinationVacancyPrecondition `json:"destinationVacancy"`
}

// MoveDestinationVacancyPrecondition requires the apply adapter to verify the
// final live filesystem immediately before rename. SiblingPaths are early
// planning evidence only and never satisfy this precondition.
type MoveDestinationVacancyPrecondition struct {
	DestinationPath                string `json:"destinationPath"`
	RequireExactVacancy            bool   `json:"requireExactVacancy"`
	RequirePortableCaseFoldVacancy bool   `json:"requirePortableCaseFoldVacancy"`
}

type RepairPostcheckIntent struct {
	MembershipKeys []string        `json:"membershipKeys"`
	Check          RepairPostcheck `json:"check"`
}

// RepairDiagnostic flattens planner diagnostics without erasing their typed
// source evidence. Exactly one source pointer is populated.
type RepairDiagnostic struct {
	MembershipKeys []string                               `json:"membershipKeys"`
	Kind           string                                 `json:"kind"`
	Blocking       bool                                   `json:"blocking"`
	Field          *reference.IdentifierRewriteDiagnostic `json:"field,omitempty"`
	Link           *reference.LinkRewriteDiagnostic       `json:"link,omitempty"`
	MoveConflict   *obsidian.MoveConflict                 `json:"moveConflict,omitempty"`
}

// CollisionRepairIntent is the legacy-named, independently blockable semantic
// component used by both collision reconciliation and whole-pool migration.
// Every child intent repeats MembershipKeys so later batching cannot lose the
// originating collision membership.
type CollisionRepairIntent struct {
	MembershipKeys []string                 `json:"membershipKeys"`
	Blocked        bool                     `json:"blocked"`
	Rewrites       []IdentifierRepairIntent `json:"rewrites"`
	FieldEdits     []FieldRepairIntent      `json:"fieldEdits,omitempty"`
	AliasEdits     []FieldRepairIntent      `json:"aliasEdits,omitempty"`
	LinkEdits      []LinkRepairIntent       `json:"linkEdits,omitempty"`
	Moves          []MoveRepairIntent       `json:"moves,omitempty"`
	Diagnostics    []RepairDiagnostic       `json:"diagnostics,omitempty"`
	Postchecks     []RepairPostcheckIntent  `json:"postchecks"`
}

type RepairAssembly struct {
	PlanFingerprint     string                  `json:"planFingerprint"`
	SchemaHash          string                  `json:"schemaHash"`
	SourcePreconditions []SourcePrecondition    `json:"sourcePreconditions"`
	Components          []CollisionRepairIntent `json:"components"`
	Fingerprint         string                  `json:"fingerprint"`
	sealed              string
}

// SourcePrecondition binds one file's exact source bytes to the repair plan.
// Physical-operation hashes survive EFF-0047 mapping; EFF-0048 integration
// revalidates the complete read-only inventory under the apply lease.
type SourcePrecondition struct {
	NotePath   string `json:"notePath"`
	SourceHash string `json:"sourceHash"`
}

type rekeyComponentInput struct {
	key       string
	kind      string
	collision *PlannedCollision
	migration *MigrationPlan
	rewrites  []reference.IdentifierRewrite
	moves     []GovernedMoveResult
}

// AssembleRekeyRepairIntents validates and composes collision and migration
// components over one sealed rewrite discovery. Each migration is one
// independently attributable component keyed by its sealed pool mapping.
func AssembleRekeyRepairIntents(input RekeyRepairAssemblyInput) (*RepairAssembly, error) {
	var plan *Plan
	var err error
	if input.Plan != nil {
		plan, err = input.Plan.ValidatedSnapshot()
		if err != nil {
			return nil, err
		}
	}
	if plan == nil && len(input.Collisions) > 0 {
		return nil, fmt.Errorf("collision inputs require an identifier reconciliation plan")
	}
	if plan == nil && len(input.Migrations) == 0 {
		return nil, fmt.Errorf("at least one collision or migration component is required")
	}

	expected := make(map[string]PlannedCollision)
	if plan != nil {
		expected = make(map[string]PlannedCollision, len(plan.Collisions))
		for _, collision := range plan.Collisions {
			expected[collision.Key] = collision
		}
	}
	components := make([]rekeyComponentInput, 0, len(input.Collisions)+len(input.Migrations))
	for _, collisionInput := range input.Collisions {
		if _, err := canonicalRepairRewriteSet(collisionInput.Rewrites); err != nil {
			return nil, err
		}
	}
	byCollisionKey := make(map[string]CollisionRepairInput, len(input.Collisions))
	for _, collisionInput := range input.Collisions {
		_, ok := expected[collisionInput.CollisionKey]
		if !ok {
			return nil, fmt.Errorf("extraneous collision input %q", collisionInput.CollisionKey)
		}
		if _, duplicate := byCollisionKey[collisionInput.CollisionKey]; duplicate {
			return nil, fmt.Errorf("duplicate collision input %q", collisionInput.CollisionKey)
		}
		byCollisionKey[collisionInput.CollisionKey] = collisionInput
	}
	if plan != nil {
		for _, collision := range plan.Collisions {
			collisionInput, ok := byCollisionKey[collision.Key]
			if !ok {
				return nil, fmt.Errorf("missing collision input %q", collision.Key)
			}
			collisionCopy := collision
			components = append(components, rekeyComponentInput{key: collision.Key, kind: "collision", collision: &collisionCopy, rewrites: collisionInput.Rewrites, moves: collisionInput.Moves})
		}
	}

	migrationFingerprints := make([]migrationAssemblyAuthority, 0, len(input.Migrations))
	seenMembership := make(map[string]struct{}, len(components)+len(input.Migrations))
	for _, component := range components {
		seenMembership[component.key] = struct{}{}
	}
	for _, migrationInput := range input.Migrations {
		migration, err := migrationInput.Plan.ValidatedSnapshot()
		if err != nil {
			return nil, err
		}
		if _, duplicate := seenMembership[migration.Key]; duplicate {
			return nil, fmt.Errorf("duplicate repair component membership %q", migration.Key)
		}
		if len(migration.Rewrites) == 0 {
			return nil, fmt.Errorf("migration %s has no root rewrites", migration.Key)
		}
		seenMembership[migration.Key] = struct{}{}
		roots, err := canonicalRepairRewriteSet(migrationInput.Rewrites)
		if err != nil {
			return nil, err
		}
		for _, root := range roots {
			if !root.DerivedFrom.IsZero() {
				return nil, fmt.Errorf("migration inputs accept root rewrites only; descendants come from sealed discovery")
			}
		}
		if _, _, err := validateMigrationRewrites(migration, roots); err != nil {
			return nil, fmt.Errorf("migration %s: %w", migration.Key, err)
		}
		components = append(components, rekeyComponentInput{key: migration.Key, kind: "migration", migration: migration, rewrites: roots, moves: migrationInput.Moves})
		migrationFingerprints = append(migrationFingerprints, migrationAssemblyAuthority{Key: migration.Key, Fingerprint: migration.Fingerprint})
	}
	sort.Slice(components, func(i, j int) bool { return components[i].key < components[j].key })
	sort.Slice(migrationFingerprints, func(i, j int) bool { return migrationFingerprints[i].Key < migrationFingerprints[j].Key })
	if input.FieldDiscovery == nil {
		return nil, fmt.Errorf("complete identifier field discovery is required")
	}
	fieldDiscovery, err := input.FieldDiscovery.validatedSnapshotFor(input.FieldDiscovery.rewrites)
	if err != nil {
		return nil, err
	}
	rootInputs := make([]CollisionRepairInput, 0, len(components))
	for _, component := range components {
		rootInputs = append(rootInputs, CollisionRepairInput{CollisionKey: component.key, Rewrites: component.rewrites, Moves: component.moves})
	}
	expandedInputs, err := expandCollisionInputsWithDiscoveredRewrites(rootInputs, fieldDiscovery.Rewrites)
	if err != nil {
		return nil, err
	}
	expandedByKey := make(map[string]CollisionRepairInput, len(expandedInputs))
	for _, componentInput := range expandedInputs {
		expandedByKey[componentInput.CollisionKey] = componentInput
	}
	unionRewrites := make([]reference.IdentifierRewrite, 0)
	for _, component := range components {
		unionRewrites = append(unionRewrites, expandedByKey[component.key].Rewrites...)
	}
	if err := validateFieldDiscoveryMembership(fieldDiscovery, unionRewrites); err != nil {
		return nil, err
	}
	linkDiscovery, err := input.LinkDiscovery.validatedSnapshotFor(unionRewrites)
	if err != nil {
		return nil, err
	}
	if jsonKey(fieldDiscovery.SourcePreconditions) != jsonKey(linkDiscovery.SourcePreconditions) {
		return nil, fmt.Errorf("field and link discoveries do not share one complete source snapshot")
	}
	if fieldDiscovery.SchemaHash == "" || fieldDiscovery.SchemaHash != linkDiscovery.SchemaHash {
		return nil, fmt.Errorf("field and link discoveries do not share one schema hash")
	}
	sourcePreconditions := append([]SourcePrecondition(nil), fieldDiscovery.SourcePreconditions...)

	planFingerprint, err := rekeyAssemblyPlanFingerprint(plan, migrationFingerprints)
	if err != nil {
		return nil, err
	}
	assembly := &RepairAssembly{PlanFingerprint: planFingerprint, SchemaHash: fieldDiscovery.SchemaHash, SourcePreconditions: sourcePreconditions, Components: make([]CollisionRepairIntent, 0, len(components))}
	for _, componentInput := range components {
		expanded := expandedByKey[componentInput.key]
		var component CollisionRepairIntent
		switch componentInput.kind {
		case "collision":
			component, err = assembleCollisionRepair(*componentInput.collision, expanded, fieldDiscovery, linkDiscovery)
		case "migration":
			component, err = assembleMigrationRepair(componentInput.migration, expanded, fieldDiscovery, linkDiscovery)
		default:
			err = fmt.Errorf("unsupported rekey component kind %q", componentInput.kind)
		}
		if err != nil {
			return nil, fmt.Errorf("%s %s: %w", componentInput.kind, componentInput.key, err)
		}
		assembly.Components = append(assembly.Components, component)
	}
	if err := validateReviewDiagnosticsPreserved(assembly.Components, linkDiscovery); err != nil {
		return nil, err
	}
	unionIdenticalIntentMemberships(assembly.Components)
	if err := bindIntentSourceHashes(assembly); err != nil {
		return nil, err
	}
	if err := assembly.seal(); err != nil {
		return nil, err
	}
	return assembly, nil
}

type migrationAssemblyAuthority struct {
	Key         string `json:"key"`
	Fingerprint string `json:"fingerprint"`
}

func rekeyAssemblyPlanFingerprint(plan *Plan, migrations []migrationAssemblyAuthority) (string, error) {
	collisionFingerprint := ""
	if plan != nil {
		collisionFingerprint = plan.Fingerprint
	}
	payload, err := json.Marshal(struct {
		Collision  string                       `json:"collision,omitempty"`
		Migrations []migrationAssemblyAuthority `json:"migrations,omitempty"`
	}{Collision: collisionFingerprint, Migrations: migrations})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func assembleCollisionRepair(collision PlannedCollision, input CollisionRepairInput, fieldDiscovery *identifierFieldDiscoverySnapshot, linkDiscovery *identifierLinkDiscoverySnapshot) (CollisionRepairIntent, error) {
	rewrites, err := canonicalRepairRewriteSet(input.Rewrites)
	if err != nil {
		return CollisionRepairIntent{}, err
	}
	rootIndexes, err := validateCollisionRewrites(collision, rewrites)
	if err != nil {
		return CollisionRepairIntent{}, err
	}

	component := CollisionRepairIntent{MembershipKeys: singletonMembership(collision.Key)}
	for _, rewrite := range rewrites {
		component.Rewrites = append(component.Rewrites, IdentifierRepairIntent{MembershipKeys: singletonMembership(collision.Key), Rewrite: rewrite})
	}
	if err := appendMoveIntents(&component, rewrites, rootIndexes, input.Moves); err != nil {
		return CollisionRepairIntent{}, err
	}
	if err := appendFieldDiscovery(&component, rewrites, fieldDiscovery); err != nil {
		return CollisionRepairIntent{}, err
	}
	if err := appendLinkDiscovery(&component, rewrites, linkDiscovery); err != nil {
		return CollisionRepairIntent{}, err
	}
	if err := validateRequiredFieldObligations(component, rewrites, rootIndexes, fieldDiscovery.SynthesizedDerived); err != nil {
		return CollisionRepairIntent{}, err
	}

	sortCollisionIntents(&component)
	component.Blocked = hasBlockingDiagnostic(component.Diagnostics)
	checks := []RepairPostcheck{PostcheckIdentifiers, PostcheckOntology, PostcheckBrokenLinks}
	if rewritesNonBlockHeading(component.LinkEdits) {
		checks = append(checks, PostcheckFragileExternal)
	}
	for _, check := range checks {
		component.Postchecks = append(component.Postchecks, RepairPostcheckIntent{MembershipKeys: singletonMembership(collision.Key), Check: check})
	}
	return component, nil
}

func assembleMigrationRepair(plan *MigrationPlan, input CollisionRepairInput, fieldDiscovery *identifierFieldDiscoverySnapshot, linkDiscovery *identifierLinkDiscoverySnapshot) (CollisionRepairIntent, error) {
	rewrites, err := canonicalRepairRewriteSet(input.Rewrites)
	if err != nil {
		return CollisionRepairIntent{}, err
	}
	rootIndexes, noteRootIndexes, err := validateMigrationRewrites(plan, rewrites)
	if err != nil {
		return CollisionRepairIntent{}, err
	}

	component := CollisionRepairIntent{MembershipKeys: singletonMembership(plan.Key)}
	for _, rewrite := range rewrites {
		component.Rewrites = append(component.Rewrites, IdentifierRepairIntent{MembershipKeys: singletonMembership(plan.Key), Rewrite: rewrite})
	}
	if err := appendMoveIntents(&component, rewrites, noteRootIndexes, input.Moves); err != nil {
		return CollisionRepairIntent{}, err
	}
	if err := appendFieldDiscovery(&component, rewrites, fieldDiscovery); err != nil {
		return CollisionRepairIntent{}, err
	}
	if err := appendLinkDiscovery(&component, rewrites, linkDiscovery); err != nil {
		return CollisionRepairIntent{}, err
	}
	if err := validateRequiredFieldObligations(component, rewrites, rootIndexes, fieldDiscovery.SynthesizedDerived); err != nil {
		return CollisionRepairIntent{}, err
	}

	sortCollisionIntents(&component)
	component.Blocked = hasBlockingDiagnostic(component.Diagnostics)
	checks := []RepairPostcheck{PostcheckIdentifiers, PostcheckOntology, PostcheckBrokenLinks}
	if rewritesNonBlockHeading(component.LinkEdits) {
		checks = append(checks, PostcheckFragileExternal)
	}
	for _, check := range checks {
		component.Postchecks = append(component.Postchecks, RepairPostcheckIntent{MembershipKeys: singletonMembership(plan.Key), Check: check})
	}
	return component, nil
}

func validateMigrationRewrites(plan *MigrationPlan, rewrites []reference.IdentifierRewrite) (map[int]struct{}, map[int]struct{}, error) {
	if plan == nil {
		return nil, nil, fmt.Errorf("identifier migration plan is required")
	}
	roots := make(map[int]struct{}, len(plan.Rewrites))
	noteRoots := make(map[int]struct{}, len(plan.Rewrites))
	for _, planned := range plan.Rewrites {
		var matches []int
		for index, rewrite := range rewrites {
			if rewrite.DerivedFrom.IsZero() && rewriteMatchesNode(rewrite, planned.Node) {
				matches = append(matches, index)
			}
		}
		if len(matches) != 1 {
			return nil, nil, fmt.Errorf("migration member %s requires exactly one root rewrite, found %d", planned.Node.String(), len(matches))
		}
		index := matches[0]
		if _, duplicate := roots[index]; duplicate {
			return nil, nil, fmt.Errorf("root rewrite maps multiple migration members")
		}
		rewrite := rewrites[index]
		if rewrite.Mode != reference.IdentifierRewritePreferredRekey ||
			strings.TrimSpace(rewrite.OldIdentifier) != planned.OldIdentifier ||
			strings.TrimSpace(rewrite.NewIdentifier) != planned.NewIdentifier {
			return nil, nil, fmt.Errorf("migration member %s root rewrite does not match sealed mapping", planned.Node.String())
		}
		if strings.TrimSpace(rewrite.AliasesField) == "" {
			return nil, nil, fmt.Errorf("migration root rewrite aliases field is required")
		}
		if err := validateRepairRefTransition(rewrite); err != nil {
			return nil, nil, err
		}
		expectedKind := ontology.NodeKindNote
		if planned.Node.Fragment != "" {
			expectedKind = ontology.NodeKindEmbedded
		}
		if rewrite.OldRef.Kind != expectedKind || rewrite.NewRef.Kind != expectedKind {
			return nil, nil, fmt.Errorf("migration member %s root kind does not match canonical node", planned.Node.String())
		}
		if expectedKind == ontology.NodeKindEmbedded && rewrite.NewRef.NotePath != rewrite.OldRef.NotePath {
			return nil, nil, fmt.Errorf("embedded migration member %s must rekey within its containing note", planned.Node.String())
		}
		roots[index] = struct{}{}
		if expectedKind == ontology.NodeKindNote {
			noteRoots[index] = struct{}{}
		}
	}
	for index, rewrite := range rewrites {
		if rewrite.DerivedFrom.IsZero() {
			if _, ok := roots[index]; !ok {
				return nil, nil, fmt.Errorf("extraneous migration root rewrite for %s", rewrite.OldRef.String())
			}
		}
	}
	if err := validateDerivedRewrites(rewrites, roots); err != nil {
		return nil, nil, err
	}
	return roots, noteRoots, nil
}

func validateCollisionRewrites(collision PlannedCollision, rewrites []reference.IdentifierRewrite) (map[int]struct{}, error) {
	roots := make(map[int]struct{}, len(collision.Losers))
	for _, loser := range collision.Losers {
		var matches []int
		for index, rewrite := range rewrites {
			if rewrite.DerivedFrom.IsZero() && rewriteMatchesNode(rewrite, loser.Claim.Node) {
				matches = append(matches, index)
			}
		}
		if len(matches) != 1 {
			return nil, fmt.Errorf("loser %s requires exactly one root rewrite, found %d", loser.Claim.Node.String(), len(matches))
		}
		index := matches[0]
		if _, duplicate := roots[index]; duplicate {
			return nil, fmt.Errorf("root rewrite maps multiple losers")
		}
		if err := validateRootRewrite(loser, rewrites[index]); err != nil {
			return nil, err
		}
		roots[index] = struct{}{}
	}
	for index, rewrite := range rewrites {
		if rewrite.DerivedFrom.IsZero() {
			if _, ok := roots[index]; !ok {
				return nil, fmt.Errorf("extraneous root rewrite for %s", rewrite.OldRef.String())
			}
		}
	}
	if err := validateDerivedRewrites(rewrites, roots); err != nil {
		return nil, err
	}
	return roots, nil
}

func validateRootRewrite(loser PlannedLoser, rewrite reference.IdentifierRewrite) error {
	if IdentifierComparisonKey(rewrite.OldIdentifier) != IdentifierComparisonKey(loser.Claim.Value) {
		return fmt.Errorf("root rewrite old identifier does not match loser claim")
	}
	if err := validateRepairRefTransition(rewrite); err != nil {
		return err
	}
	switch loser.Claim.Kind {
	case ClaimPreferred:
		if rewrite.Mode != reference.IdentifierRewritePreferredRekey || strings.TrimSpace(rewrite.NewIdentifier) != loser.Replacement {
			return fmt.Errorf("preferred loser requires PREFERRED_REKEY to planned replacement %q", loser.Replacement)
		}
	case ClaimAlias:
		if rewrite.Mode != reference.IdentifierRewriteAliasRemoval {
			return fmt.Errorf("alias loser requires ALIAS_REMOVAL")
		}
		if !sameRepairRef(rewrite.OldRef, rewrite.NewRef) || strings.TrimSpace(rewrite.NewIdentifier) == "" || IdentifierComparisonKey(rewrite.NewIdentifier) == IdentifierComparisonKey(rewrite.OldIdentifier) {
			return fmt.Errorf("alias loser must retain its canonical ref and target the supplied current preferred identifier")
		}
	default:
		return fmt.Errorf("unsupported loser claim kind %q", loser.Claim.Kind)
	}
	if strings.TrimSpace(rewrite.AliasesField) == "" {
		return fmt.Errorf("root rewrite aliases field is required")
	}
	return nil
}

func validateDerivedRewrites(rewrites []reference.IdentifierRewrite, roots map[int]struct{}) error {
	parents := make(map[string]int)
	for index, rewrite := range rewrites {
		if rewrite.Mode != reference.IdentifierRewritePreferredRekey {
			continue
		}
		key := repairRefKey(rewrite.OldRef)
		if _, duplicate := parents[key]; duplicate {
			return fmt.Errorf("multiple preferred rewrites share old ref %s", rewrite.OldRef.String())
		}
		parents[key] = index
	}
	for index, rewrite := range rewrites {
		if rewrite.DerivedFrom.IsZero() {
			continue
		}
		if rewrite.Mode != reference.IdentifierRewritePreferredRekey {
			return fmt.Errorf("derived rewrite %s must use PREFERRED_REKEY", rewrite.OldRef.String())
		}
		seen := map[int]struct{}{index: {}}
		child := index
		for {
			parent, ok := parents[repairRefKey(rewrites[child].DerivedFrom)]
			if !ok || !validDerivedRepair(rewrites[parent], rewrites[child]) || !validDerivedRefTransition(rewrites[parent], rewrites[child]) {
				return fmt.Errorf("derived rewrite %s has invalid parent cascade", rewrites[child].OldRef.String())
			}
			if _, root := roots[parent]; root {
				break
			}
			if _, cycle := seen[parent]; cycle {
				return fmt.Errorf("derived rewrite cycle at %s", rewrites[parent].OldRef.String())
			}
			seen[parent] = struct{}{}
			child = parent
		}
	}
	return nil
}

func appendMoveIntents(component *CollisionRepairIntent, rewrites []reference.IdentifierRewrite, roots map[int]struct{}, moves []GovernedMoveResult) error {
	canonicalMoves := make([]GovernedMoveResult, len(moves))
	for index, move := range moves {
		plan, conflict, err := obsidian.PlanGovernedIdentifierMove(move.Request)
		if err != nil {
			return err
		}
		if plan != move.Plan || !sameMoveConflict(conflict, move.Conflict) {
			return fmt.Errorf("caller move result does not match rederived governed move: got %+v conflict=%+v, want %+v conflict=%+v", move.Plan, move.Conflict, plan, conflict)
		}
		canonicalMoves[index] = GovernedMoveResult{Request: move.Request, Plan: plan, Conflict: conflict}
	}
	moves = canonicalMoves
	used := make(map[int]struct{})
	rootIndexes := make([]int, 0, len(roots))
	for rootIndex := range roots {
		rootIndexes = append(rootIndexes, rootIndex)
	}
	sort.Ints(rootIndexes)
	for _, rootIndex := range rootIndexes {
		root := rewrites[rootIndex]
		if root.Mode == reference.IdentifierRewriteAliasRemoval {
			continue
		}
		var matches []int
		for index, move := range moves {
			if move.Plan.SourcePath == root.OldRef.NotePath {
				matches = append(matches, index)
			}
		}
		if len(matches) != 1 {
			return fmt.Errorf("preferred root %s requires exactly one governed move plan", root.OldRef.String())
		}
		index := matches[0]
		used[index] = struct{}{}
		move := moves[index]
		if move.Request.SourcePath != root.OldRef.NotePath || IdentifierComparisonKey(move.Request.OldID) != IdentifierComparisonKey(root.OldIdentifier) || strings.TrimSpace(move.Request.NewID) != root.NewIdentifier {
			return fmt.Errorf("governed move request does not match preferred root rewrite")
		}
		if move.Plan.DestinationPath != root.NewRef.NotePath || move.Plan.Renamed != (move.Plan.SourcePath != move.Plan.DestinationPath) {
			return fmt.Errorf("governed move does not match root canonical refs")
		}
		if move.Plan.Renamed {
			component.Moves = append(component.Moves, MoveRepairIntent{
				MembershipKeys: append([]string(nil), component.MembershipKeys...), Move: move.Plan,
				DestinationVacancy: MoveDestinationVacancyPrecondition{
					DestinationPath: move.Plan.DestinationPath, RequireExactVacancy: true, RequirePortableCaseFoldVacancy: true,
				},
			})
		}
		if move.Conflict != nil {
			conflict := *move.Conflict
			if conflict.DestinationPath != move.Plan.DestinationPath {
				return fmt.Errorf("move conflict destination does not match governed move")
			}
			component.Diagnostics = append(component.Diagnostics, RepairDiagnostic{MembershipKeys: append([]string(nil), component.MembershipKeys...), Kind: "MOVE_CONFLICT", Blocking: true, MoveConflict: &conflict})
		}
	}
	if len(used) != len(moves) {
		return fmt.Errorf("extraneous governed move plan")
	}
	return nil
}

func appendFieldDiscovery(component *CollisionRepairIntent, rewrites []reference.IdentifierRewrite, discovery *identifierFieldDiscoverySnapshot) error {
	for _, edit := range discovery.Edits {
		rewrite, ok := fieldEditRewrite(edit, rewrites)
		if !ok {
			continue
		}
		intent := FieldRepairIntent{MembershipKeys: append([]string(nil), component.MembershipKeys...), Edit: edit}
		if edit.Kind == reference.StructuredFieldAliasIdentifier {
			if edit.Operation != reference.StructuredFieldEditAppend && edit.Operation != reference.StructuredFieldEditRemove {
				return fmt.Errorf("semantic alias edit must append or remove")
			}
			if edit.Operation == reference.StructuredFieldEditRemove && IdentifierComparisonKey(edit.Expected) != IdentifierComparisonKey(rewrite.OldIdentifier) {
				return fmt.Errorf("alias removal does not match semantic rewrite membership")
			}
			if edit.Operation == reference.StructuredFieldEditAppend && IdentifierComparisonKey(edit.Replacement) != IdentifierComparisonKey(rewrite.NewIdentifier) {
				return fmt.Errorf("alias append does not match semantic rewrite membership")
			}
			component.AliasEdits = append(component.AliasEdits, intent)
		} else {
			component.FieldEdits = append(component.FieldEdits, intent)
		}
	}
	for _, raw := range discovery.Diagnostics {
		if !fieldDiagnosticMatchesRewrites(raw, rewrites) {
			continue
		}
		diagnostic := raw
		blocking := diagnostic.Kind != reference.IdentifierRewriteDiagnosticReviewOnly
		component.Diagnostics = append(component.Diagnostics, RepairDiagnostic{MembershipKeys: append([]string(nil), component.MembershipKeys...), Kind: string(diagnostic.Kind), Blocking: blocking, Field: &diagnostic})
	}
	return nil
}

func sortCollisionIntents(component *CollisionRepairIntent) {
	sort.Slice(component.FieldEdits, func(i, j int) bool {
		return jsonKey(component.FieldEdits[i].Edit) < jsonKey(component.FieldEdits[j].Edit)
	})
	sort.Slice(component.AliasEdits, func(i, j int) bool {
		return jsonKey(component.AliasEdits[i].Edit) < jsonKey(component.AliasEdits[j].Edit)
	})
	sort.Slice(component.LinkEdits, func(i, j int) bool {
		return jsonKey(component.LinkEdits[i].Edit) < jsonKey(component.LinkEdits[j].Edit)
	})
	sort.Slice(component.Moves, func(i, j int) bool { return jsonKey(component.Moves[i].Move) < jsonKey(component.Moves[j].Move) })
	sort.Slice(component.Diagnostics, func(i, j int) bool { return jsonKey(component.Diagnostics[i]) < jsonKey(component.Diagnostics[j]) })
	component.FieldEdits = dedupeFieldIntents(component.FieldEdits)
	component.AliasEdits = dedupeFieldIntents(component.AliasEdits)
	component.LinkEdits = dedupeLinkIntents(component.LinkEdits)
}

func rewritesNonBlockHeading(edits []LinkRepairIntent) bool {
	for _, intent := range edits {
		edit := intent.Edit
		if edit.Component == reference.LinkComponentFragment && edit.Expected != edit.Replacement && (!strings.HasPrefix(edit.Expected, "^") || !strings.HasPrefix(edit.Replacement, "^")) {
			return true
		}
	}
	return false
}

func hasBlockingDiagnostic(input []RepairDiagnostic) bool {
	for _, diagnostic := range input {
		if diagnostic.Blocking {
			return true
		}
	}
	return false
}

func jsonKey(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
