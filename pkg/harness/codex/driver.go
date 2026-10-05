package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/internal/command"
	"github.com/atomicobject/rhizome/pkg/harness/internal/eventstream"
)

const (
	turnCancelTimeout      = 5 * time.Second
	defaultShutdownTimeout = 3 * time.Second
)

type transportStarter func(context.Context, string, []string, string) (transport, error)

type commandRunner interface {
	LookPath(string) (string, error)
	Run(context.Context, command.Spec) (command.Result, error)
}

type Driver struct {
	binary          string
	start           transportStarter
	runner          commandRunner
	cancelTimeout   time.Duration
	shutdownTimeout time.Duration
	versionMu       sync.Mutex
	versionAt       time.Time
	versionValue    string
	versionErr      error
}

func New() *Driver {
	return &Driver{binary: "codex", start: startStdio, runner: command.OSRunner{Label: "codex command"}, cancelTimeout: turnCancelTimeout, shutdownTimeout: defaultShutdownTimeout}
}

func (d *Driver) StartSession(ctx context.Context, options harness.SessionOptions) (harness.Session, error) {
	if !supportedPermissionMode(options.PermissionMode) {
		return nil, harness.Phase(harness.ErrSpawn, fmt.Errorf("unsupported Codex permission mode %q", options.PermissionMode))
	}
	if len(options.AllowedTools) > 0 || len(options.DisallowedTools) > 0 {
		return nil, harness.Phase(harness.ErrSpawn, errors.New("codex cannot enforce AllowedTools/DisallowedTools"))
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
	args, err := appServerArgs(options.MCPServers)
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
	s := newSession(connection, options, cwd, d.cancelTimeout, d.shutdownTimeout, true)
	version, err := s.client.initialize(ctx)
	if err != nil {
		_ = s.Stop()
		return nil, s.preferTerminal(err)
	}
	if err := checkMinimumVersion(version); err != nil {
		_ = s.Stop()
		return nil, err
	}
	params := s.threadParams()
	var threadID string
	if options.Resume != "" {
		params.ThreadID, params.ExcludeTurns = options.Resume, true
		threadID, err = s.client.resumeThread(ctx, params, nil)
		if err != nil && recoverableThreadError(err) {
			params.ThreadID, params.ExcludeTurns = "", false
			threadID, err = s.client.startThread(ctx, params, nil)
		}
	} else {
		threadID, err = s.client.startThread(ctx, params, nil)
	}
	if err != nil {
		_ = s.Stop()
		return nil, s.preferTerminal(err)
	}
	s.stateMu.Lock()
	s.threadID = threadID
	s.stateMu.Unlock()
	s.transport.BindSessionID(threadID)
	return s, nil
}

func newSession(connection transport, options harness.SessionOptions, cwd string, cancelTimeout, shutdownTimeout time.Duration, interactive bool) *session {
	lifetime, cancel := context.WithCancel(context.Background())
	s := &session{
		transport: connection, options: options, cwd: cwd, stream: eventstream.New(),
		lifetime: lifetime, cancel: cancel, responses: make(map[string]chan rpcMessage),
		responseBacklog: make(map[string]rpcMessage),
		pending:         make(map[string]pendingApproval), deltas: make(map[string]bool), cancelTimeout: cancelTimeout,
		shutdownTimeout: shutdownTimeout, interactive: interactive,
	}
	if s.cancelTimeout <= 0 {
		s.cancelTimeout = turnCancelTimeout
	}
	if s.shutdownTimeout <= 0 {
		s.shutdownTimeout = defaultShutdownTimeout
	}
	s.client = newSessionClient(connection, s.request)
	s.reader.Add(1)
	go s.readLoop()
	return s
}

type pendingApproval struct {
	id     json.RawMessage
	method string
	params approvalParams
}

type session struct {
	client          *client
	transport       transport
	options         harness.SessionOptions
	cwd             string
	stream          *eventstream.Stream
	lifetime        context.Context
	cancel          context.CancelFunc
	cancelTimeout   time.Duration
	shutdownTimeout time.Duration
	interactive     bool

	stateMu         sync.Mutex
	dispatchMu      sync.Mutex // serializes backlog replay in SendTurn with readLoop notification dispatch
	stopped         bool
	terminalErr     error
	threadID        string
	turnID          string
	lastTurnID      string // most recent completed turn; late notifications for it are stale
	running         bool
	turnDone        chan error
	responses       map[string]chan rpcMessage
	responseBacklog map[string]rpcMessage
	pending         map[string]pendingApproval
	deltas          map[string]bool
	reader          sync.WaitGroup
	stopOnce        sync.Once
	closeErr        error
}

func (s *session) ID() string {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.threadID
}

func (s *session) threadParams() threadParams {
	approval, sandbox := permissionConfig(s.options.PermissionMode)
	return threadParams{
		Model: s.options.Model, Cwd: s.cwd, ApprovalPolicy: approval, Sandbox: sandbox,
		DeveloperInstructions: strings.TrimSpace(s.options.Instructions),
	}
}

func (s *session) SendTurn(ctx context.Context, prompt string) error {
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
	s.running = true
	s.turnID = ""
	clear(s.deltas)
	done := make(chan error, 1)
	s.turnDone = done
	threadID := s.threadID
	s.stateMu.Unlock()
	defer s.clearTurn(done)
	s.dispatchMu.Unlock()
	turnCtx, cancelTurnCtx := context.WithCancel(ctx)
	stopLifetimeCancel := context.AfterFunc(s.lifetime, cancelTurnCtx)
	defer func() {
		stopLifetimeCancel()
		cancelTurnCtx()
	}()

	if len(s.options.MCPServers) > 0 {
		if err := s.client.reloadMCP(turnCtx, nil); err != nil {
			if ctx.Err() != nil {
				return s.cancelTurn(ctx.Err(), done)
			}
			if s.lifetime.Err() != nil {
				return s.transportError()
			}
			return err
		}
	}
	approval, sandbox := permissionConfig(s.options.PermissionMode)
	started, err := s.startTurn(turnCtx, turnStartParams{
		ThreadID: threadID, Input: []userInput{{Type: "text", Text: prompt}},
		Model: s.options.Model, Effort: s.options.Effort, Cwd: s.cwd,
		ApprovalPolicy: approval, SandboxPolicy: sandboxPolicy(sandbox),
	})
	if err != nil {
		return err
	}
	s.stateMu.Lock()
	if s.turnID == "" {
		s.turnID = started.ID
	}
	s.stateMu.Unlock()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return s.cancelTurn(ctx.Err(), done)
	case <-s.lifetime.Done():
		return s.transportError()
	}
}

type turnStartResult struct {
	turn turn
	err  error
}

func (s *session) startTurn(ctx context.Context, params turnStartParams) (turn, error) {
	result := make(chan turnStartResult, 1)
	go func() {
		started, err := s.client.startTurn(ctx, params, nil)
		result <- turnStartResult{turn: started, err: err}
	}()
	select {
	case value := <-result:
		if s.lifetime.Err() != nil {
			return turn{}, s.transportError()
		}
		if ctx.Err() != nil {
			return turn{}, s.cancelTurn(ctx.Err(), s.currentTurnDone())
		}
		return value.turn, s.preferTerminal(value.err)
	case <-ctx.Done():
		if s.lifetime.Err() != nil {
			return turn{}, s.transportError()
		}
		return turn{}, s.cancelTurn(ctx.Err(), s.currentTurnDone())
	case <-s.lifetime.Done():
		return turn{}, s.transportError()
	}
}

func (s *session) cancelTurn(ctxErr error, done <-chan error) error {
	timer := time.NewTimer(s.cancelTimeout)
	defer timer.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	interruptSent := false
	for {
		if !interruptSent {
			interruptCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			interruptSent = s.sendInterrupt(interruptCtx) == nil
			cancel()
		}
		select {
		case <-done:
			return harness.Phase(harness.ErrTimeout, ctxErr)
		case <-s.lifetime.Done():
			return harness.Phase(harness.ErrTransportClosed, ctxErr)
		case <-timer.C:
			_ = s.Stop()
			return harness.Phase(harness.ErrTransportClosed, ctxErr)
		case <-ticker.C:
		}
	}
}

func (s *session) clearTurn(done chan error) {
	s.stateMu.Lock()
	if s.turnDone == done {
		if s.turnID != "" {
			s.lastTurnID = s.turnID
		}
		s.running, s.turnID, s.turnDone = false, "", nil
	}
	s.stateMu.Unlock()
}

func (s *session) currentTurnDone() <-chan error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.turnDone
}

func (s *session) Interrupt(ctx context.Context) error { return s.sendInterrupt(ctx) }

func (s *session) sendInterrupt(ctx context.Context) error {
	s.stateMu.Lock()
	threadID, turnID, running := s.threadID, s.turnID, s.running
	s.stateMu.Unlock()
	if !running || threadID == "" || turnID == "" {
		return errors.New("no running turn")
	}
	return s.client.interrupt(ctx, threadID, turnID)
}

func (s *session) Respond(requestID string, decision harness.Decision) error {
	if !validDecision(decision) {
		return fmt.Errorf("invalid approval decision %q", decision)
	}
	s.stateMu.Lock()
	pending, ok := s.pending[requestID]
	if ok {
		delete(s.pending, requestID)
	}
	s.stateMu.Unlock()
	if !ok {
		return fmt.Errorf("approval request %q not found", requestID)
	}
	return s.sendApproval(pending, decision)
}

func (s *session) Events() <-chan harness.Event { return s.stream.Events() }

func (s *session) request(ctx context.Context, phase error, request rpcMessage, result any) error {
	key := string(request.ID)
	response := make(chan rpcMessage, 1)
	s.stateMu.Lock()
	if s.stopped {
		s.stateMu.Unlock()
		return harness.Phase(harness.ErrTransportClosed, errors.New("session stopped"))
	}
	s.responses[key] = response
	if buffered, ok := s.responseBacklog[key]; ok {
		delete(s.responseBacklog, key)
		response <- buffered
	}
	s.stateMu.Unlock()
	defer func() {
		s.stateMu.Lock()
		delete(s.responses, key)
		s.stateMu.Unlock()
	}()
	if err := s.transport.Send(ctx, request); err != nil {
		return classifyError(ctx, phase, err)
	}
	select {
	case message := <-response:
		if message.Error != nil {
			return harness.Phase(phase, message.Error)
		}
		if result != nil && len(message.Result) > 0 && string(message.Result) != "null" {
			if err := json.Unmarshal(message.Result, result); err != nil {
				return harness.Phase(harness.ErrDecode, err)
			}
		}
		return nil
	case <-ctx.Done():
		return classifyError(ctx, phase, ctx.Err())
	case <-s.lifetime.Done():
		return s.transportError()
	}
}

func (s *session) readLoop() {
	defer s.reader.Done()
	for {
		message, err := s.transport.Recv(s.lifetime)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				s.fail(err)
			}
			return
		}
		if message.Method != "" && len(message.ID) > 0 {
			if s.notificationIsStale(message) {
				s.emitDiagnostic("discarded stale Codex request: " + message.Method)
				if approvalMethods[message.Method] {
					var params approvalParams
					_ = json.Unmarshal(message.Params, &params)
					_ = s.sendApproval(pendingApproval{id: message.ID, method: message.Method, params: params}, harness.DecisionDeny)
				} else {
					s.sendError(message.ID, -32600, "request belongs to an inactive turn")
				}
			} else {
				s.dispatchRequest(message)
			}
			continue
		}
		if message.Method != "" {
			s.dispatchMu.Lock()
			if s.notificationIsStale(message) {
				s.emitDiagnostic("discarded stale Codex notification: " + message.Method)
			} else {
				s.handleNotification(message)
			}
			s.dispatchMu.Unlock()
			continue
		}
		s.stateMu.Lock()
		waiter := s.responses[string(message.ID)]
		if waiter == nil {
			s.responseBacklog[string(message.ID)] = message
		}
		s.stateMu.Unlock()
		if waiter != nil {
			waiter <- message
		}
	}
}

func validDecision(decision harness.Decision) bool {
	return decision == harness.DecisionAllow || decision == harness.DecisionDeny || decision == harness.DecisionAllowForSession
}

func supportedPermissionMode(mode harness.PermissionMode) bool {
	return mode == "" || mode == harness.PermissionApprovalRequired || mode == harness.PermissionAutoAcceptEdits || mode == harness.PermissionFullAccess
}
