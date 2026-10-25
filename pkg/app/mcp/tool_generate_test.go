package mcp

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/harnesstest"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestGenerateReturnsEnvelopeAndPassesRequest(t *testing.T) {
	fake := &harnesstest.Harness{GenerateResult: harness.GenerateResult{
		Output: json.RawMessage(`{"answer":"pong"}`),
		Usage:  harness.TokenUsage{InputTokens: 2, CacheWriteInputTokens: 900, OutputTokens: 5, TotalTokens: 907, CostUSD: 0.01},
		Model:  "claude-haiku-5-5", ToolPolicy: harness.ToolPolicyNone,
	}}
	handler := generateTool(map[harness.Kind]harness.Harness{harness.KindClaude: fake}, noConfiguredHarness)
	result, err := handler(t.Context(), evaluationCall(t, `{"provider":"claude","model":"haiku","effort":"low","prompt":"ping","schema":{"type":"object"}}`))
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.JSONEq(t, `{"provider":"claude","output":{"answer":"pong"},"model":"claude-haiku-5-5","toolPolicy":"none",
		"usage":{"inputTokens":2,"cachedInputTokens":0,"cacheWriteInputTokens":900,"outputTokens":5,"reasoningOutputTokens":0,"totalTokens":907,"costUsd":0.01}}`, result.Content[0].(mcp.TextContent).Text)
	require.Equal(t, harness.GenerateRequest{Prompt: "ping", Model: "haiku", Effort: "low", Schema: json.RawMessage(`{"type":"object"}`)}, fake.GeneratedWith[0])
}

func TestGenerateRejectsBadInputAndSurfacesUnsupported(t *testing.T) {
	unsupported := &harnesstest.Harness{GenerateError: harness.Phase(harness.ErrUnsupported, errors.New("no tool-free mode"))}
	handler := generateTool(map[harness.Kind]harness.Harness{harness.KindCodex: unsupported}, noConfiguredHarness)
	for name, test := range map[string]struct{ args, message string }{
		"unknown provider": {`{"provider":"gemini","prompt":"ping"}`, "provider must be one of"},
		"missing prompt":   {`{"provider":"codex"}`, "prompt is required"},
		"unsupported":      {`{"provider":"codex","prompt":"ping"}`, "harness unsupported: no tool-free mode"},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := handler(t.Context(), evaluationCall(t, test.args))
			require.NoError(t, err)
			require.True(t, result.IsError)
			require.Contains(t, result.Content[0].(mcp.TextContent).Text, test.message)
		})
	}
	require.Len(t, unsupported.GeneratedWith, 1, "invalid input must not reach the provider")
}

func noConfiguredHarness() (harness.Kind, error) { return "", nil }

func TestGenerateDefaultsProviderToSettingThenOnlyReadyProvider(t *testing.T) {
	ready := harness.Status{Installed: true, LoggedIn: true}
	drivers := func(claude, codex harness.Status) map[harness.Kind]harness.Harness {
		return map[harness.Kind]harness.Harness{
			harness.KindClaude: &harnesstest.Harness{StatusResult: claude, GenerateResult: harness.GenerateResult{Output: json.RawMessage(`"from claude"`)}},
			harness.KindCodex:  &harnesstest.Harness{StatusResult: codex, GenerateResult: harness.GenerateResult{Output: json.RawMessage(`"from codex"`)}},
		}
	}
	configured := func(kind harness.Kind) func() (harness.Kind, error) {
		return func() (harness.Kind, error) { return kind, nil }
	}
	for name, test := range map[string]struct {
		args       string
		drivers    map[harness.Kind]harness.Harness
		configured func() (harness.Kind, error)
		provider   string
		failure    string
	}{
		"named provider wins over the setting": {args: `{"provider":"claude","prompt":"ping"}`, drivers: drivers(harness.Status{}, ready), configured: configured(harness.KindCodex), provider: "claude"},
		"agent.harness setting":                {args: `{"prompt":"ping"}`, drivers: drivers(ready, ready), configured: configured(harness.KindCodex), provider: "codex"},
		"only ready provider":                  {args: `{"prompt":"ping"}`, drivers: drivers(harness.Status{Installed: true}, ready), configured: noConfiguredHarness, provider: "codex"},
		"several ready providers": {args: `{"prompt":"ping"}`, drivers: drivers(ready, ready), configured: noConfiguredHarness,
			failure: "name a provider (claude, codex) or set agent.harness: it is unset and claude, codex are installed and signed in"},
		"no ready provider": {args: `{"prompt":"ping"}`, drivers: drivers(harness.Status{}, harness.Status{Installed: true}), configured: noConfiguredHarness,
			failure: "name a provider (claude, codex) or set agent.harness: it is unset and no provider is installed and signed in"},
		"unreadable setting": {args: `{"prompt":"ping"}`, drivers: drivers(ready, ready), configured: func() (harness.Kind, error) { return "", errors.New("bad yaml") },
			failure: "read the agent.harness setting: bad yaml"},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := generateTool(test.drivers, test.configured)(t.Context(), evaluationCall(t, test.args))
			require.NoError(t, err)
			text := result.Content[0].(mcp.TextContent).Text
			if test.failure != "" {
				require.True(t, result.IsError)
				require.Equal(t, test.failure, text)
				return
			}
			require.False(t, result.IsError, text)
			var envelope struct{ Provider, Output string }
			require.NoError(t, json.Unmarshal([]byte(text), &envelope))
			require.Equal(t, test.provider, envelope.Provider)
			require.Equal(t, "from "+test.provider, envelope.Output)
		})
	}
}
