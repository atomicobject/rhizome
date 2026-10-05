## LLM-based compression for budget-exceeded context packing

Implements `contextpack.Compressor` interface to intelligently compress content using an LLM when standard packing omits too much.

- **Entry points**: `NewLLMCompressor`, `NewFromLocalConfig`
- **Disabled compression**: a nil compressor keeps deterministic packing; no pass-through compressor is constructed.
- **Key invariant**: compression only triggers when >25% would be omitted; falls back to truncation on failure
- **Providers**: cerebras, openai, anthropic (requires API key env var)
- **Chunking + parallelism**: large inputs are split into chunks and compressed in parallel, then concatenated (budget > 40k tokens auto-increases chunk count)
- **Config knobs**: maxOutputTokens (provider cap), reasoningTokenReserve, chunkChars, parallelism

### Files

- `llm.go` — LLMCompressor implementation
- `prompt.go` — compression prompt templates (with/without intent)

### Deep docs

- [[Contextpack (Hub)]]
- [[Intent-driven compression]]
