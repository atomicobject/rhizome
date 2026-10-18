package mcp

import (
	"context"
	"fmt"

	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/contextpack"

	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"

	"github.com/atomicobject/rhizome/pkg/indexstore"
	"github.com/mark3labs/mcp-go/mcp"
)

const defaultCodeToolMaxBytes = 20000

// CodeSymbolTool resolves an indexed code symbol and returns definition metadata plus source.
func CodeSymbolTool(config Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if managedIndexUnavailable(config) {
			return managedIndexUnavailableResult(config, "code_symbol"), nil
		}
		args := request.GetArguments()
		symbol := strings.TrimSpace(stringArg(args, "symbol"))
		if symbol == "" {
			return mcp.NewToolResultError("symbol is required"), nil
		}
		store, cleanup, err := codeToolStore(config)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if cleanup != nil {
			defer cleanup()
		}
		resp, err := resolveCodeSymbol(ctx, config, store, codeSymbolRequest{
			Symbol:       symbol,
			Path:         stringArg(args, "path"),
			Language:     stringArg(args, "language"),
			ContextLines: intArg(args, "contextLines", 3),
			MaxBytes:     intArg(args, "maxBytes", defaultCodeToolMaxBytes),
		})
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return respondJSON(resp, "marshal code_symbol failed")
	}
}

// CodeReferencesTool resolves callers/callees for an indexed symbol.
func CodeReferencesTool(config Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if managedIndexUnavailable(config) {
			return managedIndexUnavailableResult(config, "code_references"), nil
		}
		args := request.GetArguments()
		symbol := strings.TrimSpace(stringArg(args, "symbol"))
		if symbol == "" {
			return mcp.NewToolResultError("symbol is required"), nil
		}
		store, cleanup, err := codeToolStore(config)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if cleanup != nil {
			defer cleanup()
		}
		limit := intArg(args, "limit", 20)
		base, err := resolveCodeSymbol(ctx, config, store, codeSymbolRequest{
			Symbol:       symbol,
			Path:         stringArg(args, "path"),
			Language:     stringArg(args, "language"),
			ContextLines: 1,
			MaxBytes:     8000,
		})
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if base.Status != "resolved" || base.Definition == nil {
			resp := CodeReferencesResponse{
				Symbol:       symbol,
				Status:       base.Status,
				Confidence:   base.Confidence,
				Coverage:     base.Coverage,
				EvidenceKind: base.EvidenceKind,
				Definitions:  base.Candidates,
				NextQueries:  base.NextQueries,
				Warnings:     base.Warnings,
			}
			return respondJSON(resp, "marshal code_references failed")
		}
		anchor := codePayloadToAnchor(*base.Definition)
		includeDefinitions := boolArg(args, "includeDefinitions", true)
		includeCallers := boolArg(args, "includeCallers", true)
		includeCallees := boolArg(args, "includeCallees", true)
		resp := CodeReferencesResponse{
			Symbol:       symbol,
			Status:       "resolved",
			Confidence:   "high",
			Coverage:     "indexed code anchors and calls edges",
			EvidenceKind: "exact_code",
			NextQueries:  defaultCodeNextQueries(anchor.FQN),
		}
		if includeDefinitions {
			resp.Definition = base.Definition
			resp.Definitions = []CodeSymbolPayload{*base.Definition}
		}
		if includeCallers {
			callers, err := store.CodeCallers(ctx, anchor.Lang, anchor.FQN, limit)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("callers lookup failed: %v", err)), nil
			}
			resp.Callers = codePayloads(config, callers, 0, 0)
		}
		if includeCallees {
			callees, err := store.CodeCallees(ctx, anchor.Lang, anchor.FQN, limit)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("callees lookup failed: %v", err)), nil
			}
			resp.Callees = codePayloads(config, callees, 0, 0)
		}
		if len(resp.Callers) >= limit || len(resp.Callees) >= limit {
			resp.Truncated = true
			resp.Warnings = append(resp.Warnings, "reference results reached the requested limit")
		}
		return respondJSON(resp, "marshal code_references failed")
	}
}

// CodeSymbolContextTool returns a compact evidence packet around one symbol.
func CodeSymbolContextTool(config Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if managedIndexUnavailable(config) {
			return managedIndexUnavailableResult(config, "code_symbol_context"), nil
		}
		args := request.GetArguments()
		symbol := strings.TrimSpace(stringArg(args, "symbol"))
		if symbol == "" {
			return mcp.NewToolResultError("symbol is required"), nil
		}
		store, cleanup, err := codeToolStore(config)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if cleanup != nil {
			defer cleanup()
		}
		budget := intArg(args, "budgetChars", 16000)
		base, err := resolveCodeSymbol(ctx, config, store, codeSymbolRequest{
			Symbol:       symbol,
			Path:         stringArg(args, "path"),
			Language:     stringArg(args, "language"),
			ContextLines: 4,
			MaxBytes:     min(defaultCodeToolMaxBytes, budget),
		})
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		resp := CodeSymbolContextResponse{
			Symbol:       symbol,
			Status:       base.Status,
			Confidence:   base.Confidence,
			Coverage:     base.Coverage,
			EvidenceKind: base.EvidenceKind,
			Definition:   base.Definition,
			NextQueries:  base.NextQueries,
			Warnings:     base.Warnings,
		}
		if base.Status != "resolved" || base.Definition == nil {
			return respondJSON(resp, "marshal code_symbol_context failed")
		}
		anchor := codePayloadToAnchor(*base.Definition)
		callers, err := store.CodeCallers(ctx, anchor.Lang, anchor.FQN, 8)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("callers lookup failed: %v", err)), nil
		}
		callees, err := store.CodeCallees(ctx, anchor.Lang, anchor.FQN, 8)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("callees lookup failed: %v", err)), nil
		}
		resp.Callers = codePayloads(config, callers, 0, 0)
		resp.Callees = codePayloads(config, callees, 0, 0)
		if boolArg(args, "includeTests", true) {
			resp.Tests = findTestSnippets(config, firstNonEmpty(anchor.Symbol, anchor.FQN), 5, 12000)
		}
		resp.Text = contextpack.TrimToBudget(renderCodeSymbolContext(resp), budget/2)
		if resp.Text != renderCodeSymbolContext(resp) {
			resp.Truncated = true
		}
		return respondJSON(resp, "marshal code_symbol_context failed")
	}
}

type codeSymbolRequest struct {
	Symbol       string
	Path         string
	Language     string
	ContextLines int
	MaxBytes     int
}

func resolveCodeSymbol(ctx context.Context, config Config, store *semdb.Store, req codeSymbolRequest) (CodeSymbolResponse, error) {
	lang := codeanchor.Lang(strings.TrimSpace(req.Language))
	vp, _ := paths.NewVaultPaths(effectiveVaultRoot(config))
	path := strings.TrimSpace(req.Path)
	if path != "" {
		path = relCodePath(vp, path)
		if path == "" {
			return CodeSymbolResponse{}, fmt.Errorf("path is outside vault/project root")
		}
	}
	anchors, err := store.CodeSymbolCandidates(ctx, semdb.CodeSymbolLookupOptions{
		Symbol: req.Symbol,
		Lang:   lang,
		Path:   path,
		Limit:  21,
	})
	if err != nil {
		return CodeSymbolResponse{}, err
	}
	resp := CodeSymbolResponse{
		Symbol:       req.Symbol,
		EvidenceKind: "indexed_code",
		NextQueries:  defaultCodeNextQueries(req.Symbol),
	}
	switch len(anchors) {
	case 0:
		resp.Status = "not_found"
		resp.Confidence = "low"
		resp.Coverage = "no matching indexed code anchor"
		resp.Warnings = []string{"symbol was not found in the code index; run `rzm index` if the index is stale"}
	case 1:
		resp.Status = "resolved"
		resp.Confidence = "high"
		resp.Coverage = "single indexed code anchor match"
		payloads := codePayloads(config, anchors, req.ContextLines, req.MaxBytes)
		if len(payloads) > 0 {
			resp.Definition = &payloads[0]
			resp.Truncated = payloads[0].Truncated
		}
	default:
		resp.Status = "ambiguous"
		resp.Confidence = "low"
		resp.Coverage = "multiple indexed code anchors matched"
		resp.Candidates = codePayloads(config, anchors, 0, 0)
		if len(anchors) > 20 {
			resp.Truncated = true
			resp.Candidates = resp.Candidates[:20]
		}
		resp.Warnings = []string{"suffix/name lookup is ambiguous; pass path or language to disambiguate"}
	}
	return resp, nil
}

func codeToolStore(config Config) (*semdb.Store, func(), error) {
	if store := config.GetIntelStore(); store != nil {
		return store, nil, nil
	}
	if config.IntelStorePolicy == IntelStoreManagedReadOnly {
		return nil, nil, fmt.Errorf("code index unavailable; run `rzm index`")
	}
	store, cleanup, err := indexstore.Open(config.VaultPath, indexstore.Options{Access: indexstore.ReadOnly, RequireEnabled: true})
	if err != nil {
		return nil, nil, err
	}
	if store == nil {
		return nil, nil, fmt.Errorf("code index unavailable; run `rzm index`")
	}
	return store, cleanup, nil
}

func codePayloads(config Config, anchors []codeanchor.IntelAnchor, contextLines, maxBytes int) []CodeSymbolPayload {
	out := make([]CodeSymbolPayload, 0, len(anchors))
	for _, anchor := range anchors {
		payload := CodeSymbolPayload{
			AnchorID:   anchor.AnchorID,
			Language:   string(anchor.Lang),
			Kind:       anchor.Kind,
			Path:       anchor.Path,
			Symbol:     anchor.Symbol,
			FQN:        anchor.FQN,
			Signature:  anchor.Signature,
			DocComment: anchor.DocComment,
			StartLine:  anchor.StartLine,
			EndLine:    anchor.EndLine,
		}
		if contextLines > 0 || maxBytes > 0 {
			content, start, end, total, truncated, continuation := readAnchorSnippet(config, anchor, contextLines, maxBytes)
			payload.Content = content
			payload.ContentStart = start
			payload.ContentEnd = end
			payload.TotalLines = total
			payload.Truncated = truncated
			payload.Continuation = continuation
		}
		out = append(out, payload)
	}
	return out
}

func codePayloadToAnchor(payload CodeSymbolPayload) codeanchor.IntelAnchor {
	return codeanchor.IntelAnchor{
		AnchorID:   payload.AnchorID,
		Lang:       codeanchor.Lang(payload.Language),
		Kind:       payload.Kind,
		Path:       payload.Path,
		Symbol:     payload.Symbol,
		FQN:        payload.FQN,
		Signature:  payload.Signature,
		DocComment: payload.DocComment,
		StartLine:  payload.StartLine,
		EndLine:    payload.EndLine,
	}
}

func defaultCodeNextQueries(symbol string) []string {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return nil
	}
	return []string{
		fmt.Sprintf(`code_references {"symbol": %q}`, symbol),
		fmt.Sprintf(`semantic_query {"queries": [%q], "scope": "code", "requireExactSymbol": true}`, symbol),
	}
}

func stringArg(args map[string]interface{}, key string) string {
	if v, ok := args[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func boolArg(args map[string]interface{}, key string, fallback bool) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return fallback
}

func intArg(args map[string]interface{}, key string, fallback int) int {
	switch v := args[key].(type) {
	case int:
		if v > 0 {
			return v
		}
	case int64:
		if v > 0 {
			return int(v)
		}
	case float64:
		if v > 0 {
			return int(v)
		}
	}
	return fallback
}
