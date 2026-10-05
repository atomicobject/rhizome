package validate

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/validate/identifierreconcile"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type identifierFileWork struct {
	path        string
	fields      map[string]identifierreconcile.FieldRepairIntent
	links       map[string]identifierreconcile.LinkRepairIntent
	memberships map[string]struct{}
}

type identifierMoveWork struct {
	intent      identifierreconcile.MoveRepairIntent
	memberships map[string]struct{}
}

func buildIdentifierRepairPlan(ctx context.Context, runCtx RunContext, assembly *identifierreconcile.RepairAssembly, bindings []IdentifierRepairActionBinding) (*RepairPlan, error) {
	snapshot, err := assembly.ValidatedSnapshot()
	if err != nil {
		return nil, err
	}
	authority, err := bindIdentifierRepairActions(snapshot, bindings)
	if err != nil {
		return nil, err
	}
	schema, err := ontology.LoadSchema(runCtx.VaultDef.BasePath())
	if err != nil {
		return nil, err
	}
	if schema == nil || schema.Hash != snapshot.SchemaHash {
		return nil, fmt.Errorf("identifier repair schema changed after planning")
	}
	sourceHashes := make(map[string]string, len(snapshot.SourcePreconditions))
	for _, source := range snapshot.SourcePreconditions {
		sourceHashes[source.NotePath] = source.SourceHash
	}
	files, moves, blocked := collectIdentifierRepairWork(snapshot)
	followUps, err := collectIdentifierRepairFollowUps(snapshot)
	if err != nil {
		return nil, err
	}
	var operations []RepairOperation
	filePaths := sortedIdentifierWorkPaths(files)
	previewer := NewOntologyOperationAdapter()
	for _, notePath := range filePaths {
		operation, material, err := mapIdentifierFileWork(ctx, runCtx, schema, sourceHashes, files[notePath], authority, previewer)
		if err != nil {
			return nil, err
		}
		if material {
			attachIdentifierBlockedConflicts(&operation, files[notePath].memberships, blocked)
			operations = append(operations, operation)
		}
	}
	moveKeys := make([]string, 0, len(moves))
	for key := range moves {
		moveKeys = append(moveKeys, key)
	}
	sort.Strings(moveKeys)
	for _, key := range moveKeys {
		operation, err := mapIdentifierMoveWork(runCtx, sourceHashes, moves[key], authority)
		if err != nil {
			return nil, err
		}
		attachIdentifierBlockedConflicts(&operation, moves[key].memberships, blocked)
		operations = append(operations, operation)
	}
	plan := RepairPlan{
		AuthorityFingerprint:    snapshot.Fingerprint,
		RequiresLeaseHeldReplan: true,
		Actions:                 authority.actions, Operations: operations, FollowUps: followUps,
	}
	for _, action := range plan.Actions {
		plan.IssueKeys = append(plan.IssueKeys, action.IssueKeys...)
		plan.TotalCount++
		switch action.Safety {
		case FixSafetySafe:
			plan.SafeCount++
		case FixSafetyConfirm:
			plan.ConfirmationCount++
		case FixSafetyAgent:
			plan.AgentCount++
		}
	}
	finalized, err := FinalizeRepairPlan(plan)
	if err != nil {
		return nil, err
	}
	return &finalized, nil
}

func collectIdentifierRepairWork(snapshot *identifierreconcile.RepairAssembly) (map[string]*identifierFileWork, map[string]*identifierMoveWork, map[string][]string) {
	files := make(map[string]*identifierFileWork)
	moves := make(map[string]*identifierMoveWork)
	blocked := make(map[string][]string)
	fileFor := func(notePath string) *identifierFileWork {
		work := files[notePath]
		if work == nil {
			work = &identifierFileWork{path: notePath, fields: make(map[string]identifierreconcile.FieldRepairIntent), links: make(map[string]identifierreconcile.LinkRepairIntent), memberships: make(map[string]struct{})}
			files[notePath] = work
		}
		return work
	}
	for _, component := range snapshot.Components {
		for _, diagnostic := range component.Diagnostics {
			if !diagnostic.Blocking {
				continue
			}
			for _, membership := range diagnostic.MembershipKeys {
				blocked[membership] = append(blocked[membership], identifierDiagnosticMessage(diagnostic))
			}
		}
		for _, intent := range append(append([]identifierreconcile.FieldRepairIntent(nil), component.FieldEdits...), component.AliasEdits...) {
			work := fileFor(intent.Edit.OwnerRef.NotePath)
			mergeIdentifierMemberships(work.memberships, intent.MembershipKeys)
			work.fields[identifierJSONKey(intent.Edit)] = intent
		}
		for _, intent := range component.LinkEdits {
			work := fileFor(intent.Edit.NotePath)
			mergeIdentifierMemberships(work.memberships, intent.MembershipKeys)
			work.links[identifierJSONKey(intent.Edit)] = intent
		}
		for _, intent := range component.Moves {
			key := identifierJSONKey(intent.Move)
			work := moves[key]
			if work == nil {
				work = &identifierMoveWork{intent: intent, memberships: make(map[string]struct{})}
				moves[key] = work
			}
			mergeIdentifierMemberships(work.memberships, intent.MembershipKeys)
		}
	}
	for key := range blocked {
		blocked[key] = sortedUnique(blocked[key])
	}
	return files, moves, blocked
}

func mapIdentifierFileWork(
	ctx context.Context,
	runCtx RunContext,
	schema *ontology.Schema,
	sourceHashes map[string]string,
	work *identifierFileWork,
	authority identifierBindingSet,
	previewer OntologyOperationPreviewer,
) (RepairOperation, bool, error) {
	original, err := readAuthoritativeIdentifierSource(runCtx, sourceHashes, work.path)
	if err != nil {
		return RepairOperation{}, false, err
	}
	links := make([]identifierreconcile.LinkRepairIntent, 0, len(work.links))
	for _, intent := range work.links {
		links = append(links, intent)
	}
	semanticFields := make(map[string]identifierreconcile.FieldRepairIntent, len(work.fields))
	type rawIdentifierEdit struct {
		range_      ontology.ByteRange
		expected    string
		replacement string
		kind        string
		linkClaim   bool
	}
	rawEdits := make([]rawIdentifierEdit, 0, len(work.links)+len(work.fields))
	for key, intent := range work.fields {
		if intent.Edit.Kind == reference.StructuredFieldIdentifierBackedLocator && intent.Edit.FieldName == reference.StructuredFieldBlockLocator {
			rawEdits = append(rawEdits, rawIdentifierEdit{
				range_: intent.Edit.Range, expected: intent.Edit.Expected,
				replacement: intent.Edit.Replacement, kind: "block-locator",
			})
			continue
		}
		semanticFields[key] = intent
	}
	for _, intent := range links {
		rawEdits = append(rawEdits, rawIdentifierEdit{
			range_: intent.Edit.Range, expected: intent.Edit.Expected,
			replacement: intent.Edit.Replacement, kind: "link", linkClaim: true,
		})
	}
	sort.Slice(rawEdits, func(i, j int) bool {
		if rawEdits[i].range_.Start != rawEdits[j].range_.Start {
			return rawEdits[i].range_.Start > rawEdits[j].range_.Start
		}
		if rawEdits[i].range_.End != rawEdits[j].range_.End {
			return rawEdits[i].range_.End > rawEdits[j].range_.End
		}
		return rawEdits[i].kind < rawEdits[j].kind
	})
	nextStart := len(original)
	for _, edit := range rawEdits {
		if !edit.range_.Valid(len(original)) || string(original[edit.range_.Start:edit.range_.End]) != edit.expected {
			return RepairOperation{}, false, fmt.Errorf("identifier %s edit in %s is stale", edit.kind, work.path)
		}
		if edit.range_.End > nextStart {
			return RepairOperation{}, false, fmt.Errorf("identifier raw edits in %s overlap", work.path)
		}
		nextStart = edit.range_.Start
	}
	overlay := original
	claims := make([]LifecycleClaim, 0, len(links))
	for _, edit := range rawEdits {
		overlay = append(append(append([]byte(nil), overlay[:edit.range_.Start]...), []byte(edit.replacement)...), overlay[edit.range_.End:]...)
		if edit.linkClaim {
			claims = append(claims, LifecycleClaim{Kind: LifecycleEditBrokenLink, StartByte: edit.range_.Start, EndByte: edit.range_.End, ExpectedText: edit.expected, Replacement: edit.replacement})
		}
	}
	memberships := identifierMembershipSlice(work.memberships)
	actionIDs, issueKeys, requiredChecks, err := identifierAuthorityForMemberships(authority, memberships)
	if err != nil {
		return RepairOperation{}, false, err
	}
	operationID := repairOperationID(strings.Join(actionIDs, "\x00"), work.path, "identifier-write")
	finalContent := overlay
	if len(semanticFields) > 0 {
		edits, err := identifierOntologyEdits(ctx, runCtx, schema, original, overlay, semanticFields, links)
		if err != nil {
			return RepairOperation{}, false, err
		}
		if len(edits) == 0 {
			if string(finalContent) == string(original) {
				return RepairOperation{}, false, nil
			}
			return newIdentifierFileOperation(
				operationID, work.path, original, finalContent, claims,
				actionIDs, issueKeys, requiredChecks, memberships,
			), true, nil
		}
		primaryEdits, aliasEdits := partitionIdentifierOntologyEdits(edits, semanticFields)
		previewContent := overlay
		var previewConflicts []OntologyPreviewConflict
		for index, phase := range [][]OntologyEdit{primaryEdits, aliasEdits} {
			if index == 1 {
				phase, err = remapIdentifierAliasOwners(phase, primaryEdits, semanticFields)
				if err != nil {
					return RepairOperation{}, false, err
				}
			}
			if len(phase) == 0 {
				continue
			}
			previewContent, previewConflicts, err = previewIdentifierOntologyPhase(
				ctx, runCtx, schema, work.path, operationID, actionIDs, issueKeys,
				previewContent, phase, previewer,
			)
			if err != nil {
				return RepairOperation{}, false, err
			}
			if len(previewConflicts) > 0 {
				break
			}
		}
		if len(previewConflicts) > 0 {
			operation := newIdentifierFileOperation(
				operationID, work.path, original, overlay, claims,
				actionIDs, issueKeys, requiredChecks, memberships,
			)
			for _, conflict := range previewConflicts {
				conflictPath := strings.TrimSpace(conflict.NotePath)
				if conflictPath == "" {
					conflictPath = work.path
				}
				operation.PlanningConflicts = append(operation.PlanningConflicts, RepairConflict{
					Kind: RepairConflictOntology, Path: conflictPath,
					OperationIDs: []string{operationID}, Message: conflict.Message,
				})
			}
			return operation, true, nil
		}
		finalContent = previewContent
	}
	if string(finalContent) == string(original) {
		return RepairOperation{}, false, nil
	}
	return newIdentifierFileOperation(
		operationID, work.path, original, finalContent, claims,
		actionIDs, issueKeys, requiredChecks, memberships,
	), true, nil
}

func partitionIdentifierOntologyEdits(edits []OntologyEdit, fields map[string]identifierreconcile.FieldRepairIntent) ([]OntologyEdit, []OntologyEdit) {
	aliasFields := make(map[string]struct{})
	for _, intent := range fields {
		if intent.Edit.Kind != reference.StructuredFieldAliasIdentifier {
			continue
		}
		aliasFields[identifierOntologyFieldKey(intent.Edit.OwnerRef, intent.Edit.FieldName)] = struct{}{}
	}
	primary := make([]OntologyEdit, 0, len(edits))
	aliases := make([]OntologyEdit, 0, len(edits))
	for _, edit := range edits {
		key := identifierOntologyFieldKey(edit.Ref, edit.Field)
		if _, alias := aliasFields[key]; alias {
			aliases = append(aliases, edit)
		} else {
			primary = append(primary, edit)
		}
	}
	return primary, aliases
}

func previewIdentifierOntologyPhase(
	ctx context.Context,
	runCtx RunContext,
	schema *ontology.Schema,
	notePath, operationID string,
	actionIDs, issueKeys []string,
	content []byte,
	edits []OntologyEdit,
	previewer OntologyOperationPreviewer,
) ([]byte, []OntologyPreviewConflict, error) {
	overlayCtx := runCtx
	overlayCtx.NoteReader = &identifierOverlayReader{
		delegate: runCtx.NoteReader,
		content:  map[string]string{notePath: string(content)},
	}
	result, err := previewer.Preview(ctx, overlayCtx, schema, OntologyPreviewRequest{
		OperationID:           operationID,
		ActionIDs:             actionIDs,
		IssueKeys:             issueKeys,
		Edits:                 edits,
		AllowSelectorRecovery: true,
	})
	if err != nil || len(result.Conflicts) > 0 {
		return content, result.Conflicts, err
	}
	if len(result.Operations) != 1 {
		return nil, nil, fmt.Errorf("identifier ontology preview for %s produced %d writes", notePath, len(result.Operations))
	}
	return result.Operations[0].Content, nil, nil
}

func newIdentifierFileOperation(
	operationID, notePath string,
	original, finalContent []byte,
	claims []LifecycleClaim,
	actionIDs, issueKeys, requiredChecks, memberships []string,
) RepairOperation {
	sort.Slice(claims, func(i, j int) bool { return claims[i].StartByte < claims[j].StartByte })
	return RepairOperation{
		ID:       operationID,
		ActionID: actionIDs[0], ActionIDs: actionIDs, IssueKey: issueKeys[0], IssueKeys: issueKeys,
		RequiredChecks: requiredChecks,
		Kind:           RepairOperationWrite, Path: notePath, SourceHash: SourceHash(original),
		Expected: []ExpectedText{{StartByte: 0, EndByte: len(original), Text: string(original), Replacement: string(finalContent)}},
		Content:  append([]byte(nil), finalContent...), LifecycleClaims: append([]LifecycleClaim(nil), claims...),
		Lifecycle:  ClassifyLifecycleEdit(LifecycleEdit{Before: original, After: finalContent, Claims: claims}),
		Identities: identifierMembershipIdentities(memberships),
	}
}

func mapIdentifierMoveWork(runCtx RunContext, sourceHashes map[string]string, work *identifierMoveWork, authority identifierBindingSet) (RepairOperation, error) {
	move := work.intent.Move
	original, err := readAuthoritativeIdentifierSource(runCtx, sourceHashes, move.SourcePath)
	if err != nil {
		return RepairOperation{}, err
	}
	memberships := identifierMembershipSlice(work.memberships)
	actionIDs, issueKeys, requiredChecks, err := identifierAuthorityForMemberships(authority, memberships)
	if err != nil {
		return RepairOperation{}, err
	}
	operation := RepairOperation{
		ID:       repairOperationID(strings.Join(actionIDs, "\x00"), move.SourcePath+"\x00"+move.DestinationPath, "identifier-rename"),
		ActionID: actionIDs[0], ActionIDs: actionIDs, IssueKey: issueKeys[0], IssueKeys: issueKeys,
		RequiredChecks: requiredChecks,
		Kind:           RepairOperationRename, Path: move.SourcePath, DestinationPath: move.DestinationPath,
		SourceHash: SourceHash(original), Identities: identifierMembershipIdentities(memberships),
		Lifecycle: ClassifyLifecycleEdit(LifecycleEdit{Before: original, After: original}),
		DestinationVacancy: &RepairDestinationVacancyPrecondition{
			DestinationPath:                work.intent.DestinationVacancy.DestinationPath,
			RequireExactVacancy:            work.intent.DestinationVacancy.RequireExactVacancy,
			RequirePortableCaseFoldVacancy: work.intent.DestinationVacancy.RequirePortableCaseFoldVacancy,
		},
	}
	return operation, nil
}

type identifierOverlayReader struct {
	delegate obsidian.NoteReader
	content  map[string]string
}

func (r *identifierOverlayReader) GetContents(v obsidian.VaultDefinition, notePath string) (string, error) {
	if content, ok := r.content[notePath]; ok {
		return content, nil
	}
	return r.delegate.GetContents(v, notePath)
}
func (r *identifierOverlayReader) GetNotesList(v obsidian.VaultDefinition) ([]string, error) {
	return r.delegate.GetNotesList(v)
}
func (r *identifierOverlayReader) GetModTime(v obsidian.VaultDefinition, notePath string) (time.Time, error) {
	return r.delegate.GetModTime(v, notePath)
}
func (r *identifierOverlayReader) Title(notePath string) (string, bool) {
	return r.delegate.Title(notePath)
}

func readAuthoritativeIdentifierSource(runCtx RunContext, sourceHashes map[string]string, notePath string) ([]byte, error) {
	content, err := runCtx.NoteReader.GetContents(runCtx.VaultDef, notePath)
	if err != nil {
		return nil, err
	}
	hash := SourceHash([]byte(content))
	if strings.TrimPrefix(hash, "sha256:") != sourceHashes[notePath] {
		return nil, fmt.Errorf("identifier repair source %s changed after planning", notePath)
	}
	return []byte(content), nil
}

func identifierProjectionField(projection *ontology.NodeProjection, name string) *ontology.Field {
	if projection == nil || projection.Type == nil {
		return nil
	}
	if projection.Type.ByName != nil {
		return projection.Type.ByName[name]
	}
	for _, field := range projection.Type.Fields {
		if field != nil && field.Name == name {
			return field
		}
	}
	return nil
}

func indexIdentifierFieldValue(field *ontology.Field, values []string, expected string) int {
	for index, value := range values {
		if value == expected {
			return index
		}
	}
	if field == nil || !field.IsIdentifier {
		return -1
	}
	expected = ontology.NormalizeIdentifierSemanticValue(expected)
	for index, value := range values {
		if ontology.NormalizeIdentifierSemanticValue(value) == expected {
			return index
		}
	}
	return -1
}

func identifierAuthorityForMemberships(authority identifierBindingSet, memberships []string) ([]string, []string, []string, error) {
	var actionIDs, issueKeys, requiredChecks []string
	for _, membership := range memberships {
		ids, ok := authority.actionIDsByMembership[membership]
		if !ok {
			return nil, nil, nil, fmt.Errorf("identifier intent has unbound membership %q", membership)
		}
		actionIDs = append(actionIDs, ids...)
		issueKeys = append(issueKeys, authority.issueKeysByMembership[membership]...)
		requiredChecks = append(requiredChecks, authority.requiredChecksByMembership[membership]...)
	}
	actionIDs, issueKeys = sortedUnique(actionIDs), sortedUnique(issueKeys)
	if len(actionIDs) == 0 || len(issueKeys) == 0 {
		return nil, nil, nil, fmt.Errorf("identifier intent has no action or issue authority")
	}
	return actionIDs, issueKeys, sortedUnique(requiredChecks), nil
}

func attachIdentifierBlockedConflicts(operation *RepairOperation, memberships map[string]struct{}, blocked map[string][]string) {
	for _, membership := range identifierMembershipSlice(memberships) {
		for _, message := range blocked[membership] {
			operation.PlanningConflicts = append(operation.PlanningConflicts, RepairConflict{
				Kind: RepairConflictIdentifier, Path: operation.Path, OperationIDs: []string{operation.ID}, Message: message,
			})
		}
	}
}

func identifierDiagnosticMessage(diagnostic identifierreconcile.RepairDiagnostic) string {
	if diagnostic.Field != nil {
		return diagnostic.Field.Message
	}
	if diagnostic.Link != nil {
		return diagnostic.Link.Message
	}
	if diagnostic.MoveConflict != nil {
		return fmt.Sprintf("identifier move destination %s collides with %s", diagnostic.MoveConflict.DestinationPath, diagnostic.MoveConflict.ExistingPath)
	}
	return "identifier repair planning conflict: " + diagnostic.Kind
}

func sortedIdentifierWorkPaths(input map[string]*identifierFileWork) []string {
	paths := make([]string, 0, len(input))
	for notePath := range input {
		paths = append(paths, notePath)
	}
	sort.Strings(paths)
	return paths
}

func identifierMembershipSlice(input map[string]struct{}) []string {
	out := make([]string, 0, len(input))
	for membership := range input {
		out = append(out, membership)
	}
	sort.Strings(out)
	return out
}

func identifierMembershipIdentities(memberships []string) []string {
	out := make([]string, len(memberships))
	for index, membership := range memberships {
		out[index] = "identifier-membership:" + membership
	}
	return out
}

func mergeIdentifierMemberships(target map[string]struct{}, memberships []string) {
	for _, membership := range memberships {
		target[membership] = struct{}{}
	}
}

func identifierJSONKey(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
