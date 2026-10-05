package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/internal/command"
	"github.com/atomicobject/rhizome/pkg/harness/internal/eventstream"
)

const (
	turnCancelTimeout            = 5 * time.Second
	defaultShutdownTimeout       = 3 * time.Second
	shutdownApprovalWriteTimeout = 50 * time.Millisecond
)

type commandRunner interface {
	LookPath(string) (string, error)
	Run(context.Context, command.Spec) (command.Result, error)
}

type Driver struct {
	binary           string
	start            transportStarter
	runner           commandRunner
	interruptTimeout time.Duration
	cancelTimeout    time.Duration
	shutdownTimeout  time.Duration
	versionMu        sync.Mutex
	versionAt        time.Time
	versionValue     string
	versionErr       error
}

func New() *Driver {
	return &Driver{binary: "claude", start: startStdio, runner: command.OSRunner{Label: "claude command"}, interruptTimeout: turnCancelTimeout, cancelTimeout: turnCancelTimeout, shutdownTimeout: defaultShutdownTimeout}
}

func (d *Driver) StartSession(ctx context.Context, options harness.SessionOptions) (harness.Session, error) {
	if !supportedPermissionMode(options.PermissionMode) {
		return nil, harness.Phase(harness.ErrSpawn, fmt.Errorf("unsupported Claude permission mode %q", options.PermissionMode))
	}
	path, err := d.runner.LookPath(d.binary)
	if err != nil {
		return nil, harness.Phase(harness.ErrNotInstalled, err)
	}
	cwd := strings.TrimSpace(options.Cwd)
	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return nil, harness.Phase(harness.ErrSpawn, err)
		}
	}
	args, err := sessionArgs(options, cwd)
	if err != nil {
		return nil, harness.Phase(harness.ErrSpawn, err)
	}
	connection, err := d.start(ctx, path, args, cwd)
	if err != nil {
		if isNotFound(err) {
			return nil, harness.Phase(harness.ErrNotInstalled, err)
		}
		return nil, harness.Phase(harness.ErrSpawn, err)
	}
	lifetime, cancel := context.WithCancel(context.Background())
	s := &session{
		transport: connection, stream: eventstream.New(), lifetime: lifetime, cancel: cancel,
		interruptTimeout: d.interruptTimeout, cancelTimeout: d.cancelTimeout, shutdownTimeout: d.shutdownTimeout, responses: make(map[string]chan message),
		responseBacklog: make(map[string]message),
		pending:         make(map[string]pendingApproval), items: make(map[string]harness.EventKind),
		interactive: true,
	}
	if s.interruptTimeout <= 0 {
		s.interruptTimeout = turnCancelTimeout
	}
	if s.cancelTimeout <= 0 {
		s.cancelTimeout = turnCancelTimeout
	}
	if s.shutdownTimeout <= 0 {
		s.shutdownTimeout = defaultShutdownTimeout
	}
	s.reader.Add(1)
	go s.readLoop()
	if _, err := s.initialize(ctx, "init-1"); err != nil {
		_ = s.Stop()
		return nil, err
	}
	return s, nil
}

type pendingApproval struct{ request controlRequest }

type session struct {
	transport        transport
	stream           *eventstream.Stream
	lifetime         context.Context
	cancel           context.CancelFunc
	interruptTimeout time.Duration
	cancelTimeout    time.Duration
	shutdownTimeout  time.Duration
	interactive      bool

	stateMu         sync.Mutex
	dispatchMu      sync.Mutex // serializes backlog replay in SendTurn with readLoop dispatch
	stopped         bool
	terminalErr     error
	running         bool
	started         bool
	sessionID       string
	turnID          string
	turnDone        chan error
	responses       map[string]chan message
	responseBacklog map[string]message
	pending         map[string]pendingApproval
	items           map[string]harness.EventKind
	turnSeq         atomic.Int64
	reader          sync.WaitGroup
	stopOnce        sync.Once
	closeErr        error
}

func (s *session) ID() string {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.sessionID
}

func (s *session) initialize(ctx context.Context, id string) (initializeResponse, error) {
	waiter := make(chan message, 1)
	s.stateMu.Lock()
	s.responses[id] = waiter
	if buffered, ok := s.responseBacklog[id]; ok {
		delete(s.responseBacklog, id)
		waiter <- buffered
	}
	s.stateMu.Unlock()
	defer func() {
		s.stateMu.Lock()
		delete(s.responses, id)
		s.stateMu.Unlock()
	}()
	if err := s.transport.Send(ctx, initializeMessage(id)); err != nil {
		if errors.Is(err, io.ErrClosedPipe) || errors.Is(err, io.EOF) {
			// The read loop may already have failed the session (bad version,
			// decode error) and closed the transport; report that cause.
			return initializeResponse{}, s.transportError()
		}
		return initializeResponse{}, classifyError(ctx, harness.ErrInitialize, err)
	}
	select {
	case msg := <-waiter:
		var response initializeResponse
		if err := json.Unmarshal(msg.Response, &response); err != nil {
			return response, harness.Phase(harness.ErrDecode, err)
		}
		return response, nil
	case <-ctx.Done():
		return initializeResponse{}, classifyError(ctx, harness.ErrInitialize, ctx.Err())
	case <-s.lifetime.Done():
		return initializeResponse{}, s.transportError()
	}
}

func (s *session) SendTurn(ctx context.Context, prompt string) error {
	if err := ctx.Err(); err != nil {
		return harness.Phase(harness.ErrTimeout, err)
	}
	s.dispatchMu.Lock()
	s.stateMu.Lock()
	if s.stopped {
		s.stateMu.Unlock()
		s.dispatchMu.Unlock()
		return harness.Phase(harness.ErrTransportClosed, errors.New("session stopped"))
	}
	if s.running {
		s.stateMu.Unlock()
		s.dispatchMu.Unlock()
		return harness.Phase(harness.ErrTurnStart, errors.New("turn already running"))
	}
	s.running, s.started = true, false
	s.turnID = fmt.Sprintf("turn-%d", s.turnSeq.Add(1))
	done := make(chan error, 2)
	s.turnDone = done
	s.items = make(map[string]harness.EventKind)
	s.stateMu.Unlock()
	defer s.clearTurn(done)
	s.dispatchMu.Unlock()
	turnCtx, cancelTurnCtx := context.WithCancel(ctx)
	stopLifetimeCancel := context.AfterFunc(s.lifetime, cancelTurnCtx)
	defer func() {
		stopLifetimeCancel()
		cancelTurnCtx()
	}()
	if err := s.transport.Send(turnCtx, userMessage(prompt)); err != nil {
		if ctx.Err() != nil {
			return s.cancelTurn(ctx.Err(), done)
		}
		if s.lifetime.Err() != nil {
			return s.transportError()
		}
		if errors.Is(err, io.ErrClosedPipe) || errors.Is(err, io.EOF) {
			return s.transportError()
		}
		return classifyError(turnCtx, harness.ErrTurnStart, err)
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return s.cancelTurn(ctx.Err(), done)
	case <-s.lifetime.Done():
		return s.transportError()
	}
}

func (s *session) clearTurn(done chan error) {
	s.stateMu.Lock()
	if s.turnDone == done {
		s.running, s.started, s.turnID, s.turnDone = false, false, "", nil
	}
	s.stateMu.Unlock()
}

func (s *session) cancelTurn(ctxErr error, done <-chan error) error {
	interruptCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	_ = s.sendInterrupt(interruptCtx)
	cancel()
	timer := time.NewTimer(s.cancelTimeout)
	defer timer.Stop()
	select {
	case <-done:
		return harness.Phase(harness.ErrTimeout, ctxErr)
	case <-s.lifetime.Done():
		return harness.Phase(harness.ErrTransportClosed, ctxErr)
	case <-timer.C:
		_ = s.Stop()
		return harness.Phase(harness.ErrTransportClosed, ctxErr)
	}
}

func (s *session) Interrupt(ctx context.Context) error {
	s.stateMu.Lock()
	done, running := s.turnDone, s.running
	s.stateMu.Unlock()
	if !running {
		return errors.New("no running turn")
	}
	if err := s.sendInterrupt(ctx); err != nil {
		return err
	}
	timer := time.NewTimer(s.interruptTimeout)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return harness.Phase(harness.ErrTimeout, ctx.Err())
	case <-s.lifetime.Done():
		return s.transportError()
	case <-timer.C:
		_ = s.Stop()
		return harness.Phase(harness.ErrTimeout, errors.New("Claude did not end the turn after interrupt"))
	}
}

func (s *session) sendInterrupt(ctx context.Context) error {
	s.stateMu.Lock()
	running := s.running
	seq := s.turnSeq.Load()
	s.stateMu.Unlock()
	if !running {
		return errors.New("no running turn")
	}
	request, _ := json.Marshal(struct {
		Subtype string `json:"subtype"`
	}{Subtype: "interrupt"})
	if err := s.transport.Send(ctx, message{Type: "control_request", RequestID: fmt.Sprintf("interrupt-%d", seq), Request: request}); err != nil {
		return classifyError(ctx, harness.ErrTransportClosed, err)
	}
	return nil
}

func (s *session) Respond(requestID string, decision harness.Decision) error {
	if !validDecision(decision) {
		return fmt.Errorf("invalid approval decision %q", decision)
	}
	s.stateMu.Lock()
	pending, ok := s.pending[requestID]
	if ok && decision == harness.DecisionAllowForSession && !supportsAllowForSession(pending.request) {
		s.stateMu.Unlock()
		return fmt.Errorf("approval request %q does not support allow-for-session", requestID)
	}
	if ok {
		delete(s.pending, requestID)
	}
	s.stateMu.Unlock()
	if !ok {
		return fmt.Errorf("approval request %q not found", requestID)
	}
	return s.sendApproval(requestID, pending.request, decision)
}

func supportsAllowForSession(request controlRequest) bool {
	for _, suggestion := range request.PermissionSuggestions {
		if suggestion.Destination == "session" {
			return true
		}
	}
	return false
}

func (s *session) Events() <-chan harness.Event { return s.stream.Events() }

func (s *session) readLoop() {
	defer s.reader.Done()
	for {
		msg, err := s.transport.Recv(s.lifetime)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				s.fail(err)
			}
			return
		}
		s.dispatchMu.Lock()
		s.dispatch(msg)
		s.dispatchMu.Unlock()
	}
}

func (s *session) dispatch(msg message) {
	if msg.SessionID != "" {
		s.stateMu.Lock()
		s.sessionID = msg.SessionID
		s.stateMu.Unlock()
		s.transport.BindSessionID(msg.SessionID)
	}
	if msg.Type == "system" && msg.Subtype == "init" && msg.Version != "" {
		if err := checkMinimumVersion(msg.Version); err != nil {
			s.fail(err)
			return
		}
	}
	if msg.Type == "control_response" {
		var response struct {
			RequestID string `json:"request_id"`
		}
		if json.Unmarshal(msg.Response, &response) == nil {
			s.stateMu.Lock()
			waiter := s.responses[response.RequestID]
			if waiter == nil && (strings.HasPrefix(response.RequestID, "init-") || response.RequestID == "status-init") {
				s.responseBacklog[response.RequestID] = msg
			}
			s.stateMu.Unlock()
			if waiter != nil {
				waiter <- msg
				return
			}
			return
		}
	}
	if msg.Type == "control_request" {
		s.handleControlRequest(msg)
		return
	}
	if msg.Type == "control_cancel_request" {
		s.cancelApproval(msg.RequestID)
		return
	}
	s.stateMu.Lock()
	if !s.running && claudeTurnScoped(msg.Type) {
		s.stateMu.Unlock()
		s.emitDiagnostic(msg.RequestID, "discarded stale Claude message: "+msg.Type)
		return
	}
	s.stateMu.Unlock()

	s.stateMu.Lock()
	turnID, running := s.turnID, s.running
	if running && !s.started && (msg.Type == "stream_event" || msg.Type == "assistant") {
		s.started = true
		started := harness.Event{Kind: harness.EventTurnStarted, Phase: harness.PhaseStarted, SessionID: s.sessionID, TurnID: turnID, Status: "in_progress"}
		s.stateMu.Unlock()
		s.emit(started)
		s.stateMu.Lock()
	}
	events, complete := mapMessage(msg, turnID, s.items)
	done := s.turnDone
	s.stateMu.Unlock()
	for _, event := range events {
		s.emit(event)
	}
	if complete && done != nil {
		signalTurn(done, nil)
	}
}

func (s *session) handleControlRequest(msg message) {
	var request controlRequest
	if json.Unmarshal(msg.Request, &request) != nil {
		s.sendControlError(msg.RequestID, "invalid control request")
		s.emitDiagnostic(msg.RequestID, "could not decode Claude control request")
		return
	}
	if request.Subtype != "can_use_tool" {
		s.sendControlError(msg.RequestID, "unsupported control request: "+request.Subtype)
		s.emitDiagnostic(msg.RequestID, "unknown Claude control request: "+request.Subtype)
		return
	}
	if !s.interactive {
		_ = s.sendApproval(msg.RequestID, request, harness.DecisionDeny)
		return
	}
	s.stateMu.Lock()
	if s.stopped {
		s.stateMu.Unlock()
		_ = s.sendApproval(msg.RequestID, request, harness.DecisionDeny)
		return
	}
	s.pending[msg.RequestID] = pendingApproval{request: request}
	turnID := s.turnID
	s.stateMu.Unlock()
	s.emit(approvalEvent(msg, request, turnID))
}

func (s *session) cancelApproval(requestID string) {
	s.stateMu.Lock()
	_, ok := s.pending[requestID]
	delete(s.pending, requestID)
	s.stateMu.Unlock()
	if ok {
		// Typed terminal state so consumers drop the pending request and the UI
		// disables the approval controls; a diagnostic would be invisible.
		s.emit(harness.Event{Kind: harness.EventApprovalRequested, Phase: harness.PhaseCancelled, SessionID: s.ID(), RequestID: requestID, Reason: "Claude cancelled the approval request"})
	}
}

func (s *session) sendApproval(id string, request controlRequest, decision harness.Decision) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.transport.Send(ctx, approvalResponse(id, request, decision))
}

func (s *session) sendApprovalDuringStop(parent context.Context, id string, request controlRequest) error {
	ctx, cancel := context.WithTimeout(parent, shutdownApprovalWriteTimeout)
	defer cancel()
	return s.transport.Send(ctx, approvalResponse(id, request, harness.DecisionDeny))
}

func (s *session) sendControlError(id, text string) {
	response, _ := json.Marshal(map[string]any{"subtype": "error", "request_id": id, "error": text})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.transport.Send(ctx, message{Type: "control_response", Response: response}); err != nil {
		s.emitDiagnostic(id, fmt.Sprintf("could not reply to Claude control request: %v", err))
	}
}

func (s *session) emitDiagnostic(requestID, text string) {
	s.emit(harness.Event{Kind: harness.EventDiagnostic, Phase: harness.PhaseFailed, SessionID: s.ID(), RequestID: requestID, Text: text})
}

func (s *session) emit(event harness.Event) {
	if event.SessionID == "" {
		event.SessionID = s.ID()
	}
	s.stream.Emit(event)
}

func validDecision(decision harness.Decision) bool {
	return decision == harness.DecisionAllow || decision == harness.DecisionDeny || decision == harness.DecisionAllowForSession
}

func supportedPermissionMode(mode harness.PermissionMode) bool {
	return mode == "" || mode == harness.PermissionApprovalRequired || mode == harness.PermissionAutoAcceptEdits || mode == harness.PermissionFullAccess
}

func claudeTurnScoped(messageType string) bool {
	return messageType == "result" || messageType == "stream_event" || messageType == "assistant" || messageType == "user"
}

func signalTurn(done chan error, err error) {
	for range 2 {
		select {
		case done <- err:
		default:
			return
		}
	}
}

func classifyError(ctx context.Context, phase, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) {
		return harness.Phase(harness.ErrTimeout, ctx.Err())
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
		return harness.Phase(harness.ErrTransportClosed, err)
	}
	return harness.Phase(phase, err)
}
