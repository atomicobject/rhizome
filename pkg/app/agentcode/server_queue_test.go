package agentcode

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestQueuedCancellationReleasesCapacityWhileHandlerRuns(t *testing.T) {
	description, err := Describe([]string{"files"})
	require.NoError(t, err)
	reader, writer := io.Pipe()
	cancelledAll := make(chan struct{})
	overflowRejected := make(chan struct{})
	completedLast := make(chan struct{})
	cancelledReplies := 0
	output := observingWriter{observe: func(data []byte) {
		var envelope struct {
			ID int `json:"id"`
		}
		if json.Unmarshal(data, &envelope) == nil && envelope.ID == 100 {
			close(completedLast)
		}
		var reply parsedReply
		if json.Unmarshal(data, &reply) == nil && reply.Error != nil && reply.Error.Data.(map[string]any)["code"] == "queue_full" {
			close(overflowRejected)
		}
		if reply.Error != nil && reply.Error.Data.(map[string]any)["code"] == "cancelled" {
			cancelledReplies++
			if cancelledReplies == maxQueue {
				close(cancelledAll)
			}
		}
	}}
	started, release := make(chan struct{}), make(chan struct{})
	cancelledEarly := make(chan struct{})
	var calls atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- serveWithConcurrency(1, context.Background(), reader, &output, func(ctx context.Context, _ string, _ map[string]any) (CallOutcome, error) {
			if calls.Add(1) == 1 {
				close(started)
				select {
				case <-release:
				case <-ctx.Done():
					close(cancelledEarly)
				}
			}
			return CallOutcome{OK: true}, nil
		})
	}()
	_, err = writer.Write([]byte(frames(t,
		request(1, "initialize", map[string]any{"protocolVersion": ProtocolVersion, "selected": description.Selected, "contractHash": description.ContractHash}),
		request(2, "call", map[string]any{"operation": "files", "input": map[string]any{"inputs": []string{"first"}}, "timeoutMs": 10000}),
	)))
	require.NoError(t, err)
	<-started
	var queued []map[string]any
	for id := 3; id < 3+maxQueue; id++ {
		queued = append(queued, request(id, "call", map[string]any{"operation": "files", "input": map[string]any{"inputs": []string{"cancelled"}}, "timeoutMs": 10000}))
	}
	_, err = writer.Write([]byte(frames(t, queued...)))
	require.NoError(t, err)
	_, err = writer.Write([]byte(frames(t, request(1000, "call", map[string]any{"operation": "files", "input": map[string]any{"inputs": []string{"overflow"}}, "timeoutMs": 10000}))))
	require.NoError(t, err)
	<-overflowRejected
	select {
	case <-cancelledEarly:
		t.Fatal("overflow cancelled the running handler")
	default:
	}
	var cancelled []map[string]any
	for id := 3; id < 3+maxQueue; id++ {
		cancelled = append(cancelled, map[string]any{"jsonrpc": "2.0", "method": "$/cancelRequest", "params": map[string]any{"id": id}})
	}
	cancelled = append(cancelled, request(100, "call", map[string]any{"operation": "files", "input": map[string]any{"inputs": []string{"last"}}, "timeoutMs": 10000}))
	_, err = writer.Write([]byte(frames(t, cancelled...)))
	require.NoError(t, err)
	<-cancelledAll
	close(release)
	<-completedLast
	require.NoError(t, writer.Close())
	require.NoError(t, <-done)
	require.Equal(t, int32(2), calls.Load())
	var cancelledCount int
	for _, reply := range parseReplies(t, output.String()) {
		if reply.Error != nil {
			code := reply.Error.Data.(map[string]any)["code"]
			if code == "queue_full" {
				continue
			}
			require.Equal(t, "cancelled", code)
			cancelledCount++
		}
	}
	require.GreaterOrEqual(t, cancelledCount, maxQueue)
}

func TestFatalInputDoesNotExecuteQueuedHandlers(t *testing.T) {
	description, err := Describe([]string{"files"})
	require.NoError(t, err)
	reader, writer := io.Pipe()
	var output bytes.Buffer
	started := make(chan struct{})
	var calls atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- serveWithConcurrency(1, context.Background(), reader, &output, func(ctx context.Context, _ string, _ map[string]any) (CallOutcome, error) {
			if calls.Add(1) == 1 {
				close(started)
				<-ctx.Done()
			}
			return CallOutcome{OK: true}, nil
		})
	}()
	_, err = writer.Write([]byte(frames(t,
		request(1, "initialize", map[string]any{"protocolVersion": ProtocolVersion, "selected": description.Selected, "contractHash": description.ContractHash}),
		request(2, "call", map[string]any{"operation": "files", "input": map[string]any{"inputs": []string{"first"}}, "timeoutMs": 10000}),
	)))
	require.NoError(t, err)
	<-started
	_, err = writer.Write([]byte(frames(t, request(3, "call", map[string]any{"operation": "files", "input": map[string]any{"inputs": []string{"must-not-run"}}, "timeoutMs": 10000})) + "not-json\n"))
	require.NoError(t, err)
	require.ErrorContains(t, <-done, "invalid agent code frame")
	require.Equal(t, int32(1), calls.Load())
	require.NoError(t, writer.Close())
}

type observingWriter struct {
	bytes.Buffer
	observe func([]byte)
}

func (w *observingWriter) Write(data []byte) (int, error) {
	n, err := w.Buffer.Write(data)
	w.observe(data)
	return n, err
}

func TestDisconnectDoesNotExecuteQueuedHandlers(t *testing.T) {
	description, err := Describe([]string{"files"})
	require.NoError(t, err)
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	var output bytes.Buffer
	started := make(chan struct{})
	var calls atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- serveWithConcurrency(1, context.Background(), reader, &output, func(ctx context.Context, _ string, _ map[string]any) (CallOutcome, error) {
			if calls.Add(1) == 1 {
				close(started)
				<-ctx.Done()
			}
			return CallOutcome{OK: true}, nil
		})
	}()
	_, err = io.WriteString(writer, frames(t,
		request(1, "initialize", map[string]any{"protocolVersion": ProtocolVersion, "selected": description.Selected, "contractHash": description.ContractHash}),
		request(2, "call", map[string]any{"operation": "files", "input": map[string]any{"inputs": []string{"first"}}, "timeoutMs": 10000}),
	))
	require.NoError(t, err)
	<-started
	_, err = io.WriteString(writer, frames(t, request(3, "call", map[string]any{"operation": "files", "input": map[string]any{"inputs": []string{"must-not-run"}}, "timeoutMs": 10000})))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.NoError(t, <-done)
	require.Equal(t, int32(1), calls.Load())
}

func TestAbortCancelsDequeuedCallBeforeHandlerStarts(t *testing.T) {
	state := &serverState{selected: map[string]struct{}{"files": {}}, running: map[string]context.CancelFunc{}, cancelled: map[string]struct{}{}}
	id := json.RawMessage("2")
	require.True(t, state.enqueue(id))
	state.cancelAll()
	params, err := json.Marshal(map[string]any{"operation": "files", "input": map[string]any{"inputs": []string{"must-not-run"}}, "timeoutMs": 1000})
	require.NoError(t, err)
	_, err = state.run(context.Background(), id, params, time.Now(), func(context.Context, string, map[string]any) (CallOutcome, error) {
		t.Fatal("dequeued request must not start after transport abort")
		return CallOutcome{}, nil
	})
	require.Equal(t, "cancelled", errorCode(err))
}
