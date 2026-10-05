// Package harness defines the process-independent boundary for coding agents.
package harness

import (
	"context"
	"encoding/json"
)

type Kind string

const (
	KindCodex  Kind = "codex"
	KindClaude Kind = "claude"
)

type Harness interface {
	Status(context.Context) (Status, error)
	StartSession(context.Context, SessionOptions) (Session, error)
	Generate(context.Context, GenerateRequest) (json.RawMessage, error)
}

type Session interface {
	ID() string
	SendTurn(context.Context, string) error
	Interrupt(context.Context) error
	Respond(string, Decision) error
	Events() <-chan Event
	Stop() error
}

type SessionOptions struct {
	Instructions    string
	AllowedTools    []string
	DisallowedTools []string
	PermissionMode  PermissionMode
	MCPServers      []MCPServer
	Model           string
	Effort          string
	Cwd             string
	Resume          string
}

type MCPServer struct {
	Name    string
	Command string
	Args    []string
	Env     []string
}

type GenerateRequest struct {
	Prompt string
	Schema json.RawMessage
	Model  string
	Effort string
}

type PermissionMode string

const (
	PermissionApprovalRequired PermissionMode = "approval-required"
	PermissionAutoAcceptEdits  PermissionMode = "auto-accept-edits"
	PermissionFullAccess       PermissionMode = "full-access"
)

type Decision string

const (
	DecisionAllow           Decision = "allow"
	DecisionDeny            Decision = "deny"
	DecisionAllowForSession Decision = "allow-for-session"
)

type EventKind string

const (
	EventTurnStarted        EventKind = "turn-started"
	EventTurnCompleted      EventKind = "turn-completed"
	EventAssistantTextDelta EventKind = "assistant-text-delta"
	EventReasoning          EventKind = "reasoning"
	EventCommandExecution   EventKind = "command-execution"
	EventFileChange         EventKind = "file-change"
	EventToolCall           EventKind = "tool-call"
	EventApprovalRequested  EventKind = "approval-requested"
	EventTokenUsage         EventKind = "token-usage"
	EventDiagnostic         EventKind = "diagnostic"
	EventError              EventKind = "error"
)

type Event struct {
	Kind            EventKind       `json:"kind"`
	SessionID       string          `json:"sessionId,omitempty"`
	TurnID          string          `json:"turnId,omitempty"`
	ItemID          string          `json:"itemId,omitempty"`
	RequestID       string          `json:"requestId,omitempty"`
	Text            string          `json:"text,omitempty"`
	Name            string          `json:"name,omitempty"`
	Command         string          `json:"command,omitempty"`
	Cwd             string          `json:"cwd,omitempty"`
	Paths           []string        `json:"paths,omitempty"`
	Status          string          `json:"status,omitempty"`
	Phase           string          `json:"phase,omitempty"`
	Reason          string          `json:"reason,omitempty"`
	AllowForSession bool            `json:"allowForSession,omitempty"`
	Input           json.RawMessage `json:"input,omitempty"`
	Output          json.RawMessage `json:"output,omitempty"`
	Truncated       bool            `json:"truncated,omitempty"`
	ExitCode        *int            `json:"exitCode,omitempty"`
	Diff            string          `json:"diff,omitempty"`
	Usage           TokenUsage      `json:"usage,omitempty"`
}

const (
	PhaseStarted   = "started"
	PhaseUpdated   = "updated"
	PhaseCompleted = "completed"
	PhaseFailed    = "failed"
	PhaseDeclined  = "declined"
	PhaseCancelled = "cancelled" // an approval request withdrawn by the vendor before a decision
)

type TokenUsage struct {
	InputTokens           int64 `json:"inputTokens"`
	CachedInputTokens     int64 `json:"cachedInputTokens"`
	OutputTokens          int64 `json:"outputTokens"`
	ReasoningOutputTokens int64 `json:"reasoningOutputTokens"`
	TotalTokens           int64 `json:"totalTokens"`
}

type Status struct {
	Kind         Kind          `json:"kind"`
	Installed    bool          `json:"installed"`
	Version      string        `json:"version,omitempty"`
	LoggedIn     bool          `json:"loggedIn"`
	Account      string        `json:"account,omitempty"`
	Models       []ModelOption `json:"models,omitempty"`
	Capabilities Capabilities  `json:"capabilities"`
	LastError    string        `json:"lastError,omitempty"`
	LoginHint    string        `json:"loginHint,omitempty"`
}

type ModelOption struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"displayName"`
	Efforts     []string `json:"efforts,omitempty"`
	Default     bool     `json:"default,omitempty"`
}

type Capabilities struct {
	SupportsAllowedTools    bool             `json:"supportsAllowedTools"`
	SupportsAllowForSession bool             `json:"supportsAllowForSession"`
	PermissionModes         []PermissionMode `json:"permissionModes"`
}
