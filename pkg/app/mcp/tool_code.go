package mcp

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/codefile"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
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
	store, cleanup, err := obsidian.OpenIntelStoreBestEffort(config.VaultPath, true)
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

func readAnchorSnippet(config Config, anchor codeanchor.IntelAnchor, contextLines, maxBytes int) (string, int, int, int, bool, string) {
	root := effectiveVaultRoot(config)
	vp, err := paths.NewVaultPaths(root)
	if err != nil || vp.Root() == "" {
		return "", 0, 0, 0, false, ""
	}
	rel, err := vp.RelCodeStrict(anchor.Path)
	if err != nil {
		return "", 0, 0, 0, false, ""
	}
	abs, err := vp.AbsCode(rel)
	if err != nil {
		return "", 0, 0, 0, false, ""
	}
	data, err := os.ReadFile(abs.String())
	if err != nil {
		return "", 0, 0, 0, false, ""
	}
	start := int(anchor.StartLine)
	end := int(anchor.EndLine)
	return sliceTextLines(string(data), start, end, contextLines, maxBytes)
}

func sliceTextLines(text string, startLine, endLine, contextLines, maxBytes int) (string, int, int, int, bool, string) {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	total := len(lines)
	if total == 1 && lines[0] == "" {
		total = 0
		lines = nil
	}
	if total == 0 {
		return "", 0, 0, 0, false, ""
	}
	if startLine <= 0 {
		startLine = 1
	}
	if endLine <= 0 || endLine < startLine {
		endLine = startLine
	}
	startLine -= contextLines
	endLine += contextLines
	if startLine < 1 {
		startLine = 1
	}
	if endLine > total {
		endLine = total
	}
	content := strings.Join(lines[startLine-1:endLine], "\n")
	truncated := false
	continuation := ""
	if maxBytes > 0 && len(content) > maxBytes {
		content = content[:maxBytes]
		truncated = true
		continuation = fmt.Sprintf("increase maxBytes or read %s:%d", "next range", endLine+1)
	}
	if endLine < total && continuation == "" {
		continuation = fmt.Sprintf("nextStartLine=%d", endLine+1)
	}
	return content, startLine, endLine, total, truncated, continuation
}

func findTestSnippets(config Config, needle string, limit int, maxBytes int) []CodeSymbolPayload {
	needle = strings.TrimSpace(needle)
	root := effectiveVaultRoot(config)
	if needle == "" || root == "" {
		return nil
	}
	vp, err := paths.NewVaultPaths(root)
	if err != nil || vp.Root() == "" {
		return nil
	}
	var out []CodeSymbolPayload
	_ = filepath.WalkDir(vp.Root(), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if limit > 0 && len(out) >= limit {
			return filepath.SkipAll
		}
		if entry.IsDir() {
			if shouldSkipCodeContextDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := vp.RelCodeStrict(path)
		if err != nil {
			return nil
		}
		if !looksLikeTestPath(rel.String()) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(data), needle) {
			return nil
		}
		line := firstLineContaining(string(data), needle)
		content, start, end, total, truncated, continuation := sliceTextLines(string(data), line, line, 3, maxBytes)
		out = append(out, CodeSymbolPayload{
			Path:         rel.String(),
			Symbol:       needle,
			StartLine:    int64(line),
			EndLine:      int64(line),
			Content:      content,
			ContentStart: start,
			ContentEnd:   end,
			TotalLines:   total,
			Truncated:    truncated,
			Continuation: continuation,
		})
		return nil
	})
	return out
}

func shouldSkipCodeContextDir(name string) bool {
	switch name {
	case ".git", ".gocache", ".gomodcache", ".gotmp", "node_modules", "bin", "dist", "coverage":
		return true
	default:
		return false
	}
}

func firstLineContaining(text string, needle string) int {
	scanner := bufio.NewScanner(strings.NewReader(text))
	line := 1
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), needle) {
			return line
		}
		line++
	}
	return 1
}

func looksLikeTestPath(path string) bool {
	lower := strings.ToLower(path)
	base := filepath.Base(lower)
	return strings.Contains(lower, "/test/") ||
		strings.Contains(lower, "/tests/") ||
		strings.HasSuffix(lower, "_test.go") ||
		(codefile.IsTypeScriptJavaScriptPath(base) && strings.Contains(base, ".test.")) ||
		strings.HasSuffix(lower, "_test.py") ||
		strings.HasSuffix(lower, "test.cs")
}

func renderCodeSymbolContext(resp CodeSymbolContextResponse) string {
	var b strings.Builder
	if resp.Definition != nil {
		fmt.Fprintf(&b, "Definition: %s %s %s\n", resp.Definition.Language, resp.Definition.FQN, resp.Definition.Path)
		if strings.TrimSpace(resp.Definition.Content) != "" {
			fmt.Fprintf(&b, "\n%s\n", resp.Definition.Content)
		}
	}
	if len(resp.Callers) > 0 {
		b.WriteString("\nCallers:\n")
		for _, caller := range resp.Callers {
			fmt.Fprintf(&b, "- %s %s\n", caller.FQN, caller.Path)
		}
	}
	if len(resp.Callees) > 0 {
		b.WriteString("\nCallees:\n")
		for _, callee := range resp.Callees {
			fmt.Fprintf(&b, "- %s %s\n", callee.FQN, callee.Path)
		}
	}
	if len(resp.Tests) > 0 {
		b.WriteString("\nTests:\n")
		for _, test := range resp.Tests {
			fmt.Fprintf(&b, "- %s:%d\n", test.Path, test.StartLine)
		}
	}
	return strings.TrimSpace(b.String())
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
