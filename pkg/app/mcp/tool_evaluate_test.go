package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/typesafe"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestEvaluateToolMixedQuestions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer fixture-key", r.Header.Get("Authorization"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "jev-latest", body["model"])
		require.Equal(t, map[string]any{"text": "stale spec"}, body["state"])
		require.NotContains(t, body, "sessionId")
		require.NotContains(t, body, "budgetChars")
		require.Len(t, body["questions"], 3)
		w.Header().Set("x-typesafe-request-id", "fixture-request")
		fmt.Fprint(w, `{"model":"jev-test","answers":{"action":{"type":"choice","choice":"update","probabilities":{"keep":0.2,"update":0.8},"confidence":0.6},"priority":{"type":"score","score":0.8,"probabilities":{"0":0.2,"1":0.8},"legend":{"0":"low","1":"high"},"confidence":0.6},"stale":{"type":"noul","noul":0.9}},"usage":{"input_tokens":100,"output_tokens":20}}`)
	}))
	defer server.Close()
	handler := evaluateTool(func() (*typesafe.Client, error) {
		return typesafe.NewClient("fixture-key", typesafe.WithBaseURL(server.URL))
	})
	result, err := handler(context.Background(), evaluationCall(t, `{"state":{"text":"stale spec"},"sessionId":"fixture-session","budgetChars":1,"questions":{"action":{"type":"choice","criteria":{"keep":"accurate","update":"outdated"}},"priority":{"type":"score","criteria":["low","high"]},"stale":{"type":"noul","instructions":"Is it stale?"}}}`))
	require.NoError(t, err)
	require.False(t, result.IsError)
	var payload struct {
		Model     string                     `json:"model"`
		Answers   map[string]json.RawMessage `json:"answers"`
		Usage     typesafe.Usage             `json:"usage"`
		RequestID string                     `json:"requestId"`
	}
	decodeToolResult(t, result, &payload)
	require.Equal(t, "fixture-request", payload.RequestID)
	require.Equal(t, int64(100), payload.Usage.InputTokens)
	require.JSONEq(t, `{"type":"noul","noul":0.9}`, string(payload.Answers["stale"]))
	require.Contains(t, string(payload.Answers["action"]), `"choice":"update"`)
	require.Contains(t, string(payload.Answers["priority"]), `"score":0.8`)
}

func TestEvaluateToolRejectsInvalidInputBeforeResolvingCredentials(t *testing.T) {
	handler := evaluateTool(func() (*typesafe.Client, error) { t.Fatal("invalid request must not create a client"); return nil, nil })
	for _, input := range []string{
		`{"state":"test","questions":{"bad":{"type":"unknown"}}}`,
		`{"state":null,"questions":{"ok":{"type":"noul"}}}`,
		`{"state":"test","questions":{"bad":{"type":"score","criteria":["one"]}}}`,
	} {
		result, err := handler(context.Background(), evaluationCall(t, input))
		require.NoError(t, err)
		require.True(t, result.IsError)
	}
}

func TestEvaluateToolPreservesProviderFailureWithoutResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-typesafe-request-id", "failed-request")
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, "private upstream response")
	}))
	defer server.Close()
	handler := evaluateTool(func() (*typesafe.Client, error) {
		return typesafe.NewClient("fixture-key", typesafe.WithBaseURL(server.URL), typesafe.WithMaxRetries(0))
	})
	result, err := handler(context.Background(), evaluationCall(t, `{"state":"test","questions":{"ok":{"type":"noul"}}}`))
	require.NoError(t, err)
	require.True(t, result.IsError)
	data, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(data), "private upstream response")
	require.NotContains(t, string(data), "fixture-key")
	text := result.Content[0].(mcp.TextContent).Text
	var failure map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &failure))
	require.Equal(t, "typesafe_api_error", failure["code"])
	require.Equal(t, float64(429), failure["statusCode"])
	require.Equal(t, float64(2000), failure["retryAfterMs"])
	require.Equal(t, "failed-request", failure["requestId"])
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err = handler(ctx, evaluationCall(t, `{"state":"test","questions":{"ok":{"type":"noul"}}}`))
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.True(t, strings.Contains(result.Content[0].(mcp.TextContent).Text, "context canceled"))
}

func evaluationCall(t *testing.T, input string) mcp.CallToolRequest {
	t.Helper()
	var args map[string]any
	require.NoError(t, json.Unmarshal([]byte(input), &args))
	return mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "evaluate", Arguments: args}}
}
