package agentcode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// executeTransport multiplexes the Node child's protocol requests and final
// result. Configuration never crosses the process boundary: OperationHost runs
// inside the caller and writes protocol responses to the child's stdin.
type executeTransport struct {
	ctx        context.Context
	cancel     context.CancelFunc
	nodeWriter io.WriteCloser
	hostInput  *io.PipeReader
	hostWriter *io.PipeWriter
	workers    sync.WaitGroup
	closeOnce  sync.Once
	failed     chan struct{}
	mu         sync.Mutex
	err        error
	frame      []byte
	result     []byte
	// readyAt and resultAt are written by the stdout copier and read after
	// the process wait, which orders them.
	readyAt, resultAt time.Time
}

func newExecuteTransport(ctx context.Context, nodeWriter io.WriteCloser) *executeTransport {
	ctx, cancel := context.WithCancel(ctx)
	hostInput, hostWriter := io.Pipe()
	return &executeTransport{ctx: ctx, cancel: cancel,
		nodeWriter: nodeWriter, hostInput: hostInput, hostWriter: hostWriter, failed: make(chan struct{})}
}

func (transport *executeTransport) start(options ExecuteOptions, input []byte) {
	transport.workers.Add(2)
	go func() {
		defer transport.workers.Done()
		if _, err := transport.nodeWriter.Write(append(input, '\n')); err != nil {
			transport.fail(fmt.Errorf("send execution input: %w", err))
		}
	}()
	go func() {
		defer transport.workers.Done()
		defer transport.hostInput.Close()
		defer transport.nodeWriter.Close()
		// A spawning host must copy through Write instead of inheriting the
		// Node stdin descriptor while Execute may close it during shutdown.
		output := struct{ io.Writer }{transport.nodeWriter}
		if err := options.OperationHost(transport.ctx, transport.hostInput, output, options.ReadWrite); err != nil {
			transport.fail(fmt.Errorf("operation host: %w", err))
		}
	}()
}

func (transport *executeTransport) close() {
	transport.closeOnce.Do(func() {
		transport.cancel()
		_ = transport.hostWriter.Close()
		_ = transport.hostInput.Close()
		_ = transport.nodeWriter.Close()
		transport.workers.Wait()
	})
}

func (transport *executeTransport) fail(err error) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if transport.err == nil && transport.ctx.Err() == nil {
		transport.err = err
		close(transport.failed)
	}
}

func (transport *executeTransport) failure() error {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return transport.err
}

func (transport *executeTransport) Write(data []byte) (int, error) {
	length := len(data)
	for len(data) > 0 {
		end := bytes.IndexByte(data, '\n')
		part := data
		if end >= 0 {
			part = data[:end+1]
		}
		if len(transport.frame)+len(part) > maxExecuteOutputBytes {
			err := errors.New("execution frame exceeded its output limit")
			transport.fail(err)
			return 0, err
		}
		transport.frame = append(transport.frame, part...)
		data = data[len(part):]
		if end < 0 {
			break
		}
		if err := transport.routeFrame(transport.frame); err != nil {
			transport.fail(err)
			return 0, err
		}
		transport.frame = transport.frame[:0]
	}
	return length, nil
}

func (transport *executeTransport) routeFrame(line []byte) error {
	var frame struct {
		JSONRPC string          `json:"jsonrpc"`
		Method  string          `json:"method"`
		Result  json.RawMessage `json:"executionResult"`
		Ready   bool            `json:"executionReady"`
	}
	if err := json.Unmarshal(line, &frame); err != nil {
		return errors.New("Node.js emitted malformed execution framing")
	}
	if transport.result != nil {
		return errors.New("Node.js emitted a frame after its final result")
	}
	if frame.JSONRPC == "2.0" && frame.Method != "" && frame.Result == nil {
		if len(line) > maxFrameBytes {
			return errors.New("operation request exceeded its frame limit")
		}
		_, err := transport.hostWriter.Write(line)
		return err
	}
	if frame.JSONRPC == "" && frame.Result != nil {
		transport.result = append([]byte(nil), frame.Result...)
		transport.resultAt = time.Now()
		return nil
	}
	if frame.JSONRPC == "" && frame.Ready && transport.readyAt.IsZero() {
		transport.readyAt = time.Now()
		return nil
	}
	return errors.New("Node.js emitted an invalid execution frame")
}

func (transport *executeTransport) timing(started time.Time) ExecuteTiming {
	if transport.readyAt.IsZero() {
		return ExecuteTiming{}
	}
	end := transport.resultAt
	if end.IsZero() {
		end = time.Now()
	}
	return ExecuteTiming{Startup: transport.readyAt.Sub(started), Script: end.Sub(transport.readyAt)}
}
