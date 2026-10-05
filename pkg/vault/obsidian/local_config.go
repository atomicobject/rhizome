package obsidian

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/fileio"
	"github.com/atomicobject/rhizome/pkg/llm"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/codepatterns"
	"gopkg.in/yaml.v3"
)

// ErrNoLocalConfig is returned when no local config file is found.
var ErrNoLocalConfig = errors.New("no .rhizome/config.yml found")

const LocalConfigDocsHint = "docs/reference/guides/Advanced configuration tuning.md"

var unknownYAMLFieldPattern = regexp.MustCompile(`field ([^ ]+) not found in type`)

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

// NonCanonicalConfigError is retained as the warning shape's historical name.
type NonCanonicalConfigError = ConfigWarning

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
	// When 0 or omitted, uses contextpack.DefaultBudgetChars.
	BudgetChars int `yaml:"budgetChars,omitempty"`

	// Compression configures intent-driven compression for context packing.
	// When enabled and content exceeds budget, uses an LLM to compress intelligently.
	Compression *LocalCompressionConfig `yaml:"compression,omitempty"`

	// Runtime configures the vault runtime process; nil means defaults.
	Runtime     *LocalRuntimeConfig `yaml:"runtime,omitempty"`
	Diagnostics *diagnostics.Config `yaml:"diagnostics,omitempty"`

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

func decodeLocalConfig(configPath string, data []byte) (*LocalConfig, error) {
	cfg, warnings, err := decodeCanonicalYAML[LocalConfig](configPath, data)
	if err != nil {
		return nil, err
	}
	cfg.Rhizome.BinaryManager = strings.TrimSpace(cfg.Rhizome.BinaryManager)
	if _, err := cfg.Rhizome.UsesExternalBinaryManager(); err != nil {
		return nil, fmt.Errorf("%s: %w", configPath, err)
	}
	cfg.Warnings = warnings
	cfg.sourceYAML, err = parseYAMLDocument(data)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func decodeCanonicalYAML[T any](configPath string, data []byte) (T, []ConfigWarning, error) {
	var cfg T
	if strings.TrimSpace(string(data)) == "" {
		return cfg, nil, nil
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return cfg, nil, nil
		}
		keys := unknownYAMLFields(err)
		if len(keys) == 0 {
			return cfg, nil, err
		}
		warning := ConfigWarning{
			Path:          configPath,
			OffendingKeys: keys,
			DocsHint:      LocalConfigDocsHint,
		}
		decoder = yaml.NewDecoder(bytes.NewReader(data))
		if err := decoder.Decode(&cfg); err != nil {
			return cfg, nil, err
		}
		if err := ensureSingleYAMLDocument(decoder); err != nil {
			return cfg, nil, err
		}
		return cfg, []ConfigWarning{warning}, nil
	}
	if err := ensureSingleYAMLDocument(decoder); err != nil {
		return cfg, nil, err
	}
	return cfg, nil, nil
}

func ensureSingleYAMLDocument(decoder *yaml.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("repo-local config must contain exactly one YAML document")
		}
		return err
	}
	return nil
}

func parseYAMLDocument(data []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if strings.TrimSpace(string(data)) != "" {
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, err
		}
	}
	if len(doc.Content) == 0 {
		doc.Kind = yaml.DocumentNode
		doc.Content = []*yaml.Node{{
			Kind: yaml.MappingNode,
			Tag:  "!!map",
		}}
		return &doc, nil
	}
	if doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("repo-local config must contain a YAML mapping")
	}
	return &doc, nil
}

func saveYAMLDocumentPreservingUnknown(path string, value any, source *yaml.Node, schema reflect.Type) error {
	desired, err := encodeYAMLDocument(value)
	if err != nil {
		return err
	}
	if source != nil {
		sourceRoot := yamlDocumentRoot(source)
		desiredRoot := yamlDocumentRoot(desired)
		if sourceRoot == nil || desiredRoot == nil {
			return errors.New("repo-local config must contain a YAML mapping")
		}
		mergeKnownYAMLMapping(sourceRoot, desiredRoot, schema)
		desired = source
	}
	root := yamlDocumentRoot(desired)
	if root != nil && len(root.Content) == 0 && strings.HasSuffix(path, localWorkflowStateFile) {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(LocalConfigYAMLIndent)
	if err := enc.Encode(desired); err != nil {
		_ = enc.Close()
		return err
	}
	_ = enc.Close()
	// Identical replacement would spuriously notify configuration watchers.
	if existing, err := fileio.ReadFile(path); err == nil && bytes.Equal(existing, buf.Bytes()) {
		return nil
	}
	return WriteFileAtomicPreservingMode(path, buf.Bytes(), 0o644)
}

func encodeYAMLDocument(value any) (*yaml.Node, error) {
	data, err := yaml.Marshal(value)
	if err != nil {
		return nil, err
	}
	return parseYAMLDocument(data)
}

func yamlDocumentRoot(doc *yaml.Node) *yaml.Node {
	if doc == nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	return doc.Content[0]
}

func mergeKnownYAMLMapping(existing, desired *yaml.Node, schema reflect.Type) {
	fields := yamlKnownFieldTypes(schema)
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fieldType := fields[key]
		existingIndex := yamlMappingKeyIndex(existing, key)
		desiredIndex := yamlMappingKeyIndex(desired, key)
		if desiredIndex >= 0 {
			desiredValue := desired.Content[desiredIndex+1]
			if existingIndex >= 0 && yamlStructType(fieldType) && existing.Content[existingIndex+1].Kind == yaml.MappingNode && desiredValue.Kind == yaml.MappingNode {
				mergeKnownYAMLMapping(existing.Content[existingIndex+1], desiredValue, fieldType)
				continue
			}
			yamlSetMappingValue(existing, key, desiredValue)
			continue
		}
		if existingIndex < 0 {
			continue
		}
		existingValue := existing.Content[existingIndex+1]
		if yamlStructType(fieldType) && existingValue.Kind == yaml.MappingNode {
			empty := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			mergeKnownYAMLMapping(existingValue, empty, fieldType)
			if len(existingValue.Content) > 0 {
				continue
			}
		}
		yamlDeleteMappingKey(existing, key)
	}
}

// MergeKnownYAMLMapping updates fields owned by schema while retaining
// unrecognized fields in the existing mapping, including nested fields.
func MergeKnownYAMLMapping(existing, desired *yaml.Node, schema any) {
	if existing == nil || desired == nil {
		return
	}
	mergeKnownYAMLMapping(existing, desired, reflect.TypeOf(schema))
}

func yamlKnownFieldTypes(schema reflect.Type) map[string]reflect.Type {
	for schema.Kind() == reflect.Pointer {
		schema = schema.Elem()
	}
	fields := make(map[string]reflect.Type)
	if schema.Kind() != reflect.Struct {
		return fields
	}
	for i := 0; i < schema.NumField(); i++ {
		field := schema.Field(i)
		if field.PkgPath != "" {
			continue
		}
		tag := field.Tag.Get("yaml")
		parts := strings.Split(tag, ",")
		if parts[0] == "-" {
			continue
		}
		if slices.Contains(parts[1:], "inline") {
			for key, fieldType := range yamlKnownFieldTypes(field.Type) {
				fields[key] = fieldType
			}
			continue
		}
		name := parts[0]
		if name == "" {
			name = strings.ToLower(field.Name)
		}
		fields[name] = field.Type
	}
	return fields
}

func yamlStructType(valueType reflect.Type) bool {
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	return valueType.Kind() == reflect.Struct
}

func yamlMappingKeyIndex(mapping *yaml.Node, key string) int {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return i
		}
	}
	return -1
}

func yamlSetMappingValue(mapping *yaml.Node, key string, value *yaml.Node) {
	if index := yamlMappingKeyIndex(mapping, key); index >= 0 {
		mapping.Content[index+1] = value
		return
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}

func yamlDeleteMappingKey(mapping *yaml.Node, key string) {
	if index := yamlMappingKeyIndex(mapping, key); index >= 0 {
		mapping.Content = append(mapping.Content[:index], mapping.Content[index+2:]...)
	}
}

func unknownYAMLFields(err error) []string {
	matches := unknownYAMLFieldPattern.FindAllStringSubmatch(err.Error(), -1)
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if len(match) > 1 {
			seen[match[1]] = struct{}{}
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func hasAnyTopLevelYAMLKey(data []byte, keys ...string) bool {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		return false
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return false
	}
	wanted := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		wanted[key] = struct{}{}
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if _, ok := wanted[root.Content[i].Value]; ok {
			return true
		}
	}
	return false
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

// FindLocalConfig searches for a .rhizome/config.yml file starting from dir
// and walking up to parent directories. Returns the directory containing
// the config and the parsed config, or ErrNoLocalConfig if not found.
func FindLocalConfig(startDir string) (string, *LocalConfig, error) {
	dir := paths.ResolveSymlinks(startDir).String()
	if dir == "" {
		return "", nil, errors.New("start directory required")
	}
	gitRoot := findNearestGitRoot(dir)

	for {
		cfg, err := LoadLocalConfig(dir)
		if err == nil {
			return dir, cfg, nil
		}
		if !errors.Is(err, ErrNoLocalConfig) {
			return "", nil, err
		}
		if gitRoot != "" && dir == gitRoot {
			// Stop at the nearest repo root so tools launched in a nested project
			// do not inherit a parent checkout's Rhizome config.
			return "", nil, ErrNoLocalConfig
		}

		dirOS := filepath.FromSlash(dir)
		parent := filepath.Dir(dirOS)
		if parent == dirOS {
			// Reached filesystem root
			return "", nil, ErrNoLocalConfig
		}
		dir = paths.ResolveSymlinks(parent).String()
	}
}

// FindLocalConfigForDelegation loads only repo-binary selection fields. The
// selected binary, not the invoking global binary, owns the full config schema.
func FindLocalConfigForDelegation(startDir string) (string, *LocalConfig, error) {
	dir := paths.ResolveSymlinks(startDir).String()
	if dir == "" {
		return "", nil, errors.New("start directory required")
	}
	gitRoot := findNearestGitRoot(dir)

	for {
		configPath := rhizomeConfigPath(dir)
		data, err := fileio.ReadFile(configPath)
		if err == nil {
			var selection struct {
				Rhizome LocalRhizomeConfig `yaml:"rhizome,omitempty"`
			}
			decoder := yaml.NewDecoder(bytes.NewReader(data))
			if err := decoder.Decode(&selection); err != nil {
				return "", nil, err
			}
			if err := ensureSingleYAMLDocument(decoder); err != nil {
				return "", nil, err
			}
			selection.Rhizome.BinaryManager = strings.TrimSpace(selection.Rhizome.BinaryManager)
			if _, err := selection.Rhizome.UsesExternalBinaryManager(); err != nil {
				return "", nil, fmt.Errorf("%s: %w", configPath, err)
			}
			return dir, &LocalConfig{Rhizome: selection.Rhizome}, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", nil, err
		}
		if gitRoot != "" && dir == gitRoot {
			return "", nil, ErrNoLocalConfig
		}
		dirOS := filepath.FromSlash(dir)
		parent := filepath.Dir(dirOS)
		if parent == dirOS {
			return "", nil, ErrNoLocalConfig
		}
		dir = paths.ResolveSymlinks(parent).String()
	}
}

func findNearestGitRoot(startDir string) string {
	dir := paths.ResolveSymlinks(startDir).String()
	if dir == "" {
		return ""
	}

	for {
		if _, err := os.Stat(filepath.Join(filepath.FromSlash(dir), ".git")); err == nil {
			return dir
		}
		dirOS := filepath.FromSlash(dir)
		parent := filepath.Dir(dirOS)
		if parent == dirOS {
			return ""
		}
		dir = paths.ResolveSymlinks(parent).String()
	}
}

// LocalConfigToDefinition converts a LocalConfig to a VaultDefinition,
// resolving relative paths against the config file's directory.
func LocalConfigToDefinition(configDir string, cfg *LocalConfig) VaultDefinition {
	// The local config format is repo-rooted: includes/excludes stay as authoring
	// globs and are interpreted by discovery/ignore code relative to Root.
	def := VaultDefinition{
		Root:     configDir,
		Includes: cfg.Notes.Includes,
		Excludes: cfg.Notes.Excludes,
		Links:    cfg.Notes.Links,
	}

	if len(def.Includes) == 0 {
		def.Includes = []string{"**/*.md"}
	}

	return def
}

// LoadDefinitionFromPath attempts to load a VaultDefinition from a directory
// containing a local config file. Returns ErrNoLocalConfig if not found.
func LoadDefinitionFromPath(dir string) (VaultDefinition, error) {
	absDir := paths.ResolveSymlinks(dir).String()
	if absDir == "" {
		return VaultDefinition{}, errors.New("directory required")
	}

	cfg, err := LoadLocalConfig(absDir)
	if err != nil {
		return VaultDefinition{}, err
	}

	def := LocalConfigToDefinition(absDir, cfg)
	// Use directory name as vault name if not otherwise specified
	def.Name = filepath.Base(absDir)
	return def, nil
}

// HasLocalConfig checks if a directory contains a local config file.
func HasLocalConfig(dir string) bool {
	_, err := os.Stat(rhizomeConfigPath(dir))
	return err == nil
}

// DefaultCodeConfig values applied when omitted.
var DefaultCodeConfig = LocalCodeConfig{
	Scan:   []string{},
	Ignore: codepatterns.DefaultIgnoreGlobs(),
}

// FileContextConfigDefaults define defaults for file_context doc discovery.
var FileContextConfigDefaults = LocalFileContextConfig{
	DocPatterns:        []string{"CONTEXT.md"},
	MaxEmptyLevels:     3,
	ContextBudget:      contextpack.DefaultBudgetChars,
	IncludeDocsInGraph: boolPtr(true),
	ExpandNoteLinks: []string{
		"tag:type/hub",
		"OR",
		"tag:maps-of-content",
		"OR",
		"tag:type/moc",
	},
	ExpandNoteLinksLimit: 8,
}

// FileContextConfigFromLocal applies defaults to a LocalFileContextConfig.
func FileContextConfigFromLocal(local LocalFileContextConfig) LocalFileContextConfig {
	result := LocalFileContextConfig{
		DocPatterns:          local.DocPatterns,
		MaxEmptyLevels:       local.MaxEmptyLevels,
		ContextBudget:        local.ContextBudget,
		IncludeDocsInGraph:   local.IncludeDocsInGraph,
		ExpandNoteLinks:      local.ExpandNoteLinks,
		ExpandNoteLinksLimit: local.ExpandNoteLinksLimit,
	}

	if len(result.DocPatterns) == 0 {
		result.DocPatterns = FileContextConfigDefaults.DocPatterns
	}
	if result.MaxEmptyLevels <= 0 {
		result.MaxEmptyLevels = FileContextConfigDefaults.MaxEmptyLevels
	}
	if result.ContextBudget <= 0 {
		result.ContextBudget = FileContextConfigDefaults.ContextBudget
	}
	if result.IncludeDocsInGraph == nil {
		result.IncludeDocsInGraph = FileContextConfigDefaults.IncludeDocsInGraph
	}
	if len(result.ExpandNoteLinks) == 0 {
		result.ExpandNoteLinks = FileContextConfigDefaults.ExpandNoteLinks
	}
	if result.ExpandNoteLinksLimit <= 0 {
		result.ExpandNoteLinksLimit = FileContextConfigDefaults.ExpandNoteLinksLimit
	}

	return result
}

// FileContextConfigFromLocalOrDefault returns defaults when the local config is missing.
func FileContextConfigFromLocalOrDefault(localCfg *LocalConfig) LocalFileContextConfig {
	if localCfg == nil {
		return FileContextConfigDefaults
	}
	return FileContextConfigFromLocal(localCfg.FileCtx)
}

// FileContextConfigForVault loads the vault config and applies defaults for file_context.
// Returns defaults alongside any load error so callers can proceed best-effort.
func FileContextConfigForVault(vaultPath string) (LocalFileContextConfig, error) {
	if strings.TrimSpace(vaultPath) == "" {
		return FileContextConfigDefaults, nil
	}
	localCfg, err := LoadLocalConfig(vaultPath)
	if err != nil {
		return FileContextConfigDefaults, err
	}
	return FileContextConfigFromLocal(localCfg.FileCtx), nil
}

// NormalizeCodeRefPatterns returns includes/excludes for coderefs scanning.
// Order of precedence:
// 1) code.scan / code.ignore
// 2) enabled language blocks and their defaults
// 3) disabled (no includes)
func NormalizeCodeRefPatterns(local LocalConfig) (includes, excludes []string) {
	// New config
	if len(local.Code.Scan) > 0 {
		includes = append([]string{}, local.Code.Scan...)
	}
	if len(local.Code.Ignore) > 0 {
		excludes = append([]string{}, local.Code.Ignore...)
	} else {
		excludes = append([]string{}, DefaultCodeConfig.Ignore...)
	}

	// Language blocks also imply coderefs for that language; add defaults.
	if local.Code.Python != nil {
		if len(local.Code.Python.Scan) > 0 {
			includes = append(includes, local.Code.Python.Scan...)
		} else {
			includes = append(includes, codepatterns.DefaultPythonGlobs()...)
		}
		if len(local.Code.Python.Ignore) > 0 {
			excludes = append(excludes, local.Code.Python.Ignore...)
		}
	}
	if local.Code.Go != nil {
		if len(local.Code.Go.Scan) > 0 {
			includes = append(includes, local.Code.Go.Scan...)
		} else {
			includes = append(includes, codepatterns.DefaultGoGlobs()...)
		}
		if len(local.Code.Go.Ignore) > 0 {
			excludes = append(excludes, local.Code.Go.Ignore...)
		}
	}
	if local.Code.TypeScript != nil || local.Code.JavaScript != nil {
		langCfgs := []*LocalCodeLangConfig{local.Code.TypeScript, local.Code.JavaScript}
		// If either block provides scan globs, prefer those; otherwise include default TS+JS globs.
		haveScan := false
		for _, cfg := range langCfgs {
			if cfg != nil && len(cfg.Scan) > 0 {
				haveScan = true
				includes = append(includes, cfg.Scan...)
			}
		}
		if !haveScan {
			includes = append(includes, codepatterns.DefaultTypeScriptGlobs()...)
		}
		for _, cfg := range langCfgs {
			if cfg != nil && len(cfg.Ignore) > 0 {
				excludes = append(excludes, cfg.Ignore...)
			}
		}
	}
	if local.Code.PHP != nil {
		if len(local.Code.PHP.Scan) > 0 {
			includes = append(includes, local.Code.PHP.Scan...)
		} else {
			includes = append(includes, codepatterns.DefaultPHPGlobs()...)
		}
		if len(local.Code.PHP.Ignore) > 0 {
			excludes = append(excludes, local.Code.PHP.Ignore...)
		}
	}

	// If explicitly enabled and no includes were specified, use defaults.
	if local.Code.Enabled && len(includes) == 0 {
		includes = append(includes, codepatterns.DefaultScanGlobs()...)
	}

	// Deduplicate empty values handled by caller.
	return includes, excludes
}

// NormalizePythonCodeConfig extracts python-specific config (coderefs + anchors).
func NormalizePythonCodeConfig(local LocalConfig) (enabled bool, roots, scan, ignore []string) {
	if local.Code.Python == nil {
		return false, nil, nil, nil
	}
	enabled = true
	if len(local.Code.Python.Roots) > 0 {
		roots = append(roots, local.Code.Python.Roots...)
	}
	if len(local.Code.Python.Scan) > 0 {
		scan = append(scan, local.Code.Python.Scan...)
	} else {
		scan = append(scan, codepatterns.DefaultPythonGlobs()...)
	}
	if len(local.Code.Python.Ignore) > 0 {
		ignore = append(ignore, local.Code.Python.Ignore...)
	}
	return
}

// NormalizeGoCodeConfig extracts go-specific config (coderefs + anchors).
func NormalizeGoCodeConfig(local LocalConfig) (enabled bool, roots, scan, ignore []string) {
	if local.Code.Go == nil {
		return false, nil, nil, nil
	}
	enabled = true
	if len(local.Code.Go.Roots) > 0 {
		roots = append(roots, local.Code.Go.Roots...)
	}
	if len(local.Code.Go.Scan) > 0 {
		scan = append(scan, local.Code.Go.Scan...)
	} else {
		scan = append(scan, codepatterns.DefaultGoGlobs()...)
	}
	if len(local.Code.Go.Ignore) > 0 {
		ignore = append(ignore, local.Code.Go.Ignore...)
	}
	return
}

// NormalizeTSCodeConfig extracts TypeScript/JavaScript config (coderefs + anchors).
// Both `code.typescript` and `code.javascript` map to the `ts` codeanchor language.
func NormalizeTSCodeConfig(local LocalConfig) (enabled bool, roots, scan, ignore []string) {
	cfgs := []*LocalCodeLangConfig{local.Code.TypeScript, local.Code.JavaScript}
	if cfgs[0] == nil && cfgs[1] == nil {
		return false, nil, nil, nil
	}
	enabled = true

	for _, cfg := range cfgs {
		if cfg == nil {
			continue
		}
		if len(cfg.Roots) > 0 {
			roots = append(roots, cfg.Roots...)
		}
		if len(cfg.Ignore) > 0 {
			ignore = append(ignore, cfg.Ignore...)
		}
		if len(cfg.Scan) > 0 {
			scan = append(scan, cfg.Scan...)
		}
	}
	if len(scan) == 0 {
		scan = append(scan, codepatterns.DefaultTypeScriptGlobs()...)
	}
	return
}

// NormalizeCSharpCodeConfig extracts csharp-specific config (coderefs + anchors).
func NormalizeCSharpCodeConfig(local LocalConfig) (enabled bool, roots, scan, ignore []string) {
	if local.Code.CSharp == nil {
		return false, nil, nil, nil
	}
	enabled = true
	if len(local.Code.CSharp.Roots) > 0 {
		roots = append(roots, local.Code.CSharp.Roots...)
	}
	if len(local.Code.CSharp.Scan) > 0 {
		scan = append(scan, local.Code.CSharp.Scan...)
	} else {
		scan = append(scan, "**/*.cs")
	}
	if len(local.Code.CSharp.Ignore) > 0 {
		ignore = append(ignore, local.Code.CSharp.Ignore...)
	}
	return
}

// NormalizePHPCodeConfig extracts php-specific config (coderefs + anchors).
func NormalizePHPCodeConfig(local LocalConfig) (enabled bool, roots, scan, ignore []string) {
	if local.Code.PHP == nil {
		return false, nil, nil, nil
	}
	enabled = true
	if len(local.Code.PHP.Roots) > 0 {
		roots = append(roots, local.Code.PHP.Roots...)
	}
	if len(local.Code.PHP.Scan) > 0 {
		scan = append(scan, local.Code.PHP.Scan...)
	} else {
		scan = append(scan, codepatterns.DefaultPHPGlobs()...)
	}
	if len(local.Code.PHP.Ignore) > 0 {
		ignore = append(ignore, local.Code.PHP.Ignore...)
	}
	return
}

func boolPtr(b bool) *bool { return &b }

// EffectiveBudgetChars returns the budget from config or the default if unset.
func (c *LocalConfig) EffectiveBudgetChars() int {
	if c != nil && c.BudgetChars > 0 {
		return c.BudgetChars
	}
	return contextpack.DefaultBudgetChars
}

// GetBudgetChars returns the effective budget for the given vault path.
// Loads config from the vault path and returns the configured or default budget.
func GetBudgetChars(vaultPath string) int {
	if vaultPath == "" {
		return contextpack.DefaultBudgetChars
	}
	cfg, err := LoadLocalConfig(vaultPath)
	if err != nil || cfg == nil {
		return contextpack.DefaultBudgetChars
	}
	return cfg.EffectiveBudgetChars()
}

// ScopeConfig contains only fields that affect index scan scope.
// Changes to these fields require re-evaluating which files should be indexed.
type ScopeConfig struct {
	NotesIncludes []string `json:"notesIncludes,omitempty"`
	NotesExcludes []string `json:"notesExcludes,omitempty"`
	CodeEnabled   bool     `json:"codeEnabled,omitempty"`
	PythonRoots   []string `json:"pythonRoots,omitempty"`
	PythonScan    []string `json:"pythonScan,omitempty"`
	PythonIgnore  []string `json:"pythonIgnore,omitempty"`
	GoRoots       []string `json:"goRoots,omitempty"`
	GoScan        []string `json:"goScan,omitempty"`
	GoIgnore      []string `json:"goIgnore,omitempty"`
	TSRoots       []string `json:"tsRoots,omitempty"`
	TSScan        []string `json:"tsScan,omitempty"`
	TSIgnore      []string `json:"tsIgnore,omitempty"`
	JSRoots       []string `json:"jsRoots,omitempty"`
	JSScan        []string `json:"jsScan,omitempty"`
	JSIgnore      []string `json:"jsIgnore,omitempty"`
	CSharpRoots   []string `json:"csharpRoots,omitempty"`
	CSharpScan    []string `json:"csharpScan,omitempty"`
	CSharpIgnore  []string `json:"csharpIgnore,omitempty"`
	PHPRoots      []string `json:"phpRoots,omitempty"`
	PHPScan       []string `json:"phpScan,omitempty"`
	PHPIgnore     []string `json:"phpIgnore,omitempty"`
	CodeScan      []string `json:"codeScan,omitempty"`
	CodeIgnore    []string `json:"codeIgnore,omitempty"`
}

// ScopeConfigHash returns a SHA256 hash of config fields that affect index scan scope.
// When this hash changes, files that were previously out-of-scope may now be in-scope
// and need to be indexed.
func (c *LocalConfig) ScopeConfigHash() string {
	if c == nil {
		return ""
	}

	sc := ScopeConfig{
		NotesIncludes: c.Notes.Includes,
		NotesExcludes: c.Notes.Excludes,
		CodeEnabled:   c.Code.Enabled,
		CodeScan:      c.Code.Scan,
		CodeIgnore:    c.Code.Ignore,
	}

	if c.Code.Python != nil {
		sc.PythonRoots = c.Code.Python.Roots
		sc.PythonScan = c.Code.Python.Scan
		sc.PythonIgnore = c.Code.Python.Ignore
	}
	if c.Code.Go != nil {
		sc.GoRoots = c.Code.Go.Roots
		sc.GoScan = c.Code.Go.Scan
		sc.GoIgnore = c.Code.Go.Ignore
	}
	if c.Code.TypeScript != nil {
		sc.TSRoots = c.Code.TypeScript.Roots
		sc.TSScan = c.Code.TypeScript.Scan
		sc.TSIgnore = c.Code.TypeScript.Ignore
	}
	if c.Code.JavaScript != nil {
		sc.JSRoots = c.Code.JavaScript.Roots
		sc.JSScan = c.Code.JavaScript.Scan
		sc.JSIgnore = c.Code.JavaScript.Ignore
	}
	if c.Code.CSharp != nil {
		sc.CSharpRoots = c.Code.CSharp.Roots
		sc.CSharpScan = c.Code.CSharp.Scan
		sc.CSharpIgnore = c.Code.CSharp.Ignore
	}
	if c.Code.PHP != nil {
		sc.PHPRoots = c.Code.PHP.Roots
		sc.PHPScan = c.Code.PHP.Scan
		sc.PHPIgnore = c.Code.PHP.Ignore
	}

	data, err := json.Marshal(sc)
	if err != nil {
		return ""
	}

	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
