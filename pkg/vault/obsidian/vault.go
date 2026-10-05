package obsidian

import (
	"path/filepath"

	"github.com/atomicobject/rhizome/pkg/llm"
)

type VaultManager interface {
	DefaultName() (string, error)
	SetDefaultName(name string) error
	Path() (string, error)
	Definition() (VaultDefinition, error)
}

type Vault struct {
	Name               string
	ObsidianConfigFile func() (string, error)
}

// VaultDefinition describes either a classic single-root vault (Path) or a
// collection-style vault defined by globs (Root + Includes).
//
// Only one of Path or Root should normally be set:
//   - Path: classic Obsidian vault rooted at a single directory
//   - Root: collection vault with files matching Includes globs
//
// If both are set, the vault is treated as a collection rooted at Root;
// Path is ignored for discovery and should be considered legacy.
type VaultDefinition struct {
	Name     string   `json:"name" yaml:"name"`
	Path     string   `json:"path,omitempty" yaml:"path,omitempty"`         // classic vault root
	Root     string   `json:"root,omitempty" yaml:"root,omitempty"`         // collection root for include globs
	Includes []string `json:"includes,omitempty" yaml:"includes,omitempty"` // globs relative to Root
	Excludes []string `json:"excludes,omitempty" yaml:"excludes,omitempty"` // glob or prefix patterns to exclude
	Links    string   `json:"links,omitempty" yaml:"links,omitempty"`       // wikilinks | markdown | both
}

// IsCollection reports whether the definition represents a glob-based collection.
// A vault is a collection if Root is set (with or without explicit Includes).
// When Includes is empty, the default pattern **/*.md is used.
func (d VaultDefinition) IsCollection() bool {
	return d.Root != ""
}

// BasePath returns the root filesystem path for the vault.
// For collections (Root is set), returns Root.
// For classic vaults, returns Path.
// This is consistent with IsCollection() - if IsCollection() is true, BasePath() returns Root.
func (d VaultDefinition) BasePath() string {
	// Prefer Root for collections (consistent with IsCollection)
	if d.Root != "" {
		return filepath.Clean(d.Root)
	}
	if d.Path != "" {
		return filepath.Clean(d.Path)
	}
	return ""
}

// LinkType constants for VaultDefinition.Links field.
const (
	LinkTypeWikilinks = "wikilinks"
	LinkTypeMarkdown  = "markdown"
	LinkTypeBoth      = "both"
)

// SupportsWikilinks returns true if the vault supports wikilink syntax.
func (d VaultDefinition) SupportsWikilinks() bool {
	switch d.Links {
	case LinkTypeMarkdown:
		return false
	case LinkTypeBoth, LinkTypeWikilinks, "":
		return true
	default:
		return true // default to supporting wikilinks
	}
}

// SupportsMarkdownLinks returns true if the vault supports markdown link syntax.
func (d VaultDefinition) SupportsMarkdownLinks() bool {
	switch d.Links {
	case LinkTypeWikilinks:
		return false
	case LinkTypeBoth, LinkTypeMarkdown, "":
		return true // default to supporting both
	default:
		return true
	}
}

// CliConfig stores user-managed vault mappings (classic or collection).
type CliConfig struct {
	DefaultVaultName string                     `json:"default_vault_name" yaml:"default_vault_name"`
	Vaults           map[string]VaultDefinition `json:"vaults,omitempty" yaml:"vaults,omitempty"`
	LLM              *llm.Config                `json:"llm,omitempty" yaml:"llm,omitempty"`
	Env              map[string]string          `json:"env,omitempty" yaml:"env,omitempty"`
	Agent            *AgentUserConfig           `json:"agent,omitempty" yaml:"agent,omitempty"`
	CredentialSkips  map[string]bool            `json:"credential_skips,omitempty" yaml:"credentialSkips,omitempty"`
}

type AgentUserConfig struct {
	Harness   string                        `json:"harness,omitempty" yaml:"harness,omitempty"`
	Harnesses map[string]AgentHarnessConfig `json:"harnesses,omitempty" yaml:"harnesses,omitempty"`
}

type AgentHarnessConfig struct {
	Model          string `json:"model,omitempty" yaml:"model,omitempty"`
	Effort         string `json:"effort,omitempty" yaml:"effort,omitempty"`
	PermissionMode string `json:"permissionMode,omitempty" yaml:"permission-mode,omitempty"`
}

// ObsidianVaultConfig models Obsidian's own vault configuration file.
type ObsidianVaultConfig struct {
	Vaults map[string]VaultPathEntry `json:"vaults"`
}

// VaultPathEntry is preserved for parsing Obsidian's config file shape.
type VaultPathEntry struct {
	Path string `json:"path"`
}
