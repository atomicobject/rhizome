package agentcode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestServeInitializesAndReturnsDomainOutcomes(t *testing.T) {
	description, err := Describe([]string{"files"})
	require.NoError(t, err)
	input := frames(t,
		request(1, "initialize", map[string]any{"protocolVersion": ProtocolVersion, "selected": description.Selected, "contractHash": description.ContractHash, "sessionId": "session-1"}),
		request(2, "call", map[string]any{"operation": "files", "input": map[string]any{"inputs": []string{"a.go"}}, "timeoutMs": 1000}),
	)
	var output bytes.Buffer
	var got map[string]any
	err = serveUntilShutdown(t, input, &output, func(_ context.Context, operation string, value map[string]any) (CallOutcome, error) {
		got = value
		return CallOutcome{OK: false, ExitCode: 2, Payload: map[string]any{"operation": operation}, Diagnostic: map[string]any{"code": "invalid"}}, nil
	})
	require.NoError(t, err)
	require.Equal(t, "session-1", got["sessionId"])
	replies := parseReplies(t, output.String())
	require.Equal(t, ProtocolVersion, replies[0].Result.(map[string]any)["protocolVersion"])
	outcome := replies[1].Result.(map[string]any)
	require.False(t, outcome["ok"].(bool))
	require.Equal(t, float64(2), outcome["exitCode"])
	require.Equal(t, "files", outcome["payload"].(map[string]any)["operation"])
}

func TestServeRejectsUnselectedAndQueuedCancelledCalls(t *testing.T) {
	description, err := Describe([]string{"files"})
	require.NoError(t, err)
	reader, writer := io.Pipe()
	var output bytes.Buffer
	started := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	notifiedOutput := &matchingWriter{
		Writer:  &output,
		match:   []byte(`"code":"cancelled"`),
		matched: cancelled,
	}
	var calls int
	var once sync.Once
	go func() {
		done <- serveWithConcurrency(1, context.Background(), reader, notifiedOutput, func(context.Context, string, map[string]any) (CallOutcome, error) {
			calls++
			once.Do(func() { close(started) })
			<-release
			return CallOutcome{OK: true}, nil
		})
	}()
	_, err = writer.Write([]byte(frames(t,
		request(1, "initialize", map[string]any{"protocolVersion": ProtocolVersion, "selected": description.Selected, "contractHash": description.ContractHash}),
		request(2, "call", map[string]any{"operation": "file_context", "input": map[string]any{}, "timeoutMs": 1000}),
		request(3, "call", map[string]any{"operation": "files", "input": map[string]any{"inputs": []string{"a.go"}}, "timeoutMs": 1000}),
		request(4, "call", map[string]any{"operation": "files", "input": map[string]any{"inputs": []string{"b.go"}}, "timeoutMs": 1000}),
	)))
	require.NoError(t, err)
	<-started
	_, err = writer.Write([]byte(frames(t,
		map[string]any{"jsonrpc": "2.0", "method": "$/cancelRequest", "params": map[string]any{"id": 4}},
		request(5, "shutdown", map[string]any{}),
	)))
	require.NoError(t, err)
	<-cancelled
	close(release)
	require.NoError(t, <-done)
	require.NoError(t, writer.Close())
	require.Equal(t, 1, calls)
	replies := parseReplies(t, output.String())
	var codes []string
	for _, reply := range replies {
		if reply.Error != nil {
			codes = append(codes, reply.Error.Data.(map[string]any)["code"].(string))
		}
	}
	require.ElementsMatch(t, []string{"operation_not_selected", "cancelled"}, codes)
}

type matchingWriter struct {
	io.Writer
	match   []byte
	matched chan struct{}
	once    sync.Once
}

func (w *matchingWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, w.match) {
		w.once.Do(func() { close(w.matched) })
	}
	return w.Writer.Write(p)
}

func request(id int, method string, params map[string]any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
}

func frames(t *testing.T, values ...map[string]any) string {
	t.Helper()
	var out strings.Builder
	for _, value := range values {
		data, err := json.Marshal(value)
		require.NoError(t, err)
		out.Write(data)
		out.WriteByte('\n')
	}
	return out.String()
}

type parsedReply struct {
	Result any       `json:"result"`
	Error  *rpcError `json:"error"`
}

func parseReplies(t *testing.T, output string) []parsedReply {
	t.Helper()
	var replies []parsedReply
	for _, line := range strings.FieldsFunc(output, func(r rune) bool { return r == '\n' }) {
		var reply parsedReply
		require.NoError(t, json.Unmarshal([]byte(line), &reply))
		replies = append(replies, reply)
	}
	return replies
}

func TestServeCancellationReachesRunningHandler(t *testing.T) {
	description, err := Describe([]string{"files"})
	require.NoError(t, err)
	reader, writer := io.Pipe()
	var output bytes.Buffer
	started := make(chan struct{})
	var once sync.Once
	done := make(chan error, 1)
	go func() {
		done <- serveWithConcurrency(1, context.Background(), reader, &output, func(ctx context.Context, _ string, _ map[string]any) (CallOutcome, error) {
			once.Do(func() { close(started) })
			<-ctx.Done()
			return CallOutcome{OK: true, Payload: map[string]any{"committed": true}}, nil
		})
	}()
	_, err = writer.Write([]byte(frames(t,
		request(1, "initialize", map[string]any{"protocolVersion": ProtocolVersion, "selected": description.Selected, "contractHash": description.ContractHash}),
		request(2, "call", map[string]any{"operation": "files", "input": map[string]any{"inputs": []string{"a.go"}}, "timeoutMs": 1000}),
	)))
	require.NoError(t, err)
	<-started
	_, err = writer.Write([]byte(frames(t, map[string]any{"jsonrpc": "2.0", "method": "$/cancelRequest", "params": map[string]any{"id": 2}})))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.NoError(t, <-done)
	replies := parseReplies(t, output.String())
	data := replies[1].Error.Data.(map[string]any)
	require.Equal(t, "cancelled", data["code"])
	require.Equal(t, true, data["mayHaveExecuted"])
	require.Equal(t, true, data["outcome"].(map[string]any)["payload"].(map[string]any)["committed"])
}

func TestServeRejectsOverlongUnterminatedFrame(t *testing.T) {
	var output bytes.Buffer
	err := serveWithConcurrency(1, context.Background(), strings.NewReader(strings.Repeat("x", maxFrameBytes+1)), &output, func(context.Context, string, map[string]any) (CallOutcome, error) {
		return CallOutcome{}, nil
	})
	require.ErrorContains(t, err, "frame exceeds")
}

func TestQueuedCallDeadlineStartsWhenReceived(t *testing.T) {
	state := &serverState{selected: map[string]struct{}{"files": {}}, running: map[string]context.CancelFunc{}, cancelled: map[string]struct{}{}}
	id := json.RawMessage("2")
	require.True(t, state.enqueue(id))
	params, err := json.Marshal(map[string]any{"operation": "files", "input": map[string]any{"inputs": []string{"a.go"}}, "timeoutMs": 1})
	require.NoError(t, err)
	_, err = state.run(context.Background(), id, params, time.Now().Add(-time.Second), func(context.Context, string, map[string]any) (CallOutcome, error) {
		t.Fatal("expired queued request must not invoke its handler")
		return CallOutcome{}, nil
	})
	require.Equal(t, "deadline_exceeded", errorCode(err))
	state.mu.Lock()
	_, retained := state.running["n:2"]
	state.mu.Unlock()
	require.False(t, retained)
}

func TestRunningDeadlinePreservesExecutionUncertainty(t *testing.T) {
	for _, handlerError := range []bool{false, true} {
		t.Run(fmt.Sprint(handlerError), func(t *testing.T) {
			state := &serverState{selected: map[string]struct{}{"files": {}}, running: map[string]context.CancelFunc{}, cancelled: map[string]struct{}{}}
			id := json.RawMessage("2")
			require.True(t, state.enqueue(id))
			params, err := json.Marshal(map[string]any{"operation": "files", "input": map[string]any{"inputs": []string{"a.go"}}, "timeoutMs": 20})
			require.NoError(t, err)
			_, err = state.run(context.Background(), id, params, time.Now(), func(ctx context.Context, _ string, _ map[string]any) (CallOutcome, error) {
				<-ctx.Done()
				if handlerError {
					return CallOutcome{}, ctx.Err()
				}
				return CallOutcome{OK: true}, nil
			})
			var reply rpcReply
			require.NoError(t, writeFault(func(value rpcReply) error { reply = value; return nil }, id, err))
			data := reply.Error.Data.(map[string]any)
			require.Equal(t, "deadline_exceeded", data["code"])
			require.Equal(t, true, data["mayHaveExecuted"])
			_, hasOutcome := data["outcome"]
			require.Equal(t, !handlerError, hasOutcome)
		})
	}
}

func TestOversizedOutcomeRetainsExecutionUncertainty(t *testing.T) {
	description, err := Describe([]string{"files"})
	require.NoError(t, err)
	input := frames(t,
		request(1, "initialize", map[string]any{"protocolVersion": ProtocolVersion, "selected": description.Selected, "contractHash": description.ContractHash}),
		request(2, "call", map[string]any{"operation": "files", "input": map[string]any{"inputs": []string{"a.go"}}, "timeoutMs": 1000}),
	)
	var output bytes.Buffer
	require.NoError(t, serveUntilShutdown(t, input, &output, func(context.Context, string, map[string]any) (CallOutcome, error) {
		return CallOutcome{OK: false, ExitCode: 2, Payload: strings.Repeat("x", maxFrameBytes)}, nil
	}))
	replies := parseReplies(t, output.String())
	data := replies[1].Error.Data.(map[string]any)
	require.Equal(t, "output_limit_exceeded", data["code"])
	require.Equal(t, true, data["mayHaveExecuted"])
	require.Equal(t, true, data["truncated"])
	outcome := data["outcome"].(map[string]any)
	require.Equal(t, false, outcome["ok"])
	require.Equal(t, float64(2), outcome["exitCode"])
}

// Keep the transport open until the server acknowledges the complete sequence.
func serveUntilShutdown(t *testing.T, input string, output io.Writer, call func(context.Context, string, map[string]any) (CallOutcome, error)) error {
	t.Helper()
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	input += frames(t, request(999, "shutdown", map[string]any{}))
	go func() { _, _ = io.WriteString(writer, input) }()
	return serveWithConcurrency(1, context.Background(), reader, output, call)
}
