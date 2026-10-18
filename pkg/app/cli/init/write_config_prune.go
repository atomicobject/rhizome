package init

import (
	"slices"
	"strings"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/codepatterns"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

func isEmptyLocalCodeConfig(cfg repoconfig.LocalCodeConfig) bool {
	return !hasAnyCodeConfig(cfg)
}

func isEmptyLocalNotesConfig(cfg repoconfig.LocalVaultConfig) bool {
	return len(cfg.Includes) == 0 && len(cfg.Excludes) == 0 && strings.TrimSpace(cfg.Links) == ""
}

func isEmptyLocalFileContextConfig(cfg repoconfig.LocalFileContextConfig) bool {
	return len(cfg.DocPatterns) == 0 &&
		cfg.MaxEmptyLevels == 0 &&
		cfg.ContextBudget == 0 &&
		cfg.IncludeDocsInGraph == nil &&
		len(cfg.ExpandNoteLinks) == 0 &&
		cfg.ExpandNoteLinksLimit == 0
}

func isEmptyGraphConfig(cfg *repoconfig.LocalGraphConfig) bool {
	if cfg == nil {
		return true
	}
	return len(cfg.Ignore) == 0 &&
		len(cfg.KeyNotePatterns) == 0 &&
		len(cfg.AuthorityFactors) == 0
}

func pruneConfigForWrite(cfg *repoconfig.LocalConfig) {
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
	if slices.Equal(cfg.Code.Ignore, repoconfig.DefaultCodeConfig.Ignore) {
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
	if slices.Equal(cfg.FileCtx.DocPatterns, repoconfig.FileContextConfigDefaults.DocPatterns) {
		cfg.FileCtx.DocPatterns = nil
	}
	if cfg.FileCtx.MaxEmptyLevels == repoconfig.FileContextConfigDefaults.MaxEmptyLevels {
		cfg.FileCtx.MaxEmptyLevels = 0
	}
	if cfg.FileCtx.ContextBudget == repoconfig.FileContextConfigDefaults.ContextBudget {
		cfg.FileCtx.ContextBudget = 0
	}
	if cfg.FileCtx.IncludeDocsInGraph != nil &&
		repoconfig.FileContextConfigDefaults.IncludeDocsInGraph != nil &&
		*cfg.FileCtx.IncludeDocsInGraph == *repoconfig.FileContextConfigDefaults.IncludeDocsInGraph {
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

func isEmptyWorkflowTemplateAddons(addons repoconfig.WorkflowTemplateAddons) bool {
	return len(addons.Enabled) == 0 && len(addons.Disabled) == 0
}

func isEmptyWorkflowTemplateUpdatePolicy(policy repoconfig.WorkflowTemplateUpdatePolicy) bool {
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

func impliedCodeScanPatterns(cfg repoconfig.LocalCodeConfig) []string {
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
