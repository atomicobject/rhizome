package validate

import (
	"fmt"

	"os"
	"slices"
	"sort"
	"strings"
)

type plainRepairEdit struct {
	action FixAction
	edit   FixEdit
}

func planPlainRepairGroups(runCtx RunContext, edits []plainRepairEdit) ([]RepairOperation, error) {
	byPathAction := map[string]map[string][]plainRepairEdit{}
	for _, item := range edits {
		path := strings.TrimSpace(item.edit.NotePath)
		if path == "" {
			path = strings.TrimSpace(item.edit.SourcePath)
		}
		if path == "" {
			return nil, fmt.Errorf("repair action %s edit %s has no source path", item.action.ID, item.edit.Kind)
		}
		if byPathAction[path] == nil {
			byPathAction[path] = map[string][]plainRepairEdit{}
		}
		byPathAction[path][item.action.ID] = append(byPathAction[path][item.action.ID], item)
	}
	paths := make([]string, 0, len(byPathAction))
	for path := range byPathAction {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var operations []RepairOperation
	for _, path := range paths {
		abs, err := repairAbsPath(runCtx, path)
		if err != nil {
			return nil, err
		}
		var before []byte
		if runCtx.sourceSnapshot != nil {
			content, readErr := runCtx.sourceSnapshot.reader(runCtx.NoteReader).GetContents(runCtx.VaultDef, path)
			before, err = []byte(content), readErr
		} else {
			before, err = os.ReadFile(abs)
		}
		if err != nil {
			return nil, err
		}
		for _, items := range groupPlainRepairEdits(byPathAction[path]) {
			var actionIDs, issueKeys []string
			for _, item := range items {
				actionIDs = append(actionIDs, item.action.ID)
				issueKeys = append(issueKeys, item.action.IssueKeys...)
			}
			actionIDs, issueKeys = sortedUnique(actionIDs), sortedUnique(issueKeys)
			conflict := plainFrontmatterConflict(items)
			applicable := items
			if conflict != "" {
				applicable = nil
			}
			after := string(before)
			for _, item := range applicable {
				if item.edit.Kind == FixKindRewriteLinkGroup {
					if rewritten, _, ok, rewriteErr := applyBlockFragmentRewrite(
						runCtx, path, after, item.edit.OldTarget, item.edit.NewTarget,
					); rewriteErr != nil {
						return nil, rewriteErr
					} else if ok {
						after = rewritten
					} else {
						after, err = ApplyFixEditInMemory(after, item.edit)
					}
				} else {
					after, err = ApplyFixEditInMemory(after, item.edit)
				}
				if err != nil {
					return nil, fmt.Errorf("plan repair action %s: %w", item.action.ID, err)
				}
			}
			if after == string(before) && conflict == "" {
				continue
			}
			action := items[0].action
			kind := "write"
			if plainFrontmatterEdit(items[0].edit) {
				kind = "frontmatter"
			}
			operation := wholeFileRepairOperation(
				repairOperationID(strings.Join(actionIDs, "\x00"), path, kind), action, path, before, []byte(after),
			)
			operation.Expected = []ExpectedText{minimalRepairExpectedText(before, []byte(after))}
			operation.ActionIDs, operation.IssueKeys = actionIDs, issueKeys
			operation.IssueKey = issueKeys[0]
			if conflict != "" {
				operation.Expected = nil
				operation.PlanningConflicts = []RepairConflict{{
					Kind: RepairConflictEditOverlap, Path: path, OperationIDs: []string{operation.ID}, Message: conflict,
				}}
			}
			operation.LifecycleClaims = lifecycleClaimsForPlainEdits(before, items)
			operation.Lifecycle = ClassifyLifecycleEdit(LifecycleEdit{
				Before: before, After: []byte(after), Claims: operation.LifecycleClaims,
			})
			operations = append(operations, operation)
		}
	}
	return operations, nil
}

func groupPlainRepairEdits(byAction map[string][]plainRepairEdit) [][]plainRepairEdit {
	actionIDs := make([]string, 0, len(byAction))
	for actionID := range byAction {
		actionIDs = append(actionIDs, actionID)
	}
	sort.Strings(actionIDs)
	var groups [][]plainRepairEdit
	var frontmatter []plainRepairEdit
	for _, actionID := range actionIDs {
		var other []plainRepairEdit
		for _, item := range byAction[actionID] {
			if plainFrontmatterEdit(item.edit) {
				frontmatter = append(frontmatter, item)
			} else {
				other = append(other, item)
			}
		}
		if len(other) > 0 {
			groups = append(groups, other)
		}
	}
	if len(frontmatter) > 0 {
		groups = append(groups, frontmatter)
	}
	return groups
}

func plainFrontmatterEdit(edit FixEdit) bool {
	return edit.Kind == FixKindSetFrontmatter || edit.Kind == FixKindAppendAlias
}

func plainFrontmatterConflict(items []plainRepairEdit) string {
	for index, item := range items {
		left := item.edit
		if !plainFrontmatterEdit(left) {
			continue
		}
		for _, previous := range items[:index] {
			right := previous.edit
			if left.Kind == FixKindSetFrontmatter && right.Kind == FixKindSetFrontmatter {
				if !strings.EqualFold(strings.TrimSpace(left.Property), strings.TrimSpace(right.Property)) {
					continue
				}
				if (left.Values == nil) == (right.Values == nil) &&
					((left.Values == nil && left.Value == right.Value) || (left.Values != nil && slices.Equal(left.Values, right.Values))) {
					continue
				}
				return fmt.Sprintf("frontmatter repairs assign conflicting values to %s", strings.TrimSpace(left.Property))
			}
			set, alias := left, right
			if right.Kind == FixKindSetFrontmatter {
				set, alias = right, left
			}
			if set.Kind == FixKindSetFrontmatter && alias.Kind == FixKindAppendAlias && strings.EqualFold(strings.TrimSpace(set.Property), "aliases") {
				return "frontmatter repairs both replace aliases and append an alias"
			}
		}
	}
	return ""
}

func lifecycleClaimsForPlainEdits(before []byte, items []plainRepairEdit) []LifecycleClaim {
	var claims []LifecycleClaim
	for _, item := range items {
		edit := item.edit
		switch edit.Kind {
		case FixKindRewriteLinkTarget:
			if edit.StartByte < 0 || edit.EndByte < edit.StartByte || edit.EndByte > len(before) {
				continue
			}
			claims = append(claims, LifecycleClaim{
				Kind: LifecycleEditBrokenLink, StartByte: edit.StartByte, EndByte: edit.EndByte,
				ExpectedText: string(before[edit.StartByte:edit.EndByte]), Replacement: edit.Value,
			})
		case FixKindRewriteLinkGroup:
			claims = append(claims, brokenLinkGroupLifecycleClaims(before, edit.OldTarget, edit.NewTarget)...)
		case FixKindSetFrontmatter:
			if !strings.EqualFold(strings.TrimSpace(edit.Property), "status") ||
				!strings.EqualFold(strings.TrimSpace(edit.Value), "archived") {
				continue
			}
			start, end, ok := frontmatterScalarRange(before, "status")
			if ok {
				claim := LifecycleClaim{
					Kind: LifecycleEditArchiveStatus, StartByte: start, EndByte: end,
					ExpectedText: string(before[start:end]), Replacement: edit.Value,
				}
				if !slices.Contains(claims, claim) {
					claims = append(claims, claim)
				}
			}
		}
	}
	return claims
}

func brokenLinkGroupLifecycleClaims(before []byte, oldTarget, newTarget string) []LifecycleClaim {
	if oldTarget == "" || newTarget == "" {
		return nil
	}
	var claims []LifecycleClaim
	for cursor := 0; cursor < len(before); {
		offset := strings.Index(string(before[cursor:]), oldTarget)
		if offset < 0 {
			break
		}
		start := cursor + offset
		claim := LifecycleClaim{
			Kind: LifecycleEditBrokenLink, StartByte: start, EndByte: start + len(oldTarget),
			ExpectedText: oldTarget, Replacement: newTarget,
		}
		if validateBrokenLinkClaim(before, claim) == nil {
			claims = append(claims, claim)
		}
		cursor = claim.EndByte
	}
	return claims
}

func minimalRepairExpectedText(before, after []byte) ExpectedText {
	start := 0
	for start < len(before) && start < len(after) && before[start] == after[start] {
		start++
	}
	beforeEnd, afterEnd := len(before), len(after)
	for beforeEnd > start && afterEnd > start && before[beforeEnd-1] == after[afterEnd-1] {
		beforeEnd--
		afterEnd--
	}
	return ExpectedText{
		StartByte: start, EndByte: beforeEnd,
		Text: string(before[start:beforeEnd]), Replacement: string(after[start:afterEnd]),
	}
}

func wholeFileRepairOperation(
	id string,
	action FixAction,
	path string,
	before, after []byte,
) RepairOperation {
	return RepairOperation{
		ID: id, ActionID: action.ID, IssueKey: action.IssueKeys[0], Kind: RepairOperationWrite,
		Path: path, SourceHash: SourceHash(before), Content: append([]byte(nil), after...),
		Expected:  []ExpectedText{{StartByte: 0, EndByte: len(before), Text: string(before), Replacement: string(after)}},
		Lifecycle: ClassifyLifecycleEdit(LifecycleEdit{Before: before, After: after}),
	}
}

func repairOperationID(actionID, path, kind string) string {
	digest := strings.TrimPrefix(SourceHash([]byte(actionID+"\x00"+path+"\x00"+kind)), "sha256:")
	return "operation:v1:" + digest
}
