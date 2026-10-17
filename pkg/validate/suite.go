package validate

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/validate/identifierreconcile"
)

func resultIssueKeys(result Result) []string {
	var keys []string
	for _, check := range result.Checks {
		if len(check.allIssueKeys) > 0 {
			keys = append(keys, check.allIssueKeys...)
			continue
		}
		for _, issue := range check.Issues {
			keys = append(keys, issue.Key)
		}
	}
	return sortedUnique(keys)
}

func mergePostApplyRemainingEvidence(exec *FixExecution, result Result) {
	if exec == nil {
		return
	}
	priorUnkeyedFindings := exec.RemainingFindings - len(exec.RemainingIssueKeys)
	if priorUnkeyedFindings < 0 {
		priorUnkeyedFindings = 0
	}
	postcheckKeys := resultIssueKeys(result)
	exec.RemainingIssueKeys = sortedUnique(append(exec.RemainingIssueKeys, postcheckKeys...))
	unkeyedPostcheckFindings := result.IssueCount - len(postcheckKeys)
	if unkeyedPostcheckFindings < 0 {
		unkeyedPostcheckFindings = 0
	}
	exec.RemainingFindings = len(exec.RemainingIssueKeys) + priorUnkeyedFindings + unkeyedPostcheckFindings
}

// RunSuiteOncePrepared executes selected checks against a caller-prepared runtime.
// Runtime-backed checks fail when their prepared dependency is unavailable.
func RunSuiteOncePrepared(ctx context.Context, opts ScanOptions, preparedRuntime *ontology.Runtime, preparedRuntimeErr error) (Result, RunContext, error) {
	checks, err := ResolveChecks(opts.Checks)
	if err != nil {
		return Result{}, RunContext{}, err
	}
	if opts.MaxIssues <= 0 {
		opts.MaxIssues = 20
	}
	runCtx := opts.RunContext
	if runCtx == nil {
		return Result{}, RunContext{}, fmt.Errorf("RunContext is required")
	}
	if err := runCtx.NoteMetadata.Validate(); err != nil {
		return Result{}, RunContext{}, fmt.Errorf("RunContext.NoteMetadata: %w", err)
	}
	runCtx.MaxIssues = effectiveMaxIssues(*runCtx, opts)
	pendingJournals, err := DetectPendingRepairJournals(*runCtx)
	if err != nil {
		return Result{}, RunContext{}, fmt.Errorf("inspect repair journals: %w", err)
	}
	if len(pendingJournals) > 0 && !opts.postApplyJournalValidation {
		out, _ := RepairRecoveryOutcome(*runCtx, checks, pendingJournals, opts.ApplyCommand)
		return out, *runCtx, nil
	}

	start := time.Now()
	executionCtx := *runCtx
	executionCtx.unresolvedLinks = &sharedUnresolvedLinkScan{}
	if len(opts.postcheckPaths) > 0 {
		executionCtx.postcheckPaths = make(map[string]struct{}, len(opts.postcheckPaths))
		for _, notePath := range opts.postcheckPaths {
			executionCtx.postcheckPaths[notePath] = struct{}{}
		}
	}
	if checksNeedValidationRunSnapshot(checks) {
		snapshotCtx := executionCtx
		// Other checks still need whole-vault candidate and source inventories.
		if len(checks) != 1 || checks[0] != CheckOntology {
			snapshotCtx.postcheckPaths = nil
		}
		sourceSnapshot, err := captureValidationRunSnapshot(ctx, snapshotCtx)
		if err != nil {
			return Result{}, RunContext{}, err
		}
		executionCtx.sourceSnapshot = sourceSnapshot
		executionCtx.brokenLinkCandidates = &sourceSnapshot.candidates
		executionCtx.NoteReader = sourceSnapshot.reader(runCtx.NoteReader)
	}

	sharedRuntime := preparedRuntime
	sharedRuntimeErr := preparedRuntimeErr

	resultsCh := make(chan CheckResult, len(checks))
	var wg sync.WaitGroup
	workers := 4
	if workers > len(checks) {
		workers = len(checks)
	}
	if workers < 1 {
		workers = 1
	}
	sem := make(chan struct{}, workers)
	for _, checkName := range checks {
		checkName := checkName
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			resultsCh <- runCheckRecovered(ctx, executionCtx, opts, checkName, sharedRuntime, sharedRuntimeErr)
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return Result{}, RunContext{}, err
	}
	close(resultsCh)

	order := make(map[string]int, len(checks))
	for i, name := range checks {
		order[name] = i
	}
	results := make([]CheckResult, 0, len(checks))
	for result := range resultsCh {
		result = normalizeCheckResult(result)
		results = append(results, result)
	}
	results, err = attachStableRepairIssueKeys(results)
	if err != nil {
		return Result{}, RunContext{}, fmt.Errorf("assign stable validation issue keys: %w", err)
	}
	// Ontology repair suggestions depend on complete assessment context, but
	// action membership must bind only to the stable public issue identity.
	// Enrich after key assignment without preparing source-backed operations.
	if !opts.postApplyJournalValidation {
		if err := enrichOntologyRepairActions(ctx, executionCtx, results, sharedRuntime); err != nil {
			return Result{}, RunContext{}, fmt.Errorf("enrich ontology repair actions: %w", err)
		}
	}
	results = enrichRemediationActions(results)
	sort.SliceStable(results, func(i, j int) bool {
		return order[results[i].Name] < order[results[j].Name]
	})

	for i := range results {
		foldIdentifierAliasMirrorActions(&results[i])
	}
	fixPlan, err := BuildFixPlan(results)
	if err != nil {
		return Result{}, RunContext{}, fmt.Errorf("summarize repair actions: %w", err)
	}
	var preparation *repairPreparation
	if fixPlan != nil {
		preparation, err = captureRepairPreparation(ctx, executionCtx, sharedRuntime, results)
		if err != nil {
			return Result{}, RunContext{}, err
		}
		fixPlan.PreparationRequired = true
		fixPlan.AuthorityFingerprint = preparation.fingerprint
		fingerprint, err := RepairPlanFingerprint(*fixPlan)
		if err != nil {
			return Result{}, RunContext{}, err
		}
		fixPlan.Fingerprint = fingerprint
		preparation.summaryFingerprint = fixPlan.Fingerprint
	}
	visiblePendingJournals := pendingJournals
	if opts.postApplyJournalValidation {
		visiblePendingJournals = nil
	}
	var identifierReconciliation *identifierreconcile.ReconciliationResult
	for _, check := range results {
		if check.identifierReconciliation != nil {
			identifierReconciliation = check.identifierReconciliation
			break
		}
	}
	out := Result{
		OK: false,

		DurationMs:               time.Since(start).Milliseconds(),
		VaultName:                strings.TrimSpace(runCtx.VaultDef.Name),
		ApplyCommand:             strings.TrimSpace(opts.ApplyCommand),
		SelectedChecks:           checks,
		Checks:                   results,
		FixPlan:                  fixPlan,
		RepairJournals:           visiblePendingJournals,
		IdentifierReconciliation: identifierReconciliation,
		repairPreparation:        preparation,
	}
	out.SummarizeChecks()
	out.NextActions = BuildNextActions(out)
	return out, *runCtx, nil
}

// RepairRecoveryOutcome blocks every selected check with one exact recovery command.
func RepairRecoveryOutcome(runCtx RunContext, checks []string, journals []RepairJournalEvidence, applyCommand string) (Result, []CheckApplicabilityResult) {
	recoveryChecks := mergeRepairRecoveryChecks(checks, journals)
	recoveryCommand := strings.TrimSpace(applyCommand)
	if recoveryCommand == "" {
		recoveryCommand = buildSafeAutoFixCommand(runCtx.VaultDef.Name, recoveryChecks)
	}
	out := Result{
		OK:             false,
		ErrorCount:     1,
		VaultName:      strings.TrimSpace(runCtx.VaultDef.Name),
		ApplyCommand:   strings.TrimSpace(applyCommand),
		SelectedChecks: checks,
		RepairJournals: journals,
	}
	out.NextActions = &NextActions{
		CheckErrorCount: 1,
		Actions: []NextAction{{
			Category: "repair_recovery",
			Title:    "Recover interrupted validation repairs",
			Message:  "Run the recovery command, then replan fixes from a fresh validation result.",
			Command:  recoveryCommand,
			Count:    len(journals),
		}},
	}
	outcomes := make([]CheckApplicabilityResult, 0, len(checks))
	for _, check := range checks {
		outcomes = append(outcomes, CheckApplicabilityResult{
			Check: check, Outcome: CheckOutcomeBlocked,
			Summary:            "pending repair journal must be recovered before validation can inspect or refresh projections",
			PreparationCommand: recoveryCommand,
			Evidence:           []ApplicabilityEvidence{{Code: "pending_repair_journal", Message: "Interrupted repair evidence is present; recover it before running validation."}},
		})
	}
	return out, outcomes
}

// SummarizeChecks derives totals from completed check results without losing
// their full counts when visible issues have been truncated.
func (result *Result) SummarizeChecks() {
	result.IssueCount = 0
	result.ErrorCount = 0
	for _, check := range result.Checks {
		result.IssueCount += check.IssueCount
		if strings.TrimSpace(check.Error) != "" {
			result.ErrorCount++
		}
	}
	result.OK = result.IssueCount == 0 && result.ErrorCount == 0 && len(result.RepairJournals) == 0
}

func mergeRepairRecoveryChecks(selected []string, journals []RepairJournalEvidence) []string {
	checks := append([]string(nil), selected...)
	for _, journal := range journals {
		checks = append(checks, journal.RequiredChecks...)
	}
	return sortedUnique(checks)
}

func normalizeCheckResult(result CheckResult) CheckResult {
	if result.IssueCount > 0 || strings.TrimSpace(result.Error) != "" {
		result.OK = false
	}
	return result
}

func runRegisteredCheck(ctx context.Context, runCtx RunContext, opts ScanOptions, name string, runtime *ontology.Runtime, runtimeErr error) CheckResult {
	registration, ok := lookupCheck(name)
	if !ok {
		return CheckResult{Name: name, Error: "unknown check"}
	}
	if registration.Needs.Ontology && runtime == nil {
		if runtimeErr == nil {
			runtimeErr = fmt.Errorf("prepared ontology runtime is unavailable")
		}
		return sharedRuntimeError(name, runtimeErr)
	}
	if registration.Needs.CodeStore && (runtime == nil || runtime.Store == nil) {
		if runtimeErr == nil {
			runtimeErr = fmt.Errorf("prepared code index store is unavailable")
		}
		return sharedRuntimeError(name, runtimeErr)
	}
	runCtx.MaxIssues = effectiveMaxIssues(runCtx, opts)
	return registration.Run(ctx, runCtx, opts, runtime)
}

func runCheckRecovered(ctx context.Context, runCtx RunContext, opts ScanOptions, name string, sharedRuntime *ontology.Runtime, sharedRuntimeErr error) (result CheckResult) {
	runCtx.MaxIssues = effectiveMaxIssues(runCtx, opts)
	start := time.Now()
	defer func() {
		if r := recover(); r != nil {
			result = CheckResult{
				Name:  name,
				OK:    false,
				Error: fmt.Sprintf("check panicked: %v", r),
			}
		}
		result = finalizeCheckResult(name, result, time.Since(start), runCtx.MaxIssues)
	}()
	return runRegisteredCheck(ctx, runCtx, opts, name, sharedRuntime, sharedRuntimeErr)
}

func effectiveMaxIssues(runCtx RunContext, opts ScanOptions) int {
	if runCtx.MaxIssues > 0 {
		return runCtx.MaxIssues
	}
	if opts.MaxIssues > 0 {
		return opts.MaxIssues
	}
	return 20
}

func finalizeCheckResult(name string, result CheckResult, elapsed time.Duration, maxIssues int) CheckResult {
	result.Name = name
	result.IssueCount = len(result.Issues)
	result.fullIssues = append([]Issue(nil), result.Issues...)
	result.allIssueKeys = nil
	for i := range result.fullIssues {
		key, err := StableIssueKey(name, result.fullIssues[i])
		if err != nil {
			result.OK = false
			result.Error = fmt.Sprintf("identify %s issue: %v", name, err)
			result.IssueCount = 0
			result.Issues = nil
			result.fullIssues = nil
			result.Fixes = nil
			result.allIssueKeys = nil
			break
		}
		result.allIssueKeys = append(result.allIssueKeys, key)
	}
	result.allIssueKeys = sortedUnique(result.allIssueKeys)
	if maxIssues <= 0 {
		maxIssues = 20
	}
	if len(result.Issues) > maxIssues {
		result.Issues = result.Issues[:maxIssues]
	}
	result = normalizeCheckResult(result)
	if result.IssueCount == 0 && strings.TrimSpace(result.Error) == "" && result.NotApplicable == "" {
		result.OK = true
	}
	result.DurationMs = elapsed.Milliseconds()
	return result
}
