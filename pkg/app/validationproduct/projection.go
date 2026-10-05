// Package validationproduct composes validation product orchestration across
// the CLI contract, lightweight projection, and prepared suite execution.
package validationproduct

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/atomicobject/rhizome/pkg/app/indexing"
	"github.com/atomicobject/rhizome/pkg/app/validationrun"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type projectionRefresh func(context.Context, indexing.ValidationProjectionRequest) (*indexing.ValidationProjectionResult, error)

var errPendingRepairJournal = errors.New("pending repair journal appeared before validation projection mutation")

type projectionAdapter struct {
	request        indexing.ValidationProjectionRequest
	codeConfigured bool
	refresh        projectionRefresh

	once   sync.Once
	result *indexing.ValidationProjectionResult
	err    error
}

func newProjectionAdapter(request indexing.ValidationProjectionRequest, codeConfigured bool, refresh projectionRefresh) *projectionAdapter {
	return &projectionAdapter{request: request, codeConfigured: codeConfigured, refresh: refresh}
}

// RunWithProjection refreshes one lightweight validation projection, resolves
// applicability from its evidence, and executes completed checks against the
// same prepared runtime. It is deliberately read-only; repair/apply owns a
// separate lease-bound orchestration path.
func RunWithProjection(
	ctx context.Context,
	request validationrun.ValidationRunRequest,
	projectionRequest indexing.ValidationProjectionRequest,
	options validate.Options,
) (validationrun.ValidationResult, error) {
	return runWithProjection(ctx, request, projectionRequest, options, indexing.RefreshValidationProjection)
}

func runWithProjection(
	ctx context.Context,
	request validationrun.ValidationRunRequest,
	projectionRequest indexing.ValidationProjectionRequest,
	options validate.Options,
	refresh projectionRefresh,
) (validationrun.ValidationResult, error) {
	if options.Fix {
		return validationrun.ValidationResult{}, fmt.Errorf("projected validation run is read-only")
	}
	request.Surface = normalizedSurface(request.Surface)
	projectionRequest.Target = normalizedProjectionTarget(request.Surface, projectionRequest.Target)
	if err := validateProjectionTarget(request.Surface, projectionRequest.Target); err != nil {
		return validationrun.ValidationResult{}, err
	}
	if err := projectionRequest.NoteMetadata.Validate(); err != nil {
		return validationrun.ValidationResult{}, fmt.Errorf("validation projection note metadata indexer: %w", err)
	}

	if options.RunContext == nil {
		options.RunContext = projectionRunContext(projectionRequest, options.MaxIssues)
	} else if err := validateProjectionRunContext(projectionRequest, *options.RunContext); err != nil {
		return validationrun.ValidationResult{}, err
	}
	if request.ApplyCommand != "" {
		options.ApplyCommand = request.ApplyCommand
	}
	journalResult, blocked, err := pendingRepairJournalResult(ctx, request, options)
	if err != nil {
		return validationrun.ValidationResult{}, err
	}
	if blocked {
		return journalResult, nil
	}
	existingBarrier := projectionRequest.BeforeMutation
	projectionRequest.BeforeMutation = func(ctx context.Context) error {
		if existingBarrier != nil {
			if err := existingBarrier(ctx); err != nil {
				return err
			}
		}
		journals, err := validate.DetectPendingRepairJournals(*options.RunContext)
		if err != nil {
			return fmt.Errorf("inspect repair journals under projection lock: %w", err)
		}
		if len(journals) > 0 {
			return errPendingRepairJournal
		}
		return nil
	}

	adapter := newProjectionAdapter(projectionRequest, request.Features.CodeConfigured, refresh)
	request.Probes.Projection = adapter
	request.Probes.CodeIndex = adapter

	result, runErr := validationrun.RunValidation(ctx, request, func(ctx context.Context, checks []string) (validate.Result, error) {
		return adapter.runSuite(ctx, options, checks)
	})
	closeErr := adapter.Close()
	if errors.Is(runErr, errPendingRepairJournal) || errors.Is(adapter.err, errPendingRepairJournal) {
		journalResult, blocked, journalErr := pendingRepairJournalResult(ctx, request, options)
		if journalErr != nil {
			return validationrun.ValidationResult{}, errors.Join(journalErr, closeErr)
		}
		if !blocked {
			return validationrun.ValidationResult{}, errors.Join(
				fmt.Errorf("repair journal state changed after the locked validation barrier; retry validation"),
				closeErr,
			)
		}
		return journalResult, closeErr
	}
	return result, errors.Join(runErr, closeErr)
}

func validateProjectionRunContext(request indexing.ValidationProjectionRequest, runContext validate.RunContext) error {
	if err := runContext.NoteMetadata.Validate(); err != nil {
		return fmt.Errorf("validation RunContext note metadata indexer: %w", err)
	}
	requestRoot := request.VaultDef.BasePath()
	if requestRoot == "" {
		requestRoot = request.VaultPath
	}
	requestPaths, err := paths.NewVaultPaths(requestRoot)
	if err != nil {
		return fmt.Errorf("resolve projection request root: %w", err)
	}
	contextPaths, err := paths.NewVaultPaths(runContext.VaultPath)
	if err != nil {
		return fmt.Errorf("resolve validation run context root: %w", err)
	}
	if requestPaths.Root() == "" || contextPaths.Root() == "" || !paths.CaseEqual(requestPaths.Root(), contextPaths.Root()) {
		return fmt.Errorf("validation RunContext root %q does not match projection root %q", contextPaths.Root(), requestPaths.Root())
	}
	if runContext.VaultDef.BasePath() == "" {
		return fmt.Errorf("validation RunContext vault definition root is required")
	}
	definitionPaths, err := paths.NewVaultPaths(runContext.VaultDef.BasePath())
	if err != nil {
		return fmt.Errorf("resolve validation RunContext definition root: %w", err)
	}
	if !paths.CaseEqual(definitionPaths.Root(), requestPaths.Root()) {
		return fmt.Errorf("validation RunContext definition root %q does not match projection root %q", definitionPaths.Root(), requestPaths.Root())
	}
	return nil
}

func pendingRepairJournalResult(
	ctx context.Context,
	request validationrun.ValidationRunRequest,
	options validate.Options,
) (validationrun.ValidationResult, bool, error) {
	if options.RunContext == nil {
		return validationrun.ValidationResult{}, false, fmt.Errorf("validation RunContext is required")
	}
	if options.RunContext.VaultPath == "" {
		return validationrun.ValidationResult{}, false, nil
	}
	journals, err := validate.DetectPendingRepairJournals(*options.RunContext)
	if err != nil {
		return validationrun.ValidationResult{}, false, fmt.Errorf("inspect repair journals: %w", err)
	}
	if len(journals) == 0 {
		return validationrun.ValidationResult{}, false, nil
	}

	selection, err := validate.ResolveSelection(request.Selectors, request.Config)
	if err != nil {
		return validationrun.ValidationResult{}, true, err
	}
	options.Checks = append([]string(nil), selection.Checks...)
	if request.ApplyCommand != "" {
		options.ApplyCommand = request.ApplyCommand
	}
	suite, _, err := validate.RunSuiteOncePrepared(ctx, options, nil, nil)
	if err != nil {
		return validationrun.ValidationResult{}, true, err
	}
	if len(suite.RepairJournals) == 0 {
		return validationrun.ValidationResult{}, true, fmt.Errorf("repair journal state changed while establishing the validation barrier; retry validation")
	}

	recoveryCommand := ""
	if suite.NextActions != nil {
		for _, action := range suite.NextActions.Actions {
			if action.Category == "repair_recovery" {
				recoveryCommand = action.Command
				break
			}
		}
	}
	if recoveryCommand == "" {
		return validationrun.ValidationResult{}, true, fmt.Errorf("pending repair journal result omitted its recovery command")
	}

	applicability := make([]validate.CheckApplicabilityResult, 0, len(selection.Checks))
	for _, check := range selection.Checks {
		applicability = append(applicability, validate.CheckApplicabilityResult{
			Check:              check,
			Outcome:            validate.CheckOutcomeBlocked,
			Summary:            "pending repair journal must be recovered before validation can inspect or refresh projections",
			PreparationCommand: recoveryCommand,
			Evidence: []validate.ApplicabilityEvidence{{
				Code:    "pending_repair_journal",
				Message: "Interrupted repair evidence is present; recover it before running validation.",
			}},
		})
	}
	result, err := validationrun.BuildValidationResult(selection, applicability, suite)
	return result, true, err
}

func normalizedSurface(surface validate.ExecutionSurface) validate.ExecutionSurface {
	if surface == "" {
		return validate.SurfaceLocal
	}
	return surface
}

func normalizedProjectionTarget(surface validate.ExecutionSurface, target indexing.ValidationProjectionTarget) indexing.ValidationProjectionTarget {
	if target != "" {
		return target
	}
	if surface == validate.SurfaceCI {
		return indexing.ValidationProjectionScratch
	}
	return indexing.ValidationProjectionLive
}

func validateProjectionTarget(surface validate.ExecutionSurface, target indexing.ValidationProjectionTarget) error {
	if surface == validate.SurfaceCI && target != indexing.ValidationProjectionScratch {
		return fmt.Errorf("CI validation requires a scratch projection")
	}
	if surface != validate.SurfaceCI && target != indexing.ValidationProjectionLive {
		return fmt.Errorf("%s validation requires the live projection", surface)
	}
	return nil
}

func projectionRunContext(request indexing.ValidationProjectionRequest, maxIssues int) *validate.RunContext {
	vaultDef := request.VaultDef
	if vaultDef.BasePath() == "" {
		vaultDef.Path = request.VaultPath
	}
	reader := request.NoteReader
	if reader == nil {
		reader = &obsidian.Note{}
	}
	if maxIssues <= 0 {
		maxIssues = 20
	}
	return &validate.RunContext{
		VaultDef:     vaultDef,
		VaultPath:    vaultDef.BasePath(),
		VaultMgr:     &projectionVaultManager{def: vaultDef},
		NoteReader:   reader,
		NoteMetadata: request.NoteMetadata,
		MaxIssues:    maxIssues,
	}
}

func (a *projectionAdapter) ensure(ctx context.Context) (*indexing.ValidationProjectionResult, error) {
	a.once.Do(func() {
		if a.refresh == nil {
			a.err = fmt.Errorf("validation projection refresher is not configured")
			return
		}
		a.result, a.err = a.refresh(ctx, a.request)
		if a.err == nil && a.result == nil {
			a.err = fmt.Errorf("validation projection refresh returned no result")
		}
	})
	return a.result, a.err
}

func (a *projectionAdapter) AutoManagedProjection(ctx context.Context, _ validate.CheckDescriptor, domain validate.ProjectionDomain) (validate.AutoManagedProjectionSnapshot, error) {
	result, err := a.ensure(ctx)
	snapshot := validate.AutoManagedProjectionSnapshot{Domain: domain}
	if err != nil {
		return snapshot, err
	}
	snapshot.Refreshed = true
	switch domain {
	case validate.ProjectionValidation:
		snapshot.Available = projectionDomainsFresh(result,
			indexing.ProjectionDomainMetadata,
			indexing.ProjectionDomainLinks,
			indexing.ProjectionDomainMarkdownTargets,
		)
	case validate.ProjectionOntology:
		snapshot.Available = result.Runtime != nil && projectionDomainsFresh(result, indexing.ProjectionDomainOntology)
	default:
		return snapshot, fmt.Errorf("unsupported auto-managed projection domain %q", domain)
	}
	return snapshot, nil
}

func projectionDomainsFresh(result *indexing.ValidationProjectionResult, domains ...indexing.ProjectionDomain) bool {
	if result == nil {
		return false
	}
	for _, domain := range domains {
		if result.Freshness[domain].State != indexing.ProjectionFresh {
			return false
		}
	}
	return true
}

func (a *projectionAdapter) CodeIndexSnapshot(ctx context.Context) (validate.CodeIndexSnapshot, error) {
	snapshot := validate.CodeIndexSnapshot{ConfiguredCapability: a.codeConfigured}
	result, err := a.ensure(ctx)
	if err != nil {
		return snapshot, err
	}
	if a.request.Target == indexing.ValidationProjectionScratch {
		return snapshot, nil
	}
	if result.Runtime == nil || result.Runtime.Store == nil {
		return snapshot, nil
	}
	snapshot.StorePresent = true
	stored, err := result.Runtime.Store.CodeIndexPrerequisiteSnapshot(ctx)
	if err != nil {
		return snapshot, err
	}
	snapshot.RowsPresent = stored.CodeRowsPresent
	snapshot.IndexedFileCount = stored.IndexedFileCount
	if stored.IndexerVersionPresent {
		snapshot.IndexerVersion = stored.IndexerVersion
	}
	if stored.ScopeConfigHashPresent {
		snapshot.ScopeHash = stored.ScopeConfigHash
	}
	if stored.IndexedAtPresent {
		snapshot.IndexedAt = stored.IndexedAt
	}
	return snapshot, nil
}

func (a *projectionAdapter) runSuite(ctx context.Context, options validate.Options, checks []string) (validate.Result, error) {
	result, err := a.ensure(ctx)
	if err != nil {
		return validate.Result{}, err
	}
	options.Checks = append([]string(nil), checks...)
	suite, _, err := validate.RunSuiteOncePrepared(ctx, options, result.Runtime, nil)
	return suite, err
}

func (a *projectionAdapter) Close() error {
	if a == nil || a.result == nil {
		return nil
	}
	return a.result.Close()
}

type projectionVaultManager struct {
	def obsidian.VaultDefinition
}

func (m *projectionVaultManager) DefaultName() (string, error) { return m.def.Name, nil }
func (m *projectionVaultManager) SetDefaultName(name string) error {
	m.def.Name = name
	return nil
}
func (m *projectionVaultManager) Path() (string, error) { return m.def.BasePath(), nil }
func (m *projectionVaultManager) Definition() (obsidian.VaultDefinition, error) {
	return m.def, nil
}
