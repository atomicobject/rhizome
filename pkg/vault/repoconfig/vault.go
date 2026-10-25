package repoconfig

import (
	"github.com/atomicobject/rhizome/pkg/llm"
	"github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type VaultManager interface {
	DefaultName() (string, error)
	SetDefaultName(name string) error
	Path() (string, error)
	Definition() (obsidian.VaultDefinition, error)
}

type Vault struct {
	Name string
	Env  config.Env `json:"-" yaml:"-"`
}

// CliConfig stores user-managed vault mappings (classic or collection).
type CliConfig struct {
	DefaultVaultName string                              `json:"default_vault_name" yaml:"default_vault_name"`
	Vaults           map[string]obsidian.VaultDefinition `json:"vaults,omitempty" yaml:"vaults,omitempty"`
	LLM              *llm.Config                         `json:"llm,omitempty" yaml:"llm,omitempty"`
	Env              map[string]string                   `json:"env,omitempty" yaml:"env,omitempty"`
	Agent            *AgentUserConfig                    `json:"agent,omitempty" yaml:"agent,omitempty"`
	CredentialSource *CredentialSource                   `json:"credential_source,omitempty" yaml:"credential_source,omitempty"`
	CredentialSkips  map[string]bool                     `json:"credential_skips,omitempty" yaml:"credentialSkips,omitempty"`
}

// CredentialSource stores non-secret import references only in user configuration.
type CredentialSource struct {
	Account     string   `json:"account" yaml:"account"`
	Environment string   `json:"environment" yaml:"environment"`
	Keys        []string `json:"keys" yaml:"keys"`
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
