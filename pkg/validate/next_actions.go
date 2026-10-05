package validate

import (
	"fmt"
	"sort"
	"strings"
)

// BuildNextActions turns validation output into agent-facing follow-up guidance.
// It deliberately uses existing fix safety tiers rather than inventing a second
// classifier: safe fixes can run automatically, confirmation fixes need review,
// and agent-required fixes need targeted remediation.
func BuildNextActions(result Result) *NextActions {
	if result.IssueCount == 0 && result.ErrorCount == 0 {
		return nil
	}

	next := &NextActions{
		CheckErrorCount: result.ErrorCount,
	}

	if result.ErrorCount > 0 {
		next.Actions = append(next.Actions, NextAction{
			Category: "check_errors",
			Title:    "Resolve validation check errors",
			Message:  "Resolve validation check errors first; errors can make issue counts and fix suggestions incomplete.",
			Count:    result.ErrorCount,
		})
	}

	if result.FixPlan != nil {
		next.SafeFixCount = result.FixPlan.SafeCount
		next.NeedsConfirmationCount = result.FixPlan.ConfirmationCount
		next.AgentRequiredCount = result.FixPlan.AgentCount
		evidenceIssueKeys := resultRemediationIssueKeySet(result)
		coveredIssueKeys := repairPlanCoveredIssueKeys(result.FixPlan, evidenceIssueKeys)
		if result.FixPlan.SafeCount > 0 {
			next.SafeAutoFixCommand = strings.TrimSpace(result.ApplyCommand)
			if next.SafeAutoFixCommand == "" {
				next.SafeAutoFixCommand = buildSafeAutoFixCommand(result.VaultName, result.SelectedChecks)
			}
			issueKeys := repairPlanIssueKeysBySafety(result.FixPlan, FixSafetySafe, evidenceIssueKeys)
			next.Actions = append(next.Actions, NextAction{
				Category:  "safe_auto_fix",
				Title:     "Apply safe deterministic fixes",
				Message:   fmt.Sprintf("Apply %d safe deterministic fix(es) with the positional fix command, then re-run validation without --apply.", result.FixPlan.SafeCount),
				Command:   next.SafeAutoFixCommand,
				Count:     exactIssueOrActionCount(issueKeys, result.FixPlan.SafeCount),
				IssueKeys: issueKeys,
			})
		}
		if result.FixPlan.ConfirmationCount > 0 {
			issueKeys := repairPlanIssueKeysBySafety(result.FixPlan, FixSafetyConfirm, evidenceIssueKeys)
			next.Actions = append(next.Actions, NextAction{
				Category:  "needs_confirmation",
				Title:     "Review ambiguous fixes",
				Message:   fmt.Sprintf("Review %d needs_confirmation fix(es); apply only after the target/content intent is clear.", result.FixPlan.ConfirmationCount),
				Command:   executableConfirmationCommand(result),
				Count:     exactIssueOrActionCount(issueKeys, result.FixPlan.ConfirmationCount),
				IssueKeys: issueKeys,
			})
		}
		if result.FixPlan.AgentCount > 0 {
			issueKeys := repairPlanIssueKeysBySafety(result.FixPlan, FixSafetyAgent, evidenceIssueKeys)
			reason := "No deterministic repair transaction exists for these findings; an agent must make the evidence-backed source decision and rerun the same selected validation scope."
			next.Actions = append(next.Actions, NextAction{
				Category:         "agent_required",
				Title:            "Resolve targeted remediation work",
				Message:          fmt.Sprintf("Resolve %d agent_required fix(es) with targeted code/docs/ontology work; inspect affected paths and governing guidance before editing.", result.FixPlan.AgentCount),
				Count:            exactIssueOrActionCount(issueKeys, result.FixPlan.AgentCount),
				IssueKeys:        issueKeys,
				NonFixableReason: reason,
			})
		}

		appendRegistryNextActions(next, result, coveredIssueKeys)
	} else {
		appendRegistryNextActions(next, result, nil)
	}

	next.ClassificationRequired = next.UnclassifiedIssueCount > 0
	return next
}

func repairPlanCoveredIssueKeys(plan *FixPlan, evidence map[string]struct{}) map[string]struct{} {
	covered := make(map[string]struct{})
	if plan == nil {
		return covered
	}
	for _, action := range plan.Actions {
		for _, issueKey := range action.IssueKeys {
			if _, exact := evidence[issueKey]; exact {
				covered[issueKey] = struct{}{}
			}
		}
	}
	return covered
}

func repairPlanIssueKeysBySafety(plan *FixPlan, safety FixSafety, evidence map[string]struct{}) []string {
	if plan == nil {
		return nil
	}
	var keys []string
	for _, action := range plan.Actions {
		if action.Safety == safety {
			for _, issueKey := range action.IssueKeys {
				if _, exact := evidence[issueKey]; exact {
					keys = append(keys, issueKey)
				}
			}
		}
	}
	return sortedUnique(keys)
}

func exactIssueOrActionCount(issueKeys []string, actionCount int) int {
	if len(issueKeys) > 0 {
		return len(issueKeys)
	}
	return actionCount
}

type remediationIssueEvidence struct {
	check string
	issue Issue
	key   string
}

func appendRegistryNextActions(next *NextActions, result Result, covered map[string]struct{}) {
	evidence := resultRemediationIssueEvidence(result)
	for _, item := range evidence {
		if _, ok := covered[item.key]; ok {
			continue
		}
		registration, registered := lookupIssueRemediation(item.check, item.issue.Code)
		if !registered {
			reason := fmt.Sprintf("No remediation registration exists for validation issue code %s/%s; add an exact registry entry before claiming this finding is repairable.", item.check, item.issue.Code)
			next.Actions = append(next.Actions, NextAction{
				Category: "registry_gap", Title: "Register validation remediation", Message: reason,
				Count: 1, IssueKeys: []string{item.key}, NonFixableReason: reason,
			})
			next.UnclassifiedIssueCount++
			continue
		}
		if registration.NonFixableReason != "" {
			next.Actions = append(next.Actions, NextAction{
				Category: "non_fixable", Title: "Resolve validation prerequisite", Message: registration.NonFixableReason,
				Count: 1, IssueKeys: []string{item.key}, NonFixableReason: registration.NonFixableReason,
			})
			continue
		}
		next.AgentRequiredCount++
		reason := "No deterministic repair transaction exists for this finding; follow the exact agent action and rerun the same selected validation scope."
		next.Actions = append(next.Actions, NextAction{
			Category: "agent_required", Title: "Resolve " + strings.ReplaceAll(item.issue.Code, "_", " "),
			Message: registration.AgentAction, Count: 1, IssueKeys: []string{item.key},
			NonFixableReason: reason,
		})
	}

	missingEvidence := result.IssueCount - len(evidence)
	if missingEvidence > 0 {
		reason := fmt.Sprintf("%d validation finding(s) lack structured issue rows and stable issue keys; the producing check must emit exact issue evidence before remediation can be classified.", missingEvidence)
		next.Actions = append(next.Actions, NextAction{
			Category: "registry_gap", Title: "Emit stable validation issue evidence", Message: reason,
			Count: missingEvidence, NonFixableReason: reason,
		})
		next.UnclassifiedIssueCount += missingEvidence
	}
}

func resultRemediationIssueEvidence(result Result) []remediationIssueEvidence {
	var evidence []remediationIssueEvidence
	for _, check := range result.Checks {
		for _, issue := range remediationEvidenceIssues(check) {
			key := strings.TrimSpace(issue.Key)
			if key == "" {
				stableKey, err := StableIssueKey(check.Name, issue)
				if err != nil {
					// Production results have already passed
					// attachStableRepairIssueKeys. Only a manually assembled Result can
					// reach this fallback without a valid canonical identity; omit it so
					// missingEvidence reports the explicit registry gap.
					continue
				}
				key = stableKey
			}
			evidence = append(evidence, remediationIssueEvidence{check: check.Name, issue: issue, key: key})
		}
	}
	sort.SliceStable(evidence, func(i, j int) bool { return evidence[i].key < evidence[j].key })
	return evidence
}

func resultRemediationIssueKeySet(result Result) map[string]struct{} {
	keys := make(map[string]struct{})
	for _, item := range resultRemediationIssueEvidence(result) {
		keys[item.key] = struct{}{}
	}
	return keys
}

func exactScopedRepairCommand(result Result) string {
	if command := strings.TrimSpace(result.ApplyCommand); command != "" {
		return command
	}
	return buildSafeAutoFixCommand(result.VaultName, result.SelectedChecks)
}

// executableConfirmationCommand converts the JSON-only agent apply surface
// to the interactive human surface. Agent apply deliberately skips
// needs_confirmation actions, so returning it here would be a no-op command.
func executableConfirmationCommand(result Result) string {
	command := exactScopedRepairCommand(result)
	if strings.HasPrefix(command, "rzm agent validate fix ") {
		return "rzm validate fix " + strings.TrimPrefix(command, "rzm agent validate fix ")
	}
	return command
}

func buildSafeAutoFixCommand(vaultName string, selectedChecks []string) string {
	vaultName = strings.TrimSpace(vaultName)
	commands := make([]string, 0, len(selectedChecks))
	for _, check := range selectedChecks {
		canonical, ok := CanonicalCheck(check)
		if !ok {
			continue
		}
		registration, _ := lookupCheck(canonical)
		var b strings.Builder
		b.WriteString("rzm agent validate fix ")
		b.WriteString(shellQuoteCommandArg(registration.CLIName))
		b.WriteString(" --apply")
		if vaultName != "" {
			b.WriteString(" --vault ")
			b.WriteString(shellQuoteCommandArg(vaultName))
		}
		commands = append(commands, b.String())
	}
	return strings.Join(commands, " && ")
}

func shellQuoteCommandArg(arg string) string {
	if arg == "" {
		return "''"
	}
	safe := true
	for _, r := range arg {
		if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' || r == '/' || r == ':') {
			safe = false
			break
		}
	}
	if safe {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'"
}
