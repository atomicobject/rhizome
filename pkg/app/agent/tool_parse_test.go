package agent

import "testing"

func TestParseToolCalls(t *testing.T) {
	for _, tc := range []struct{ name, payload, tool, key, value string }{
		{"plain", `{"tool_calls":[{"name":"semantic_query","arguments":{"queries":["alpha"]}}]}`, "semantic_query", "queries", "alpha"},
		{"embedded", "Here are calls:\n```json\n{\"tool_calls\":[{\"name\":\"file_context\",\"arguments\":{\"files\":[\"README.md\"]}}]}\n```", "file_context", "files", "README.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, err := ParseToolCalls(tc.payload)
			if err != nil {
				t.Fatalf("parse failed: %v", err)
			}
			if len(calls) != 1 || calls[0].Name != tc.tool || calls[0].Arguments[tc.key].([]any)[0] != tc.value {
				t.Fatalf("parsed calls: %#v", calls)
			}
		})
	}
}
