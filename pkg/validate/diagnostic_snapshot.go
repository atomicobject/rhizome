package validate

import (
	"fmt"
	"sort"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
)

const (
	LocationUnitUTF8Bytes = semdb.ValidationLocationUnitUTF8Bytes
	LocationUnitLine      = semdb.ValidationLocationUnitLine
)

type IssueLocation struct {
	Unit     string `json:"unit"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
	NodeID   string `json:"nodeId,omitempty"`
	Field    string `json:"field,omitempty"`
	Relation string `json:"relation,omitempty"`
}

type DiagnosticSnapshotContext struct {
	VaultIdentity  string
	Scope          string
	Generation     int64
	SchemaIdentity string
	ConfigIdentity string
	InputRevision  string
	StartedAt      int64
	FinishedAt     int64
	DurationMs     int64
	Completion     string
	StaleReason    string
	CheckOutcomes  []DiagnosticCheckOutcome
	NotePaths      []string
}

type DiagnosticCheckOutcome struct {
	Check   string
	Outcome CheckOutcome
	Summary string
}

func BuildDiagnosticSnapshot(result Result, build DiagnosticSnapshotContext) (semdb.ValidationSnapshot, error) {
	completion := strings.TrimSpace(build.Completion)
	if completion == "" {
		completion = semdb.ValidationCompletionComplete
	}
	if completion != semdb.ValidationCompletionComplete && completion != semdb.ValidationCompletionIncomplete {
		return semdb.ValidationSnapshot{}, fmt.Errorf("unknown validation completion %q", completion)
	}

	actions, actionIDsByIssue, err := diagnosticActionSnapshots(result.FixPlan)
	if err != nil {
		return semdb.ValidationSnapshot{}, err
	}
	checks, err := diagnosticCheckSnapshots(result, build.CheckOutcomes)
	if err != nil {
		return semdb.ValidationSnapshot{}, err
	}
	failedChecks := 0
	blocked := false
	for _, check := range checks {
		if check.Outcome == semdb.ValidationCheckOutcomeFailed {
			failedChecks++
		}
		if check.Outcome == semdb.ValidationCheckOutcomeBlocked {
			blocked = true
			completion = semdb.ValidationCompletionIncomplete
		}
	}
	if result.ErrorCount > failedChecks && !blocked {
		return semdb.ValidationSnapshot{}, fmt.Errorf("validation errors have no failed or blocked check outcome")
	}
	diagnostics := make([]semdb.ValidationDiagnostic, 0, result.IssueCount)
	diagnosticIndex := make(map[string]int, result.IssueCount)
	filePaths := make(map[string]struct{})
	notePaths := make(map[string]struct{})
	noteInventory := make(map[string]struct{}, len(build.NotePaths))
	for _, path := range build.NotePaths {
		if path = strings.TrimSpace(path); path != "" {
			noteInventory[path] = struct{}{}
		}
	}
	for _, check := range result.Checks {
		checkName := diagnosticCanonicalCheck(check.Name)
		issues := check.fullIssues
		if issues == nil && check.IssueCount == len(check.Issues) {
			issues = check.Issues
		}
		if len(issues) != check.IssueCount {
			return semdb.ValidationSnapshot{}, fmt.Errorf("validation check %q has %d complete issues for issueCount %d", check.Name, len(issues), check.IssueCount)
		}
		for _, issue := range issues {
			if strings.TrimSpace(issue.Key) == "" {
				return semdb.ValidationSnapshot{}, fmt.Errorf("validation check %q has an issue without a stable key", check.Name)
			}
			notes := sortedUnique(issue.AffectedNotePaths)
			affected := sortedUnique(append(append(append([]string(nil), issue.AffectedPaths...), notes...), issue.Path))
			if len(noteInventory) > 0 {
				notes = notes[:0]
				for _, path := range affected {
					if _, ok := noteInventory[path]; ok {
						notes = append(notes, path)
					}
				}
			}
			notes = sortedUnique(notes)
			for _, path := range affected {
				if path != "" {
					filePaths[path] = struct{}{}
				}
			}
			for _, path := range notes {
				if path != "" {
					notePaths[path] = struct{}{}
				}
			}
			diagnostic := semdb.ValidationDiagnostic{
				IssueKey: issue.Key, Check: checkName, Code: issue.Code, Message: issue.Message,
				Evidence: append([]byte(nil), issue.Data...), PrimaryPath: issue.Path,
				AffectedPaths: affected, AffectedNotePaths: notes, Type: issue.Type, Field: issue.Field,
				Source: issue.Source, Target: issue.Target, ActionIDs: sortedUnique(actionIDsByIssue[issue.Key]),
			}
			if issue.Variant != nil {
				diagnostic.Variant = &semdb.ValidationIssueVariant{Key: issue.Variant.Key, Label: issue.Variant.Label}
			}
			if issue.Location != nil {
				diagnostic.Location = &semdb.ValidationDiagnosticLocation{
					Unit: issue.Location.Unit, Start: issue.Location.Start, End: issue.Location.End,
					NodeID: issue.Location.NodeID, Field: issue.Location.Field, Relation: issue.Location.Relation,
				}
				diagnostic.AffectedNodeIDs = sortedUnique(append(diagnostic.AffectedNodeIDs, issue.Location.NodeID))
			} else if issue.Line > 0 {
				diagnostic.Location = &semdb.ValidationDiagnosticLocation{Unit: semdb.ValidationLocationUnitLine, Start: issue.Line, End: issue.Line}
			}
			diagnostic.AffectedNodeIDs = sortedUnique(append(diagnostic.AffectedNodeIDs, issue.AffectedNodeIDs...))
			diagnostic.AffectedTypes = sortedUnique(append(append(diagnostic.AffectedTypes, issue.AffectedTypes...), issue.Type))
			diagnostic.AffectedInterfaces = sortedUnique(issue.AffectedInterfaces)
			if index, duplicate := diagnosticIndex[diagnostic.IssueKey]; duplicate {
				// One stable key is one issue; a check reporting it twice adds
				// membership rather than a second diagnostic.
				diagnostics[index] = mergeDuplicateDiagnostic(diagnostics[index], diagnostic)
				continue
			}
			diagnosticIndex[diagnostic.IssueKey] = len(diagnostics)
			diagnostics = append(diagnostics, diagnostic)
		}
	}

	fingerprint := ""
	if result.FixPlan != nil {
		fingerprint = result.FixPlan.Fingerprint
	}
	return semdb.ValidationSnapshot{
		VaultIdentity: build.VaultIdentity, Generation: build.Generation, Scope: build.Scope,
		SelectedChecks: diagnosticCanonicalChecks(result.SelectedChecks), SchemaIdentity: build.SchemaIdentity,
		ConfigIdentity: build.ConfigIdentity, InputRevision: build.InputRevision,
		StartedAt: build.StartedAt, FinishedAt: build.FinishedAt, DurationMs: build.DurationMs,
		Completion: completion, StaleReason: build.StaleReason,
		IssueCount: len(diagnostics), ErrorCount: failedChecks, AffectedFileCount: len(filePaths),
		AffectedNoteCount: len(notePaths), RepairActionCount: len(actions), RepairPlanFingerprint: fingerprint,
		Checks: checks, Diagnostics: diagnostics, Actions: actions,
	}, nil
}

// ApplyDiagnosticNoteInventory derives note membership from the indexed note
// inventory. It is used when the inventory owner is available only at the
// publication boundary rather than during suite execution.
func ApplyDiagnosticNoteInventory(snapshot *semdb.ValidationSnapshot, notePaths []string) {
	if snapshot == nil {
		return
	}
	inventory := make(map[string]struct{}, len(notePaths))
	for _, path := range notePaths {
		if path = strings.TrimSpace(path); path != "" {
			inventory[path] = struct{}{}
		}
	}
	affectedNotes := make(map[string]struct{})
	for index := range snapshot.Diagnostics {
		diagnostic := &snapshot.Diagnostics[index]
		diagnostic.AffectedNotePaths = diagnostic.AffectedNotePaths[:0]
		for _, path := range diagnostic.AffectedPaths {
			if _, ok := inventory[path]; ok {
				diagnostic.AffectedNotePaths = append(diagnostic.AffectedNotePaths, path)
				affectedNotes[path] = struct{}{}
			}
		}
		diagnostic.AffectedNotePaths = sortedUnique(diagnostic.AffectedNotePaths)
	}
	snapshot.AffectedNoteCount = len(affectedNotes)
}

func diagnosticCheckSnapshots(result Result, outcomes []DiagnosticCheckOutcome) ([]semdb.ValidationCheckSnapshot, error) {
	results := make(map[string]CheckResult, len(result.Checks))
	for _, check := range result.Checks {
		name := diagnosticCanonicalCheck(check.Name)
		if _, duplicate := results[name]; duplicate {
			return nil, fmt.Errorf("duplicate validation check result %q", name)
		}
		check.Name = name
		results[name] = check
	}
	if len(outcomes) == 0 {
		checks := make([]semdb.ValidationCheckSnapshot, 0, len(result.Checks))
		for _, check := range result.Checks {
			checks = append(checks, diagnosticCompletedCheckSnapshot(check))
		}
		return checks, nil
	}
	checks := make([]semdb.ValidationCheckSnapshot, 0, len(outcomes))
	seen := make(map[string]struct{}, len(outcomes))
	for _, outcome := range outcomes {
		name := diagnosticCanonicalCheck(outcome.Check)
		if name == "" {
			return nil, fmt.Errorf("validation check outcome is missing a check name")
		}
		if _, duplicate := seen[name]; duplicate {
			return nil, fmt.Errorf("duplicate validation check outcome %q", name)
		}
		seen[name] = struct{}{}
		snapshot := semdb.ValidationCheckSnapshot{Check: name, Summary: outcome.Summary}
		switch outcome.Outcome {
		case CheckOutcomeCompleted:
			check, ok := results[name]
			if !ok {
				return nil, fmt.Errorf("completed validation check %q is missing its result", name)
			}
			snapshot = diagnosticCompletedCheckSnapshot(check)
			delete(results, name)
		case CheckOutcomeBlocked:
			snapshot.Outcome = semdb.ValidationCheckOutcomeBlocked
		case CheckOutcomeNotApplicable:
			snapshot.Outcome = semdb.ValidationCheckOutcomeNotApplicable
		default:
			return nil, fmt.Errorf("validation check %q has unknown outcome %q", name, outcome.Outcome)
		}
		checks = append(checks, snapshot)
	}
	if len(results) != 0 {
		return nil, fmt.Errorf("validation snapshot has check results without applicability outcomes")
	}
	return checks, nil
}

func diagnosticCompletedCheckSnapshot(check CheckResult) semdb.ValidationCheckSnapshot {
	outcome := semdb.ValidationCheckOutcomeCompleted
	if strings.TrimSpace(check.Error) != "" {
		outcome = semdb.ValidationCheckOutcomeFailed
	} else if check.Skipped {
		outcome = semdb.ValidationCheckOutcomeSkipped
	}
	return semdb.ValidationCheckSnapshot{
		Check: diagnosticCanonicalCheck(check.Name), Outcome: outcome, IssueCount: check.IssueCount,
		Summary: check.Summary, Notes: append([]string(nil), check.Notes...), Error: check.Error, DurationMs: check.DurationMs,
	}
}

func diagnosticActionSnapshots(plan *FixPlan) ([]semdb.ValidationActionSnapshot, map[string][]string, error) {
	byIssue := make(map[string][]string)
	if plan == nil {
		return nil, byIssue, nil
	}
	actions := make([]semdb.ValidationActionSnapshot, 0, len(plan.Actions))
	for _, action := range plan.Actions {
		if strings.TrimSpace(action.ID) == "" {
			return nil, nil, fmt.Errorf("validation repair action is missing a stable id")
		}
		issueKeys := sortedUnique(action.IssueKeys)
		for _, issueKey := range issueKeys {
			byIssue[issueKey] = append(byIssue[issueKey], action.ID)
		}
		actions = append(actions, semdb.ValidationActionSnapshot{
			ID: action.ID, Check: diagnosticCanonicalCheck(action.Check), IssueCode: action.IssueCode, Kind: action.Kind,
			Safety: string(action.Safety), Title: action.Title, Summary: action.Summary, Question: action.Question,
			InstanceCount: action.InstanceCount, IssueKeys: issueKeys,
			AffectedPaths: sortedUnique(action.AffectedPaths), CandidatePaths: sortedUnique(action.CandidatePaths),
		})
	}
	sort.SliceStable(actions, func(i, j int) bool { return actions[i].ID < actions[j].ID })
	return actions, byIssue, nil
}

func diagnosticCanonicalCheck(raw string) string {
	if canonical, ok := CanonicalCheck(raw); ok {
		return canonical
	}
	return strings.TrimSpace(raw)
}

func diagnosticCanonicalChecks(checks []string) []string {
	canonical := make([]string, 0, len(checks))
	seen := make(map[string]struct{}, len(checks))
	for _, check := range checks {
		name := diagnosticCanonicalCheck(check)
		if _, ok := seen[name]; name == "" || ok {
			continue
		}
		seen[name] = struct{}{}
		canonical = append(canonical, name)
	}
	return canonical
}

func mergeDuplicateDiagnostic(existing, duplicate semdb.ValidationDiagnostic) semdb.ValidationDiagnostic {
	existing.AffectedPaths = sortedUnique(append(existing.AffectedPaths, duplicate.AffectedPaths...))
	existing.AffectedNotePaths = sortedUnique(append(existing.AffectedNotePaths, duplicate.AffectedNotePaths...))
	existing.AffectedNodeIDs = sortedUnique(append(existing.AffectedNodeIDs, duplicate.AffectedNodeIDs...))
	existing.AffectedTypes = sortedUnique(append(existing.AffectedTypes, duplicate.AffectedTypes...))
	existing.AffectedInterfaces = sortedUnique(append(existing.AffectedInterfaces, duplicate.AffectedInterfaces...))
	existing.ActionIDs = sortedUnique(append(existing.ActionIDs, duplicate.ActionIDs...))
	if existing.Variant == nil {
		existing.Variant = duplicate.Variant
	}
	return existing
}
