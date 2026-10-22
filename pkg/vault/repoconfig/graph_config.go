package repoconfig

// Docs: [Graph (Hub)](docs/hubs/Graph (Hub).md), [List + prompt matching DSL](docs/reference/domain/List + prompt matching DSL.md)

import (
	"errors"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
)

// VaultConfig holds per-vault agent preferences (graph, semantic search, etc.).
type VaultConfig struct {
	GraphIgnore      []string              `json:"graphIgnore,omitempty" yaml:"graphIgnore,omitempty"`
	KeyNotePatterns  []string              `json:"keyNotePatterns,omitempty" yaml:"keyNotePatterns,omitempty"`
	AuthorityFactors []AuthorityFactorRule `json:"authorityFactors,omitempty" yaml:"authorityFactors,omitempty"`
	Agents           *AgentPreferences     `json:"agents,omitempty" yaml:"agents,omitempty"`
	// IndexPath configures the unified SQLite DB path (semantic + code intel). Optional; defaults to .rhizome/db.sqlite.
	IndexPath      string             `json:"indexPath,omitempty" yaml:"indexPath,omitempty"`
	Embeddings     *embeddings.Config `json:"embeddings,omitempty" yaml:"embeddings,omitempty"`
	CodeEmbeddings *embeddings.Config `json:"codeEmbeddings,omitempty" yaml:"codeEmbeddings,omitempty"`
	Code           *codeanchor.Config `json:"code,omitempty" yaml:"code,omitempty"`
}

type AgentPreferences struct {
	Cursor      string `json:"cursor,omitempty" yaml:"cursor,omitempty"`
	Claude      string `json:"claude,omitempty" yaml:"claude,omitempty"`
	Codex       string `json:"codex,omitempty" yaml:"codex,omitempty"`
	AgentSkills string `json:"agentSkills,omitempty" yaml:"agentSkills,omitempty"`
	AgentsMd    string `json:"agentsmd,omitempty" yaml:"agentsmd,omitempty"`
	// Hooks is on when the person turned on starter hooks; empty or off
	// leaves them off ([[agent-surface-integration-modes#^SPEC-0045-US5-AC2]]).
	Hooks string `json:"hooks,omitempty" yaml:"hooks,omitempty"`
}

func (a AgentPreferences) empty() bool {
	return strings.TrimSpace(a.Cursor) == "" &&
		strings.TrimSpace(a.Claude) == "" &&
		strings.TrimSpace(a.Codex) == "" &&
		strings.TrimSpace(a.AgentSkills) == "" &&
		strings.TrimSpace(a.AgentsMd) == "" &&
		strings.TrimSpace(a.Hooks) == ""
}

func (a AgentPreferences) Empty() bool {
	return a.empty()
}

// AuthorityFactorRule multiplies authority scores for notes that match criteria.
// Criteria are expressed using the list/prompt DSL (tag:/find:/key:value/paths with AND/OR/NOT).
type AuthorityFactorRule struct {
	Inputs []string `json:"inputs,omitempty" yaml:"inputs,omitempty"`
	Factor float64  `json:"factor" yaml:"factor"`
}

// SaveGraphConfig writes the canonical graph section in .rhizome/config.yml.
func SaveGraphConfig(vaultPath string, graphCfg LocalGraphConfig) error {
	localCfg, err := LoadLocalConfig(vaultPath)
	if err != nil {
		if !errors.Is(err, ErrNoLocalConfig) {
			return err
		}
		localCfg = &LocalConfig{}
	}
	localCfg.Graph = &LocalGraphConfig{
		Ignore:           append([]string{}, graphCfg.Ignore...),
		KeyNotePatterns:  append([]string{}, graphCfg.KeyNotePatterns...),
		AuthorityFactors: append([]AuthorityFactorRule{}, graphCfg.AuthorityFactors...),
	}
	return SaveLocalConfig(vaultPath, *localCfg)
}
