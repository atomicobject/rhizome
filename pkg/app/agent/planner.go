package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/llm"
)

// PlanHints supplies optional user-provided seeds to avoid duplication.
type PlanHints struct {
	Queries []string
	Files   []string
}

// PlanToolCalls asks the LLM to propose tool calls for retrieval.
func PlanToolCalls(ctx context.Context, provider llm.Provider, profile llm.ResolvedProfile, task string, instructions string, tools []ToolSpec, hints PlanHints) ([]ToolCall, error) {
	if provider == nil {
		return nil, fmt.Errorf("provider is nil")
	}
	prompt := buildPlannerPrompt(task, tools, hints)
	system := plannerSystemPrompt
	if strings.TrimSpace(instructions) != "" {
		system = strings.TrimSpace(instructions) + "\n\n" + plannerSystemPrompt
	}
	resp, err := provider.Complete(ctx, llm.Request{
		Model:           profile.Model,
		Messages:        []llm.Message{{Role: "system", Content: system}, {Role: "user", Content: prompt}},
		ReasoningEffort: profile.ReasoningEffort,
		MaxOutputTokens: plannerOutputTokens(profile.ReasoningEffort),
	})
	if err != nil {
		return nil, err
	}
	return ParseToolCalls(resp.Content)
}

// plannerOutputTokens bounds the planner's reply. The plan is a short JSON list,
// but thinking counts toward output tokens on current reasoning models, so the
// bound grows with the profile's reasoning effort.
func plannerOutputTokens(effort llm.ReasoningEffort) int {
	switch effort {
	case "", llm.ReasoningNone, llm.ReasoningLow:
		return 4096
	case llm.ReasoningMedium:
		return 8192
	default:
		return 16000
	}
}

const plannerSystemPrompt = "You are a retrieval planner. Return ONLY JSON with a tool_calls array."

func buildPlannerPrompt(task string, tools []ToolSpec, hints PlanHints) string {
	var b strings.Builder
	b.WriteString("Task:\n")
	b.WriteString(strings.TrimSpace(task))
	b.WriteString("\n\nAvailable tools (name, description, args_schema):\n")
	for _, tool := range tools {
		b.WriteString(fmt.Sprintf("- %s: %s", tool.Name, tool.Description))
		if strings.TrimSpace(tool.ArgsSchema) != "" {
			b.WriteString("\n  args_schema: ")
			b.WriteString(tool.ArgsSchema)
		}
		b.WriteString("\n")
	}
	if len(hints.Queries) > 0 {
		b.WriteString("\nUser-provided queries (avoid duplicates):\n")
		for _, q := range hints.Queries {
			b.WriteString("- ")
			b.WriteString(q)
			b.WriteString("\n")
		}
	}
	if len(hints.Files) > 0 {
		b.WriteString("\nUser-provided files/dirs (avoid duplicates):\n")
		for _, f := range hints.Files {
			b.WriteString("- ")
			b.WriteString(f)
			b.WriteString("\n")
		}
	}
	b.WriteString("\nReturn JSON like:\n{\"tool_calls\":[{\"name\":\"semantic_query\",\"arguments\":{\"queries\":[\"...\"]}}]}\n")
	return b.String()
}
