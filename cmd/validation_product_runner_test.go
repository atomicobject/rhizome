package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/indexing"
	"github.com/atomicobject/rhizome/pkg/app/validationrun"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestProductionValidationRunnerBuildsOneScratchCIRuntimeFromStrictConfig(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{
		filepath.Join(root, ".rhizome", "ontology"),
		filepath.Join(root, ".rhizome", "query-recipes"),
		filepath.Join(root, ".rhizome", "views"),
		filepath.Join(root, "docs", "efforts"),
	} {
		require.NoError(t, os.MkdirAll(path, 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "query-recipes", "example.md"), []byte(projectRecipeMarkdown()), 0o644))
	local := obsidian.LocalConfig{
		Code: obsidian.LocalCodeConfig{Go: &obsidian.LocalCodeLangConfig{Roots: []string{"."}}},
		Validation: obsidian.LocalValidationConfig{
			Default: obsidian.LocalValidationSuiteConfig{Add: []string{"link-hygiene"}},
		},
	}
	require.NoError(t, obsidian.SaveLocalConfig(root, local))

	var gotRun validationrun.ValidationRunRequest
	var gotProjection indexing.ValidationProjectionRequest
	var gotOptions validate.Options
	runner := newProductionValidationRunner()
	runner.resolveVault = func(context.Context, string) (obsidian.VaultDefinition, error) {
		return obsidian.VaultDefinition{Name: "docs", Path: root}, nil
	}
	runner.runProjection = func(_ context.Context, request validationrun.ValidationRunRequest, projection indexing.ValidationProjectionRequest, options validate.Options) (validationrun.ValidationResult, error) {
		gotRun, gotProjection, gotOptions = request, projection, options
		return validationrun.ValidationResult{Selector: "default", Result: validate.Result{OK: true}}, nil
	}

	_, err := runner.Run(context.Background(), validationProductRequest{
		Surface:      validate.SurfaceCI,
		ApplyCommand: "rzm validate fix --apply --vault docs",
		MaxIssues:    7,
	})
	require.NoError(t, err)
	require.Equal(t, indexing.ValidationProjectionScratch, gotProjection.Target)
	require.Equal(t, gotProjection.VaultPath, gotOptions.RunContext.VaultPath)
	require.Equal(t, gotProjection.VaultDef, gotOptions.RunContext.VaultDef)
	require.NoError(t, gotProjection.NoteMetadata.Validate())
	require.NoError(t, gotOptions.RunContext.NoteMetadata.Validate())
	require.Equal(t, 7, gotOptions.RunContext.MaxIssues)
	require.Equal(t, []string{"link-hygiene"}, gotRun.Config.Default.Add)
	require.True(t, gotRun.Features.OntologyConfigured)
	require.True(t, gotRun.Features.CodeConfigured)
	require.True(t, gotRun.Features.CodeAnchorRootsConfigured)
	require.False(t, gotRun.Features.QueryRecipesConfigured)
	require.True(t, gotRun.Features.ViewsConfigured)
	require.True(t, gotRun.Features.EffortsPresent)
	require.Equal(t, codeanchor.IndexerVersion, gotRun.Features.RequiredCodeIndexerVersion)
	require.Equal(t, local.ScopeConfigHash(), gotRun.Features.RequiredCodeScopeHash)
}

func TestProductionValidationRunnerApplyUsesExactRootRefresherAndPreservesFailureEvidence(t *testing.T) {
	root := t.TempDir()
	_, canonicalRoot, err := canonicalValidationVault(obsidian.VaultDefinition{Path: root})
	require.NoError(t, err)
	planned := validationrun.ValidationResult{
		Result: validate.Result{
			IssueCount:     1,
			SelectedChecks: []string{validate.CheckBrokenLinks},
			Checks:         []validate.CheckResult{{Name: validate.CheckBrokenLinks, IssueCount: 1}},
		},
		Selector:        "broken-links",
		EffectiveChecks: []string{"broken-links"},
		Outcomes: []validationrun.ValidationCheckOutcome{{
			Check: "broken-links", Outcome: validate.CheckOutcomeCompleted,
		}},
	}
	var plannedArg validate.Result
	var runContext validate.RunContext
	var applyOptions validate.Options
	runner := newProductionValidationRunner()
	runner.resolveVault = func(context.Context, string) (obsidian.VaultDefinition, error) {
		return obsidian.VaultDefinition{Name: "docs", Path: root}, nil
	}
	runner.loadConfig = func(string) (*obsidian.LocalConfig, error) { return &obsidian.LocalConfig{}, nil }
	runner.runProjection = func(context.Context, validationrun.ValidationRunRequest, indexing.ValidationProjectionRequest, validate.Options) (validationrun.ValidationResult, error) {
		return planned, nil
	}
	runner.runRepair = func(_ context.Context, input validate.Result, context validate.RunContext, options validate.Options) (validate.Result, *validate.FixExecution, error) {
		plannedArg, runContext, applyOptions = input, context, options
		return validate.Result{
			OK:             true,
			SelectedChecks: []string{validate.CheckBrokenLinks},
			Checks:         []validate.CheckResult{{Name: validate.CheckBrokenLinks, OK: true}},
		}, &validate.FixExecution{Applied: []string{"repair:one"}}, errors.New("journal cleanup failed")
	}

	got, err := runner.Run(context.Background(), validationProductRequest{
		Surface:        validate.SurfaceLocal,
		Repair:         true,
		Apply:          true,
		ApplyCommand:   "rzm validate fix broken-links --apply --vault docs",
		ApplySelection: []string{"repair:one", "issue:v1:two"},
	})
	require.NoError(t, err)
	require.Equal(t, planned.Result, plannedArg)
	require.Equal(t, []string{"repair:one", "issue:v1:two"}, applyOptions.ApplySelection)
	require.Equal(t, canonicalRoot, runContext.VaultPath)
	require.True(t, applyOptions.Fix)
	require.Equal(t, "rzm validate fix broken-links --vault docs", applyOptions.ReplanCommand)
	refresher, ok := applyOptions.PostApplyRefresher.(indexing.ValidationProjectionPostApplyRefresher)
	require.True(t, ok)
	require.Equal(t, canonicalRoot, refresher.VaultPath)
	require.True(t, sameCanonicalValidationRoot(canonicalRoot, refresher.VaultDef.BasePath()))
	require.NoError(t, refresher.NoteMetadata.Validate())
	require.Equal(t, "journal cleanup failed", got.ExecutionError)
	require.False(t, got.OK)
	require.Equal(t, validationrun.ValidationExitFailure, got.ExitCode())
	require.Zero(t, got.IssueCount, "stale planning findings must be replaced")
	require.Equal(t, []string{"repair:one"}, got.FixExecution.Applied)
}

func TestValidateProductionRepairRootsAcceptsCanonicalNativeSeparatorForms(t *testing.T) {
	root := t.TempDir()
	_, canonicalRoot, err := canonicalValidationVault(obsidian.VaultDefinition{Path: root})
	require.NoError(t, err)
	noteMetadata, err := newNoteMetadataIndexer()
	require.NoError(t, err)
	nativeRoot := filepath.FromSlash(canonicalRoot)
	runContext := &validate.RunContext{
		VaultPath: canonicalRoot,
		VaultDef:  obsidian.VaultDefinition{Path: nativeRoot},
	}
	refresher := indexing.ValidationProjectionPostApplyRefresher{
		VaultPath:    canonicalRoot,
		VaultDef:     obsidian.VaultDefinition{Path: nativeRoot},
		NoteMetadata: noteMetadata,
	}

	require.NoError(t, validateProductionRepairRoots(canonicalRoot, runContext, refresher))
}

func TestLoadValidationLocalConfigAcceptsUnknownFields(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("validation:\n  unknown: true\n"), 0o644))

	config, err := loadValidationLocalConfig(root)
	require.NoError(t, err)
	require.Len(t, config.Warnings, 1)
	require.Equal(t, []string{"unknown"}, config.Warnings[0].OffendingKeys)
}

func TestValidationVaultDefinitionExplicitNameIgnoresMalformedCWDConfig(t *testing.T) {
	base := t.TempDir()
	cwd := filepath.Join(base, "cwd")
	selected := filepath.Join(base, "other")
	require.NoError(t, os.MkdirAll(filepath.Join(cwd, ".rhizome"), 0o755))
	require.NoError(t, os.MkdirAll(selected, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, ".rhizome", "config.yml"), []byte("unknown: true\n"), 0o644))
	obsidianConfig := filepath.Join(base, "obsidian.json")
	encoded, err := json.Marshal(obsidian.ObsidianVaultConfig{Vaults: map[string]obsidian.VaultPathEntry{
		"id": {Path: selected},
	}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(obsidianConfig, encoded, 0o644))
	ctx := contextWithCommandEnv(context.Background(), commandEnv{
		Getwd: func() (string, error) { return cwd, nil },
		ObsidianConfigFile: func() (string, error) {
			return obsidianConfig, nil
		},
	})

	def, err := validationVaultDefinition(ctx, "other")
	require.NoError(t, err)
	require.Equal(t, selected, def.BasePath())
}

func TestValidationProductCommandsReplaceLegacyRegistrations(t *testing.T) {
	validateCommand, _, err := rootCmd.Find([]string{"validate"})
	require.NoError(t, err)
	require.Equal(t, "validate [selector]", validateCommand.Use)
	require.Nil(t, validateCommand.Flags().Lookup("check"))
	require.Nil(t, validateCommand.Flags().Lookup("fix"))
	require.NotNil(t, validateCommand.Commands())

	ciCommand, _, err := rootCmd.Find([]string{"ci"})
	require.NoError(t, err)
	require.Equal(t, "ci [selector]", ciCommand.Use)

	agentValidate, _, err := rootCmd.Find([]string{"agent", "validate"})
	require.NoError(t, err)
	require.Equal(t, "validate [selector]", agentValidate.Use)
	require.Nil(t, agentValidate.Flags().Lookup("check"))
	require.Nil(t, agentValidate.Flags().Lookup("fix"))
}
