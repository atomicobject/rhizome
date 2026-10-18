package mcp

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	searchapplication "github.com/atomicobject/rhizome/pkg/app/unifiedsearch/application"
	"path/filepath"
	"strings"
)

type SemanticQueryOptions struct {
	Profile            searchapplication.Profile
	Query              string               // single query text; ignored when Queries is set
	Queries            []SemanticQueryInput // optional multi-facet inputs with optional per-query Mode
	SeedTokens         []string
	Types              []string
	Mode               string
	Limit              int
	BudgetChars        int
	Explain            bool
	Timings            bool
	Scope              string
	PathPrefix         string
	NoteType           string
	ContinuationToken  string
	RequireExactSymbol bool
	IncludeTests       bool
	ExcludeTests       bool
	Compact            bool
}

// SemanticQueryUnifiedWithOptions is the single public entry point for the
// shared search path. Continuation requests must resend the same queries,
// seeds, mode, and controls; the application layer rejects mismatches.
func SemanticQueryUnifiedWithOptions(ctx context.Context, cfg Config, options SemanticQueryOptions) (SemanticQueryResponse, error) {
	inputs := options.Queries
	if len(inputs) == 0 {
		inputs = []SemanticQueryInput{{Text: options.Query}}
	}
	queries := make([]unifiedsearch.QueryInput, 0, len(inputs))
	for _, input := range inputs {
		queries = append(queries, unifiedsearch.QueryInput{Text: input.Text, Mode: input.Mode})
	}
	return runSemanticQuery(ctx, cfg, nil, "", semanticQueryRequest{
		profile:            options.Profile,
		queries:            queries,
		seeds:              append([]string(nil), options.SeedTokens...),
		types:              options.Types,
		mode:               options.Mode,
		limit:              options.Limit,
		budgetChars:        options.BudgetChars,
		explain:            options.Explain,
		timings:            options.Timings,
		scope:              options.Scope,
		pathPrefix:         options.PathPrefix,
		noteType:           options.NoteType,
		continuation:       options.ContinuationToken,
		requireExactSymbol: options.RequireExactSymbol,
		excludeTests:       options.ExcludeTests,
		compact:            options.Compact,
	})
}

// semanticQueryRequest is the normalized adapter request shared by the MCP tool
// handler and the typed public entry point.
type semanticQueryRequest struct {
	profile            searchapplication.Profile
	queries            []unifiedsearch.QueryInput
	seeds              []string
	types              []string
	mode               string
	limit              int
	budgetChars        int
	explain            bool
	timings            bool
	scope              string
	pathPrefix         string
	noteType           string
	continuation       string
	requireExactSymbol bool
	excludeTests       bool
	compact            bool
}

// normalizeSemanticQueryInputs trims inputs and splits blank-line separated
// text into independent facets while retaining each facet's mode.
func normalizeSemanticQueryInputs(inputs []unifiedsearch.QueryInput) []unifiedsearch.QueryInput {
	out := make([]unifiedsearch.QueryInput, 0, len(inputs))
	for _, input := range inputs {
		mode := strings.TrimSpace(input.Mode)
		for _, text := range normalizeQueryStrings([]string{input.Text}) {
			out = append(out, unifiedsearch.QueryInput{Text: text, Mode: mode})
		}
	}
	return out
}

func semanticQueryTexts(inputs []unifiedsearch.QueryInput) []string {
	out := make([]string, 0, len(inputs))
	for _, input := range inputs {
		out = append(out, input.Text)
	}
	return out
}

func normalizeSemanticScope(scope string) string {
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "all" {
		return ""
	}
	if scope == "notes" {
		return "docs"
	}
	return scope
}

func normalizeSemanticPathPrefix(prefix string) string {
	prefix = strings.TrimSpace(filepath.ToSlash(prefix))
	prefix = filepath.ToSlash(filepath.Clean(prefix))
	prefix = strings.TrimSuffix(prefix, "/")
	prefix = strings.TrimPrefix(prefix, "./")
	if prefix == "." || prefix == "/" {
		return ""
	}
	return prefix
}

func validateSemanticPathPrefix(prefix string) (string, error) {
	raw := strings.TrimSpace(filepath.ToSlash(prefix))
	if raw == "" {
		return "", nil
	}
	windowsVolume := len(raw) >= 2 && raw[1] == ':' && ((raw[0] >= 'a' && raw[0] <= 'z') || (raw[0] >= 'A' && raw[0] <= 'Z'))
	if strings.HasPrefix(raw, "/") || filepath.VolumeName(prefix) != "" || windowsVolume {
		return "", fmt.Errorf("pathPrefix must be vault-relative")
	}
	normalized := normalizeSemanticPathPrefix(raw)
	if normalized == ".." || strings.HasPrefix(normalized, "../") {
		return "", fmt.Errorf("pathPrefix must stay within the vault")
	}
	return normalized, nil
}
