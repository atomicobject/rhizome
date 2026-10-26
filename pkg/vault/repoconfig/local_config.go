package repoconfig

import (
	"errors"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/llm"

	"github.com/atomicobject/rhizome/pkg/embeddings"

	"gopkg.in/yaml.v3"
)

// ErrNoLocalConfig is returned when no local config file is found.
var ErrNoLocalConfig = errors.New("no .rhizome/config.yml found")

const LocalConfigDocsHint = "docs/reference/guides/Advanced configuration tuning.md"

// ConfigWarning reports keys that are not part of this binary's repo-local
// configuration schema. Unknown keys are preserved and do not block loading.
type ConfigWarning struct {
	Path          string
	OffendingKeys []string
	DocsHint      string
}

func (e ConfigWarning) Error() string {
	return fmt.Sprintf("non-canonical Rhizome config %s: unsupported key(s) %s; see %s", e.Path, strings.Join(e.OffendingKeys, ", "), e.DocsHint)
}

// LocalConfigYAMLIndent is the canonical indentation for repo-local YAML config
// files written by Rhizome.
const LocalConfigYAMLIndent = 2

// LocalCodeConfig configures code-to-note linking (coderefs + codeanchors).
// Top-level scan/ignore apply coderefs-only to all languages.
// Language blocks enable coderefs + anchors for that language.
type LocalCodeConfig struct {
	Enabled bool     `yaml:"enabled,omitempty"` // enables coderefs scanning with defaults when scan is empty
	Scan    []string `yaml:"scan,omitempty"`    // glob patterns for coderefs-only
	Ignore  []string `yaml:"ignore,omitempty"`  // exclusion globs for coderefs-only
	// Tree-sitter parse timeout (applies to Python/TS/C#/PHP indexers).
	TSParseTimeout time.Duration        `yaml:"tsParseTimeout,omitempty"`
	Python         *LocalCodeLangConfig `yaml:"python,omitempty"` // enables python coderefs + anchors
	Go             *LocalCodeLangConfig `yaml:"go,omitempty"`     // enables go coderefs + anchors
	// TypeScript/JavaScript share the same codeanchor language id (ts).
	TypeScript *LocalCodeLangConfig `yaml:"typescript,omitempty"` // enables ts/js coderefs + anchors
	JavaScript *LocalCodeLangConfig `yaml:"javascript,omitempty"` // enables ts/js coderefs + anchors
	CSharp     *LocalCodeLangConfig `yaml:"csharp,omitempty"`     // enables csharp coderefs + anchors
	PHP        *LocalCodeLangConfig `yaml:"php,omitempty"`        // enables php coderefs + anchors
	// DisabledLanguages records codeanchor language IDs the user never wants to
	// be prompted to enable/configure for this vault/repo.
	// Supported language IDs: "python", "go", "ts", "cs", "php".
	DisabledLanguages []string `yaml:"disabledLanguages,omitempty"`
}

// IndexesCode reports whether the config turns code indexing on. Any code
// setting other than disabledLanguages counts.
func (c LocalCodeConfig) IndexesCode() bool {
	return c.Enabled || c.TSParseTimeout > 0 || c.Python != nil || c.Go != nil ||
		c.TypeScript != nil || c.JavaScript != nil || c.CSharp != nil || c.PHP != nil
}

// LanguageBlocks returns pointers to every per-language block, so callers can
// read or replace them without listing the languages again.
func (c *LocalCodeConfig) LanguageBlocks() []**LocalCodeLangConfig {
	return []**LocalCodeLangConfig{&c.Python, &c.Go, &c.TypeScript, &c.JavaScript, &c.CSharp, &c.PHP}
}

// LocalCodeLangConfig configures a single language block.
type LocalCodeLangConfig struct {
	Roots  []string `yaml:"roots,omitempty"`  // module roots (e.g., src, packages)
	Scan   []string `yaml:"scan,omitempty"`   // optional override globs for this language
	Ignore []string `yaml:"ignore,omitempty"` // optional exclusions for this language
}

// LocalGraphConfig configures graph analysis behavior.
type LocalGraphConfig struct {
	Ignore           []string              `yaml:"ignore,omitempty"`           // notes to exclude from graph analysis
	KeyNotePatterns  []string              `yaml:"keyNotePatterns,omitempty"`  // patterns for key notes
	AuthorityFactors []AuthorityFactorRule `yaml:"authorityFactors,omitempty"` // authority score multipliers
}

// LocalRuntimeConfig configures the per-vault runtime process (SPEC-0104):
// whether clients may auto-start a headless `rzm serve`, and how long a
// headless runtime lives without client activity.
type LocalRuntimeConfig struct {
	Autostart   *bool  `yaml:"autostart,omitempty"`
	IdleTimeout string `yaml:"idleTimeout,omitempty"`
}

// LocalTasksConfig configures scheduled tasks (SPEC-0127): the branch task
// files are read from and work starts from, and whether tasks run
// automatically or only notify unless a person overrides a task.
type LocalTasksConfig struct {
	PrimaryRef  string `yaml:"primaryRef,omitempty"`
	DefaultMode string `yaml:"defaultMode,omitempty"`
}

// TemplateConfig configures output directories for template artifacts.
type TemplateConfig struct {
	Claude    *AgentTemplateConfig  `yaml:"claude,omitempty"`
	Codex     *AgentTemplateConfig  `yaml:"codex,omitempty"`
	Cursor    *CursorTemplateConfig `yaml:"cursor,omitempty"`
	AgentsMd  string                `yaml:"agentsMd,omitempty"`
	ClaudeMd  string                `yaml:"claudeMd,omitempty"`
	RhizomeMd string                `yaml:"rhizomeMd,omitempty"`
}

// AgentTemplateConfig configures output directories for a specific agent (Claude/Codex).
type AgentTemplateConfig struct {
	Skills   string `yaml:"skills,omitempty"`
	Commands string `yaml:"commands,omitempty"`
	Prompts  string `yaml:"prompts,omitempty"`
}

// CursorTemplateConfig configures output directories for Cursor.
type CursorTemplateConfig struct {
	Rules    string `yaml:"rules,omitempty"`
	Commands string `yaml:"commands,omitempty"`
}

// LocalRhizomeConfig configures which Rhizome CLI a repo expects agents and
// humans to use.
type LocalRhizomeConfig struct {
	BinaryManager string `yaml:"binaryManager,omitempty"`
	Version       string `yaml:"version,omitempty"`
	DevBinaryDir  string `yaml:"devBinaryDir,omitempty"`
	BinaryDir     string `yaml:"binaryDir,omitempty"`
	BinaryPath    string `yaml:"binaryPath,omitempty"` // deprecated: use implicit .rhizome/bin/<os>-<arch>/rzm
}

// BinaryManagerExternal declares that a tool outside Rhizome owns the
// repository's Rhizome version and executable selection.
const BinaryManagerExternal = "external"

// UsesExternalBinaryManager validates and reports the repository binary owner.
// WHY: callers must not route around an unknown manager or mixed ownership;
// one decision point keeps config loading, dispatch, and update behavior aligned.
func (c LocalRhizomeConfig) UsesExternalBinaryManager() (bool, error) {
	manager := strings.TrimSpace(c.BinaryManager)
	if manager == "" {
		return false, nil
	}
	if manager != BinaryManagerExternal {
		return false, fmt.Errorf(
			"unsupported rhizome.binaryManager %q; use %q or omit binaryManager for Rhizome-managed installation",
			manager,
			BinaryManagerExternal,
		)
	}

	conflicts := make([]string, 0, 4)
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "rhizome.version", value: c.Version},
		{name: "rhizome.devBinaryDir", value: c.DevBinaryDir},
		{name: "rhizome.binaryDir", value: c.BinaryDir},
		{name: "rhizome.binaryPath", value: c.BinaryPath},
	} {
		if strings.TrimSpace(field.value) != "" {
			conflicts = append(conflicts, field.name)
		}
	}
	if len(conflicts) > 0 {
		return false, fmt.Errorf(
			"rhizome.binaryManager %q cannot be combined with %s; remove the Rhizome-managed setting(s) because an external manager owns the version and binary",
			BinaryManagerExternal,
			strings.Join(conflicts, ", "),
		)
	}
	return true, nil
}

const localWorkflowStateFile = "workflows.yml"

// WorkflowTemplateAddons records optional init templates the user explicitly
// enabled or disabled. Required dependencies are resolved from template
// metadata and are not persisted here.
type WorkflowTemplateAddons struct {
	Enabled  []string `yaml:"enabled,omitempty"`
	Disabled []string `yaml:"disabled,omitempty"`
}

// WorkflowTemplateUpdatePolicy is retired. It is read only so rzm init can
// remove it; the generated-files record decides updates now.
type WorkflowTemplateUpdatePolicy struct {
	Docs         string `yaml:"docs,omitempty"`
	Skills       string `yaml:"skills,omitempty"`
	ManagedDocs  string `yaml:"managedDocs,omitempty"`
	Ontology     string `yaml:"ontology,omitempty"`
	QueryRecipes string `yaml:"queryRecipes,omitempty"`
	Views        string `yaml:"views,omitempty"`
}

// WorkflowTemplateManagement records init starter management state separate
// from adoption. Workflow templates remain the record of adopted starters;
// ejected starters stay adopted but are no longer refreshed by init.
type WorkflowTemplateManagement struct {
	Ejected []string `yaml:"ejected,omitempty"`
	// UpdatePolicy and SourceFingerprints are retired and read only so rzm
	// init can remove them without unknown-key warnings in other commands.
	UpdatePolicy       WorkflowTemplateUpdatePolicy `yaml:"updatePolicy,omitempty"`
	SourceFingerprints map[string]string            `yaml:"sourceFingerprints,omitempty"`
}

// LocalWorkflowConfig is stored in .rhizome/workflows.yml. It is kept separate
// from config.yml because starter adoption and refresh bookkeeping are project
// workflow state, not runtime settings.
type LocalWorkflowConfig struct {
	Templates  []string                   `yaml:"templates,omitempty"`
	Addons     WorkflowTemplateAddons     `yaml:"addons,omitempty"`
	Management WorkflowTemplateManagement `yaml:"management,omitempty"`
	Warnings   []ConfigWarning            `yaml:"-"`
	sourceYAML *yaml.Node                 `yaml:"-"`
}

type legacyWorkflowLocalConfig struct {
	LocalConfig                `yaml:",inline"`
	WorkflowTemplates          []string                   `yaml:"workflowTemplates,omitempty"`
	WorkflowTemplateAddons     WorkflowTemplateAddons     `yaml:"workflowTemplateAddons,omitempty"`
	WorkflowTemplateManagement WorkflowTemplateManagement `yaml:"workflowTemplateManagement,omitempty"`
}

// LocalValidationSuiteConfig composes one configured validation suite.
// Check-name validation and suite composition belong to pkg/validate.
type LocalValidationSuiteConfig struct {
	Add  []string `yaml:"add,omitempty"`
	Skip []string `yaml:"skip,omitempty"`
}

// LocalValidationConfig configures the independently composed default and all
// validation suites. Audit membership is fixed and is not configurable.
type LocalValidationConfig struct {
	Default     LocalValidationSuiteConfig `yaml:"default,omitempty"`
	All         LocalValidationSuiteConfig `yaml:"all,omitempty"`
	BrokenLinks LocalBrokenLinksConfig     `yaml:"brokenLinks,omitempty"`
}

// Placeholder policies for unresolved links whose target never existed.
const (
	// PlaceholderLinksHistory (the default) uses git history: only links whose
	// target was deleted or renamed away count as broken; links that never had
	// a target are placeholders reported by the placeholder-links check.
	PlaceholderLinksHistory = "history"
	// PlaceholderLinksStrict counts every unresolved link as broken.
	PlaceholderLinksStrict = "strict"
)

// LocalBrokenLinksConfig tunes the broken-links check.
type LocalBrokenLinksConfig struct {
	Placeholders string `yaml:"placeholders,omitempty"`
}

// LocalConfig represents the structure of a project-local .rhizome/config.yml file.
type LocalConfig struct {
	Rhizome LocalRhizomeConfig     `yaml:"rhizome,omitempty"`
	Notes   LocalVaultConfig       `yaml:"notes,omitempty"`
	Code    LocalCodeConfig        `yaml:"code,omitempty"`
	FileCtx LocalFileContextConfig `yaml:"fileContext,omitempty"`
	// Validation composes named checks into the default and all suites.
	Validation LocalValidationConfig `yaml:"validation,omitempty"`
	// WorkflowTemplates records the active init workflow bundles after merging
	// .rhizome/workflows.yml into the in-memory project config.
	WorkflowTemplates          []string                   `yaml:"-"`
	WorkflowTemplateAddons     WorkflowTemplateAddons     `yaml:"-"`
	WorkflowTemplateManagement WorkflowTemplateManagement `yaml:"-"`
	Warnings                   []ConfigWarning            `yaml:"-"`
	sourceYAML                 *yaml.Node                 `yaml:"-"`
	workflowSourceYAML         *yaml.Node                 `yaml:"-"`

	// New top-level config sections (replacing agent: block)
	NoteEmbeddings *embeddings.Config `yaml:"noteEmbeddings,omitempty"` // semantic embeddings for notes
	CodeEmbeddings *embeddings.Config `yaml:"codeEmbeddings,omitempty"` // semantic embeddings for code
	Graph          *LocalGraphConfig  `yaml:"graph,omitempty"`          // graph analysis config
	Agents         *AgentPreferences  `yaml:"agents,omitempty"`         // agent harness preferences (cursor/claude/codex/.agents)
	LLM            *llm.Config        `yaml:"llm,omitempty"`            // local LLM profile overrides

	// IndexPath configures the unified SQLite DB path. Optional; defaults to .rhizome/db.sqlite.
	IndexPath string `yaml:"indexPath,omitempty"`

	// BudgetChars sets the default character budget for LLM context packing (file_context, vault_context, semantic_query).
	// When 0 or omitted, uses DefaultBudgetChars.
	BudgetChars int `yaml:"budgetChars,omitempty"`

	// Compression configures intent-driven compression for context packing.
	// When enabled and content exceeds budget, uses an LLM to compress intelligently.
	Compression *LocalCompressionConfig `yaml:"compression,omitempty"`

	// Runtime configures the vault runtime process; nil means defaults.
	Runtime     *LocalRuntimeConfig `yaml:"runtime,omitempty"`
	Diagnostics *diagnostics.Config `yaml:"diagnostics,omitempty"`

	// Tasks configures scheduled tasks; nil means defaults.
	Tasks *LocalTasksConfig `yaml:"tasks,omitempty"`

	// Templates configures output directories for template artifacts (skills, commands, prompts).
	Templates *TemplateConfig `yaml:"templates,omitempty"`
}

// LocalVaultConfig is the repo-local notes section of a local config file.
// Relative globs resolve against the directory containing .rhizome/config.yml.
type LocalVaultConfig struct {
	Includes []string `yaml:"includes,omitempty"` // glob patterns
	Excludes []string `yaml:"excludes,omitempty"` // exclusion globs
	Links    string   `yaml:"links,omitempty"`    // wikilinks | markdown | both
}

// LocalFileContextConfig configures module-level documentation discovery for file_context.
type LocalFileContextConfig struct {
	DocPatterns        []string `yaml:"docPatterns,omitempty"`        // priority-ordered doc filenames
	MaxEmptyLevels     int      `yaml:"maxEmptyLevels,omitempty"`     // stop after this many empty ancestor dirs (0=use default)
	ContextBudget      int      `yaml:"contextBudget,omitempty"`      // max characters for ancestor docs (0=use default)
	IncludeDocsInGraph *bool    `yaml:"includeDocsInGraph,omitempty"` // when true, docPatterns are added as graph notes (default: true)
	// ExpandNoteLinks controls a bounded "hub/MOC expansion" behavior for file_context:
	// when a linked note matches this expression, its outgoing wikilinks are added as
	// additional linked notes (as stubs: blessed frontmatter only).
	//
	// Uses the same boolean DSL as other tools (terms like tag:foo, find:bar and operators AND/OR/NOT).
	ExpandNoteLinks      []string `yaml:"expandNoteLinks,omitempty"`
	ExpandNoteLinksLimit int      `yaml:"expandNoteLinksLimit,omitempty"` // maximum additional links to add (0=use default)
}

// LocalCompressionConfig configures intent-driven compression for context packing.
type LocalCompressionConfig struct {
	// Enabled controls whether compression is used when content exceeds budget.
	// When true (and an API key is available), uses an LLM to intelligently compress.
	// Default: true if CEREBRAS_API_KEY is set.
	Enabled *bool `yaml:"enabled,omitempty"`

	// Provider is the LLM provider to use (cerebras, openai, anthropic, etc.).
	// Default: cerebras
	Provider string `yaml:"provider,omitempty"`

	// Model is the provider-specific model to use.
	// Default: gpt-oss-120b (for cerebras)
	Model string `yaml:"model,omitempty"`

	// TimeoutMS is the timeout in milliseconds for compression requests.
	// Default: 5000
	TimeoutMS int `yaml:"timeoutMs,omitempty"`

	// MaxInputTokens is the model's input token limit, used to determine
	// how much content to collect for compression. Default: 100000
	MaxInputTokens int `yaml:"maxInputTokens,omitempty"`

	// MaxOutputTokens caps tokens returned in a single compression response.
	// Default: provider-specific; Cerebras uses a generous budget-based limit.
	MaxOutputTokens int `yaml:"maxOutputTokens,omitempty"`

	// ReasoningTokenReserve leaves headroom for model reasoning.
	// Default: 20000 (tuned for gpt-oss-120b).
	ReasoningTokenReserve int `yaml:"reasoningTokenReserve,omitempty"`

	// ChunkChars caps approximate input size (in chars) per compression request.
	// Default: derived from MaxInputTokens to leave output headroom.
	ChunkChars int `yaml:"chunkChars,omitempty"`

	// Parallelism sets the maximum number of concurrent compression requests.
	// Default: small safe value.
	Parallelism int `yaml:"parallelism,omitempty"`

	// ReasoningEffort controls the reasoning effort for providers that support it.
	// Valid values: "none", "low", "medium", "high". Default: "low" (for cerebras).
	ReasoningEffort string `yaml:"reasoningEffort,omitempty"`
}
