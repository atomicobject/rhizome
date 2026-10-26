package init

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/fileio"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"gopkg.in/yaml.v3"
)

func writeConfigNew(root string, cfg repoconfig.LocalConfig) error {
	rhizomeDir := filepath.Join(root, ".rhizome")
	if err := ensureDirExists(rhizomeDir); err != nil {
		return err
	}
	pruneConfigForWrite(&cfg)
	workflow := workflowStateFromConfig(cfg)
	// The in-memory config accepts legacy workflow keys solely so init can
	// migrate them. Canonical writes always keep workflow state in its own file.
	cfg.WorkflowTemplates = nil
	cfg.WorkflowTemplateAddons = repoconfig.WorkflowTemplateAddons{}
	cfg.WorkflowTemplateManagement = repoconfig.WorkflowTemplateManagement{}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(repoconfig.LocalConfigYAMLIndent)
	if err := enc.Encode(&cfg); err != nil {
		_ = enc.Close()
		return err
	}
	_ = enc.Close()
	if err := writeMigratedWorkflowConfig(root, workflow); err != nil {
		return err
	}
	if err := fileio.WriteFileAtomicPreservingMode(filepath.Join(rhizomeDir, "config.yml"), buf.Bytes(), 0o644); err != nil {
		return err
	}
	return nil
}

func writeConfigPatched(root string, cfg repoconfig.LocalConfig, changes changeSet) error {
	rhizomeDir := filepath.Join(root, ".rhizome")
	if err := ensureDirExists(rhizomeDir); err != nil {
		return err
	}
	path := filepath.Join(rhizomeDir, "config.yml")

	var doc *yaml.Node
	if node, err := loadConfigNode(path); err == nil {
		doc = node
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else {
		doc = ensureConfigNode()
	}

	rootNode := rootMapping(doc)
	if rootNode == nil {
		doc = ensureConfigNode()
		rootNode = rootMapping(doc)
	}
	legacyWorkflowKeysPresent := getMappingValue(rootNode, sectionWorkflowTmpls) != nil ||
		getMappingValue(rootNode, sectionWorkflowAddons) != nil ||
		getMappingValue(rootNode, sectionWorkflowMgmt) != nil

	if changes[sectionRhizome] {
		if strings.TrimSpace(cfg.Rhizome.BinaryManager) == "" && strings.TrimSpace(cfg.Rhizome.Version) == "" && strings.TrimSpace(cfg.Rhizome.DevBinaryDir) == "" && strings.TrimSpace(cfg.Rhizome.BinaryDir) == "" && strings.TrimSpace(cfg.Rhizome.BinaryPath) == "" {
			deleteKnownMappingKey(rootNode, sectionRhizome, repoconfig.LocalRhizomeConfig{})
		} else if node, err := encodeSectionNode(cfg.Rhizome); err != nil {
			return err
		} else {
			setKnownMappingKey(rootNode, sectionRhizome, node, repoconfig.LocalRhizomeConfig{})
		}
	}
	if changes[sectionNotes] {
		if isEmptyLocalNotesConfig(cfg.Notes) {
			deleteKnownMappingKey(rootNode, sectionNotes, repoconfig.LocalVaultConfig{})
		} else if node, err := encodeSectionNode(cfg.Notes); err != nil {
			return err
		} else {
			setKnownMappingKey(rootNode, sectionNotes, node, repoconfig.LocalVaultConfig{})
		}
	}
	if changes[sectionCode] {
		if isEmptyLocalCodeConfig(cfg.Code) {
			deleteKnownMappingKey(rootNode, sectionCode, repoconfig.LocalCodeConfig{})
		} else if node, err := encodeSectionNode(cfg.Code); err != nil {
			return err
		} else {
			setKnownMappingKey(rootNode, sectionCode, node, repoconfig.LocalCodeConfig{})
		}
	}
	if changes[sectionFileContext] {
		if isEmptyLocalFileContextConfig(cfg.FileCtx) {
			deleteKnownMappingKey(rootNode, sectionFileContext, repoconfig.LocalFileContextConfig{})
		} else if node, err := encodeSectionNode(cfg.FileCtx); err != nil {
			return err
		} else {
			setKnownMappingKey(rootNode, sectionFileContext, node, repoconfig.LocalFileContextConfig{})
		}
	}
	deleteMappingKey(rootNode, sectionWorkflowTmpls)
	deleteMappingKey(rootNode, sectionWorkflowAddons)
	deleteMappingKey(rootNode, sectionWorkflowMgmt)
	if changes[sectionNoteEmbeddings] {
		if cfg.NoteEmbeddings == nil {
			deleteKnownMappingKey(rootNode, sectionNoteEmbeddings, embeddings.Config{})
		} else if node, err := encodeSectionNode(cfg.NoteEmbeddings); err != nil {
			return err
		} else {
			setKnownMappingKey(rootNode, sectionNoteEmbeddings, node, embeddings.Config{})
		}
	}
	if changes[sectionCodeEmbeddings] {
		if cfg.CodeEmbeddings == nil {
			deleteKnownMappingKey(rootNode, sectionCodeEmbeddings, embeddings.Config{})
		} else if node, err := encodeSectionNode(cfg.CodeEmbeddings); err != nil {
			return err
		} else {
			setKnownMappingKey(rootNode, sectionCodeEmbeddings, node, embeddings.Config{})
		}
	}
	if changes[sectionGraph] {
		if cfg.Graph == nil || isEmptyGraphConfig(cfg.Graph) {
			deleteKnownMappingKey(rootNode, sectionGraph, repoconfig.LocalGraphConfig{})
		} else if node, err := encodeSectionNode(cfg.Graph); err != nil {
			return err
		} else {
			setKnownMappingKey(rootNode, sectionGraph, node, repoconfig.LocalGraphConfig{})
		}
	}
	if changes[sectionAgents] {
		if cfg.Agents == nil || cfg.Agents.Empty() {
			deleteKnownMappingKey(rootNode, sectionAgents, repoconfig.AgentPreferences{})
		} else if node, err := encodeSectionNode(cfg.Agents); err != nil {
			return err
		} else {
			setKnownMappingKey(rootNode, sectionAgents, node, repoconfig.AgentPreferences{})
		}
	}
	if changes[sectionIndexPath] {
		if strings.TrimSpace(cfg.IndexPath) == "" {
			deleteMappingKey(rootNode, sectionIndexPath)
		} else {
			setMappingKey(rootNode, sectionIndexPath, &yaml.Node{
				Kind:  yaml.ScalarNode,
				Tag:   "!!str",
				Value: cfg.IndexPath,
			})
		}
	}
	if changes[sectionCompression] {
		if cfg.Compression == nil || isEmptyCompressionConfig(cfg.Compression) {
			deleteKnownMappingKey(rootNode, sectionCompression, repoconfig.LocalCompressionConfig{})
		} else if node, err := encodeSectionNode(cfg.Compression); err != nil {
			return err
		} else {
			setKnownMappingKey(rootNode, sectionCompression, node, repoconfig.LocalCompressionConfig{})
		}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(repoconfig.LocalConfigYAMLIndent)
	if err := enc.Encode(doc); err != nil {
		_ = enc.Close()
		return err
	}
	_ = enc.Close()
	workflowChanged := changes[sectionWorkflowTmpls] || changes[sectionWorkflowAddons] || changes[sectionWorkflowMgmt]
	if legacyWorkflowKeysPresent || workflowChanged {
		// Workflow state must be durable before legacy keys disappear from
		// config.yml. Unrelated config patches leave workflows.yml untouched.
		if err := writeMigratedWorkflowConfig(root, workflowStateFromConfig(cfg)); err != nil {
			return err
		}
	}
	if err := fileio.WriteFileAtomicPreservingMode(path, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return nil
}

func workflowStateFromConfig(cfg repoconfig.LocalConfig) repoconfig.LocalWorkflowConfig {
	return repoconfig.LocalWorkflowConfig{
		Templates:  cfg.WorkflowTemplates,
		Addons:     cfg.WorkflowTemplateAddons,
		Management: cfg.WorkflowTemplateManagement,
	}
}

func migrateLegacyWorkflowConfig(startDir string) (string, bool, error) {
	return migrateLegacyWorkflowConfigWithSave(startDir, repoconfig.SaveLocalWorkflowConfig)
}

func migrateLegacyWorkflowConfigWithSave(startDir string, saveWorkflow func(string, repoconfig.LocalWorkflowConfig) error) (string, bool, error) {
	root, _, err := repoconfig.FindLocalConfigForDelegation(startDir)
	if err != nil {
		if errors.Is(err, repoconfig.ErrNoLocalConfig) {
			return "", false, nil
		}
		return "", false, err
	}
	cfg, legacyPresent, err := repoconfig.LoadLocalConfigForWorkflowMigration(root)
	if err != nil || !legacyPresent {
		return root, false, err
	}
	if _, err := normalizeLegacyAgenticEngineeringState(cfg); err != nil {
		return root, false, err
	}

	// Write the new state first. If a later write fails, a retry can safely
	// recognize the identical state and finish removing the legacy keys.
	if err := saveWorkflow(root, workflowStateFromConfig(*cfg)); err != nil {
		return root, false, err
	}
	if err := ensureRhizomeGitIgnore(root); err != nil {
		return root, false, err
	}

	path := filepath.Join(root, repoconfig.RhizomeDirName, repoconfig.RhizomeConfigFilename)
	doc, err := loadConfigNode(path)
	if err != nil {
		return root, false, err
	}
	rootNode := rootMapping(doc)
	if rootNode == nil {
		return root, false, errors.New("repo-local config must contain a YAML mapping")
	}
	deleteMappingKey(rootNode, sectionWorkflowTmpls)
	deleteMappingKey(rootNode, sectionWorkflowAddons)
	deleteMappingKey(rootNode, sectionWorkflowMgmt)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(repoconfig.LocalConfigYAMLIndent)
	if err := enc.Encode(doc); err != nil {
		_ = enc.Close()
		return root, false, err
	}
	_ = enc.Close()
	if err := fileio.WriteFileAtomicPreservingMode(path, buf.Bytes(), 0o644); err != nil {
		return root, false, err
	}
	return root, true, nil
}

func writeMigratedWorkflowConfig(root string, workflow repoconfig.LocalWorkflowConfig) error {
	return repoconfig.SaveLocalWorkflowConfig(root, workflow)
}

func ensureRhizomeGitIgnore(root string) error {
	rhizomeDir := filepath.Join(root, repoconfig.RhizomeDirName)
	if err := ensureDirExists(rhizomeDir); err != nil {
		return err
	}

	path := filepath.Join(rhizomeDir, ".gitignore")
	body := rhizomeGitIgnoreBody()

	data, err := os.ReadFile(path)
	if err == nil && string(data) == body {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return os.WriteFile(path, []byte(body), 0o644)
}

func rhizomeGitIgnoreBody() string {
	lines := []string{
		"# Generated by rzm init. Re-run init to refresh.",
		"*",
		"!.gitignore",
		"!" + repoconfig.RhizomeConfigFilename,
		"!workflows.yml",
		"!" + repoconfig.RhizomeIgnoreFilename,
		"!" + generatedFilesName,
		"!migrations/",
		"!migrations/**",
		"!ontology/",
		"!ontology/**",
		"!query-recipes/",
		"!query-recipes/**",
		"!tasks/",
		"!tasks/**",
		"!views/",
		"!views/**",
		"agent/",
	}
	return strings.Join(lines, "\n") + "\n"
}
