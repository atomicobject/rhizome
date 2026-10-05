package init

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/atomicobject/rhizome/pkg/fileio"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/codepatterns"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"gopkg.in/yaml.v3"
)

func writeConfigNew(root string, cfg obsidian.LocalConfig) error {
	rhizomeDir := filepath.Join(root, ".rhizome")
	if err := ensureDirExists(rhizomeDir); err != nil {
		return err
	}
	pruneConfigForWrite(&cfg)
	workflow := workflowStateFromConfig(cfg)
	// The in-memory config accepts legacy workflow keys solely so init can
	// migrate them. Canonical writes always keep workflow state in its own file.
	cfg.WorkflowTemplates = nil
	cfg.WorkflowTemplateAddons = obsidian.WorkflowTemplateAddons{}
	cfg.WorkflowTemplateManagement = obsidian.WorkflowTemplateManagement{}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(obsidian.LocalConfigYAMLIndent)
	if err := enc.Encode(&cfg); err != nil {
		_ = enc.Close()
		return err
	}
	_ = enc.Close()
	if err := writeMigratedWorkflowConfig(root, workflow); err != nil {
		return err
	}
	if err := obsidian.WriteFileAtomicPreservingMode(filepath.Join(rhizomeDir, "config.yml"), buf.Bytes(), 0o644); err != nil {
		return err
	}
	return nil
}

func writeConfigPatched(root string, cfg obsidian.LocalConfig, changes changeSet) error {
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
			deleteKnownMappingKey(rootNode, sectionRhizome, obsidian.LocalRhizomeConfig{})
		} else if node, err := encodeSectionNode(cfg.Rhizome); err != nil {
			return err
		} else {
			setKnownMappingKey(rootNode, sectionRhizome, node, obsidian.LocalRhizomeConfig{})
		}
	}
	if changes[sectionNotes] {
		if isEmptyLocalNotesConfig(cfg.Notes) {
			deleteKnownMappingKey(rootNode, sectionNotes, obsidian.LocalVaultConfig{})
		} else if node, err := encodeSectionNode(cfg.Notes); err != nil {
			return err
		} else {
			setKnownMappingKey(rootNode, sectionNotes, node, obsidian.LocalVaultConfig{})
		}
	}
	if changes[sectionCode] {
		if isEmptyLocalCodeConfig(cfg.Code) {
			deleteKnownMappingKey(rootNode, sectionCode, obsidian.LocalCodeConfig{})
		} else if node, err := encodeSectionNode(cfg.Code); err != nil {
			return err
		} else {
			setKnownMappingKey(rootNode, sectionCode, node, obsidian.LocalCodeConfig{})
		}
	}
	if changes[sectionFileContext] {
		if isEmptyLocalFileContextConfig(cfg.FileCtx) {
			deleteKnownMappingKey(rootNode, sectionFileContext, obsidian.LocalFileContextConfig{})
		} else if node, err := encodeSectionNode(cfg.FileCtx); err != nil {
			return err
		} else {
			setKnownMappingKey(rootNode, sectionFileContext, node, obsidian.LocalFileContextConfig{})
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
			deleteKnownMappingKey(rootNode, sectionGraph, obsidian.LocalGraphConfig{})
		} else if node, err := encodeSectionNode(cfg.Graph); err != nil {
			return err
		} else {
			setKnownMappingKey(rootNode, sectionGraph, node, obsidian.LocalGraphConfig{})
		}
	}
	if changes[sectionAgents] {
		if cfg.Agents == nil || cfg.Agents.Empty() {
			deleteKnownMappingKey(rootNode, sectionAgents, obsidian.AgentPreferences{})
		} else if node, err := encodeSectionNode(cfg.Agents); err != nil {
			return err
		} else {
			setKnownMappingKey(rootNode, sectionAgents, node, obsidian.AgentPreferences{})
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
			deleteKnownMappingKey(rootNode, sectionCompression, obsidian.LocalCompressionConfig{})
		} else if node, err := encodeSectionNode(cfg.Compression); err != nil {
			return err
		} else {
			setKnownMappingKey(rootNode, sectionCompression, node, obsidian.LocalCompressionConfig{})
		}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(obsidian.LocalConfigYAMLIndent)
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
	if err := obsidian.WriteFileAtomicPreservingMode(path, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return nil
}

func workflowStateFromConfig(cfg obsidian.LocalConfig) obsidian.LocalWorkflowConfig {
	return obsidian.LocalWorkflowConfig{
		Templates:  cfg.WorkflowTemplates,
		Addons:     cfg.WorkflowTemplateAddons,
		Management: cfg.WorkflowTemplateManagement,
	}
}

func migrateLegacyWorkflowConfig(startDir string) (string, bool, error) {
	return migrateLegacyWorkflowConfigWithSave(startDir, obsidian.SaveLocalWorkflowConfig)
}

func migrateLegacyWorkflowConfigWithSave(startDir string, saveWorkflow func(string, obsidian.LocalWorkflowConfig) error) (string, bool, error) {
	root, _, err := obsidian.FindLocalConfigForDelegation(startDir)
	if err != nil {
		if errors.Is(err, obsidian.ErrNoLocalConfig) {
			return "", false, nil
		}
		return "", false, err
	}
	cfg, legacyPresent, err := obsidian.LoadLocalConfigForWorkflowMigration(root)
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

	path := filepath.Join(root, obsidian.RhizomeDirName, obsidian.RhizomeConfigFilename)
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
	enc.SetIndent(obsidian.LocalConfigYAMLIndent)
	if err := enc.Encode(doc); err != nil {
		_ = enc.Close()
		return root, false, err
	}
	_ = enc.Close()
	if err := obsidian.WriteFileAtomicPreservingMode(path, buf.Bytes(), 0o644); err != nil {
		return root, false, err
	}
	return root, true, nil
}

func writeMigratedWorkflowConfig(root string, workflow obsidian.LocalWorkflowConfig) error {
	return obsidian.SaveLocalWorkflowConfig(root, workflow)
}

func loadConfigNode(path string) (*yaml.Node, error) {
	data, err := fileio.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, err
	}
	return &node, nil
}

func ensureConfigNode() *yaml.Node {
	return &yaml.Node{
		Kind: yaml.DocumentNode,
		Content: []*yaml.Node{{
			Kind: yaml.MappingNode,
			Tag:  "!!map",
		}},
	}
}

func rootMapping(doc *yaml.Node) *yaml.Node {
	if doc == nil {
		return nil
	}
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0]
	}
	if doc.Kind == yaml.MappingNode {
		return doc
	}
	return nil
}

func getMappingValue(root *yaml.Node, key string) *yaml.Node {
	if root == nil || root.Kind != yaml.MappingNode || key == "" {
		return nil
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i]
		if k != nil && k.Value == key {
			return root.Content[i+1]
		}
	}
	return nil
}

func setKnownMappingKey(root *yaml.Node, key string, value *yaml.Node, schema any) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key && root.Content[i+1].Kind == yaml.MappingNode && value.Kind == yaml.MappingNode {
			obsidian.MergeKnownYAMLMapping(root.Content[i+1], value, schema)
			return
		}
	}
	setMappingKey(root, key, value)
}

func deleteKnownMappingKey(root *yaml.Node, key string, schema any) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != key || root.Content[i+1].Kind != yaml.MappingNode {
			continue
		}
		obsidian.MergeKnownYAMLMapping(root.Content[i+1], &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}, schema)
		if len(root.Content[i+1].Content) > 0 {
			return
		}
		break
	}
	deleteMappingKey(root, key)
}

func setMappingKey(root *yaml.Node, key string, value *yaml.Node) {
	if root == nil || root.Kind != yaml.MappingNode || key == "" || value == nil {
		return
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i]
		if k != nil && k.Value == key {
			root.Content[i+1] = value
			return
		}
	}
	root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}

func deleteMappingKey(root *yaml.Node, key string) {
	if root == nil || root.Kind != yaml.MappingNode || key == "" {
		return
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i]
		if k != nil && k.Value == key {
			root.Content = append(root.Content[:i], root.Content[i+2:]...)
			return
		}
	}
}

func encodeSectionNode(v any) (*yaml.Node, error) {
	data, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	root := rootMapping(&doc)
	if root == nil {
		root = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	}
	return root, nil
}

func isEmptyLocalCodeConfig(cfg obsidian.LocalCodeConfig) bool {
	return !hasAnyCodeConfig(cfg)
}

func isEmptyLocalNotesConfig(cfg obsidian.LocalVaultConfig) bool {
	return len(cfg.Includes) == 0 && len(cfg.Excludes) == 0 && strings.TrimSpace(cfg.Links) == ""
}

func isEmptyLocalFileContextConfig(cfg obsidian.LocalFileContextConfig) bool {
	return len(cfg.DocPatterns) == 0 &&
		cfg.MaxEmptyLevels == 0 &&
		cfg.ContextBudget == 0 &&
		cfg.IncludeDocsInGraph == nil &&
		len(cfg.ExpandNoteLinks) == 0 &&
		cfg.ExpandNoteLinksLimit == 0
}

func isEmptyGraphConfig(cfg *obsidian.LocalGraphConfig) bool {
	if cfg == nil {
		return true
	}
	return len(cfg.Ignore) == 0 &&
		len(cfg.KeyNotePatterns) == 0 &&
		len(cfg.AuthorityFactors) == 0
}

func ensureIgnoreFile(root string) error {
	path := filepath.Join(root, ".rhizome", "ignore")
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	body := ignore.DefaultIgnoreFile()
	return os.WriteFile(path, []byte(body), 0o644)
}

func ensureRhizomeGitIgnore(root string) error {
	rhizomeDir := filepath.Join(root, obsidian.RhizomeDirName)
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
		"!" + obsidian.RhizomeConfigFilename,
		"!workflows.yml",
		"!" + obsidian.RhizomeIgnoreFilename,
		"!" + generatedFilesName,
		"!migrations/",
		"!migrations/**",
		"!ontology/",
		"!ontology/**",
		"!query-recipes/",
		"!query-recipes/**",
		"!views/",
		"!views/**",
		"agent/",
	}
	return strings.Join(lines, "\n") + "\n"
}

func pruneConfigForWrite(cfg *obsidian.LocalConfig) {
	if cfg == nil {
		return
	}

	// notes defaults
	if cfg.Notes.Links == obsidian.LinkTypeBoth {
		cfg.Notes.Links = ""
	}
	if len(cfg.Notes.Includes) == 1 && cfg.Notes.Includes[0] == "**/*.md" {
		cfg.Notes.Includes = nil
	}

	// code defaults + redundancy
	if slices.Equal(cfg.Code.Ignore, obsidian.DefaultCodeConfig.Ignore) {
		cfg.Code.Ignore = nil
	}

	implied := impliedCodeScanPatterns(cfg.Code)
	if len(implied) > 0 && len(cfg.Code.Scan) > 0 {
		var pruned []string
		for _, p := range cfg.Code.Scan {
			if !contains(implied, p) {
				pruned = append(pruned, p)
			}
		}
		cfg.Code.Scan = pruned
	}

	// language block scan defaults (omit when they match the runtime default)
	if cfg.Code.Python != nil && slices.Equal(cfg.Code.Python.Scan, codepatterns.DefaultPythonGlobs()) {
		cfg.Code.Python.Scan = nil
	}
	if cfg.Code.Go != nil && slices.Equal(cfg.Code.Go.Scan, codepatterns.DefaultGoGlobs()) {
		cfg.Code.Go.Scan = nil
	}
	if cfg.Code.CSharp != nil && slices.Equal(cfg.Code.CSharp.Scan, codepatterns.DefaultCSharpGlobs()) {
		cfg.Code.CSharp.Scan = nil
	}
	if cfg.Code.TypeScript != nil && slices.Equal(cfg.Code.TypeScript.Scan, codepatterns.DefaultTypeScriptGlobs()) {
		cfg.Code.TypeScript.Scan = nil
	}
	if cfg.Code.JavaScript != nil && slices.Equal(cfg.Code.JavaScript.Scan, codepatterns.DefaultTypeScriptGlobs()) {
		cfg.Code.JavaScript.Scan = nil
	}

	// fileContext defaults
	if slices.Equal(cfg.FileCtx.DocPatterns, obsidian.FileContextConfigDefaults.DocPatterns) {
		cfg.FileCtx.DocPatterns = nil
	}
	if cfg.FileCtx.MaxEmptyLevels == obsidian.FileContextConfigDefaults.MaxEmptyLevels {
		cfg.FileCtx.MaxEmptyLevels = 0
	}
	if cfg.FileCtx.ContextBudget == obsidian.FileContextConfigDefaults.ContextBudget {
		cfg.FileCtx.ContextBudget = 0
	}
	if cfg.FileCtx.IncludeDocsInGraph != nil &&
		obsidian.FileContextConfigDefaults.IncludeDocsInGraph != nil &&
		*cfg.FileCtx.IncludeDocsInGraph == *obsidian.FileContextConfigDefaults.IncludeDocsInGraph {
		cfg.FileCtx.IncludeDocsInGraph = nil
	}

	if len(cfg.WorkflowTemplates) == 0 {
		cfg.WorkflowTemplates = nil
	}
	if len(cfg.WorkflowTemplateAddons.Enabled) == 0 {
		cfg.WorkflowTemplateAddons.Enabled = nil
	}
	if len(cfg.WorkflowTemplateAddons.Disabled) == 0 {
		cfg.WorkflowTemplateAddons.Disabled = nil
	}
	if len(cfg.WorkflowTemplateManagement.Ejected) == 0 {
		cfg.WorkflowTemplateManagement.Ejected = nil
	}
	if len(cfg.WorkflowTemplateManagement.SourceFingerprints) == 0 {
		cfg.WorkflowTemplateManagement.SourceFingerprints = nil
	}

	// noteEmbeddings defaults
	if cfg.NoteEmbeddings != nil {
		pruneEmbeddingsConfig(cfg.NoteEmbeddings, false)
		if isEmptyEmbeddingsConfig(cfg.NoteEmbeddings) {
			cfg.NoteEmbeddings = nil
		}
	}

	// codeEmbeddings defaults
	if cfg.CodeEmbeddings != nil {
		pruneEmbeddingsConfig(cfg.CodeEmbeddings, true)
		if isEmptyEmbeddingsConfig(cfg.CodeEmbeddings) {
			cfg.CodeEmbeddings = nil
		}
	}

	// graph defaults (empty graph config can be omitted)
	if cfg.Graph != nil && isEmptyGraphConfig(cfg.Graph) {
		cfg.Graph = nil
	}

	// agents defaults (empty agents can be omitted)
	if cfg.Agents != nil && cfg.Agents.Empty() {
		cfg.Agents = nil
	}

	// compression defaults (empty compression config can be omitted)
	if cfg.Compression != nil && isEmptyCompressionConfig(cfg.Compression) {
		cfg.Compression = nil
	}

	// indexPath default (empty means default)
	// Already empty by default, no action needed
}

func isEmptyWorkflowTemplateAddons(addons obsidian.WorkflowTemplateAddons) bool {
	return len(addons.Enabled) == 0 && len(addons.Disabled) == 0
}

func isEmptyWorkflowTemplateUpdatePolicy(policy obsidian.WorkflowTemplateUpdatePolicy) bool {
	return strings.TrimSpace(policy.Docs) == "" &&
		strings.TrimSpace(policy.Skills) == "" &&
		strings.TrimSpace(policy.ManagedDocs) == "" &&
		strings.TrimSpace(policy.Ontology) == "" &&
		strings.TrimSpace(policy.QueryRecipes) == "" &&
		strings.TrimSpace(policy.Views) == ""
}

// pruneEmbeddingsConfig removes default values from embeddings config.
// Note: Provider is NEVER pruned - it must always be explicit to avoid
// silent fallback to defaults that could change between versions.
func pruneEmbeddingsConfig(cfg *embeddings.Config, isCode bool) {
	if cfg == nil {
		return
	}
	isOllama := strings.EqualFold(strings.TrimSpace(cfg.Provider), "ollama")
	defaultEndpoint := embeddings.DefaultConfig("").Endpoint
	defaultModel := embeddings.DefaultConfig("").Model
	if isCode {
		defaultModel = embeddings.DefaultCodeConfig("").Model
	}
	if embeddings.OllamaInstalled() {
		defaultEndpoint = embeddings.DefaultOllamaConfig("").Endpoint
		defaultModel = embeddings.DefaultOllamaConfig("").Model
	}
	// Provider is NEVER pruned - must be explicit
	// Default endpoint
	if cfg.Endpoint == defaultEndpoint || (isOllama && embeddings.NormalizeOllamaEndpoint(cfg.Endpoint) == defaultEndpoint) {
		cfg.Endpoint = ""
	}
	// Default model (different for note vs code)
	if cfg.Model == defaultModel {
		cfg.Model = ""
	}
	// Zero values are already omitted by omitempty
}

// isEmptyEmbeddingsConfig checks if embeddings config has no meaningful content.
func isEmptyEmbeddingsConfig(cfg *embeddings.Config) bool {
	if cfg == nil {
		return true
	}
	// Only enabled flag matters if everything else is default
	return !cfg.Enabled &&
		cfg.Provider == "" &&
		cfg.Model == "" &&
		cfg.Endpoint == "" &&
		cfg.Dimensions == 0 &&
		cfg.BatchSize == 0 &&
		cfg.MaxConcurrency == 0 &&
		cfg.MaxSectionBytes == 0 &&
		cfg.IndexPath == ""
}

func impliedCodeScanPatterns(cfg obsidian.LocalCodeConfig) []string {
	var implied []string

	if cfg.Python != nil {
		implied = append(implied, codepatterns.DefaultPythonGlobs()...)
	}
	if cfg.Go != nil {
		implied = append(implied, codepatterns.DefaultGoGlobs()...)
	}
	if cfg.CSharp != nil {
		implied = append(implied, codepatterns.DefaultCSharpGlobs()...)
	}
	if cfg.TypeScript != nil || cfg.JavaScript != nil {
		implied = append(implied, codepatterns.DefaultTypeScriptGlobs()...)
	}

	return implied
}
