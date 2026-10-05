package actions

// Docs: [Graph (Hub)](docs/hubs/Graph (Hub).md), [Graph analysis (wikilinks + communities + authority)](docs/reference/analysis/Graph analysis (wikilinks + communities + authority).md)

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// ErrManagedGraphSnapshotUnavailable means a caller-owned read-only graph
// store cannot provide a persisted snapshot. Managed callers must surface this
// state instead of silently switching to live graph computation.
var ErrManagedGraphSnapshotUnavailable = errors.New("managed graph snapshot unavailable")

// GraphAnalysisParams control graph analysis inputs.
type GraphAnalysisParams struct {
	Options               obsidian.GraphAnalysisOptions
	ExcludePatterns       []string
	IncludePatterns       []string
	UseConfig             bool
	SessionStore          *semdb.Store
	NoteMetadata          notemeta.Indexer
	MetadataStoreFallback MetadataStoreFallbackPolicy
}

// GraphAnalysis returns a richer graph representation (hub/authority scores, communities, degrees, neighbors).
func GraphAnalysis(vault obsidian.VaultManager, note obsidian.NoteReader, params GraphAnalysisParams) (*obsidian.GraphAnalysis, error) {
	vaultDef, err := vault.Definition()
	if err != nil {
		return nil, err
	}
	vaultPath := vaultDef.BasePath()

	fileCtxCfg := obsidian.FileContextConfigDefaults
	var authorityFactors []obsidian.AuthorityFactorRule
	if params.UseConfig {
		if cfgDir, cfg, cfgErr := obsidian.FindLocalConfig(vaultPath); cfgErr == nil && cfg != nil {
			_ = cfgDir
			fileCtxCfg = obsidian.FileContextConfigFromLocal(cfg.FileCtx)
		}
	}

	excludePatterns := params.ExcludePatterns
	if params.UseConfig {
		if cfg, err := obsidian.LoadGraphConfig(vaultPath); err == nil {
			excludePatterns = append(cfg.Ignore, excludePatterns...)
			authorityFactors = cfg.AuthorityFactors
		}
	}
	excludePatterns = expandPatterns(excludePatterns)
	includePatterns := expandPatterns(params.IncludePatterns)

	excludedSet := make(map[string]struct{})
	if len(excludePatterns) > 0 {
		parsed, expr, err := ParseInputsWithExpression(excludePatterns)
		if err != nil {
			return nil, err
		}
		matches, err := ListFiles(vault, note, ListParams{
			Inputs:                parsed,
			Expression:            expr,
			MaxDepth:              0,
			SkipAnchors:           false,
			SkipEmbeds:            false,
			SessionStore:          params.SessionStore,
			MetadataStoreFallback: params.MetadataStoreFallback,
		})
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			excludedSet[string(paths.Normalize(m))] = struct{}{}
		}
	}

	includedSet := make(map[string]struct{})
	if len(includePatterns) > 0 {
		parsed, expr, err := ParseInputsWithExpression(includePatterns)
		if err != nil {
			return nil, err
		}
		matches, err := ListFiles(vault, note, ListParams{
			Inputs:                parsed,
			Expression:            expr,
			MaxDepth:              0,
			SkipAnchors:           false,
			SkipEmbeds:            false,
			SessionStore:          params.SessionStore,
			MetadataStoreFallback: params.MetadataStoreFallback,
		})
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			includedSet[string(paths.Normalize(m))] = struct{}{}
		}
	}

	options := params.Options
	if !options.RecencyCascadeSet {
		options.RecencyCascade = true
		options.RecencyCascadeSet = true
	}
	if fileCtxCfg.IncludeDocsInGraph != nil {
		options.IncludeDocsInGraph = *fileCtxCfg.IncludeDocsInGraph
	} else {
		options.IncludeDocsInGraph = true
	}
	options.DocPatterns = fileCtxCfg.DocPatterns
	if options.DocMinBytes == 0 {
		options.DocMinBytes = 200
	}
	options.ExcludedPaths = excludedSet
	options.IncludedPaths = includedSet

	cleanup := func() {}
	var analysis *obsidian.GraphAnalysis
	managedReader := params.SessionStore != nil && !params.MetadataStoreFallback.allowsStoreOpen()
	if managedReader {
		persisted, closeStore, loadErr := loadPersistedGraphSnapshotWithStore(vaultDef, note, params.NoteMetadata, params.SessionStore, params.MetadataStoreFallback)
		if closeStore != nil {
			cleanup = closeStore
		}
		if loadErr != nil {
			cleanup()
			return nil, fmt.Errorf("%w: %v", ErrManagedGraphSnapshotUnavailable, loadErr)
		}
		if persisted == nil {
			cleanup()
			cleanup = func() {}
		}
		if persisted != nil {
			analysis = obsidian.ComputeGraphAnalysisFromSnapshot(persisted, options)
		}
	} else if params.SessionStore != nil || params.MetadataStoreFallback.allowsStoreOpen() {
		if persisted, closeStore, err := loadPersistedGraphSnapshotWithStore(vaultDef, note, params.NoteMetadata, params.SessionStore, params.MetadataStoreFallback); err == nil && persisted != nil {
			if closeStore != nil {
				cleanup = closeStore
			}
			analysis = obsidian.ComputeGraphAnalysisFromSnapshot(persisted, options)
		} else if closeStore != nil {
			closeStore()
		}
	}
	defer cleanup()

	if analysis == nil {
		analysis, err = obsidian.ComputeGraphAnalysis(vaultDef, note, options)
		if err != nil {
			return nil, err
		}
	}

	if params.UseConfig && len(authorityFactors) > 0 {
		changed, err := ApplyAuthorityFactorRules(analysis, vaultDef, note, authorityFactors)
		if err != nil {
			return nil, err
		}
		if changed > 0 {
			obsidian.RefreshGraphAnalysisDerived(analysis)
		}
	}

	return analysis, nil
}

// ApplyAuthorityFactorRules multiplies authority scores for graph nodes that match rule inputs.
// Rules use the list/prompt DSL (tag:/find:/key:value/paths with AND/OR/NOT).
// Returns the number of nodes whose authority was modified.
func ApplyAuthorityFactorRules(analysis *obsidian.GraphAnalysis, vaultDef obsidian.VaultDefinition, note obsidian.NoteReader, rules []obsidian.AuthorityFactorRule) (int, error) {
	if analysis == nil || len(analysis.Nodes) == 0 || len(rules) == 0 {
		return 0, nil
	}

	nodePaths := make([]string, 0, len(analysis.Nodes))
	for p := range analysis.Nodes {
		nodePaths = append(nodePaths, p)
	}
	sort.Strings(nodePaths)

	changed := 0
	for i, rule := range rules {
		if len(rule.Inputs) == 0 {
			return 0, fmt.Errorf("authorityFactors[%d]: inputs is required", i)
		}
		if rule.Factor <= 0 || math.IsInf(rule.Factor, 0) || math.IsNaN(rule.Factor) {
			return 0, fmt.Errorf("authorityFactors[%d]: invalid factor %v (must be finite and > 0)", i, rule.Factor)
		}
		if rule.Factor == 1 {
			continue
		}

		_, expr, err := ParseInputsWithExpression(rule.Inputs)
		if err != nil {
			return 0, fmt.Errorf("authorityFactors[%d]: invalid inputs: %w", i, err)
		}
		if expr == nil {
			continue
		}

		matches := evaluateExpressionMatches(nodePaths, note, expr)
		for _, m := range matches {
			if node, ok := analysis.Nodes[m]; ok {
				if node.Kind == "code" {
					continue
				}
				node.Authority *= rule.Factor
				analysis.Nodes[m] = node
				changed++
				continue
			}
			// Be defensive: normalize and try again.
			norm := string(paths.NormalizeNote(m))
			if node, ok := analysis.Nodes[norm]; ok {
				if node.Kind == "code" {
					continue
				}
				node.Authority *= rule.Factor
				analysis.Nodes[norm] = node
				changed++
			}
		}
	}

	return changed, nil
}

func expandPatterns(patterns []string) []string {
	var out []string
	for _, p := range patterns {
		fields := strings.Fields(p)
		if len(fields) == 0 {
			continue
		}
		out = append(out, fields...)
	}
	return out
}

// ExpandPatterns splits whitespace-separated pattern strings into individual patterns.
// For example, ["tag:foo tag:bar", "find:*"] becomes ["tag:foo", "tag:bar", "find:*"].
func ExpandPatterns(patterns []string) []string {
	return expandPatterns(patterns)
}

// ApplyCodeRefAuthorityBoost modifies the authority scores of graph nodes based on code references.
// Nodes referenced from code get a soft logarithmic boost.
// Returns the number of nodes that were boosted.
func ApplyCodeRefAuthorityBoost(analysis *obsidian.GraphAnalysis, codeRefsByNote map[string][]coderefs.CodeRef) int {
	if analysis == nil || len(codeRefsByNote) == 0 {
		return 0
	}

	boosted := 0
	for notePath, refs := range codeRefsByNote {
		if node, ok := analysis.Nodes[notePath]; ok {
			refCount := len(refs)
			if refCount > 0 {
				// Soft logarithmic boost: 1 + 0.1 * ln(1 + refCount)
				// This gives diminishing returns for many refs
				boost := 1.0 + 0.1*math.Log1p(float64(refCount))
				node.Authority *= boost
				node.ExternalRefCount = refCount
				analysis.Nodes[notePath] = node
				boosted++
			}
		}
	}
	return boosted
}
