package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/atomicobject/rhizome/pkg/typesafe"
	"github.com/mark3labs/mcp-go/mcp"
)

// EvaluateBatchTool schedules independent Jev states without initializing a vault runtime.
func EvaluateBatchTool(_ Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return evaluateBatchTool(newEvaluationClient)
}

func evaluateBatchTool(newClient func() (*typesafe.Client, error)) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, call mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var input struct {
			Items []struct {
				ID        string                     `json:"id"`
				State     any                        `json:"state"`
				Model     string                     `json:"model"`
				Questions map[string]json.RawMessage `json:"questions"`
			} `json:"items"`
			Concurrency int `json:"concurrency"`
			BudgetMS    int `json:"budgetMs"`
		}
		data, err := json.Marshal(call.GetArguments())
		if err != nil {
			return mcp.NewToolResultError("invalid batch input"), nil
		}
		if err = json.Unmarshal(data, &input); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if input.BudgetMS == 0 {
			input.BudgetMS = 20000
		}
		if input.BudgetMS < 1 || input.BudgetMS > 300000 {
			return mcp.NewToolResultError("budgetMs must be 1..300000"), nil
		}
		items := make([]typesafe.BatchItem, len(input.Items))
		for i, item := range input.Items {
			raw, _ := json.Marshal(item)
			var request typesafe.Request
			if err = json.Unmarshal(raw, &request); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("item %q: %v", item.ID, err)), nil
			}
			items[i] = typesafe.BatchItem{ID: item.ID, Request: request}
		}
		client, err := newClient()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		// Finish before the transport deadline so partial results can be returned.
		deadline := time.Now().Add(time.Duration(input.BudgetMS) * time.Millisecond)
		if outer, ok := ctx.Deadline(); ok && outer.Add(-250*time.Millisecond).Before(deadline) {
			deadline = outer.Add(-250 * time.Millisecond)
		}
		batchCtx, cancel := context.WithDeadline(ctx, deadline)
		defer cancel()
		results, err := client.EvaluateBatch(batchCtx, items, input.Concurrency)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		usage := typesafe.Usage{}
		for _, result := range results {
			if result.Response != nil {
				usage.InputTokens += result.Response.Usage.InputTokens
				usage.OutputTokens += result.Response.Usage.OutputTokens
			}
		}
		return respondJSON(struct {
			Items []typesafe.BatchResult `json:"items"`
			Usage typesafe.Usage         `json:"usage"`
		}{results, usage}, "marshal evaluate batch failed")
	}
}
