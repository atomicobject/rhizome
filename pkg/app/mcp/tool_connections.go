package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexstore"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/mark3labs/mcp-go/mcp"
	"sort"
	"strings"
)

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
			store, cleanup, err = indexstore.Open(config.VaultPath, indexstore.Options{Access: indexstore.ReadOnly, RequireEnabled: true})
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

		aggByNote, totalSkipped, err := semantic.AggregateIntelChunkMatches(ctx, store, queryChunks, perChunkLimit, skipID, 3, 32, true, 3, paths.NoteTitle)
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
