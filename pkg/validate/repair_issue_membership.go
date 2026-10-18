package validate

import (
	"encoding/json"

	"strings"
)

func attachStableRepairIssueKeys(checks []CheckResult) ([]CheckResult, error) {
	checks = append([]CheckResult(nil), checks...)
	for checkIndex := range checks {
		check := &checks[checkIndex]
		check.Issues = append([]Issue(nil), check.Issues...)
		evidenceIssues := append([]Issue(nil), check.fullIssues...)
		if len(evidenceIssues) == 0 {
			evidenceIssues = append(evidenceIssues, check.Issues...)
		}
		issuesByCode := map[string][]Issue{}
		var allKeys []string
		for issueIndex := range evidenceIssues {
			issue := &evidenceIssues[issueIndex]
			key, err := StableIssueKey(check.Name, *issue)
			if err != nil {
				return nil, err
			}
			issue.Key = key
			issuesByCode[issue.Code] = append(issuesByCode[issue.Code], *issue)
			allKeys = append(allKeys, key)
		}
		check.fullIssues = evidenceIssues
		for issueIndex := range check.Issues {
			if issueIndex < len(evidenceIssues) {
				check.Issues[issueIndex].Key = evidenceIssues[issueIndex].Key
			}
		}
		check.allIssueKeys = sortedUnique(allKeys)
		check.Fixes = append([]FixAction(nil), check.Fixes...)
		for actionIndex := range check.Fixes {
			action := &check.Fixes[actionIndex]
			if len(action.IssueKeys) != 0 {
				continue
			}
			action.IssueKeys = matchingRepairIssueKeys(*action, issuesByCode[action.IssueCode])
			if len(action.IssueKeys) == 0 && len(allKeys) == 1 {
				action.IssueKeys = append(action.IssueKeys, allKeys[0])
			}
			if len(action.IssueKeys) == 0 {
				action.IssueKeys = []string{fallbackActionIssueKey(*action)}
			}
		}
	}
	return checks, nil
}

func matchingRepairIssueKeys(action FixAction, issues []Issue) []string {
	var keys []string
	for _, issue := range issues {
		if actionMatchesRepairIssue(action, issue) {
			keys = append(keys, issue.Key)
		}
	}
	if len(keys) == 0 && len(issues) == 1 {
		keys = append(keys, issues[0].Key)
	}
	return sortedUnique(keys)
}

func actionMatchesRepairIssue(action FixAction, issue Issue) bool {
	actionPaths := append([]string(nil), action.AffectedPaths...)
	var targets, properties, blockIDs []string
	for _, edit := range action.Edits {
		actionPaths = append(actionPaths, edit.NotePath, edit.SourcePath)
		targets = append(targets, edit.OldTarget, edit.NewTarget, edit.Value)
		targets = append(targets, edit.Values...)
		properties = append(properties, edit.Property)
		blockIDs = append(blockIDs, edit.BlockID)
		targets = append(targets, edit.BlockID)
	}
	actionPaths = nonEmptyUnique(actionPaths)
	issuePaths := nonEmptyUnique([]string{issue.Path, issue.Source})
	if len(actionPaths) > 0 && len(issuePaths) > 0 && !stringSetsIntersect(actionPaths, issuePaths) {
		return false
	}
	targets = nonEmptyUnique(targets)
	if issue.Target != "" && len(targets) > 0 && !containsString(targets, issue.Target) {
		return false
	}
	properties = nonEmptyUnique(properties)
	fieldNamedByActionID := issue.Field != "" && strings.HasSuffix(action.ID, ":"+issue.Field)
	if issue.Field != "" && len(properties) > 0 &&
		!containsStringFold(properties, issue.Field) && !fieldNamedByActionID {
		return false
	}
	blockIDs = nonEmptyUnique(blockIDs)
	if issueBlockID := structuredIssueString(issue, "blockId"); issueBlockID != "" &&
		len(blockIDs) > 0 && !containsString(blockIDs, issueBlockID) {
		return false
	}
	return len(actionPaths) > 0 || len(targets) > 0 || len(properties) > 0 || len(blockIDs) > 0
}

func structuredIssueString(issue Issue, field string) string {
	if len(issue.Data) == 0 {
		return ""
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(issue.Data, &data); err != nil {
		return ""
	}
	var value string
	if err := json.Unmarshal(data[field], &value); err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func nonEmptyUnique(values []string) []string {
	var result []string
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, value)
		}
	}
	return sortedUnique(result)
}

func stringSetsIntersect(left, right []string) bool {
	for _, value := range left {
		if containsString(right, value) {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsStringFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}

func fallbackActionIssueKey(action FixAction) string {
	payload, _ := json.Marshal(struct {
		Check string    `json:"check"`
		Code  string    `json:"code"`
		Kind  string    `json:"kind"`
		Edits []FixEdit `json:"edits"`
	}{Check: action.Check, Code: action.IssueCode, Kind: action.Kind, Edits: action.Edits})
	return "issue:action:v1:" + strings.TrimPrefix(SourceHash(payload), "sha256:")
}
