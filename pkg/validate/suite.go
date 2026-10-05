package validate

import (
	"context"
	"fmt"
	"slices"
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

// RunSuiteOnce runs all selected checks once without applying fixes.
func RunSuiteOnce(ctx context.Context, opts Options) (Result, RunContext, error) {
	return runSuiteOnce(ctx, opts, nil, nil, false)
}

// RunSuiteOncePrepared executes selected checks against a caller-prepared
// ontology runtime. Validation projection owners use this entrypoint to avoid
// re-entering runtime refresh, reacquiring the index lock, or escaping a
// scratch projection. A non-nil runtimeErr is reported by runtime-backed
// checks through their ordinary CheckResult error contract.
func RunSuiteOncePrepared(ctx context.Context, opts Options, runtime *ontology.Runtime, runtimeErr error) (Result, RunContext, error) {
	return runSuiteOnce(ctx, opts, runtime, runtimeErr, true)
}

func runSuiteOnce(ctx context.Context, opts Options, preparedRuntime *ontology.Runtime, preparedRuntimeErr error, runtimePrepared bool) (Result, RunContext, error) {
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
		out := blockedRepairJournalResult(*runCtx, checks, pendingJournals, opts.ApplyCommand)
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
		if !runtimePrepared || len(checks) != 1 || checks[0] != CheckOntology {
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

	// Pre-resolve the ontology runtime once so the ontology and aliases
	// checks share it instead of each acquiring the index lock separately.
	sharedRuntime := preparedRuntime
	var sharedRuntimeCleanup func()
	sharedRuntimeErr := preparedRuntimeErr
	needsRuntime := registryNeedsOntologyRuntime(checks)
	if needsRuntime && !runtimePrepared && sharedRuntime == nil && sharedRuntimeErr == nil {
		rt, cleanup, err := ontology.EnsureFreshRuntime(ctx, executionCtx.NoteMetadata, executionCtx.VaultDef, executionCtx.NoteReader)
		if err == nil && rt != nil {
			sharedRuntime = rt
			sharedRuntimeCleanup = cleanup
		} else if err != nil {
			sharedRuntimeErr = err
		}
	}
	if sharedRuntimeCleanup != nil {
		defer sharedRuntimeCleanup()
	}

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
			resultsCh <- runCheckRecovered(ctx, executionCtx, opts, checkName, sharedRuntime, sharedRuntimeErr, runtimePrepared)
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
	// Enrich after key assignment and before BuildRepairPlan.
	if !opts.postApplyJournalValidation {
		if err := enrichOntologyRepairActions(ctx, executionCtx, results, sharedRuntime); err != nil {
			return Result{}, RunContext{}, fmt.Errorf("enrich ontology repair actions: %w", err)
		}
	}
	results = enrichRemediationActions(results)
	sort.SliceStable(results, func(i, j int) bool {
		return order[results[i].Name] < order[results[j].Name]
	})

	totalIssues := 0
	totalErrors := 0
	for _, result := range results {
		totalIssues += result.IssueCount
		if strings.TrimSpace(result.Error) != "" {
			totalErrors++
		}
	}

	fixPlan, err := BuildRepairPlan(ctx, *runCtx, results)
	if err != nil {
		return Result{}, RunContext{}, fmt.Errorf("build repair plan: %w", err)
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
		OK:                       totalIssues == 0 && totalErrors == 0 && len(visiblePendingJournals) == 0,
		IssueCount:               totalIssues,
		ErrorCount:               totalErrors,
		DurationMs:               time.Since(start).Milliseconds(),
		VaultName:                strings.TrimSpace(runCtx.VaultDef.Name),
		ApplyCommand:             strings.TrimSpace(opts.ApplyCommand),
		SelectedChecks:           checks,
		Checks:                   results,
		FixPlan:                  fixPlan,
		RepairJournals:           visiblePendingJournals,
		IdentifierReconciliation: identifierReconciliation,
	}
	out.NextActions = BuildNextActions(out)
	return out, *runCtx, nil
}

func blockedRepairJournalResult(runCtx RunContext, checks []string, journals []RepairJournalEvidence, applyCommand string) Result {
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
	return out
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

// RunCheck executes a single named validation check.
func RunCheck(ctx context.Context, runCtx RunContext, opts Options, name string) CheckResult {
	runCtx.MaxIssues = effectiveMaxIssues(runCtx, opts)
	start := time.Now()
	result := runRegisteredCheck(ctx, runCtx, opts, name, nil, nil, false)
	return finalizeCheckResult(name, result, time.Since(start), runCtx.MaxIssues)
}

func runRegisteredCheck(ctx context.Context, runCtx RunContext, opts Options, name string, runtime *ontology.Runtime, runtimeErr error, runtimePrepared bool) CheckResult {
	registration, ok := lookupCheck(name)
	if !ok {
		return CheckResult{Name: name, Error: "unknown check"}
	}
	if runtimePrepared {
		if registration.RuntimeRequirement == runtimeRequirementOntology && runtime == nil {
			if runtimeErr == nil {
				runtimeErr = fmt.Errorf("prepared ontology runtime is unavailable")
			}
			return sharedRuntimeError(name, runtimeErr)
		}
		if preparedCodeStoreRequired(registration) && (runtime == nil || runtime.Store == nil) {
			if runtimeErr == nil {
				runtimeErr = fmt.Errorf("prepared code index store is unavailable")
			}
			return sharedRuntimeError(name, runtimeErr)
		}
	}
	runCtx.MaxIssues = effectiveMaxIssues(runCtx, opts)
	return registration.Run(ctx, runCtx, opts, runtime, runtimeErr)
}

func preparedCodeStoreRequired(registration registeredCheck) bool {
	return registration.RuntimeRequirement == runtimeRequirementNone &&
		slices.Contains(registration.ProjectionDomains, ProjectionCode)
}

func runCheckRecovered(ctx context.Context, runCtx RunContext, opts Options, name string, sharedRuntime *ontology.Runtime, sharedRuntimeErr error, runtimePrepared bool) (result CheckResult) {
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
	return runRegisteredCheck(ctx, runCtx, opts, name, sharedRuntime, sharedRuntimeErr, runtimePrepared)
}

func effectiveMaxIssues(runCtx RunContext, opts Options) int {
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
	if result.IssueCount == 0 && strings.TrimSpace(result.Error) == "" && !result.Skipped {
		result.OK = true
	}
	result.DurationMs = elapsed.Milliseconds()
	return result
}
