package validate

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

type structuredRepairEdit struct {
	action FixAction
	edit   FixEdit
}

func planOntologyRepairGroups(
	ctx context.Context, runCtx RunContext, schema *ontology.Schema,
	previewer OntologyOperationPreviewer, edits []structuredRepairEdit,
) ([]RepairOperation, []RepairOperation, error) {
	byPath := map[string][]structuredRepairEdit{}
	for _, item := range edits {
		byPath[item.edit.NotePath] = append(byPath[item.edit.NotePath], item)
	}
	paths := make([]string, 0, len(byPath))
	for path := range byPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var operations, supplemental []RepairOperation
	for _, path := range paths {
		group := byPath[path]
		var actionIDs, issueKeys []string
		for _, item := range group {
			actionIDs = append(actionIDs, item.action.ID)
			issueKeys = append(issueKeys, item.action.IssueKeys...)
		}
		actionIDs, issueKeys = sortedUnique(actionIDs), sortedUnique(issueKeys)
		request := OntologyPreviewRequest{
			OperationID: repairOperationID(strings.Join(actionIDs, "\x00"), path, "preview"),
			ActionIDs:   actionIDs, IssueKeys: issueKeys,
		}
		for _, item := range group {
			action, edit := item.action, item.edit
			kind := ontology.NodeKindEmbedded
			if strings.TrimSpace(edit.NodeID) == "" && strings.TrimSpace(edit.Structural) == "" {
				kind = ontology.NodeKindNote
			}
			ref := ontology.NodeRef{
				NotePath: strings.TrimSpace(edit.NotePath), Kind: kind,
				NodeID: strings.TrimSpace(edit.NodeID), Structural: strings.TrimSpace(edit.Structural),
			}
			switch edit.Kind {
			case FixKindOntologySetScalar:
				request.Edits = append(request.Edits, OntologyEdit{Kind: OntologyEditSetScalar, Ref: ref, Field: edit.Property, Value: edit.Value})
			case FixKindOntologySetLink:
				request.Edits = append(request.Edits, OntologyEdit{Kind: OntologyEditSetLink, Ref: ref, Field: edit.Property, Values: edit.Values})
			case FixKindOntologyAddSection:
				request.Edits = append(request.Edits, OntologyEdit{Kind: OntologyEditAddSectionField, Ref: ref, Field: edit.Property})
			case FixKindEnsureBlockID:
				request.Edits = append(request.Edits, OntologyEdit{Kind: OntologyEditEnsureBlockID, Ref: ref, BlockID: edit.BlockID})
			case FixKindRemoveBlockID:
				request.Edits = append(request.Edits, OntologyEdit{Kind: OntologyEditRemoveBlockID, Ref: ref})
			case FixKindUpgradeToBlockID:
				blockID, newTarget, err := plannedUpgradeTarget(ctx, runCtx, schema, ref, edit.BlockID)
				if err != nil {
					return nil, nil, err
				}
				request.Edits = append(request.Edits, OntologyEdit{Kind: OntologyEditEnsureBlockID, Ref: ref, BlockID: blockID})
				if strings.TrimSpace(edit.SourcePath) != "" && strings.TrimSpace(edit.OldTarget) != "" {
					rewrite, err := planUpgradeSourceRewrite(runCtx, action, edit, newTarget)
					if err != nil {
						return nil, nil, err
					}
					supplemental = append(supplemental, rewrite)
				}
			}
		}
		result, err := previewer.Preview(ctx, runCtx, schema, request)
		if err != nil {
			return nil, nil, err
		}
		if len(result.Conflicts) > 0 {
			before, _, err := readNoteForFix(runCtx, path)
			if err != nil {
				return nil, nil, err
			}
			placeholder := RepairOperation{
				ID: request.OperationID, ActionID: actionIDs[0], ActionIDs: actionIDs,
				IssueKey: issueKeys[0], IssueKeys: issueKeys, Kind: RepairOperationWrite,
				Path: path, SourceHash: SourceHash([]byte(before)), Content: []byte(before),
			}
			for _, conflict := range result.Conflicts {
				placeholder.PlanningConflicts = append(placeholder.PlanningConflicts, RepairConflict{
					Kind: RepairConflictOntology, Path: conflict.NotePath,
					OperationIDs: []string{request.OperationID}, Message: conflict.Message,
				})
			}
			operations = append(operations, placeholder)
			continue
		}
		operations = append(operations, result.Operations...)
	}
	return operations, supplemental, nil
}

func plannedUpgradeTarget(
	ctx context.Context, runCtx RunContext, schema *ontology.Schema,
	ref ontology.NodeRef, requestedBlockID string,
) (string, string, error) {
	service := ontology.NodeLinkService{VaultDef: runCtx.VaultDef, NoteReader: runCtx.NoteReader, Schema: schema}
	result, err := service.LinkTargets(ctx, ontology.LinkTargetRequest{Refs: []ontology.NodeRef{ref}, Ensure: ontology.EnsureLinkTargetPlan})
	if err != nil {
		return "", "", err
	}
	for _, target := range result.Targets {
		blockID := strings.TrimSpace(requestedBlockID)
		if blockID == "" {
			blockID = target.BlockID
		}
		newTarget := strings.TrimSuffix(strings.TrimPrefix(target.Wikilink, "[["), "]]")
		if blockID == "" || newTarget == "" {
			return "", "", fmt.Errorf("upgrade target %s is not linkable", ref.String())
		}
		return blockID, newTarget, nil
	}
	return "", "", fmt.Errorf("upgrade target %s did not resolve", ref.String())
}

func planUpgradeSourceRewrite(runCtx RunContext, action FixAction, edit FixEdit, newTarget string) (RepairOperation, error) {
	abs, err := repairAbsPath(runCtx, edit.SourcePath)
	if err != nil {
		return RepairOperation{}, err
	}
	before, err := os.ReadFile(abs)
	if err != nil {
		return RepairOperation{}, err
	}
	oldLink, after := "[["+edit.OldTarget, string(before)
	if edit.StartByte >= 0 && edit.EndByte > edit.StartByte && edit.EndByte <= len(after) {
		span := after[edit.StartByte:edit.EndByte]
		if !strings.HasPrefix(span, oldLink) {
			return RepairOperation{}, fmt.Errorf("upgrade source range no longer matches %q", edit.OldTarget)
		}
		after = after[:edit.StartByte] + strings.Replace(span, oldLink, "[["+newTarget, 1) + after[edit.EndByte:]
	} else {
		after = strings.Replace(after, oldLink, "[["+newTarget, 1)
	}
	if after == string(before) {
		return RepairOperation{}, fmt.Errorf("upgrade source link %q no longer exists", edit.OldTarget)
	}
	operation := wholeFileRepairOperation(repairOperationID(action.ID, edit.SourcePath, "upgrade-link"), action, edit.SourcePath, before, []byte(after))
	span := minimalRepairExpectedText(before, []byte(after))
	operation.LifecycleClaims = []LifecycleClaim{{
		Kind: LifecycleEditBrokenLink, StartByte: span.StartByte, EndByte: span.EndByte,
		ExpectedText: span.Text, Replacement: span.Replacement,
	}}
	operation.Lifecycle = ClassifyLifecycleEdit(LifecycleEdit{Before: before, After: []byte(after), Claims: operation.LifecycleClaims})
	return operation, nil
}
