package repoconfig

import (
	"errors"
	"fmt"

	"os"
	"path/filepath"
	"reflect"

	"strings"

	"github.com/atomicobject/rhizome/pkg/fileio"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// LoadLocalConfig loads a .rhizome/config.yml file from the given directory.
// Returns ErrNoLocalConfig if no config file exists.
func LoadLocalConfig(dir string) (*LocalConfig, error) {
	dir = paths.ResolveSymlinks(dir).String()
	configPath := rhizomeConfigPath(dir)
	data, err := fileio.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoLocalConfig
		}
		return nil, err
	}

	cfg, err := decodeLocalConfig(configPath, data)
	if err != nil {
		return nil, err
	}
	workflow, hasWorkflowFile, err := LoadLocalWorkflowConfig(dir)
	if err != nil {
		return nil, err
	}
	if hasWorkflowFile {
		applyWorkflowConfig(cfg, workflow)
		cfg.Warnings = append(cfg.Warnings, workflow.Warnings...)
	}
	return cfg, nil
}

// SaveLocalConfig writes a LocalConfig to .rhizome/config.yml under dir.
func SaveLocalConfig(dir string, cfg LocalConfig) error {
	cfg.Rhizome.BinaryManager = strings.TrimSpace(cfg.Rhizome.BinaryManager)
	if _, err := cfg.Rhizome.UsesExternalBinaryManager(); err != nil {
		return err
	}
	if err := os.MkdirAll(rhizomeDir(dir), 0o755); err != nil {
		return err
	}
	if cfg.sourceYAML == nil {
		if data, err := fileio.ReadFile(rhizomeConfigPath(dir)); err == nil {
			cfg.sourceYAML, err = parseYAMLDocument(data)
			if err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	workflow := workflowConfigFromLocalConfig(cfg)
	cfg.WorkflowTemplates = nil
	cfg.WorkflowTemplateAddons = WorkflowTemplateAddons{}
	cfg.WorkflowTemplateManagement = WorkflowTemplateManagement{}
	if err := saveYAMLDocumentPreservingUnknown(rhizomeConfigPath(dir), cfg, cfg.sourceYAML, reflect.TypeOf(LocalConfig{})); err != nil {
		return err
	}
	return SaveLocalWorkflowConfig(dir, workflow)
}

// LoadLocalWorkflowConfig loads .rhizome/workflows.yml. The boolean reports
// whether the file exists.
func LoadLocalWorkflowConfig(dir string) (LocalWorkflowConfig, bool, error) {
	workflowPath := localWorkflowPath(dir)
	data, err := fileio.ReadFile(workflowPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return LocalWorkflowConfig{}, false, nil
		}
		return LocalWorkflowConfig{}, false, err
	}
	cfg, warnings, err := decodeCanonicalYAML[LocalWorkflowConfig](workflowPath, data)
	if err != nil {
		return LocalWorkflowConfig{}, true, err
	}
	cfg.Warnings = warnings
	cfg.sourceYAML, err = parseYAMLDocument(data)
	if err != nil {
		return LocalWorkflowConfig{}, true, err
	}
	return cfg, true, nil
}

// LoadLocalConfigForWorkflowMigration accepts the three workflow fields that
// v0.49 stored in config.yml. All other unknown keys remain warnings, matching
// ordinary config loading. The boolean reports whether a legacy key is present.
func LoadLocalConfigForWorkflowMigration(dir string) (*LocalConfig, bool, error) {
	dir = paths.ResolveSymlinks(dir).String()
	configPath := rhizomeConfigPath(dir)
	data, err := fileio.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, ErrNoLocalConfig
		}
		return nil, false, err
	}

	legacy, warnings, err := decodeCanonicalYAML[legacyWorkflowLocalConfig](configPath, data)
	if err != nil {
		return nil, false, err
	}
	legacy.Rhizome.BinaryManager = strings.TrimSpace(legacy.Rhizome.BinaryManager)
	if _, err := legacy.Rhizome.UsesExternalBinaryManager(); err != nil {
		return nil, false, fmt.Errorf("%s: %w", configPath, err)
	}
	hasTemplates := hasAnyTopLevelYAMLKey(data, "workflowTemplates")
	hasAddons := hasAnyTopLevelYAMLKey(data, "workflowTemplateAddons")
	hasManagement := hasAnyTopLevelYAMLKey(data, "workflowTemplateManagement")
	present := hasTemplates || hasAddons || hasManagement
	cfg := legacy.LocalConfig
	cfg.Warnings = warnings

	workflow, hasWorkflowFile, err := LoadLocalWorkflowConfig(dir)
	if err != nil {
		return nil, false, err
	}
	if !present {
		if hasWorkflowFile {
			applyWorkflowConfig(&cfg, workflow)
			cfg.Warnings = append(cfg.Warnings, workflow.Warnings...)
		}
		return &cfg, false, nil
	}

	legacyWorkflow := workflow
	if hasTemplates {
		legacyWorkflow.Templates = legacy.WorkflowTemplates
	}
	if hasAddons {
		legacyWorkflow.Addons = legacy.WorkflowTemplateAddons
	}
	if hasManagement {
		legacyWorkflow.Management = legacy.WorkflowTemplateManagement
	}
	// The legacy fields are the migration source of truth. An existing
	// workflows.yml may have been produced by an earlier v0.50 init attempt;
	// the init writer patches these three known sections while preserving any
	// unrelated future fields in that file.
	applyWorkflowConfig(&cfg, legacyWorkflow)
	cfg.Warnings = append(cfg.Warnings, workflow.Warnings...)
	return &cfg, true, nil
}

// SaveLocalWorkflowConfig writes .rhizome/workflows.yml, or removes it when
// the workflow state is empty.
func SaveLocalWorkflowConfig(dir string, cfg LocalWorkflowConfig) error {
	path := localWorkflowPath(dir)
	if cfg.sourceYAML == nil {
		if data, err := fileio.ReadFile(path); err == nil {
			cfg.sourceYAML, err = parseYAMLDocument(data)
			if err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if isEmptyLocalWorkflowConfig(cfg) && cfg.sourceYAML == nil {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(rhizomeDir(dir), 0o755); err != nil {
		return err
	}
	return saveYAMLDocumentPreservingUnknown(path, cfg, cfg.sourceYAML, reflect.TypeOf(LocalWorkflowConfig{}))
}

func localWorkflowPath(dir string) string {
	return filepath.Join(rhizomeDir(dir), localWorkflowStateFile)
}

func workflowConfigFromLocalConfig(cfg LocalConfig) LocalWorkflowConfig {
	return LocalWorkflowConfig{
		Templates:  cfg.WorkflowTemplates,
		Addons:     cfg.WorkflowTemplateAddons,
		Management: cfg.WorkflowTemplateManagement,
		sourceYAML: cfg.workflowSourceYAML,
	}
}

func applyWorkflowConfig(cfg *LocalConfig, workflow LocalWorkflowConfig) {
	if cfg == nil {
		return
	}
	cfg.WorkflowTemplates = workflow.Templates
	cfg.WorkflowTemplateAddons = workflow.Addons
	cfg.WorkflowTemplateManagement = workflow.Management
	cfg.workflowSourceYAML = workflow.sourceYAML
}

func isEmptyLocalWorkflowConfig(cfg LocalWorkflowConfig) bool {
	return len(cfg.Templates) == 0 &&
		len(cfg.Addons.Enabled) == 0 &&
		len(cfg.Addons.Disabled) == 0 &&
		len(cfg.Management.Ejected) == 0 &&
		len(cfg.Management.SourceFingerprints) == 0 &&
		strings.TrimSpace(cfg.Management.UpdatePolicy.Docs) == "" &&
		strings.TrimSpace(cfg.Management.UpdatePolicy.Skills) == "" &&
		strings.TrimSpace(cfg.Management.UpdatePolicy.ManagedDocs) == "" &&
		strings.TrimSpace(cfg.Management.UpdatePolicy.Ontology) == "" &&
		strings.TrimSpace(cfg.Management.UpdatePolicy.QueryRecipes) == "" &&
		strings.TrimSpace(cfg.Management.UpdatePolicy.Views) == ""
}
