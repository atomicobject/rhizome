package validationrun

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	initactions "github.com/atomicobject/rhizome/pkg/app/cli/init"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// Prepared holds the configured selection, feature facts, and live source reader
// shared by command, live, and already-indexed validation producers.
type Prepared struct {
	Request    ValidationRunRequest
	RunContext validate.RunContext
}

// Prepare reads only the selected vault's configuration. Projection acquisition
// belongs to the caller; this step never opens an index or refreshes a runtime.
func Prepare(vaultDef obsidian.VaultDefinition, noteMetadata notemeta.Indexer, selectors []string, maxIssues int) (Prepared, error) {
	if err := noteMetadata.Validate(); err != nil {
		return Prepared{}, fmt.Errorf("validation note metadata indexer: %w", err)
	}
	vaultPaths, err := paths.NewVaultPaths(vaultDef.BasePath())
	if err != nil || vaultPaths.Root() == "" {
		return Prepared{}, fmt.Errorf("validation vault root is required: %v", err)
	}
	root := vaultPaths.Root()
	if vaultDef.IsCollection() {
		vaultDef.Root = root
		vaultDef.Path = ""
	} else {
		vaultDef.Path = root
	}
	local, err := LoadLocalConfig(root)
	if err != nil {
		return Prepared{}, fmt.Errorf("load validation config: %w", err)
	}
	config := SuiteConfigFromLocal(local.Validation)
	selection, err := validate.ResolveSelection(selectors, config)
	if err != nil {
		return Prepared{}, err
	}
	features, err := vaultFeatureFacts(root, local, slices.Contains(selection.Checks, validate.CheckQueryRecipes))
	if err != nil {
		return Prepared{}, err
	}
	if maxIssues <= 0 {
		maxIssues = 20
	}
	return Prepared{
		Request: ValidationRunRequest{Selectors: slices.Clone(selectors), Config: config, Features: features, selection: &selection},
		RunContext: validate.RunContext{
			VaultDef: vaultDef, VaultPath: root, NoteReader: &obsidian.Note{}, NoteMetadata: noteMetadata, MaxIssues: maxIssues,
			SkillOverlays: initactions.ValidateBundledSkillOverlays,
		},
	}, nil
}

func LoadLocalConfig(root string) (*repoconfig.LocalConfig, error) {
	local, err := repoconfig.LoadLocalConfig(root)
	if errors.Is(err, repoconfig.ErrNoLocalConfig) {
		return &repoconfig.LocalConfig{}, nil
	}
	return local, err
}

func SuiteConfigFromLocal(local repoconfig.LocalValidationConfig) validate.SuiteConfig {
	return validate.SuiteConfig{
		Default: validate.SuiteOverlay{Add: slices.Clone(local.Default.Add), Skip: slices.Clone(local.Default.Skip)},
		All:     validate.SuiteOverlay{Add: slices.Clone(local.All.Add), Skip: slices.Clone(local.All.Skip)},
	}
}

func vaultFeatureFacts(root string, local *repoconfig.LocalConfig, includeQueryRecipes bool) (validate.VaultFeatureFacts, error) {
	present := func(parts ...string) (bool, error) {
		_, err := os.Stat(filepath.Join(append([]string{root}, parts...)...))
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return err == nil, err
	}
	ontologyConfigured, err := present(".rhizome", "ontology")
	if err != nil {
		return validate.VaultFeatureFacts{}, fmt.Errorf("inspect ontology configuration: %w", err)
	}
	viewsConfigured, err := present(".rhizome", "views")
	if err != nil {
		return validate.VaultFeatureFacts{}, fmt.Errorf("inspect view configuration: %w", err)
	}
	effortsPresent, err := present("docs", "efforts")
	if err != nil {
		return validate.VaultFeatureFacts{}, fmt.Errorf("inspect effort configuration: %w", err)
	}
	code := local.Code
	codeConfigured := code.Enabled || len(code.Scan) > 0 || code.Python != nil || code.Go != nil || code.TypeScript != nil || code.JavaScript != nil || code.CSharp != nil || code.PHP != nil
	facts := validate.VaultFeatureFacts{
		OntologyConfigured: ontologyConfigured, CodeConfigured: codeConfigured,
		CodeAnchorRootsConfigured: repoconfig.CodeAnchorRootsConfigured(code),
		QueryRecipesConfigured:    includeQueryRecipes && queryrecipe.HasDefaultSources(root),
		ViewsConfigured:           viewsConfigured, EffortsPresent: effortsPresent,
	}
	if codeConfigured {
		facts.RequiredCodeIndexerVersion = codeanchor.IndexerVersion
		facts.RequiredCodeScopeHash = strings.TrimSpace(local.ScopeConfigHash())
	}
	return facts, nil
}
