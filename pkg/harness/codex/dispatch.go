package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
)

var approvalMethods = map[string]bool{
	"item/commandExecution/requestApproval": true,
	"item/fileChange/requestApproval":       true,
	"item/permissions/requestApproval":      true,
	"applyPatchApproval":                    true,
	"execCommandApproval":                   true,
}

const shutdownApprovalWriteTimeout = 50 * time.Millisecond

func (s *session) dispatchRequest(message rpcMessage) {
	if approvalMethods[message.Method] {
		if s.registerApproval(message) {
			return
		}
		s.emitDiagnostic("could not decode Codex approval request: " + message.Method)
		_ = s.sendApproval(pendingApproval{id: append(json.RawMessage(nil), message.ID...), method: message.Method}, harness.DecisionDeny)
		return
	}
	s.emitDiagnostic("unsupported Codex server request: " + message.Method)
	s.sendError(message.ID, -32601, "method not supported")
}

func (s *session) registerApproval(message rpcMessage) bool {
	var params approvalParams
	if len(message.Params) == 0 || json.Unmarshal(message.Params, &params) != nil {
		return false
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(message.Params, &object) != nil {
		return false
	}
	if !approvalParamsFit(message.Method, params) {
		return false
	}
	requestID := displayID(message.ID)
	if params.ThreadID == "" {
		params.ThreadID = params.ConversationID
	}
	if params.ItemID == "" {
		params.ItemID = params.CallID
	}
	pending := pendingApproval{id: append(json.RawMessage(nil), message.ID...), method: message.Method, params: params}
	if !s.interactive {
		_ = s.sendApproval(pending, harness.DecisionDeny)
		return true
	}
	s.stateMu.Lock()
	if s.stopped {
		s.stateMu.Unlock()
		_ = s.sendApproval(pending, harness.DecisionDeny)
		return true
	}
	s.pending[requestID] = pending
	s.stateMu.Unlock()
	paths := append([]string(nil), params.Paths...)
	if params.GrantRoot != "" {
		paths = append(paths, params.GrantRoot)
	}
	for path := range params.FileChanges {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	s.emit(harness.Event{
		Kind: harness.EventApprovalRequested, Phase: harness.PhaseStarted,
		SessionID: params.ThreadID, TurnID: params.TurnID, ItemID: params.ItemID,
		RequestID: requestID, Command: approvalCommand(params.Command), Cwd: params.Cwd,
		Paths: paths, Reason: params.Reason, Name: message.Method, AllowForSession: true,
		Input: append(json.RawMessage(nil), message.Params...),
	})
	return true
}

func approvalParamsFit(method string, params approvalParams) bool {
	switch method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		return params.ThreadID != "" && params.TurnID != "" && params.ItemID != ""
	case "item/permissions/requestApproval":
		return params.ThreadID != "" && params.TurnID != "" && params.ItemID != "" && len(params.Permissions) > 0
	case "applyPatchApproval":
		return params.ConversationID != "" && params.CallID != "" && len(params.FileChanges) > 0
	case "execCommandApproval":
		return params.ConversationID != "" && params.CallID != "" && len(params.Command) > 0
	default:
		return false
	}
}

func approvalCommand(raw json.RawMessage) string {
	var command string
	if json.Unmarshal(raw, &command) == nil {
		return command
	}
	var args []string
	if json.Unmarshal(raw, &args) == nil {
		return strings.Join(args, " ")
	}
	return ""
}

func (s *session) sendApproval(pending pendingApproval, decision harness.Decision) error {
	encoded := approvalResult(pending, decision)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.transport.Send(ctx, rpcMessage{JSONRPC: "2.0", ID: pending.id, Result: encoded})
}

func (s *session) sendApprovalDuringStop(parent context.Context, pending pendingApproval) error {
	ctx, cancel := context.WithTimeout(parent, shutdownApprovalWriteTimeout)
	defer cancel()
	return s.transport.Send(ctx, rpcMessage{JSONRPC: "2.0", ID: pending.id, Result: approvalResult(pending, harness.DecisionDeny)})
}

func approvalResult(pending pendingApproval, decision harness.Decision) json.RawMessage {
	if pending.method == "item/permissions/requestApproval" {
		permissions := json.RawMessage(`{}`)
		if decision != harness.DecisionDeny && len(pending.params.Permissions) > 0 {
			permissions = pending.params.Permissions
		}
		scope := "turn"
		if decision == harness.DecisionAllowForSession {
			scope = "session"
		}
		encoded, _ := json.Marshal(struct {
			Permissions json.RawMessage `json:"permissions"`
			Scope       string          `json:"scope"`
		}{Permissions: permissions, Scope: scope})
		return encoded
	}
	if pending.method == "applyPatchApproval" || pending.method == "execCommandApproval" {
		var value any = map[string]any{"denied": map[string]string{"rejection": "denied by user"}}
		if decision == harness.DecisionAllow {
			value = "approved"
		} else if decision == harness.DecisionAllowForSession {
			value = "approved_for_session"
		}
		encoded, _ := json.Marshal(map[string]any{"decision": value})
		return encoded
	}
	response := "decline"
	if decision == harness.DecisionAllow {
		response = "accept"
	} else if decision == harness.DecisionAllowForSession {
		response = "acceptForSession"
	}
	encoded, _ := json.Marshal(struct {
		Decision string `json:"decision"`
	}{Decision: response})
	return encoded
}

func (s *session) sendError(id json.RawMessage, code int, message string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.transport.Send(ctx, rpcMessage{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}}); err != nil {
		s.emitDiagnostic(fmt.Sprintf("could not reply to Codex server request: %v", err))
	}
}

func (s *session) emitDiagnostic(text string) {
	s.emit(harness.Event{Kind: harness.EventDiagnostic, Phase: harness.PhaseFailed, Text: text})
}

func (s *session) handleNotification(message rpcMessage) {
	event, completed := mapNotification(message)
	if event.Kind != "" {
		if event.SessionID == "" {
			event.SessionID = s.ID()
		}
		if message.Method == "item/agentMessage/delta" {
			s.stateMu.Lock()
			s.deltas[event.ItemID] = true
			s.stateMu.Unlock()
		}
		if message.Method == "item/completed" && event.Kind == harness.EventAssistantTextDelta {
			s.stateMu.Lock()
			streamed := s.deltas[event.ItemID]
			delete(s.deltas, event.ItemID)
			s.stateMu.Unlock()
			if streamed {
				event.Kind = ""
			}
		}
		if event.Kind == harness.EventTurnStarted {
			s.stateMu.Lock()
			s.turnID = event.TurnID
			s.stateMu.Unlock()
		}
		if event.Kind != "" {
			s.emit(event)
		}
	}
	if completed {
		s.stateMu.Lock()
		done := s.turnDone
		s.stateMu.Unlock()
		if done != nil {
			select {
			case done <- nil:
			default:
			}
		}
	}
}

func (s *session) emit(event harness.Event) { s.stream.Emit(event) }

func displayID(id json.RawMessage) string {
	var text string
	if json.Unmarshal(id, &text) == nil {
		return text
	}
	return string(id)
}
