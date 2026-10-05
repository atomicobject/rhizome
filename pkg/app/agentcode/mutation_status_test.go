package agentcode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOversizedNoteMutationRetainsCurrentDecision(t *testing.T) {
	description, err := Describe([]string{"note_move"})
	require.NoError(t, err)
	input := frames(t,
		request(1, "initialize", map[string]any{"protocolVersion": ProtocolVersion, "selected": description.Selected, "contractHash": description.ContractHash}),
		request(2, "call", map[string]any{"operation": "note_move", "input": map[string]any{"source": "Old.md", "target": "New.md"}, "timeoutMs": 1000}),
	)
	current := map[string]any{
		"decision": "committed", "transactionId": "current-request", "receiptPath": ".rhizome/edit-receipts/current-request.json", "recoveryPending": true,
		"unboundedExtra": strings.Repeat("x", maxFrameBytes),
	}
	var recovered []any
	for i := 0; i < 20; i++ {
		recovered = append(recovered, map[string]any{
			"decision": "restored", "transactionId": fmt.Sprintf("earlier-%d", i),
			"receiptPath": strings.Repeat("\x01", 1024), "recoveryPending": false,
		})
	}
	var output bytes.Buffer
	require.NoError(t, serveUntilShutdown(t, input, &output, func(context.Context, string, map[string]any) (CallOutcome, error) {
		return CallOutcome{OK: false, ExitCode: 1, Payload: strings.Repeat("x", maxFrameBytes), Diagnostic: map[string]any{
			"mutation": map[string]any{"current": current, "recovered": recovered, "recoveredCount": 20, "recoveredTruncated": false},
		}}, nil
	}))
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		require.LessOrEqual(t, len(line), maxFrameBytes)
	}
	replies := parseReplies(t, output.String())
	data := replies[1].Error.Data.(map[string]any)
	require.Equal(t, "output_limit_exceeded", data["code"])
	outcome := data["outcome"].(map[string]any)
	require.Equal(t, false, outcome["ok"])
	require.Equal(t, float64(1), outcome["exitCode"])
	status, ok := outcome["diagnostic"].(map[string]any)["mutation"].(map[string]any)
	require.True(t, ok, "oversized results must retain the recorded mutation decision")
	retained := status["current"].(map[string]any)
	require.Equal(t, "committed", retained["decision"])
	require.Equal(t, "current-request", retained["transactionId"])
	require.Equal(t, ".rhizome/edit-receipts/current-request.json", retained["receiptPath"])
	require.Equal(t, true, retained["recoveryPending"])
	require.NotContains(t, retained, "unboundedExtra")
	require.Equal(t, float64(20), status["recoveredCount"])
	require.Equal(t, true, status["recoveredTruncated"])
	raw, err := json.Marshal(status)
	require.NoError(t, err)
	require.LessOrEqual(t, len(raw), 8<<10)
	items := status["recovered"].([]any)
	require.NotEmpty(t, items)
	require.Less(t, len(items), len(recovered))
}
