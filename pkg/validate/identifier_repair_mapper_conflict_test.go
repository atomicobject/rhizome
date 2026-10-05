package validate

import (
	"context"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/validate/identifierreconcile"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestIdentifierRepairMapperIsolatesOntologyPreviewConflict(t *testing.T) {
	fixture := buildIdentifierRepairAdapterFixture(t)
	snapshot, err := fixture.assembly.ValidatedSnapshot()
	require.NoError(t, err)
	files, _, _ := collectIdentifierRepairWork(snapshot)

	conflicted := files[fixture.oldPath]
	conflicted.links = map[string]identifierreconcile.LinkRepairIntent{}
	valid := files["notes/inbound.md"]
	validMembership := "collision:independent"
	valid.memberships = map[string]struct{}{validMembership: {}}

	schema, err := ontology.LoadSchema(fixture.root)
	require.NoError(t, err)
	sourceHashes := make(map[string]string, len(snapshot.SourcePreconditions))
	for _, source := range snapshot.SourcePreconditions {
		sourceHashes[source.NotePath] = source.SourceHash
	}
	conflictedAction := fixture.bindings[0].Action
	validAction := conflictedAction
	validAction.ID = "action:independent"
	validAction.IssueKeys = []string{"issue:independent"}
	authority := identifierBindingSet{
		actions: []FixAction{conflictedAction, validAction},
		actionIDsByMembership: map[string][]string{
			fixture.membership: {conflictedAction.ID},
			validMembership:    {validAction.ID},
		},
		issueKeysByMembership: map[string][]string{
			fixture.membership: {conflictedAction.IssueKeys[0]},
			validMembership:    {validAction.IssueKeys[0]},
		},
		requiredChecksByMembership: map[string][]string{
			fixture.membership: {CheckAliases, CheckOntology},
			validMembership:    {CheckAliases, CheckBrokenLinks},
		},
	}
	previewer := identifierConflictPreviewer{}
	conflictedOperation, material, err := mapIdentifierFileWork(
		context.Background(), fixture.runCtx, schema, sourceHashes, conflicted, authority, previewer,
	)
	require.NoError(t, err)
	require.True(t, material, "a semantic conflict with no material preview still needs a placeholder")
	require.Len(t, conflictedOperation.PlanningConflicts, 1)
	require.Equal(t, RepairConflictOntology, conflictedOperation.PlanningConflicts[0].Kind)

	validOperation, material, err := mapIdentifierFileWork(
		context.Background(), fixture.runCtx, schema, sourceHashes, valid, authority, previewer,
	)
	require.NoError(t, err)
	require.True(t, material)
	require.Empty(t, validOperation.PlanningConflicts)

	plan, err := FinalizeRepairPlan(RepairPlan{
		TotalCount: 2,
		SafeCount:  2,
		Actions:    authority.actions,
		Operations: []RepairOperation{conflictedOperation, validOperation},
	})
	require.NoError(t, err)
	require.Len(t, plan.Transactions, 2)
	conflictedTransactions := 0
	executableTransactions := 0
	for _, transaction := range plan.Transactions {
		if len(transaction.Conflicts) > 0 {
			conflictedTransactions++
			require.Equal(t, []string{conflictedOperation.ID}, transaction.OperationIDs)
			continue
		}
		executableTransactions++
		require.Equal(t, []string{validOperation.ID}, transaction.OperationIDs)
	}
	require.Equal(t, 1, conflictedTransactions)
	require.Equal(t, 1, executableTransactions)
}

func TestIdentifierRepairMapperDeduplicatesTypedFieldAndLinkTransition(t *testing.T) {
	root := t.TempDir()
	writeIdentifierAdapterFile(t, root, ".rhizome/ontology/identifiers.graphql", `
type Spec @node(paths: ["specs/*.md"]) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC", separator: "-", pad: 4)
}

type Consumer @node(paths: ["notes/*.md"]) {
  spec: Spec @link
}
`)
	writeIdentifierAdapterFile(t, root, "specs/one.md", "---\nid: SPEC-0001\n---\n")
	writeIdentifierAdapterFile(t, root, "specs/two.md", "---\nid: SPEC-0002\n---\n")
	const notePath = "notes/inbound.md"
	const original = "---\nspec: \"[[SPEC-0001]]\"\n---\n"
	writeIdentifierAdapterFile(t, root, notePath, original)

	fieldStart := strings.Index(original, "[[SPEC-0001]]")
	linkStart := strings.Index(original, "SPEC-0001")
	oldRef := ontology.NodeRef{NotePath: "specs/one.md", TypeName: "Spec", Kind: ontology.NodeKindNote}
	newRef := ontology.NodeRef{NotePath: "specs/two.md", TypeName: "Spec", Kind: ontology.NodeKindNote}
	fieldEdit := reference.StructuredFieldEdit{
		OwnerRef:  ontology.NodeRef{NotePath: notePath, TypeName: "Consumer", Kind: ontology.NodeKindNote},
		FieldName: "spec", Kind: reference.StructuredFieldTypedIdentifierReference,
		Operation: reference.StructuredFieldEditReplace,
		Range:     ontology.ByteRange{Start: fieldStart, End: fieldStart + len("[[SPEC-0001]]")},
		Expected:  "[[SPEC-0001]]", Replacement: "[[SPEC-0002]]",
		TargetOldRef: oldRef, TargetNewRef: newRef,
	}
	linkEdit := reference.StructuredLinkEdit{
		NotePath: notePath, LinkIndex: 0, Component: reference.LinkComponentPath,
		Range:    ontology.ByteRange{Start: linkStart, End: linkStart + len("SPEC-0001")},
		Expected: "SPEC-0001", Replacement: "SPEC-0002",
		TargetOldRef: oldRef, TargetNewRef: newRef,
	}
	const membership = "collision:typed-relation"
	work := &identifierFileWork{
		path: notePath,
		fields: map[string]identifierreconcile.FieldRepairIntent{
			"field": {MembershipKeys: []string{membership}, Edit: fieldEdit},
		},
		links: map[string]identifierreconcile.LinkRepairIntent{
			"link": {MembershipKeys: []string{membership}, Edit: linkEdit},
		},
		memberships: map[string]struct{}{membership: {}},
	}
	authority := identifierBindingSet{
		actions:                    []FixAction{{ID: "action:typed-relation", Check: CheckAliases, Safety: FixSafetySafe, IssueKeys: []string{"issue:typed-relation"}}},
		actionIDsByMembership:      map[string][]string{membership: {"action:typed-relation"}},
		issueKeysByMembership:      map[string][]string{membership: {"issue:typed-relation"}},
		requiredChecksByMembership: map[string][]string{membership: {CheckAliases, CheckOntology}},
	}
	vaultDef := obsidian.VaultDefinition{Path: root}
	runCtx := RunContext{VaultDef: vaultDef, VaultPath: root, NoteReader: &obsidian.Note{}}
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	operation, material, err := mapIdentifierFileWork(
		context.Background(), runCtx, schema,
		map[string]string{notePath: strings.TrimPrefix(SourceHash([]byte(original)), "sha256:")},
		work, authority, NewOntologyOperationAdapter(),
	)
	require.NoError(t, err)
	require.True(t, material)
	require.Equal(t, "---\nspec: \"[[SPEC-0002]]\"\n---\n", string(operation.Content))
	require.Empty(t, operation.PlanningConflicts)
}

func TestIdentifierRepairMapperComposesLocatorAndLinkEditsByOriginalRange(t *testing.T) {
	root := t.TempDir()
	writeIdentifierAdapterFile(t, root, ".rhizome/ontology/identifiers.graphql", `
type Inbound @node(paths: ["notes/*.md"]) {
  title: String! @field
}
`)
	const notePath = "notes/inbound.md"
	const original = "- criterion ^A\n\n[[OLD]]\n"
	writeIdentifierAdapterFile(t, root, notePath, original)

	locatorStart := strings.Index(original, "^A")
	linkStart := strings.Index(original, "OLD")
	oldRef := ontology.NodeRef{NotePath: "notes/old.md", TypeName: "Inbound", Kind: ontology.NodeKindNote}
	newRef := ontology.NodeRef{NotePath: "notes/new.md", TypeName: "Inbound", Kind: ontology.NodeKindNote}
	const membership = "collision:length-changing-raw-edits"
	work := &identifierFileWork{
		path: notePath,
		fields: map[string]identifierreconcile.FieldRepairIntent{
			"locator": {MembershipKeys: []string{membership}, Edit: reference.StructuredFieldEdit{
				OwnerRef:  ontology.NodeRef{NotePath: notePath, Fragment: "^A", TypeName: "Note", Kind: ontology.NodeKindEmbedded},
				FieldName: reference.StructuredFieldBlockLocator, Kind: reference.StructuredFieldIdentifierBackedLocator,
				Operation: reference.StructuredFieldEditReplace,
				Range:     ontology.ByteRange{Start: locatorStart, End: locatorStart + len("^A")},
				Expected:  "^A", Replacement: "^LONGER-BLOCK-LOCATOR",
			}},
		},
		links: map[string]identifierreconcile.LinkRepairIntent{
			"link": {MembershipKeys: []string{membership}, Edit: reference.StructuredLinkEdit{
				NotePath: notePath, LinkIndex: 0, Component: reference.LinkComponentPath,
				Range:    ontology.ByteRange{Start: linkStart, End: linkStart + len("OLD")},
				Expected: "OLD", Replacement: "A-MUCH-LONGER-TARGET", TargetOldRef: oldRef, TargetNewRef: newRef,
			}},
		},
		memberships: map[string]struct{}{membership: {}},
	}
	authority := identifierBindingSet{
		actions:                    []FixAction{{ID: "action:length-changing-raw-edits", Check: CheckAliases, Safety: FixSafetySafe, IssueKeys: []string{"issue:length-changing-raw-edits"}}},
		actionIDsByMembership:      map[string][]string{membership: {"action:length-changing-raw-edits"}},
		issueKeysByMembership:      map[string][]string{membership: {"issue:length-changing-raw-edits"}},
		requiredChecksByMembership: map[string][]string{membership: {CheckAliases, CheckBrokenLinks}},
	}
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	runCtx := RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}}
	operation, material, err := mapIdentifierFileWork(
		context.Background(), runCtx, schema,
		map[string]string{notePath: strings.TrimPrefix(SourceHash([]byte(original)), "sha256:")},
		work, authority, NewOntologyOperationAdapter(),
	)
	require.NoError(t, err)
	require.True(t, material)
	require.Equal(t, "- criterion ^LONGER-BLOCK-LOCATOR\n\n[[A-MUCH-LONGER-TARGET]]\n", string(operation.Content))
	require.Len(t, operation.LifecycleClaims, 1, "only the authored link edit is a broken-link lifecycle claim")

	mapWork := func(candidate *identifierFileWork) error {
		_, _, err := mapIdentifierFileWork(
			context.Background(), runCtx, schema,
			map[string]string{notePath: strings.TrimPrefix(SourceHash([]byte(original)), "sha256:")},
			candidate, authority, NewOntologyOperationAdapter(),
		)
		return err
	}
	t.Run("rejects stale locator range", func(t *testing.T) {
		candidate := *work
		candidate.fields = map[string]identifierreconcile.FieldRepairIntent{}
		for key, intent := range work.fields {
			intent.Edit.Range.Start--
			candidate.fields[key] = intent
		}
		require.ErrorContains(t, mapWork(&candidate), "identifier block-locator edit in notes/inbound.md is stale")
	})
	t.Run("rejects stale link range", func(t *testing.T) {
		candidate := *work
		candidate.links = map[string]identifierreconcile.LinkRepairIntent{}
		for key, intent := range work.links {
			intent.Edit.Range.Start--
			candidate.links[key] = intent
		}
		require.ErrorContains(t, mapWork(&candidate), "identifier link edit in notes/inbound.md is stale")
	})
	t.Run("rejects overlapping locator and link ranges", func(t *testing.T) {
		candidate := *work
		candidate.fields = map[string]identifierreconcile.FieldRepairIntent{}
		for key, intent := range work.fields {
			intent.Edit.Range = ontology.ByteRange{Start: linkStart, End: linkStart + 1}
			intent.Edit.Expected = "O"
			candidate.fields[key] = intent
		}
		require.ErrorContains(t, mapWork(&candidate), "identifier raw edits in notes/inbound.md overlap")
	})
}

type identifierConflictPreviewer struct{}

func (identifierConflictPreviewer) Preview(
	_ context.Context,
	_ RunContext,
	_ *ontology.Schema,
	request OntologyPreviewRequest,
) (OntologyPreviewResult, error) {
	return OntologyPreviewResult{Conflicts: []OntologyPreviewConflict{{
		Kind:     ontology.ConflictKindMissingField,
		NotePath: request.Edits[0].Ref.NotePath,
		Message:  "identifier field is no longer supported",
	}}}, nil
}
