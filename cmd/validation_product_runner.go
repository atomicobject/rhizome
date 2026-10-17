package cmd

import "github.com/spf13/cobra"

import (
	"context"
	"errors"
	"fmt"
	"strings"

	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/app/validationproduct"
	"github.com/atomicobject/rhizome/pkg/app/validationrun"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

type validationProjectionRun func(context.Context, validationrun.ValidationRunRequest, validationproduct.ValidationProjectionRequest, validate.ScanOptions) (validationrun.ValidationResult, error)
type validationRepairRun func(context.Context, validate.Result, validate.RunContext, validate.ApplyOptions) (validate.Result, *validate.FixExecution, error)

type productionValidationRunner struct {
	resolveVault    func(context.Context, string) (obsidian.VaultDefinition, error)
	newNoteMetadata func() (notemeta.Indexer, error)
	runProjection   validationProjectionRun
	runRepair       validationRepairRun
}

func newProductionValidationRunner() *productionValidationRunner {
	return &productionValidationRunner{
		resolveVault:    validationVaultDefinition,
		newNoteMetadata: newNoteMetadataIndexer,
		runProjection:   validationproduct.RunWithProjection,
		runRepair:       validate.ApplyRepairSession,
	}
}

func (runner *productionValidationRunner) Run(ctx context.Context, request validationProductRequest) (actions.ValidationResult, error) {
	if runner == nil || runner.resolveVault == nil || runner.newNoteMetadata == nil || runner.runProjection == nil {
		return actions.ValidationResult{}, fmt.Errorf("validation production runner is not configured")
	}
	vaultDef, err := runner.resolveVault(ctx, request.VaultName)
	if err != nil {
		return actions.ValidationResult{}, err
	}
	vaultDef, vaultPath, err := canonicalValidationVault(vaultDef)
	if err != nil {
		return actions.ValidationResult{}, err
	}
	noteMetadata, err := runner.newNoteMetadata()
	if err != nil {
		return actions.ValidationResult{}, err
	}

	prepared, err := validationrun.Prepare(vaultDef, noteMetadata, request.Selectors, normalizedValidationMaxIssues(request.MaxIssues))
	if err != nil {
		return actions.ValidationResult{}, err
	}
	runContext := &prepared.RunContext
	noteReader := runContext.NoteReader
	options := validationOptionsFromProductRequest(request, runContext)
	runRequest := prepared.Request
	runRequest.ApplyCommand = request.ApplyCommand
	runRequest.Surface = request.Surface
	projectionRequest := validationproduct.ValidationProjectionRequest{
		VaultPath:    vaultPath,
		VaultDef:     vaultDef,
		NoteMetadata: noteMetadata,
		NoteReader:   noteReader,
		Target:       validationProjectionTarget(request.Surface),
	}
	planned, err := runner.runProjection(ctx, runRequest, projectionRequest, options)
	if err != nil {
		return actions.ValidationResult{}, err
	}
	if !request.Repair {
		return planned, nil
	}
	planned.Result, err = validate.PrepareRepair(ctx, planned.Result, *runContext)
	if err != nil {
		return actions.ValidationResult{}, err
	}
	if !request.Apply {
		return planned, nil
	}
	if runner.runRepair == nil {
		return actions.ValidationResult{}, fmt.Errorf("validation repair runner is not configured")
	}

	applyOptions := validate.ApplyOptions{
		ScanOptions: options, Fix: true, NonInteractive: request.NonInteractive,
		Confirm: request.Confirm, ApplySelection: append([]string(nil), request.ApplySelection...),
		AllowHistorical: request.AllowHistorical,
		ReplanCommand:   actions.BuildValidationProductReplanCommand(request),
		PostApplyRefresher: validationproduct.ValidationProjectionPostApplyRefresher{
			VaultPath: vaultPath, VaultDef: vaultDef, NoteMetadata: noteMetadata, NoteReader: noteReader,
		},
	}
	if err := validateProductionRepairRoots(vaultPath, runContext, applyOptions.PostApplyRefresher); err != nil {
		return actions.ValidationResult{}, err
	}
	postcheck, execution, applyErr := runner.runRepair(ctx, planned.Result, *runContext, applyOptions)
	final, rebuildErr := validationrun.RebuildAfterRepair(planned, postcheck, execution)
	if rebuildErr != nil {
		return actions.ValidationResult{}, errors.Join(applyErr, rebuildErr)
	}
	if applyErr != nil {
		final.ExecutionError = applyErr.Error()
	}
	final.OK = final.ExitCode() == actions.ValidationExitClean
	return final, nil
}

func validationOptionsFromProductRequest(request validationProductRequest, runContext *validate.RunContext) validate.ScanOptions {
	return validate.ScanOptions{
		SkipAnchors:   request.SkipAnchors,
		SkipEmbeds:    request.SkipEmbeds,
		IncludeImages: request.IncludeImages,
		MaxIssues:     normalizedValidationMaxIssues(request.MaxIssues),
		ApplyCommand:  request.ApplyCommand,
		ScopeNote:     request.ScopeNote,
		ScopeTarget:   request.ScopeTarget,
		ScopeRef:      request.ScopeRef,
		RunContext:    runContext,
	}
}

func normalizedValidationMaxIssues(value int) int {
	if value <= 0 {
		return 20
	}
	return value
}

func validationProjectionTarget(surface validate.ExecutionSurface) validationproduct.ValidationProjectionTarget {
	if surface == validate.SurfaceCI {
		return validationproduct.ValidationProjectionScratch
	}
	return validationproduct.ValidationProjectionLive
}

func validationVaultDefinition(ctx context.Context, name string) (obsidian.VaultDefinition, error) {
	if strings.TrimSpace(name) != "" {
		return commandVault(ctx, name).Definition()
	}
	env := commandEnvFromContext(ctx)
	if cwd, err := env.Getwd(); err == nil {
		def, defErr := localVaultDefFromCWD(cwd)
		switch {
		case defErr == nil:
			return def, nil
		case defErr != nil && !errors.Is(defErr, repoconfig.ErrNoLocalConfig):
			return obsidian.VaultDefinition{}, defErr
		}
	}
	vault := commandVault(ctx, "")
	defaultName, err := vault.DefaultName()
	if err != nil {
		return obsidian.VaultDefinition{}, err
	}
	return commandVault(ctx, defaultName).Definition()
}

func canonicalValidationVault(def obsidian.VaultDefinition) (obsidian.VaultDefinition, string, error) {
	vaultPaths, err := paths.NewVaultPaths(def.BasePath())
	if err != nil {
		return obsidian.VaultDefinition{}, "", fmt.Errorf("resolve validation vault: %w", err)
	}
	root := vaultPaths.Root()
	if root == "" {
		return obsidian.VaultDefinition{}, "", fmt.Errorf("validation vault root is required")
	}
	if def.IsCollection() {
		def.Root = root
		def.Path = ""
	} else {
		def.Path = root
	}
	return def, root, nil
}

func validateProductionRepairRoots(vaultPath string, runContext *validate.RunContext, refresher validate.PostApplyRefresher) error {
	if runContext == nil || !sameCanonicalValidationRoot(runContext.VaultPath, vaultPath) || !sameCanonicalValidationRoot(runContext.VaultDef.BasePath(), vaultPath) {
		return fmt.Errorf("validation repair context does not match canonical vault root %q", vaultPath)
	}
	concrete, ok := refresher.(validationproduct.ValidationProjectionPostApplyRefresher)
	if !ok || !sameCanonicalValidationRoot(concrete.VaultPath, vaultPath) || !sameCanonicalValidationRoot(concrete.VaultDef.BasePath(), vaultPath) {
		return fmt.Errorf("validation post-apply refresher does not match canonical vault root %q", vaultPath)
	}
	return nil
}

func sameCanonicalValidationRoot(left, right string) bool {
	leftPaths, leftErr := paths.NewVaultPaths(left)
	rightPaths, rightErr := paths.NewVaultPaths(right)
	return leftErr == nil && rightErr == nil && leftPaths.Root() != "" && paths.CaseEqual(leftPaths.Root(), rightPaths.Root())
}

func (commandOptions *vaultCommandOptions) registerValidationProductRunnerCommands(rootCmd *cobra.Command) {
	runner := newProductionValidationRunner()
	rootCmd.AddCommand(commandOptions.newValidateProductCmdWithRunner(runner))
	rootCmd.AddCommand(newCIProductCmdWithRunner(runner))
}
