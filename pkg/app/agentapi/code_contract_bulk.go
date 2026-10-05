package agentapi

import "encoding/json"

func evaluateBatchContract() *CodeOperationContract {
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
	return contract("Evaluate independent Jev states concurrently with bounded retries and partial results.", input(map[string]codeSchema{
		"items": {"type": "array", "items": item}, "concurrency": integer(), "budgetMs": integer(),
	}, "items"), object([]string{"items", "usage"}, map[string]codeSchema{
		"items": {"type": "array", "items": result}, "usage": object([]string{"input_tokens", "output_tokens"}, map[string]codeSchema{"input_tokens": integer(), "output_tokens": integer()}),
	}), map[string]any{"items": []any{map[string]any{"id": "example", "state": "An obsolete spec", "questions": map[string]any{"stale": map[string]any{"type": "noul", "instructions": "Does this describe obsolete behavior?"}}}}}, "sends supplied evidence to TypeSafe and incurs API usage", "uses up to 32 workers; throttle pauses are shared within a batch", "does not persist results; caller owns checkpoints")
}

func checkPathsContract() *CodeOperationContract {
	return typedContract("Check paths against the vault's unified ignore rules in bulk.", input(map[string]codeSchema{
		"paths": {"type": "array", "items": object([]string{"path"}, map[string]codeSchema{"path": str(), "isDir": boolean()})},
	}, "paths"), map[string]any{"paths": []any{map[string]any{"path": "README.md"}, map[string]any{"path": "vendor", "isDir": true}}}, "reads ignore rules and resolves path containment", "does not read file contents or initialize an index")
}
