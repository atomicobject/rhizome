package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/atomicobject/rhizome/pkg/app/answer"
	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	searchapplication "github.com/atomicobject/rhizome/pkg/app/unifiedsearch/application"

	"github.com/atomicobject/rhizome/pkg/indexingperf"

	"github.com/atomicobject/rhizome/pkg/search"

	"github.com/mark3labs/mcp-go/mcp"
)

// SemanticQueryTool runs unified search and returns a budgeted, file-oriented context pack
// plus structured match summaries (with stateless continuation tokens).
func SemanticQueryTool(config Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		timingsRequested := boolArg(args, "timings", false)
		if timingsRequested {
			if indexingperf.FromContext(ctx) == nil {
				ctx = indexingperf.WithCollector(ctx, indexingperf.NewSemanticQueryCollector())
			}
			if search.TimingsFromContext(ctx) == nil {
				ctx = search.WithTimings(ctx, &search.Timings{})
			}
			ctx = withSemanticQueryDiagnosticsState(ctx)
			indexingperf.AddCount(ctx, indexingperf.AgentStartOpNotePasses, 0)
			indexingperf.AddCount(ctx, indexingperf.AgentStartOpRepoWalks, 0)
			indexingperf.AddCount(ctx, indexingperf.AgentStartOpNoteReads, 0)
			indexingperf.AddCount(ctx, indexingperf.AgentStartOpCodeReads, 0)
			indexingperf.AddCount(ctx, indexingperf.SemanticQueryOpFallbackStoreOpens, 0)
			if config.IndexedReadOnlySemanticQuery {
				indexingperf.MarkCountAvailable(ctx, indexingperf.AgentStartOpIntegrityChecks)
				indexingperf.MarkCountAvailable(ctx, indexingperf.AgentStartOpIndexWrites)
			}
		}
		finishHandler := indexingperf.StartSpan(ctx, indexingperf.SemanticQueryPhaseHandler)
		_, _, noteProvider, _ := config.NoteEmbeddings()
		_, _, codeProvider := config.CodeEmbeddingsState()
		provider := noteProvider
		indexedStoreUnavailable := config.IndexedReadOnlySemanticQuery && config.GetIntelStore() == nil
		if provider == nil {
			provider = codeProvider
		}
		if provider == nil && config.Runtime != nil && !indexedStoreUnavailable {
			snapshot := config.Runtime.Snapshot()
			if !snapshot.Semantic.Done {
				// Semantic query is provider-backed, so waiting here is worth it
				// when initialization is already in flight. Context-only tools avoid
				// this wait and can still operate from cache/metadata.
				if err := config.Runtime.WaitForSemantic(ctx); err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						return nil, err
					}
					return mcp.NewToolResultError(fmt.Sprintf("semantic search initialization failed: %v", err)), nil
				}
				_, _, noteProvider, _ = config.NoteEmbeddings()
				_, _, codeProvider = config.CodeEmbeddingsState()
				provider = noteProvider
				if provider == nil {
					provider = codeProvider
				}
			}
		}
		if provider == nil && !indexedStoreUnavailable {
			if config.Runtime != nil {
				snapshot := config.Runtime.Snapshot()
				if snapshot.Semantic.Done && snapshot.Semantic.Err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("semantic search unavailable: initialization failed: %v", snapshot.Semantic.Err)), nil
				}
			}
			return mcp.NewToolResultError("semantic search unavailable: embedding provider is not configured; run `rzm index`"), nil
		}
		if rawIntent, ok := args["intent"]; ok {
			if intentText, ok := rawIntent.(string); ok && strings.TrimSpace(intentText) != "" {
				return mcp.NewToolResultError("semantic_query uses mode (not intent); pass mode or queries[].mode"), nil
			}
		}
		finishSession := indexingperf.StartSpan(ctx, indexingperf.SemanticQueryPhaseSession)
		tracker, sessionID := resolveSessionTracker(ctx, args, config, false)
		finishSession(nil)
		queryInputs, err := parseQueryInputs(args["queries"])
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		seedPaths := extractStringArray(args["paths"])
		if len(seedPaths) == 0 {
			seedPaths = extractStringArray(args["path"])
		}
		if len(seedPaths) == 0 {
			seedPaths = extractStringArray(args["files"])
		}
		requestedTypes := extractStringArray(args["types"])
		scope, _ := args["scope"].(string)
		scope = strings.ToLower(strings.TrimSpace(scope))
		requireExactSymbol, _ := args["requireExactSymbol"].(bool)
		excludeNotes, _ := args["excludeNotes"].(bool)
		includeTests, includeTestsSet := args["includeTests"].(bool)
		compact, _ := args["compact"].(bool)
		pathPrefix, pathPrefixSet := args["pathPrefix"].(string)
		if !pathPrefixSet {
			pathPrefix, _ = args["folder"].(string)
		}
		noteType, _ := args["noteType"].(string)
		explicitMode, _ := args["mode"].(string)
		rawToken, _ := args["continuationToken"].(string)

		limit := 0
		if v, ok := args["limit"].(float64); ok && int(v) > 0 {
			limit = int(v)
		} else if v, ok := args["limit"].(int); ok && v > 0 {
			limit = v
		}
		explain := false
		if v, ok := args["explain"].(bool); ok {
			explain = v
		}
		resolvedConfig, err := resolveSemanticQueryConfig(config)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("search failed: %v", err)), nil
		}
		budgetChars := resolvedConfig.BudgetChars()

		queries := make([]unifiedsearch.QueryInput, 0, len(queryInputs))
		for _, input := range queryInputs {
			queries = append(queries, unifiedsearch.QueryInput{Text: input.Text, Mode: input.Mode})
		}
		queryTracker := tracker.deferredMarkingChild()
		resp, err := runResolvedSemanticQuery(ctx, resolvedConfig, queryTracker, sessionID, semanticQueryRequest{
			profile:            searchapplication.ProfileAgent,
			queries:            queries,
			seeds:              seedPaths,
			types:              semanticTypesForControls(requestedTypes, scope, excludeNotes),
			mode:               strings.TrimSpace(explicitMode),
			limit:              limit,
			budgetChars:        budgetChars,
			explain:            explain,
			timings:            timingsRequested,
			scope:              scope,
			pathPrefix:         pathPrefix,
			noteType:           noteType,
			continuation:       rawToken,
			requireExactSymbol: requireExactSymbol,
			excludeTests:       includeTestsSet && !includeTests,
			compact:            compact,
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("search failed: %v", err)), nil
		}

		finishHandler(nil)
		encoded, err := marshalSemanticQueryResponseWithDiagnostics(ctx, resp, budgetChars, timingsRequested)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("marshal failed: %v", err)), nil
		}
		encoded, err = finalizeSemanticQueryResponse(tracker, resp, encoded, budgetChars)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("marshal failed: %v", err)), nil
		}
		return mcp.NewToolResultText(string(encoded)), nil
	}
}

func marshalSemanticQueryResponseWithDiagnostics(ctx context.Context, resp semanticQueryResponse, budgetChars int, enabled bool) ([]byte, error) {
	if !enabled {
		return marshalSemanticQueryResponse(resp, budgetChars)
	}
	started := time.Now()
	encoded, err := marshalSemanticQueryResponse(resp, budgetChars)
	if err != nil {
		return nil, err
	}
	var projected semanticQueryResponse
	if err := json.Unmarshal(encoded, &projected); err != nil {
		return nil, err
	}
	if collector := indexingperf.FromContext(ctx); collector != nil {
		collector.RecordSpan(indexingperf.SemanticQueryPhaseMarshal, time.Since(started), nil)
		base := collector.SemanticQueryDiagnostics()
		diagnostics := &SemanticQueryDiagnostics{
			MeasurementScope: "request",
			Phases:           base.Phases,
			Operations:       base.Operations,
		}
		for _, event := range resp.searchTimings {
			diagnostics.Search = append(diagnostics.Search, SemanticQueryTimingDiagnostic{
				Name: event.Name, Kind: event.Kind, DurationMs: event.Duration.Milliseconds(), Status: event.Status, Error: event.Err,
			})
		}
		if deadline, ok := semanticQueryDiagnosticDeadline(ctx); ok {
			diagnostics.Deadline.Set = true
			diagnostics.Deadline.RemainingMs = time.Until(deadline).Milliseconds()
		}
		projected.Diagnostics = diagnostics
	}
	// The normal response is budgeted first. Opt-in diagnostics are then added
	// outside that content budget so a small caller budget cannot silently erase
	// the measurement envelope it explicitly requested.
	return json.Marshal(projected)
}

type semanticQueryDiagnosticsStateKey struct{}

type semanticQueryDiagnosticsState struct {
	mu       sync.Mutex
	deadline time.Time
}

func withSemanticQueryDiagnosticsState(ctx context.Context) context.Context {
	return context.WithValue(ctx, semanticQueryDiagnosticsStateKey{}, &semanticQueryDiagnosticsState{})
}

func observeSemanticQueryDeadline(ctx context.Context, deadline time.Time) {
	state, _ := ctx.Value(semanticQueryDiagnosticsStateKey{}).(*semanticQueryDiagnosticsState)
	if state == nil {
		return
	}
	state.mu.Lock()
	if state.deadline.IsZero() || deadline.Before(state.deadline) {
		state.deadline = deadline
	}
	state.mu.Unlock()
}

func semanticQueryDiagnosticDeadline(ctx context.Context) (time.Time, bool) {
	state, _ := ctx.Value(semanticQueryDiagnosticsStateKey{}).(*semanticQueryDiagnosticsState)
	if state != nil {
		state.mu.Lock()
		defer state.mu.Unlock()
		if !state.deadline.IsZero() {
			return state.deadline, true
		}
	}
	return ctx.Deadline()
}

func parseQueryInputs(raw any) ([]SemanticQueryInput, error) {
	if raw == nil {
		return nil, nil
	}
	switch v := raw.(type) {
	case []string:
		out := make([]SemanticQueryInput, 0, len(v))
		for _, q := range v {
			if q = strings.TrimSpace(q); q != "" {
				out = append(out, SemanticQueryInput{Text: q})
			}
		}
		return out, nil
	case []any:
		out := make([]SemanticQueryInput, 0, len(v))
		for _, item := range v {
			switch typed := item.(type) {
			case string:
				q := strings.TrimSpace(typed)
				if q == "" {
					continue
				}
				out = append(out, SemanticQueryInput{Text: q})
			case map[string]any:
				text, _ := typed["text"].(string)
				if text == "" {
					text, _ = typed["query"].(string)
				}
				text = strings.TrimSpace(text)
				if text == "" {
					return nil, fmt.Errorf("query object is missing text")
				}
				if intent, ok := typed["intent"].(string); ok && strings.TrimSpace(intent) != "" {
					return nil, fmt.Errorf("query objects use mode (not intent)")
				}
				mode, _ := typed["mode"].(string)
				mode = strings.TrimSpace(mode)
				out = append(out, SemanticQueryInput{Text: text, Mode: mode})
			default:
				return nil, fmt.Errorf("queries must be strings or objects with {text, mode}")
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("queries must be an array")
	}
}

func semanticTypesForControls(types []string, scope string, excludeNotes bool) []string {
	switch scope {
	case "code", "tests":
		return []string{"code"}
	case "docs", "notes":
		return []string{"note"}
	case "all", "":
		if excludeNotes {
			return []string{"code"}
		}
		return types
	default:
		return types
	}
}

func addExactCodeNextQuery(next []answer.SuggestedQuery, rawQuery string) []answer.SuggestedQuery {
	if len(unifiedsearch.ExactCodeToolWarnings(rawQuery)) == 0 {
		return next
	}
	symbol := exactCodeSymbolFromQuery(rawQuery)
	if symbol == "" {
		return next
	}
	for _, existing := range next {
		if existing.Mode == "code_references" && existing.Query == symbol {
			return next
		}
	}
	return append([]answer.SuggestedQuery{{
		Mode:   "code_references",
		Query:  symbol,
		Reason: "exact code-navigation query; use indexed code references for proof",
	}}, next...)
}

func exactCodeSymbolFromQuery(rawQuery string) string {
	fields := strings.Fields(rawQuery)
	for _, field := range fields {
		field = strings.Trim(field, "`'\"()[]{}<>,:;!?")
		if field == "" || strings.Contains(field, "/") {
			continue
		}
		if strings.Count(field, ".") >= 1 || hasInternalUppercase(field) || strings.Contains(field, "_") || hasDigit(field) {
			return field
		}
	}
	return ""
}

func hasInternalUppercase(s string) bool {
	for i, r := range s {
		if i > 0 && unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

func hasDigit(s string) bool {
	for _, r := range s {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}
