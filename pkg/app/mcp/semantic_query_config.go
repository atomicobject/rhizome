package mcp

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// resolveSemanticQueryConfig captures one repository snapshot for retrieval,
// packing and final wire budgeting. A runtime reload cannot split those policies.
func resolveSemanticQueryConfig(cfg Config) (Config, error) {
	if provider, ok := cfg.Runtime.(interface{ Configuration() repoconfig.Resolved }); ok {
		resolved := provider.Configuration()
		cfg.RepositoryConfig = &resolved
	}
	if cfg.RepositoryConfig == nil {
		resolved, err := repoconfig.Load(cfg.VaultPath)
		if err != nil {
			return Config{}, err
		}
		cfg.RepositoryConfig = &resolved
	} else if err := cfg.RepositoryConfig.ValidateFor(cfg.VaultPath); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// runSemanticQuery is the typed adapter boundary. The tool handler resolves its
// snapshot before selecting the encoding budget and calls the same resolved core.
func runSemanticQuery(ctx context.Context, cfg Config, tracker *sessionTracker, sessionID string, req semanticQueryRequest) (semanticQueryResponse, error) {
	resolved, err := resolveSemanticQueryConfig(cfg)
	if err != nil {
		return semanticQueryResponse{}, err
	}
	return runResolvedSemanticQuery(ctx, resolved, tracker, sessionID, req)
}
