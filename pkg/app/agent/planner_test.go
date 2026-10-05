package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/llm"
)

func TestPlanToolCalls(t *testing.T) {
	provider := &llm.MockProvider{Responses: []llm.Response{{Content: `{"tool_calls":[{"name":"semantic_query","arguments":{"queries":["alpha"]}}]}`}}}
	profile := llm.ResolvedProfile{Model: "gpt-5.2", ReasoningEffort: llm.ReasoningLow}
	calls, err := PlanToolCalls(context.Background(), provider, profile, "Find docs", "Prefer recent docs", []ToolSpec{{Name: "semantic_query", Description: "search", ArgsSchema: `{"queries":[]}`}}, PlanHints{Queries: []string{"alpha"}, Files: []string{"README.md"}})
	if err != nil {
		t.Fatalf("plan failed: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if provider.Calls[0].Model != "gpt-5.2" {
		t.Fatalf("expected model gpt-5.2, got %s", provider.Calls[0].Model)
	}
	if provider.Calls[0].ReasoningEffort != llm.ReasoningLow {
		t.Fatalf("reasoning effort: %s", provider.Calls[0].ReasoningEffort)
	}
	if calls[0].Name != "semantic_query" || calls[0].Arguments["queries"].([]any)[0] != "alpha" {
		t.Fatalf("parsed call: %#v", calls[0])
	}
	request := provider.Calls[0]
	if len(request.Messages) != 2 || request.Messages[0].Role != "system" || request.Messages[1].Role != "user" {
		t.Fatalf("planner messages: %#v", request.Messages)
	}
	for _, want := range []string{"Prefer recent docs", "retrieval planner"} {
		if !strings.Contains(request.Messages[0].Content, want) {
			t.Errorf("system message missing %q", want)
		}
	}
	for _, want := range []string{"Find docs", "semantic_query: search", `{"queries":[]}`, "alpha", "README.md"} {
		if !strings.Contains(request.Messages[1].Content, want) {
			t.Errorf("user message missing %q", want)
		}
	}
}
