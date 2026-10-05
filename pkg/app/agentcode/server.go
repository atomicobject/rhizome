package agentcode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
)

const (
	ProtocolVersion = "1"
	maxFrameBytes   = 1 << 20
	maxQueue        = 32
	maxTimeout      = 5 * time.Minute
)

type CallOutcome = agentapi.CallOutcome

type rpcFrame struct {
	JSONRPC    string          `json:"jsonrpc"`
	ID         json.RawMessage `json:"id,omitempty"`
	Method     string          `json:"method"`
	Params     json.RawMessage `json:"params,omitempty"`
	receivedAt time.Time
}

type rpcReply struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type protocolError struct {
	code    string
	message string
}

func (e protocolError) Error() string { return e.message }

type executedError struct {
	error
	outcome *CallOutcome
}

func (e executedError) Unwrap() error { return e.error }

func invalid(message string) protocolError { return protocolError{"invalid_request", message} }

type serverState struct {
	mu        sync.Mutex
	selected  map[string]struct{}
	contract  string
	sessionID string
	running   map[string]context.CancelFunc
	cancelled map[string]struct{}
}

// Serve provides the bounded JSON-RPC stdio host for one client connection.
// call must return operation failures as CallOutcome; only a broken transport
// or invalid protocol belongs in a JSON-RPC error.
func Serve(ctx context.Context, input io.Reader, output io.Writer, call func(context.Context, string, map[string]any) (CallOutcome, error)) error {
	return serveWithConcurrency(32, ctx, input, output, call)
}

func serveWithConcurrency(parallel int, ctx context.Context, input io.Reader, output io.Writer, call func(context.Context, string, map[string]any) (CallOutcome, error)) error {
	if call == nil {
		return errors.New("agent code server requires a call handler")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if closer, ok := input.(io.Closer); ok {
		defer closer.Close()
	}
	state := &serverState{running: map[string]context.CancelFunc{}, cancelled: map[string]struct{}{}}
	var writeMu sync.Mutex
	write := func(reply rpcReply) error {
		body, err := json.Marshal(reply)
		if err != nil {
			return err
		}
		if len(body) >= maxFrameBytes {
			fault := rpcFault("output_limit_exceeded", "result exceeds output limit")
			fault.Data = oversizedReplyData(reply)
			body, _ = json.Marshal(rpcReply{JSONRPC: "2.0", ID: reply.ID, Error: fault})
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		_, err = output.Write(append(body, '\n'))
		return err
	}
	var workers sync.WaitGroup
	slots := make(chan struct{}, parallel)
	workerErrors := make(chan error, 1)
	defer func() { cancel(); state.cancelAll(); workers.Wait() }()
	frames := newRequestQueue()
	go readFrames(input, frames, state, write)
	for {
		frame, ok, err := frames.next(ctx)
		if err != nil {
			state.cancelAll()
			return err
		}
		if !ok {
			workers.Wait()
			select {
			case err := <-workerErrors:
				return err
			default:
				return nil
			}
		}
		if frame.Method == "call" && parallel > 1 {
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return ctx.Err()
			}
			workers.Add(1)
			go func(frame rpcFrame) {
				defer workers.Done()
				defer func() { <-slots }()
				if err := handleFrame(ctx, frame, state, call, write); err != nil {
					select {
					case workerErrors <- err:
					default:
					}
					cancel()
					state.cancelAll()
				}
			}(frame)
			continue
		}
		workers.Wait()
		select {
		case err := <-workerErrors:
			return err
		default:
		}
		if err := handleFrame(ctx, frame, state, call, write); err != nil {
			return err
		}
		if frame.Method == "shutdown" {
			state.cancelAll()
			return nil
		}
	}
}

func readFrames(input io.Reader, frames *requestQueue, state *serverState, write func(rpcReply) error) {
	var readErr error
	defer func() { frames.close(readErr) }()
	abortRead := func(err error) { readErr = err; frames.close(err); state.cancelAll() }
	reader := bufio.NewReaderSize(input, maxFrameBytes+1)
	for {
		line, err := reader.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) || len(line) > maxFrameBytes {
			abortRead(fmt.Errorf("agent code frame exceeds %d bytes", maxFrameBytes))
			return
		}
		if len(line) > 0 {
			var frame rpcFrame
			if decodeErr := json.Unmarshal(line, &frame); decodeErr != nil {
				abortRead(fmt.Errorf("invalid agent code frame: %w", decodeErr))
				return
			}
			if frame.JSONRPC != "2.0" || frame.Method == "" {
				abortRead(invalid("JSON-RPC 2.0 method is required"))
				return
			}
			if frame.Method == "$/cancelRequest" {
				id := state.cancel(frame.Params)
				if err := removeQueuedCall(frames, id, state, write); err != nil {
					abortRead(err)
					return
				}
			} else {
				frame.receivedAt = time.Now()
				if frame.Method == "call" && !state.enqueue(frame.ID) {
					abortRead(invalid("call id must be unique and scalar"))
					return
				}
				if !frames.push(frame) {
					if id, ok := requestID(frame.ID); ok {
						state.finish(id)
					}
					if err := writeFault(write, frame.ID, protocolError{"queue_full", "too many queued requests"}); err != nil {
						abortRead(err)
						return
					}
				}
			}
		}
		if err != nil {
			abortRead(err)
			return
		}
	}
}

func removeQueuedCall(frames *requestQueue, id string, state *serverState, write func(rpcReply) error) error {
	if frame, ok := frames.remove(id); ok {
		state.finish(id)
		return writeFault(write, frame.ID, protocolError{"cancelled", "call was cancelled while queued"})
	}
	return nil
}

func handleFrame(ctx context.Context, frame rpcFrame, state *serverState, call func(context.Context, string, map[string]any) (CallOutcome, error), write func(rpcReply) error) error {
	if _, ok := requestID(frame.ID); !ok {
		return invalid("requests require a string or numeric id")
	}
	switch frame.Method {
	case "initialize":
		result, err := state.initialize(frame.Params)
		if err != nil {
			return writeFault(write, frame.ID, err)
		}
		return write(rpcReply{JSONRPC: "2.0", ID: frame.ID, Result: result})
	case "call":
		result, err := state.run(ctx, frame.ID, frame.Params, frame.receivedAt, call)
		if err != nil {
			return writeFault(write, frame.ID, err)
		}
		return write(rpcReply{JSONRPC: "2.0", ID: frame.ID, Result: result})
	case "shutdown":
		return write(rpcReply{JSONRPC: "2.0", ID: frame.ID, Result: map[string]bool{"ok": true}})
	default:
		return writeFault(write, frame.ID, protocolError{"method_not_found", "unsupported method " + frame.Method})
	}
}

func (s *serverState) initialize(raw json.RawMessage) (map[string]any, error) {
	var params struct {
		ProtocolVersion string   `json:"protocolVersion"`
		Selected        []string `json:"selected"`
		ContractHash    string   `json:"contractHash"`
		SessionID       string   `json:"sessionId"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, invalid("initialize params must be an object")
	}
	if params.ProtocolVersion != ProtocolVersion {
		return nil, protocolError{"protocol_mismatch", "unsupported protocol version"}
	}
	description, err := Describe(params.Selected)
	if err != nil {
		return nil, protocolError{"invalid_selection", err.Error()}
	}
	if params.ContractHash != description.ContractHash {
		return nil, protocolError{"code_mode_artifact_stale", "generated contract does not match this executable"}
	}
	selected := make(map[string]struct{}, len(description.Selected))
	for _, name := range description.Selected {
		selected[name] = struct{}{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.selected != nil {
		return nil, protocolError{"already_initialized", "connection is already initialized"}
	}
	s.selected, s.contract, s.sessionID = selected, description.ContractHash, params.SessionID
	return map[string]any{"protocolVersion": ProtocolVersion, "contractHash": s.contract, "pid": os.Getpid()}, nil
}

func (s *serverState) run(parent context.Context, rawID, raw json.RawMessage, receivedAt time.Time, call func(context.Context, string, map[string]any) (CallOutcome, error)) (CallOutcome, error) {
	id, ok := requestID(rawID)
	if !ok {
		return CallOutcome{}, invalid("call id must be a string or number")
	}
	defer s.finish(id)
	var params struct {
		Operation string         `json:"operation"`
		Input     map[string]any `json:"input"`
		TimeoutMS int            `json:"timeoutMs"`
		SessionID string         `json:"sessionId"`
	}
	if err := json.Unmarshal(raw, &params); err != nil || params.Operation == "" || params.Input == nil {
		return CallOutcome{}, invalid("call requires operation and object input")
	}
	if params.TimeoutMS < 1 || params.TimeoutMS > int(maxTimeout/time.Millisecond) {
		return CallOutcome{}, protocolError{"invalid_timeout", "timeoutMs is outside its allowed range"}
	}
	if receivedAt.IsZero() {
		receivedAt = time.Now()
	}
	deadline := receivedAt.Add(time.Duration(params.TimeoutMS) * time.Millisecond)
	if !deadline.After(time.Now()) {
		return CallOutcome{}, protocolError{"deadline_exceeded", "call deadline exceeded while queued"}
	}
	s.mu.Lock()
	if s.selected == nil {
		s.mu.Unlock()
		return CallOutcome{}, protocolError{"not_initialized", "initialize before calling operations"}
	}
	if _, ok := s.selected[params.Operation]; !ok {
		s.mu.Unlock()
		return CallOutcome{}, protocolError{"operation_not_selected", "operation was not selected for this client"}
	}
	if _, cancelled := s.cancelled[id]; cancelled {
		s.mu.Unlock()
		return CallOutcome{}, protocolError{"cancelled", "call was cancelled while queued"}
	}
	sessionID := s.sessionID
	if params.SessionID != "" {
		sessionID = params.SessionID
	}
	requestCtx, cancel := context.WithDeadline(parent, deadline)
	s.running[id] = cancel
	s.mu.Unlock()
	defer cancel()
	if sessionID != "" {
		params.Input["sessionId"] = sessionID
	}
	if err := agentapi.ValidateCodeInput(params.Operation, params.Input); err != nil {
		return CallOutcome{}, protocolError{"invalid_input", err.Error()}
	}
	if err := requestCtx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return CallOutcome{}, protocolError{"deadline_exceeded", "call deadline exceeded before handler start"}
		}
		return CallOutcome{}, protocolError{"cancelled", "call was cancelled before handler start"}
	}
	outcome, err := call(requestCtx, params.Operation, params.Input)
	var completion *CallOutcome
	if err == nil {
		completion = &outcome
	}
	if errors.Is(requestCtx.Err(), context.DeadlineExceeded) {
		return CallOutcome{}, executedError{protocolError{"deadline_exceeded", "call deadline exceeded"}, completion}
	}
	if errors.Is(requestCtx.Err(), context.Canceled) {
		return CallOutcome{}, executedError{protocolError{"cancelled", "call was cancelled"}, completion}
	}
	if err != nil {
		return CallOutcome{}, executedError{protocolError{"handler_failed", err.Error()}, nil}
	}
	return outcome, nil
}

func (s *serverState) finish(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.running, id)
	delete(s.cancelled, id)
}

func (s *serverState) cancel(raw json.RawMessage) string {
	var params struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(raw, &params) != nil || len(params.ID) == 0 {
		return ""
	}
	id, ok := requestID(params.ID)
	if !ok {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel := s.running[id]; cancel != nil {
		cancel()
		return id
	}
	if _, known := s.running[id]; known {
		s.cancelled[id] = struct{}{}
	}
	return id
}

func (s *serverState) enqueue(rawID json.RawMessage) bool {
	id, ok := requestID(rawID)
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.running[id]; exists {
		return false
	}
	s.running[id] = nil
	return true
}

func requestID(raw json.RawMessage) (string, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return "", false
	}
	switch value := value.(type) {
	case string:
		return "s:" + value, true
	case json.Number:
		return "n:" + value.String(), true
	default:
		return "", false
	}
}

func (s *serverState) cancelAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, cancel := range s.running {
		s.cancelled[id] = struct{}{}
		if cancel != nil {
			cancel()
		}
	}
}

func writeFault(write func(rpcReply) error, id json.RawMessage, err error) error {
	fault := rpcFault(errorCode(err), err.Error())
	var executed executedError
	if errors.As(err, &executed) {
		data := map[string]any{"code": errorCode(err), "mayHaveExecuted": true}
		if executed.outcome != nil {
			data["outcome"] = executed.outcome
		}
		fault.Data = data
	}
	return write(rpcReply{JSONRPC: "2.0", ID: id, Error: fault})
}

func rpcFault(code, message string) *rpcError {
	return &rpcError{Code: -32000, Message: message, Data: map[string]string{"code": code}}
}

func errorCode(err error) string {
	var protocol protocolError
	if errors.As(err, &protocol) {
		return protocol.code
	}
	return "internal_error"
}

// Preserve a completed status even when its payload cannot fit the transport.
func oversizedReplyData(reply rpcReply) map[string]any {
	data := map[string]any{"code": "output_limit_exceeded", "mayHaveExecuted": true, "truncated": true}
	outcome, known := reply.Result.(CallOutcome)
	if !known && reply.Error != nil {
		if original, ok := reply.Error.Data.(map[string]any); ok {
			if completed, ok := original["outcome"].(*CallOutcome); ok && completed != nil {
				outcome, known = *completed, true
			}
		}
	}
	if known {
		diagnostic := map[string]any{"code": "output_limit_exceeded", "truncated": true}
		if status := boundedMutationStatus(outcome.Diagnostic); status != nil {
			diagnostic["mutation"] = status
		}
		data["outcome"] = CallOutcome{OK: outcome.OK, ExitCode: outcome.ExitCode, Diagnostic: diagnostic}
	}
	return data
}
