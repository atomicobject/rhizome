package validationproduct

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/app/indexing"
	"github.com/atomicobject/rhizome/pkg/app/validationrun"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// AuthoritativeRun retains the exact in-process inputs needed to create a
// repair review. It must never be reconstructed from the public snapshot.
type AuthoritativeRun struct {
	Result     validationrun.ValidationResult
	RunContext validate.RunContext
	NotePaths  []string
}

// RunLive executes the configured default validation product against the live
// projection and returns its un-serialized authority.
func RunLive(
	ctx context.Context,
	noteMetadata notemeta.Indexer,
	vaultDef obsidian.VaultDefinition,
	maxIssues int,
) (AuthoritativeRun, error) {
	return RunLiveOnLane(ctx, noteMetadata, vaultDef, maxIssues, nil)
}

// RunLiveOnLane serializes live projection writes with runtime indexing jobs.
// Checks run after the projection has released the lane's index lock.
func RunLiveOnLane(ctx context.Context, noteMetadata notemeta.Indexer, vaultDef obsidian.VaultDefinition, maxIssues int, indexLane lane.Lane) (AuthoritativeRun, error) {
	if err := noteMetadata.Validate(); err != nil {
		return AuthoritativeRun{}, fmt.Errorf("validation note metadata indexer: %w", err)
	}
	vaultPath := strings.TrimSpace(vaultDef.BasePath())
	if vaultPath == "" {
		return AuthoritativeRun{}, fmt.Errorf("validation vault root is required")
	}
	local, err := obsidian.LoadLocalConfig(vaultPath)
	if errors.Is(err, obsidian.ErrNoLocalConfig) {
		local = &obsidian.LocalConfig{}
	} else if err != nil {
		return AuthoritativeRun{}, fmt.Errorf("load validation config: %w", err)
	}
	config := liveSuiteConfig(local.Validation)
	selection, err := validate.ResolveSelection(nil, config)
	if err != nil {
		return AuthoritativeRun{}, err
	}
	features, err := liveVaultFeatureFacts(vaultPath, local, slices.Contains(selection.Checks, validate.CheckQueryRecipes))
	if err != nil {
		return AuthoritativeRun{}, err
	}
	if maxIssues <= 0 {
		maxIssues = 500
	}
	reader := &obsidian.Note{}
	runContext := validate.RunContext{
		VaultDef: vaultDef, VaultPath: vaultPath, VaultMgr: &liveVaultManager{def: vaultDef},
		NoteReader: reader, NoteMetadata: noteMetadata, MaxIssues: maxIssues,
	}
	result, err := RunWithProjection(ctx, validationrun.ValidationRunRequest{
		Config: config, Surface: validate.SurfaceLocal, Features: features,
	}, indexing.ValidationProjectionRequest{
		VaultPath: vaultPath, VaultDef: vaultDef, NoteMetadata: noteMetadata,
		NoteReader: reader, Target: indexing.ValidationProjectionLive, Lane: indexLane,
	}, validate.Options{MaxIssues: maxIssues, RunContext: &runContext})
	if err != nil {
		return AuthoritativeRun{}, err
	}
	notePaths, err := reader.GetNotesListContext(ctx, vaultDef)
	if err != nil {
		return AuthoritativeRun{}, fmt.Errorf("read validation note inventory: %w", err)
	}
	slices.Sort(notePaths)
	return AuthoritativeRun{Result: result, RunContext: runContext, NotePaths: notePaths}, nil
}

func liveSuiteConfig(local obsidian.LocalValidationConfig) validate.SuiteConfig {
	return validate.SuiteConfig{
		Default: validate.SuiteOverlay{Add: slices.Clone(local.Default.Add), Skip: slices.Clone(local.Default.Skip)},
		All:     validate.SuiteOverlay{Add: slices.Clone(local.All.Add), Skip: slices.Clone(local.All.Skip)},
	}
}

func liveVaultFeatureFacts(vaultPath string, local *obsidian.LocalConfig, includeQueryRecipes bool) (validate.VaultFeatureFacts, error) {
	if local == nil {
		local = &obsidian.LocalConfig{}
	}
	pathExists := func(path string) (bool, error) {
		_, err := os.Stat(path)
		if err == nil {
			return true, nil
		}
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	ontologyConfigured, err := pathExists(filepath.Join(vaultPath, ".rhizome", "ontology"))
	if err != nil {
		return validate.VaultFeatureFacts{}, fmt.Errorf("inspect ontology configuration: %w", err)
	}
	queryRecipesConfigured := includeQueryRecipes && queryrecipe.HasDefaultSources(vaultPath)
	viewsConfigured, err := pathExists(filepath.Join(vaultPath, ".rhizome", "views"))
	if err != nil {
		return validate.VaultFeatureFacts{}, fmt.Errorf("inspect view configuration: %w", err)
	}
	effortsPresent, err := pathExists(filepath.Join(vaultPath, "docs", "efforts"))
	if err != nil {
		return validate.VaultFeatureFacts{}, fmt.Errorf("inspect effort configuration: %w", err)
	}
	codeConfigured := liveCodeConfigured(local.Code)
	facts := validate.VaultFeatureFacts{
		OntologyConfigured: ontologyConfigured, CodeConfigured: codeConfigured,
		CodeAnchorRootsConfigured: obsidian.CodeAnchorRootsConfigured(local.Code),
		QueryRecipesConfigured:    queryRecipesConfigured, ViewsConfigured: viewsConfigured,
		EffortsPresent: effortsPresent,
	}
	if codeConfigured {
		facts.RequiredCodeIndexerVersion = codeanchor.IndexerVersion
		facts.RequiredCodeScopeHash = local.ScopeConfigHash()
	}
	return facts, nil
}

func liveCodeConfigured(code obsidian.LocalCodeConfig) bool {
	return code.Enabled || len(code.Scan) > 0 || code.Python != nil || code.Go != nil ||
		code.TypeScript != nil || code.JavaScript != nil || code.CSharp != nil || code.PHP != nil
}

type liveVaultManager struct{ def obsidian.VaultDefinition }

func (m *liveVaultManager) DefaultName() (string, error)                  { return m.def.Name, nil }
func (m *liveVaultManager) SetDefaultName(name string) error              { m.def.Name = name; return nil }
func (m *liveVaultManager) Path() (string, error)                         { return m.def.BasePath(), nil }
func (m *liveVaultManager) Definition() (obsidian.VaultDefinition, error) { return m.def, nil }
