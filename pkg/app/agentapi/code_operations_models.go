package agentapi

import (
	"encoding/json"
	mcpapi "github.com/atomicobject/rhizome/pkg/app/mcp"
)

func codeEvaluateDescriptor() ToolDescriptor {
	d := agentCLI(agentCapability(descriptor("evaluate", "evaluate", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.EvaluateTool), 27), 31, AgentCLICategoryAnalysis)
	d.CodeAccess = CodeIndependent
	c := evaluateContract()
	return completeCodeDescriptor(d, c, []string{"agent evaluate"}, map[string]codeInputFieldMetadata{
		"state":     {Description: "Text, object, or array containing the evidence to evaluate. Send only content needed for the decision."},
		"questions": {Description: "Independent named choice, score, or noul questions against the same state. IDs match answers; put instructions in each question, not in its ID."},
		"model":     {Description: "TypeSafe model ID. Omission uses jev-latest.", Default: "jev-latest", HasDefault: true},
	}, "Questions run independently against the same state. Choice returns an option and distribution; score returns the expected zero-based rubric level, distribution and legend; noul returns the probability of yes. Inspect answer.type before reading variant fields. Confidence describes distribution concentration, not verified correctness. Batch related questions for one state; use separate calls for different states. API failures return a failed outcome. budgetChars and sessionId do not truncate or deduplicate evaluation.")
}

func codeEvaluateBatchDescriptor() ToolDescriptor {
	d := agentCLI(agentCapability(descriptor("evaluate_batch", "evaluate-batch", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.EvaluateBatchTool), 29), 33, AgentCLICategoryAnalysis)
	d.CodeAccess = CodeIndependent
	single := evaluateContract()
	var in, out codeSchema
	_ = json.Unmarshal(single.InputSchema, &in)
	_ = json.Unmarshal(single.OutputSchema, &out)
	properties := in["properties"].(map[string]any)
	item := object([]string{"id", "state", "questions"}, map[string]codeSchema{
		"id": str(), "state": properties["state"].(map[string]any), "questions": properties["questions"].(map[string]any), "model": str(),
	})
	result := object([]string{"id", "status", "attempts"}, map[string]codeSchema{
		"id": str(), "status": enumeration("succeeded", "failed", "not_started", "uncertain"), "attempts": integer(),
		"response": out, "requestId": str(), "error": str(), "statusCode": integer(), "retryable": boolean(),
	})
	c := contract("Evaluate independent Jev states concurrently with bounded retries and partial results.", input(map[string]codeSchema{
		"items": {"type": "array", "items": item}, "concurrency": integer(), "budgetMs": integer(),
	}, "items"), object([]string{"items", "usage"}, map[string]codeSchema{
		"items": {"type": "array", "items": result}, "usage": object([]string{"input_tokens", "output_tokens"}, map[string]codeSchema{"input_tokens": integer(), "output_tokens": integer()}),
	}), map[string]any{"items": []any{map[string]any{"id": "example", "state": "An obsolete spec", "questions": map[string]any{"stale": map[string]any{"type": "noul", "instructions": "Does this describe obsolete behavior?"}}}}}, "sends supplied evidence to TypeSafe and incurs API usage", "uses up to 32 workers; throttle pauses are shared within a batch", "does not persist results; caller owns checkpoints")
	return completeCodeDescriptor(d, c, []string{"agent evaluate-batch"}, map[string]codeInputFieldMetadata{
		"items":       {Description: "1..128 independent states and question maps with unique nonempty IDs. Results preserve input order."},
		"concurrency": {Description: "Maximum concurrent upstream requests within this batch, 1..32.", Default: 8, HasDefault: true},
		"budgetMs":    {Description: "Batch scheduling budget in milliseconds, 1..300000. Returns partial results before the call deadline. Set CallOptions.timeoutMs longer than this budget.", Default: 20000, HasDefault: true},
	}, "Questions within each item are independent against its state. Item statuses distinguish succeeded, failed, not_started and uncertain. Check every status even when the operation succeeds. Checkpoint successful results; never automatically replay uncertain items. Usage totals include validated successes only, not unknown usage from failures. Throttle coordination is per batch, not global across processes. budgetChars and sessionId do not truncate or deduplicate evaluations.")
}

func codeGenerateDescriptor() ToolDescriptor {
	d := agentCLI(agentCapability(descriptor("generate", "generate", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.GenerateTool), 30), 34, AgentCLICategoryAnalysis)
	d.CodeAccess = CodeIndependent
	number := codeSchema{"type": "number"}
	c := contract("Run one tool-free, sessionless model call through the user's Claude Code or Codex CLI.",
		input(map[string]codeSchema{"provider": enumeration("claude", "codex"), "model": str(), "effort": str(), "prompt": str(), "schema": unknownObject()}, "prompt"),
		object([]string{"provider", "output", "usage", "toolPolicy"}, map[string]codeSchema{
			"provider": str(), "output": unknownValue(), "model": str(), "toolPolicy": enumeration("none", "sandboxed"),
			"usage": object([]string{"inputTokens", "cachedInputTokens", "outputTokens", "reasoningOutputTokens", "totalTokens"}, map[string]codeSchema{
				"inputTokens": integer(), "cachedInputTokens": integer(), "cacheWriteInputTokens": integer(), "outputTokens": integer(), "reasoningOutputTokens": integer(), "totalTokens": integer(), "costUsd": number,
			}),
		}),
		map[string]any{"provider": "claude", "model": "claude-haiku-5-5", "effort": "low", "prompt": "Name one prime number.", "schema": map[string]any{"type": "object", "properties": map[string]any{"prime": map[string]any{"type": "integer"}}, "required": []string{"prime"}}},
		"sends the prompt to the provider through the installed CLI and its login, and incurs model usage", "allows no tools, or for Codex only a read-only, network-less sandbox, and persists no session", "does not read or mutate vault content")
	return completeCodeDescriptor(d, c, []string{"agent generate"}, map[string]codeInputFieldMetadata{
		"provider": {Description: "claude or codex. Omission uses the agent.harness setting, else the only provider installed and signed in, and otherwise fails naming the choices."},
		"model":    {Description: "Provider model ID or alias. Omission uses the CLI's configured default."},
		"effort":   {Description: "Provider reasoning effort, such as low or high. Omission uses the CLI's default."},
		"prompt":   {Description: "Complete prompt text. The model sees nothing else from the vault."},
		"schema":   {Description: "Optional JSON Schema for a structured result. Claude checks the top-level type and required keys; Codex sends it as a strict output schema, so objects need additionalProperties false and every property required."},
	}, "output is the schema-shaped JSON value, or a JSON string without a schema. model is the model the provider reported running and may differ from the requested alias. toolPolicy is none when the CLI removed every tool (Claude) or sandboxed when configuration removed what it can and a read-only, network-less sandbox with no approvals bounds the rest (Codex). usage and costUsd are provider-reported; cachedInputTokens are cache reads and cacheWriteInputTokens cache writes. Claude uses the five-minute prompt cache. Set CallOptions.timeoutMs above expected model latency; the 30-second default is short for large prompts. budgetChars and sessionId do not affect generation.")
}
