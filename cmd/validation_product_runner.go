package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/app/indexing"
	"github.com/atomicobject/rhizome/pkg/app/validationproduct"
	"github.com/atomicobject/rhizome/pkg/app/validationrun"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type validationProjectionRun func(context.Context, validationrun.ValidationRunRequest, indexing.ValidationProjectionRequest, validate.Options) (validationrun.ValidationResult, error)
type validationRepairRun func(context.Context, validate.Result, validate.RunContext, validate.Options) (validate.Result, *validate.FixExecution, error)

type productionValidationRunner struct {
	resolveVault    func(context.Context, string) (obsidian.VaultDefinition, error)
	loadConfig      func(string) (*obsidian.LocalConfig, error)
	pathExists      func(string) (bool, error)
	newNoteMetadata func() (notemeta.Indexer, error)
	runProjection   validationProjectionRun
	runRepair       validationRepairRun
}

func newProductionValidationRunner() *productionValidationRunner {
	return &productionValidationRunner{
		resolveVault:    validationVaultDefinition,
		loadConfig:      loadValidationLocalConfig,
		pathExists:      validationPathExists,
		newNoteMetadata: newNoteMetadataIndexer,
		runProjection:   validationproduct.RunWithProjection,
		runRepair:       validate.ApplyRepairSession,
	}
}

func (runner *productionValidationRunner) Run(ctx context.Context, request validationProductRequest) (actions.ValidationResult, error) {
	if runner == nil || runner.resolveVault == nil || runner.loadConfig == nil || runner.pathExists == nil || runner.newNoteMetadata == nil || runner.runProjection == nil {
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
	local, err := runner.loadConfig(vaultPath)
	if err != nil {
		return actions.ValidationResult{}, fmt.Errorf("load validation config: %w", err)
	}
	config := actions.ValidationSuiteConfigFromLocal(local.Validation)
	selection, err := validate.ResolveSelection(request.Selectors, config)
	if err != nil {
		return actions.ValidationResult{}, err
	}
	features, err := validationVaultFeatureFacts(vaultPath, local, runner.pathExists, slices.Contains(selection.Checks, validate.CheckQueryRecipes))
	if err != nil {
		return actions.ValidationResult{}, err
	}
	noteMetadata, err := runner.newNoteMetadata()
	if err != nil {
		return actions.ValidationResult{}, err
	}

	noteReader := &obsidian.Note{}
	runContext := &validate.RunContext{
		VaultDef:     vaultDef,
		VaultPath:    vaultPath,
		VaultMgr:     &fixedVaultDefinition{def: vaultDef},
		NoteReader:   noteReader,
		NoteMetadata: noteMetadata,
		MaxIssues:    normalizedValidationMaxIssues(request.MaxIssues),
	}
	options := validationOptionsFromProductRequest(request, runContext)
	runRequest := validationrun.ValidationRunRequest{
		Selectors:    append([]string(nil), request.Selectors...),
		ApplyCommand: request.ApplyCommand,
		Config:       config,
		Surface:      request.Surface,
		Features:     features,
	}
	projectionRequest := indexing.ValidationProjectionRequest{
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
	if !request.Repair || !request.Apply {
		return planned, nil
	}
	if runner.runRepair == nil {
		return actions.ValidationResult{}, fmt.Errorf("validation repair runner is not configured")
	}

	options.Fix = true
	options.NonInteractive = request.NonInteractive
	options.Confirm = request.Confirm
	options.ApplySelection = append([]string(nil), request.ApplySelection...)
	options.AllowHistorical = request.AllowHistorical
	options.ReplanCommand = strings.Replace(request.ApplyCommand, " --apply", "", 1)
	options.PostApplyRefresher = indexing.ValidationProjectionPostApplyRefresher{
		VaultPath:    vaultPath,
		VaultDef:     vaultDef,
		NoteMetadata: noteMetadata,
		NoteReader:   noteReader,
	}
	if err := validateProductionRepairRoots(vaultPath, runContext, options.PostApplyRefresher); err != nil {
		return actions.ValidationResult{}, err
	}
	postcheck, execution, applyErr := runner.runRepair(ctx, planned.Result, *runContext, options)
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

func validationOptionsFromProductRequest(request validationProductRequest, runContext *validate.RunContext) validate.Options {
	return validate.Options{
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

func validationProjectionTarget(surface validate.ExecutionSurface) indexing.ValidationProjectionTarget {
	if surface == validate.SurfaceCI {
		return indexing.ValidationProjectionScratch
	}
	return indexing.ValidationProjectionLive
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
		case defErr != nil && !errors.Is(defErr, obsidian.ErrNoLocalConfig):
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

func loadValidationLocalConfig(vaultPath string) (*obsidian.LocalConfig, error) {
	local, err := obsidian.LoadLocalConfig(vaultPath)
	if errors.Is(err, obsidian.ErrNoLocalConfig) {
		return &obsidian.LocalConfig{}, nil
	}
	return local, err
}

func validationVaultFeatureFacts(vaultPath string, local *obsidian.LocalConfig, exists func(string) (bool, error), includeQueryRecipes bool) (validate.VaultFeatureFacts, error) {
	if local == nil {
		local = &obsidian.LocalConfig{}
	}
	ontologyConfigured, err := exists(filepath.Join(vaultPath, ".rhizome", "ontology"))
	if err != nil {
		return validate.VaultFeatureFacts{}, fmt.Errorf("inspect ontology configuration: %w", err)
	}
	queryRecipesConfigured := includeQueryRecipes && queryrecipe.HasDefaultSources(vaultPath)
	viewsConfigured, err := exists(filepath.Join(vaultPath, ".rhizome", "views"))
	if err != nil {
		return validate.VaultFeatureFacts{}, fmt.Errorf("inspect view configuration: %w", err)
	}
	effortsPresent, err := exists(filepath.Join(vaultPath, "docs", "efforts"))
	if err != nil {
		return validate.VaultFeatureFacts{}, fmt.Errorf("inspect effort configuration: %w", err)
	}
	codeConfigured := validationCodeConfigured(local.Code)
	facts := validate.VaultFeatureFacts{
		OntologyConfigured:        ontologyConfigured,
		CodeConfigured:            codeConfigured,
		CodeAnchorRootsConfigured: obsidian.CodeAnchorRootsConfigured(local.Code),
		QueryRecipesConfigured:    queryRecipesConfigured,
		ViewsConfigured:           viewsConfigured,
		EffortsPresent:            effortsPresent,
	}
	if codeConfigured {
		facts.RequiredCodeIndexerVersion = codeanchor.IndexerVersion
		facts.RequiredCodeScopeHash = local.ScopeConfigHash()
	}
	return facts, nil
}

func validationCodeConfigured(code obsidian.LocalCodeConfig) bool {
	return code.Enabled || len(code.Scan) > 0 || code.Python != nil || code.Go != nil ||
		code.TypeScript != nil || code.JavaScript != nil || code.CSharp != nil || code.PHP != nil
}

func validationPathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func validateProductionRepairRoots(vaultPath string, runContext *validate.RunContext, refresher validate.PostApplyRefresher) error {
	if runContext == nil || !sameCanonicalValidationRoot(runContext.VaultPath, vaultPath) || !sameCanonicalValidationRoot(runContext.VaultDef.BasePath(), vaultPath) {
		return fmt.Errorf("validation repair context does not match canonical vault root %q", vaultPath)
	}
	concrete, ok := refresher.(indexing.ValidationProjectionPostApplyRefresher)
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

func init() {
	runner := newProductionValidationRunner()
	rootCmd.AddCommand(newValidateProductCmdWithRunner(runner))
	rootCmd.AddCommand(newCIProductCmdWithRunner(runner))
}
