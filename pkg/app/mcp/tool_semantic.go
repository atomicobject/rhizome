package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/atomicobject/rhizome/pkg/app/answer"
	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	searchapplication "github.com/atomicobject/rhizome/pkg/app/unifiedsearch/application"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
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
		budgetChars := config.BudgetChars()

		queries := make([]unifiedsearch.QueryInput, 0, len(queryInputs))
		for _, input := range queryInputs {
			queries = append(queries, unifiedsearch.QueryInput{Text: input.Text, Mode: input.Mode})
		}
		queryTracker := tracker.deferredMarkingChild()
		resp, err := runSemanticQuery(ctx, config, queryTracker, sessionID, semanticQueryRequest{
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

func FindConnectionsTool(config Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		tracker, sessionID := resolveSessionTracker(ctx, args, config, false)
		rawNote, _ := args["note"].(string)
		rawText, _ := args["text"].(string)
		normNote := strings.TrimSpace(rawNote)
		normText := strings.TrimSpace(rawText)

		if normNote != "" && normText != "" {
			return mcp.NewToolResultError("provide only one of note or text"), nil
		}
		if normNote == "" && normText == "" {
			return mcp.NewToolResultError("provide note or text"), nil
		}
		if managedIndexUnavailable(config) {
			return managedIndexUnavailableResult(config, "find-connections"), nil
		}

		needProvider := normText != ""
		_, _, noteProvider, _ := config.NoteEmbeddings()
		_, _, codeProvider := config.CodeEmbeddingsState()
		provider := noteProvider
		if provider == nil {
			provider = codeProvider
		}
		if needProvider && provider == nil && config.Runtime != nil {
			snapshot := config.Runtime.Snapshot()
			if !snapshot.Semantic.Done {
				// Ad-hoc text connections require a query embedding. For note-input
				// connections we can proceed from stored chunks once the intel store
				// is available, but this shared wait keeps the error path precise.
				if err := config.Runtime.WaitForSemantic(ctx); err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						return nil, err
					}
					return mcp.NewToolResultError(fmt.Sprintf("semantic connections initialization failed: %v", err)), nil
				}
				_, _, noteProvider, _ = config.NoteEmbeddings()
				_, _, codeProvider = config.CodeEmbeddingsState()
				provider = noteProvider
				if provider == nil {
					provider = codeProvider
				}
			}
		}

		store := config.GetIntelStore()
		var cleanup func()
		if store == nil && config.IntelStorePolicy == IntelStoreFallbackAllowed {
			var err error
			store, cleanup, err = obsidian.OpenIntelStoreBestEffort(config.VaultPath, true)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("semantic connections unavailable: %v", err)), nil
			}
		}
		if cleanup != nil {
			defer cleanup()
		}
		if store == nil {
			return mcp.NewToolResultError("semantic connections unavailable: intel store is not available; run `rzm index`"), nil
		}
		if needProvider && provider == nil {
			return mcp.NewToolResultError("semantic connections unavailable: embedding provider is not configured"), nil
		}

		inputType := "note"
		notePath := ""
		if normNote != "" {
			var err error
			notePath, err = resolveSemanticConnectionNotePath(config.VaultPath, normNote)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("invalid note path: %v", err)), nil
			}
		} else {
			inputType = "text"
		}

		limit := 25
		if lraw, ok := args["limit"]; ok {
			switch v := lraw.(type) {
			case float64:
				if v > 0 {
					limit = int(v)
				}
			case int:
				if v > 0 {
					limit = v
				}
			}
		}

		var (
			queryChunks       []embeddings.StoredChunk
			queryTextByIndex  map[int]string
			skipID            embeddings.NoteID
			queryTextForIndex func(int) string
		)

		if inputType == "text" {
			if provider == nil {
				return mcp.NewToolResultError("semantic connections unavailable: embedding provider is not configured"), nil
			}
			chunks, err := embeddings.ChunkNote("<ad-hoc>", "Ad Hoc Input", normText)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("chunk input: %v", err)), nil
			}
			if len(chunks) == 0 {
				return mcp.NewToolResultError("input produced no chunks"), nil
			}

			chunkTexts := make([]string, 0, len(chunks))
			for _, ch := range chunks {
				chunkTexts = append(chunkTexts, ch.Text)
			}
			vecs, err := provider.EmbedTexts(ctx, chunkTexts)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("embed input: %v", err)), nil
			}
			if len(vecs) != len(chunks) {
				return mcp.NewToolResultError(fmt.Sprintf("expected %d chunk embeddings, got %d", len(chunks), len(vecs))), nil
			}

			queryTextByIndex = make(map[int]string, len(chunks))
			queryChunks = make([]embeddings.StoredChunk, 0, len(chunks))
			for i, ch := range chunks {
				queryChunks = append(queryChunks, embeddings.StoredChunk{
					Index:      ch.Index,
					Breadcrumb: ch.Breadcrumb,
					Heading:    ch.Heading,
					Embedding:  vecs[i],
				})
				queryTextByIndex[ch.Index] = embeddings.CoreChunkBody(ch.Text)
			}
			queryTextForIndex = func(idx int) string { return queryTextByIndex[idx] }
		} else {
			var err error
			queryChunks, err = semantic.NoteSemanticChunks(ctx, store, notePath, nil, 32)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("load note embeddings: %v", err)), nil
			}
			if len(queryChunks) == 0 {
				return mcp.NewToolResultError("note has no stored embeddings; run `rzm index`"), nil
			}
			queryTextByIndex = make(map[int]string, len(queryChunks))
			for _, ch := range queryChunks {
				queryTextByIndex[ch.Index] = firstNonEmpty(ch.Breadcrumb, ch.Heading)
			}
			skipID = embeddings.NoteID(notePath)
			queryTextForIndex = func(idx int) string { return queryTextByIndex[idx] }
		}

		perChunkLimit := limit * 4
		if perChunkLimit < limit {
			perChunkLimit = limit
		}

		aggByNote, totalSkipped, err := semantic.AggregateIntelChunkMatches(ctx, store, queryChunks, perChunkLimit, skipID, 3, 32, true, 3, titleFromPath)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("search failed: %v", err)), nil
		}
		if len(aggByNote) == 0 {
			return mcp.NewToolResultError("no semantic connections found"), nil
		}

		type result struct {
			id    embeddings.NoteID
			agg   *semantic.NoteAgg
			score float64
		}
		results := make([]result, 0, len(aggByNote))
		for id, agg := range aggByNote {
			if len(agg.TopScores) == 0 {
				continue
			}
			results = append(results, result{
				id:    id,
				agg:   agg,
				score: agg.TopScores[0], // max score to keep strong chunks from being diluted
			})
		}
		sort.Slice(results, func(i, j int) bool { return results[i].score > results[j].score })
		if len(results) > limit {
			results = results[:limit]
		}

		cache := make(map[string][]embeddings.ChunkInput)
		matches := make([]map[string]any, 0, len(results))
		for _, res := range results {
			hits := semantic.DiversifyChunkHits(res.agg.TopHits, 3)
			if len(hits) == 0 {
				continue
			}
			bestHit := hits[0]
			text := embeddings.CoreChunkBody(embeddings.ChunkTextForPath(config.VaultPath, string(res.id), bestHit.Match.ChunkIndex, cache))
			queryText := queryTextForIndex(bestHit.QueryIndex)
			matchLabel := firstNonEmpty(bestHit.Match.Breadcrumb, bestHit.Match.Heading)
			queryLabel := firstNonEmpty(bestHit.QueryBreadcrumb, bestHit.QueryHeading)
			reason := ""
			if matchLabel != "" || queryLabel != "" {
				reason = fmt.Sprintf("Query section %q overlaps match section %q", queryLabel, matchLabel)
			}

			chunks := make([]map[string]any, 0, len(hits))
			allowedChunks := make(map[int]bool, len(hits))
			for _, h := range hits {
				chunkText := embeddings.CoreChunkBody(embeddings.ChunkTextForPath(config.VaultPath, string(res.id), h.Match.ChunkIndex, cache))
				chunkQueryText := queryTextForIndex(h.QueryIndex)
				item := map[string]any{
					"heading":         h.Match.Heading,
					"breadcrumb":      h.Match.Breadcrumb,
					"chunkIndex":      h.Match.ChunkIndex,
					"score":           h.Match.Score,
					"queryHeading":    h.QueryHeading,
					"queryBreadcrumb": h.QueryBreadcrumb,
					"queryChunkIndex": h.QueryIndex,
					"queryText":       chunkQueryText,
				}
				if tracker != nil {
					key := fmt.Sprintf("note:%s#chunk:%d", res.id, h.Match.ChunkIndex)
					fingerprint := fingerprintText(chunkText)
					if tracker.Allow(key, fingerprint) {
						item["text"] = chunkText
						tracker.MarkSent(key, fingerprint)
						allowedChunks[h.Match.ChunkIndex] = true
					}
				} else {
					item["text"] = chunkText
				}
				chunks = append(chunks, item)
			}

			item := map[string]any{
				"path":            res.id,
				"title":           bestHit.Match.Title,
				"score":           res.score,
				"heading":         bestHit.Match.Heading,
				"breadcrumb":      bestHit.Match.Breadcrumb,
				"chunkIndex":      bestHit.Match.ChunkIndex,
				"queryHeading":    bestHit.QueryHeading,
				"queryBreadcrumb": bestHit.QueryBreadcrumb,
				"queryChunkIndex": bestHit.QueryIndex,
				"queryText":       queryText,
				"reason":          reason,
				"chunks":          chunks,
			}
			if tracker != nil {
				if allowedChunks[bestHit.Match.ChunkIndex] {
					item["text"] = text
				}
			} else {
				item["text"] = text
			}
			matches = append(matches, item)
		}

		payload := map[string]any{
			"inputType": inputType,
			"matches":   matches,
			"count":     len(matches),
		}
		if sessionID != "" {
			payload["sessionId"] = sessionID
		}
		if tracker != nil && tracker.DedupeHits() > 0 {
			payload["dedupeHits"] = tracker.DedupeHits()
		}
		if inputType == "note" {
			payload["note"] = notePath
		}
		if totalSkipped > 0 {
			payload["skipped"] = totalSkipped
		}

		encoded, err := json.Marshal(payload)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("marshal failed: %v", err)), nil
		}
		return mcp.NewToolResultText(string(encoded)), nil
	}
}
