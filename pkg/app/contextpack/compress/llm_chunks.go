package compress

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/llm"
)

// truncateIntent truncates an intent string for logging display.
func truncateIntent(intent string) string {
	if len(intent) <= 50 {
		return intent
	}
	return intent[:47] + "..."
}

func splitPieces(pieces []contextpack.Piece, chunkChars int) [][]contextpack.Piece {
	if chunkChars <= 0 || len(pieces) == 0 {
		if len(pieces) == 0 {
			return nil
		}
		return [][]contextpack.Piece{pieces}
	}

	var chunks [][]contextpack.Piece
	var current []contextpack.Piece
	currentChars := 0

	for _, p := range pieces {
		text := strings.TrimSpace(p.Text)
		if text == "" {
			continue
		}
		size := len(text)
		if currentChars+size > chunkChars && len(current) > 0 {
			chunks = append(chunks, current)
			current = nil
			currentChars = 0
		}
		current = append(current, p)
		currentChars += size
	}

	if len(current) > 0 {
		chunks = append(chunks, current)
	}

	if len(chunks) == 0 {
		return nil
	}
	return chunks
}

func allocateChunkBudgets(chunks [][]contextpack.Piece, budget int) []int {
	if len(chunks) == 0 {
		return nil
	}
	if budget <= 0 {
		out := make([]int, len(chunks))
		return out
	}

	totalChars := 0
	chunkChars := make([]int, len(chunks))
	for i, chunk := range chunks {
		size := 0
		for _, p := range chunk {
			text := strings.TrimSpace(p.Text)
			if text == "" {
				continue
			}
			size += len(text)
		}
		chunkChars[i] = size
		totalChars += size
	}
	if totalChars == 0 {
		out := make([]int, len(chunks))
		return out
	}

	remainingBudget := budget
	remainingChars := totalChars
	out := make([]int, len(chunks))
	for i := range chunks {
		if i == len(chunks)-1 {
			out[i] = max(0, remainingBudget)
			break
		}
		// Allocate by remaining input size so earlier/later chunks are not favored by
		// position alone. The final chunk absorbs rounding drift to keep total budget exact.
		share := float64(chunkChars[i]) / float64(remainingChars)
		chunkBudget := int(math.Round(float64(remainingBudget) * share))
		if chunkBudget < minChunkBudgetChars {
			chunkBudget = min(minChunkBudgetChars, remainingBudget)
		}
		if chunkBudget > remainingBudget {
			chunkBudget = remainingBudget
		}
		out[i] = chunkBudget
		remainingBudget -= chunkBudget
		remainingChars -= chunkChars[i]
	}

	return out
}

func (c *LLMCompressor) compressChunks(ctx context.Context, chunks [][]contextpack.Piece, budgets []int, intent string, out []string) error {
	if len(chunks) == 1 {
		text, err := c.compressChunk(ctx, chunks[0], intent, budgets[0])
		if err != nil {
			return err
		}
		out[0] = text
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		index int
		text  string
		err   error
	}

	results := make(chan result, len(chunks))
	sem := make(chan struct{}, c.parallelism)
	var wg sync.WaitGroup

	for i, chunk := range chunks {
		budget := budgets[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results <- result{index: i, err: ctx.Err()}
				return
			}
			defer func() { <-sem }()

			text, err := c.compressChunk(ctx, chunk, intent, budget)
			if err != nil {
				// One failed chunk invalidates the synthesized packet; cancel siblings
				// and let PackWithIntent fall back to deterministic truncation.
				cancel()
			}
			results <- result{index: i, text: text, err: err}
		}()
	}

	wg.Wait()
	close(results)

	var firstErr error
	for res := range results {
		if res.err != nil && firstErr == nil {
			firstErr = res.err
		}
		out[res.index] = res.text
	}

	return firstErr
}

func (c *LLMCompressor) compressChunk(ctx context.Context, pieces []contextpack.Piece, intent string, budget int) (string, error) {
	prompt, err := buildPrompt(pieces, budget, intent)
	if err != nil {
		return "", fmt.Errorf("build prompt: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	llmReq := llm.Request{
		Model: c.model,
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
		ReasoningEffort: c.reasoningEffort,
		Temperature:     0.3,
		MaxOutputTokens: c.resolveMaxOutputTokens(budget, len(prompt)),
	}

	resp, err := c.provider.Complete(ctx, llmReq)
	if err != nil {
		return "", err
	}

	compressed := strings.TrimSpace(resp.Content)
	if compressed == "" {
		return "", fmt.Errorf("empty LLM response")
	}

	if budget > 0 && len(compressed) > budget+500 {
		compressed = contextpack.TrimToBudget(compressed, budget)
	}

	return compressed, nil
}

func (c *LLMCompressor) resolveMaxOutputTokens(budget int, promptChars int) int {
	if c.maxOutputTokens > 0 {
		return c.maxOutputTokens
	}
	if strings.ToLower(strings.TrimSpace(c.name)) != "cerebras" {
		return 0
	}
	if budget <= 0 {
		return 0
	}

	desired := estimateTokensForChars(budget)
	if desired <= 0 {
		return 0
	}
	if desired > DefaultCerebrasMaxOutputTokens {
		desired = DefaultCerebrasMaxOutputTokens
	}

	contextTokens := modelContextTokens(c.model)
	if contextTokens <= 0 {
		contextTokens = DefaultModelContextTokens
	}
	promptTokens := estimateTokensForChars(promptChars)
	// Keep explicit reasoning headroom before setting max output tokens. Without this,
	// long prompts can starve reasoning-capable models and turn compression into a
	// provider error instead of a useful fallback path.
	available := contextTokens - promptTokens - c.reasoningTokens
	if available > 0 {
		desired = min(desired, available)
	}
	return desired
}

func estimateTokensForChars(chars int) int {
	if chars <= 0 {
		return 0
	}
	return int(math.Ceil(float64(chars) / 4.0))
}

func modelContextTokens(model string) int {
	normalized := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(normalized, "gpt-oss-120b"):
		return DefaultModelContextTokens
	default:
		return 0
	}
}

func defaultChunkChars(maxInputTokens int) int {
	if maxInputTokens <= 0 {
		return 0
	}
	return int(float64(maxInputTokens) * 4 * defaultChunkFraction)
}

func (c *LLMCompressor) adjustChunkCharsForBudget(chunkChars int, totalChars int, budgetTokens int) int {
	if chunkChars <= 0 || totalChars <= 0 || budgetTokens <= 0 {
		return chunkChars
	}
	if c.maxOutputTokens > 0 {
		return chunkChars
	}
	if strings.ToLower(strings.TrimSpace(c.name)) != "cerebras" {
		return chunkChars
	}
	if budgetTokens <= DefaultCerebrasMaxOutputTokens {
		return chunkChars
	}

	targetChunks := int(math.Ceil(float64(budgetTokens) / float64(DefaultCerebrasMaxOutputTokens)))
	if targetChunks <= 1 {
		return chunkChars
	}

	targetChunkChars := int(math.Ceil(float64(totalChars) / float64(targetChunks)))
	if targetChunkChars < minChunkBudgetChars {
		targetChunkChars = minChunkBudgetChars
	}
	if targetChunkChars < chunkChars {
		return targetChunkChars
	}
	return chunkChars
}
