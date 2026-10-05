// Package contextpack provides deterministic, budget-aware text packing for LLM context windows.
//
// Docs: [CONTEXT.md](pkg/app/contextpack/CONTEXT.md)
//
// Core abstraction:
//   - Piece: a pre-rendered text block with Priority (higher = included first), Score (tie-breaker), and Key (stable final tie-break/debug handle).
//   - Pack: greedy bin-packing that selects pieces in priority/score/key order until budget exhausted.
//   - PackWithIntent: like Pack, but can ask a Compressor to densify a larger collected set when budget pressure is material.
//   - TrimToBudget / TrimMarkdown: truncation helpers for individual pieces.
//
// Usage pattern:
//  1. Render each result/section as a Piece with a stable Key (for deduplication/debugging).
//  2. Assign Priority by category (e.g., header=1000, results=900-rank, footer=100).
//  3. Call Pack(pieces, budgetChars) to get joined text + Meta (trimmed?, omitted count).
//  4. Or call PackWithIntent with a Compressor to enable LLM compression when over budget.
//
// Callers include vault_context, file_context, semantic_query, and answer/search packers. This package never fetches, ranks,
// or interprets content; callers must pass already-rendered pieces whose priority encodes product semantics.
package contextpack

import (
	"context"
	"reflect"
	"sort"
	"strings"
)

// DefaultBudgetChars is the package fallback for LLM context packing.
// Config, explicit caller budgets, and command-specific floors may override it.
// Docs: [[Contextpack - Budget surfaces]] and [[search-answer-workflow#^spec-0034-us4-ac2]].
const DefaultBudgetChars = 70000

// Piece is a pre-rendered, budget-aware unit of output.
// Higher Priority pieces are included first; within a priority, higher Score wins.
type Piece struct {
	Key      string
	Priority int
	Score    float64
	Text     string
}

// Meta reports packing statistics returned by Pack.
type Meta struct {
	BudgetRequested int
	BudgetUsed      int
	Trimmed         bool
	IncludedPieces  int
	OmittedPieces   int
	IncludedKeys    []string
	OmittedKeys     []string
}

// Pack greedily selects pieces under budget and returns the joined text.
// The caller is expected to include any required header as the first piece.
// Budget is measured in bytes/chars, matching the rest of Rhizome's local
// context surfaces; token conversion belongs at provider boundaries.
func Pack(pieces []Piece, budgetChars int) (string, Meta) {
	text, meta, _ := PackDetailed(pieces, budgetChars)
	return text, meta
}

// PackDetailed behaves like Pack but also returns the included pieces with the
// exact text that was packed (after trimming).
func PackDetailed(pieces []Piece, budgetChars int) (string, Meta, []Piece) {
	if budgetChars < 0 {
		budgetChars = 0
	}

	// Docs: [[unified-search-answer-architecture#^spec-0035-us3-ac3]].
	// Determinism matters for agent diffs, tests, and continuation tokens: priority
	// encodes caller semantics, score breaks ties within that semantic lane, and key
	// prevents map/query iteration order from leaking into packed output.
	sort.SliceStable(pieces, func(i, j int) bool {
		if pieces[i].Priority != pieces[j].Priority {
			return pieces[i].Priority > pieces[j].Priority
		}
		if pieces[i].Score != pieces[j].Score {
			return pieces[i].Score > pieces[j].Score
		}
		return pieces[i].Key < pieces[j].Key
	})

	var out []string
	includedPieces := make([]Piece, 0, len(pieces))
	used := 0
	included := 0
	omitted := 0
	firstPieceTrimmed := false
	includedKeys := make([]string, 0, len(pieces))
	omittedKeys := make([]string, 0, len(pieces))

	for i, p := range pieces {
		txt := strings.TrimSpace(p.Text)
		if txt == "" {
			continue
		}
		cost := len(txt)
		// Account for join newlines.
		if included > 0 {
			cost += 2
		}
		if used+cost > budgetChars && included > 0 {
			omitted++
			omittedKeys = append(omittedKeys, p.Key)
			continue
		}
		// Docs: [[search-answer-workflow#^spec-0034-us4-ac3]].
		// The first piece is the caller's contract/header. Return a trimmed contract
		// instead of an empty packet so agents still receive orientation and can ask
		// for follow-up detail.
		if used+cost > budgetChars && included == 0 {
			trimmed := TrimToBudget(txt, budgetChars)
			out = append(out, trimmed)
			used = len(trimmed)
			included++
			includedPieces = append(includedPieces, Piece{
				Key:      p.Key,
				Priority: p.Priority,
				Score:    p.Score,
				Text:     trimmed,
			})
			includedKeys = append(includedKeys, p.Key)
			for _, remaining := range pieces[i+1:] {
				if strings.TrimSpace(remaining.Text) != "" {
					omitted++
					omittedKeys = append(omittedKeys, remaining.Key)
				}
			}
			firstPieceTrimmed = true
			break
		}
		out = append(out, txt)
		used += cost
		included++
		includedPieces = append(includedPieces, Piece{
			Key:      p.Key,
			Priority: p.Priority,
			Score:    p.Score,
			Text:     txt,
		})
		includedKeys = append(includedKeys, p.Key)
	}

	return strings.Join(out, "\n\n"), Meta{
		BudgetRequested: budgetChars,
		BudgetUsed:      min(used, budgetChars),
		Trimmed:         firstPieceTrimmed || omitted > 0,
		IncludedPieces:  included,
		OmittedPieces:   omitted,
		IncludedKeys:    includedKeys,
		OmittedKeys:     omittedKeys,
	}, includedPieces
}

// Compressor transforms a set of pieces into compressed output.
// Implementations live in the compress subpackage.
type Compressor interface {
	Compress(ctx context.Context, req CompressRequest) (CompressResult, error)
	// MaxInputChars returns the collection limit in characters.
	// PackWithIntent uses this to know how much content to collect for compression.
	MaxInputChars() int
}

// CompressRequest contains the input for compression.
type CompressRequest struct {
	Pieces  []Piece // Content to compress
	Budget  int     // Target output size in chars
	Intent  string  // What the caller is trying to accomplish
	Context string  // Optional additional context (e.g., tool name)
}

// CompressResult contains the compressed output and metadata.
type CompressResult struct {
	Text             string            // Compressed output text
	Compressed       bool              // Whether compression was applied
	CompressionRatio float64           // Original size / compressed size
	Provider         string            // Which provider was used
	Model            string            // Which model was used
	PieceStatus      map[string]string // Key -> "full" | "summarized" | "omitted"
	Error            error             // Non-fatal error (compression failed, fell back)
}

// PackOptions configures PackWithIntent behavior.
type PackOptions struct {
	Intent         string     // What the caller is trying to accomplish
	Compressor     Compressor // nil = compression disabled
	Ctx            context.Context
	AlwaysCompress bool // Skip threshold check and always compress when over budget
}

// MetaWithCompression extends Meta with compression information.
type MetaWithCompression struct {
	Meta
	Compressed       bool
	CompressionRatio float64
	Provider         string
	PieceStatus      map[string]string
	Error            error
}

// CompressionThreshold is the minimum fraction of pieces that must be omitted
// before compression is triggered. For a sole trimmed piece, use the fraction of
// its bytes that exceed the budget. This avoids compression latency and provider
// variance for minor overages where deterministic truncation is good enough.
const CompressionThreshold = 0.25

// PackWithIntent packs pieces with optional LLM compression when over budget.
// When compression is enabled and content exceeds budget significantly (at least 25% omitted),
// collects more content (up to compressor's input limit) and compresses.
// Falls back to truncation if compression fails.
// When AlwaysCompress is set, compression is used for token density even when content fits.
func PackWithIntent(pieces []Piece, budget int, opts PackOptions) (string, MetaWithCompression) {
	// Standard packing is the baseline even when compression is available. It gives
	// callers deterministic output, metadata, and a safe fallback if the LLM path
	// times out, errors, or returns an empty response.
	text, meta := Pack(pieces, budget)

	if isNilCompressor(opts.Compressor) {
		return text, MetaWithCompression{Meta: meta, Compressed: false}
	}

	// AlwaysCompress is reserved for call sites that explicitly want a dense
	// synthesized body. Normal context packing only pays the LLM latency/variance
	// cost when the deterministic pass would omit a meaningful fraction of pieces.
	shouldCompress := opts.AlwaysCompress
	if !shouldCompress && meta.Trimmed {
		total := meta.IncludedPieces + meta.OmittedPieces
		if total > 0 {
			omittedFraction := float64(meta.OmittedPieces) / float64(total)
			if meta.OmittedPieces == 0 {
				// Only the first nonempty piece can be partially included. Keep it
				// included in metadata, but count its over-budget bytes as pressure.
				for _, piece := range pieces {
					if size := len(strings.TrimSpace(piece.Text)); size > 0 {
						omittedFraction = float64(size-meta.BudgetRequested) / float64(size)
						break
					}
				}
			}
			shouldCompress = omittedFraction >= CompressionThreshold
		}
	}
	if !shouldCompress {
		return text, MetaWithCompression{Meta: meta, Compressed: false}
	}

	// Compression gets a larger input window than the final response budget. The
	// Compressor owns provider-specific token math; PackDetailed preserves the same
	// semantic order while collecting as much source material as the compressor can
	// safely accept.
	collectionBudget := opts.Compressor.MaxInputChars()
	_, _, piecesToCompress := PackDetailed(pieces, collectionBudget)

	ctx := opts.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	result, err := opts.Compressor.Compress(ctx, CompressRequest{
		Pieces: piecesToCompress,
		Budget: budget,
		Intent: opts.Intent,
	})

	// Compression is opportunistic. Never fail the caller because the summarizer is
	// unavailable; keep the deterministic packet and surface the non-fatal error in
	// metadata for diagnostics.
	if err != nil || result.Text == "" {
		return text, MetaWithCompression{
			Meta:       meta,
			Compressed: false,
			Error:      err,
		}
	}

	return result.Text, MetaWithCompression{
		Meta:             meta,
		Compressed:       true,
		CompressionRatio: result.CompressionRatio,
		Provider:         result.Provider,
		PieceStatus:      result.PieceStatus,
	}
}

func isNilCompressor(c Compressor) bool {
	if c == nil {
		return true
	}
	v := reflect.ValueOf(c)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
