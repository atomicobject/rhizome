package agentcode

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"io"
	"strings"
	"testing"
	"time"
)

func TestServeOverlapsCallsAndCancelsOneSibling(t *testing.T) {
	description, err := Describe([]string{"files"})
	require.NoError(t, err)
	reader, writer := io.Pipe()
	defer writer.Close()
	var output bytes.Buffer
	started := make(chan string, 2)
	finished := make(chan string, 2)
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Serve(t.Context(), reader, &output, func(ctx context.Context, _ string, input map[string]any) (CallOutcome, error) {
			name := input["inputs"].([]any)[0].(string)
			started <- name
			if name == "cancel" {
				<-ctx.Done()
			} else {
				<-release
			}
			finished <- name
			return CallOutcome{OK: true, Payload: name}, nil
		})
	}()
	_, err = io.WriteString(writer, frames(t,
		request(1, "initialize", map[string]any{"protocolVersion": ProtocolVersion, "selected": description.Selected, "contractHash": description.ContractHash}),
		request(2, "call", map[string]any{"operation": "files", "input": map[string]any{"inputs": []string{"cancel"}}, "timeoutMs": 2000}),
		request(3, "call", map[string]any{"operation": "files", "input": map[string]any{"inputs": []string{"keep"}}, "timeoutMs": 2000}),
	))
	require.NoError(t, err)
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("calls did not overlap")
		}
	}
	_, err = io.WriteString(writer, frames(t, map[string]any{"jsonrpc": "2.0", "method": "$/cancelRequest", "params": map[string]any{"id": 2}}))
	require.NoError(t, err)
	require.Equal(t, "cancel", <-finished)
	close(release)
	require.Equal(t, "keep", <-finished)
	_, err = io.WriteString(writer, frames(t, request(4, "shutdown", map[string]any{})))
	require.NoError(t, err)
	require.NoError(t, <-done)
	var replies []rpcReply
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		var reply rpcReply
		require.NoError(t, json.Unmarshal([]byte(line), &reply))
		replies = append(replies, reply)
	}
	byID := map[string]rpcReply{}
	for _, reply := range replies {
		byID[string(reply.ID)] = reply
	}
	require.NotNil(t, byID["2"].Error)
	require.Equal(t, "keep", byID["3"].Result.(map[string]any)["payload"])
}

func TestGeneratedClientCorrelatesConcurrentOutOfOrderReplies(t *testing.T) {
	requireNode(t)
	artifact, err := Generate(t.TempDir(), []string{"files"})
	require.NoError(t, err)
	fake := writeResponseServer(t, artifact.ContractHash, `
 responses.push({jsonrpc:"2.0",id:frame.id,result:{ok:true,payload:frame.params.input.inputs[0]}});
 if(responses.length===2) for(const reply of responses.reverse()) process.stdout.write(JSON.stringify(reply)+"\n");`)
	result := runNode(t, artifact.ModulePath, fake, t.TempDir(), `
const client=createClient({executablePath,vaultPath,timeoutMs:2000});
try { const rows=await Promise.all([client.files({inputs:["first"]}),client.files({inputs:["second"]})]); console.log(JSON.stringify({values:rows.map(row=>row.payload)})); }
finally { await client.close(); }`)
	require.Equal(t, []any{"first", "second"}, result["values"])
}
