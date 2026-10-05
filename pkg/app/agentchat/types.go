package agentchat

import (
	"encoding/json"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
)

type HarnessSettings struct {
	Model          string                 `json:"model,omitempty" yaml:"model,omitempty"`
	Effort         string                 `json:"effort,omitempty" yaml:"effort,omitempty"`
	PermissionMode harness.PermissionMode `json:"permissionMode" yaml:"permission-mode"`
}

type Settings struct {
	Harness   harness.Kind                     `json:"harness,omitempty" yaml:"harness,omitempty"`
	Harnesses map[harness.Kind]HarnessSettings `json:"harnesses" yaml:"harnesses"`
}

type ResolvedHarness struct {
	Harness harness.Kind `json:"harness,omitempty"`
	Reason  string       `json:"reason"`
}

type HarnessStatus struct {
	Kind   harness.Kind `json:"kind"`
	Status Status       `json:"status"`
}

type Status struct {
	Installed    bool                  `json:"installed"`
	Version      string                `json:"version,omitempty"`
	LoggedIn     bool                  `json:"loggedIn"`
	Account      string                `json:"account,omitempty"`
	Models       []harness.ModelOption `json:"models,omitempty"`
	Efforts      []string              `json:"efforts,omitempty"`
	Capabilities harness.Capabilities  `json:"capabilities"`
	LastError    string                `json:"lastError,omitempty"`
	LoginHint    string                `json:"loginHint,omitempty"`
}

type SettingsResponse struct {
	Settings  Settings        `json:"settings"`
	Resolved  ResolvedHarness `json:"resolved"`
	Harnesses []HarnessStatus `json:"harnesses"`
}

type Session struct {
	ID               string       `json:"id"`
	Title            string       `json:"title,omitempty"`
	Harness          harness.Kind `json:"harness,omitempty"`
	HarnessSessionID string       `json:"harnessSessionId,omitempty"`
	LastTurnID       string       `json:"-"`
	Model            string       `json:"model,omitempty"`
	CreatedAt        time.Time    `json:"createdAt"`
	UpdatedAt        time.Time    `json:"updatedAt"`
	Archived         bool         `json:"archived,omitempty"`
	ReadOnly         bool         `json:"readOnly"`
	TurnRunning      bool         `json:"turnRunning"`
}

type Message struct {
	ID        int64     `json:"id"`
	SessionID string    `json:"sessionId"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"createdAt"`
}

type EventResult struct {
	TurnID          string          `json:"turnId,omitempty"`
	ItemID          string          `json:"itemId,omitempty"`
	RequestID       string          `json:"requestId,omitempty"`
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
	Usage           EventUsage      `json:"usage,omitempty"`
}

type EventUsage struct {
	InputTokens           int64 `json:"inputTokens,omitempty"`
	CachedInputTokens     int64 `json:"cachedInputTokens,omitempty"`
	OutputTokens          int64 `json:"outputTokens,omitempty"`
	ReasoningOutputTokens int64 `json:"reasoningOutputTokens,omitempty"`
	TotalTokens           int64 `json:"totalTokens,omitempty"`
}

type Event struct {
	ID        int64        `json:"id"`
	SessionID string       `json:"sessionId"`
	Type      string       `json:"type"`
	Role      string       `json:"role,omitempty"`
	Content   string       `json:"content,omitempty"`
	ToolName  string       `json:"toolName,omitempty"`
	Result    *EventResult `json:"result,omitempty"`
	Error     string       `json:"error,omitempty"`
	CreatedAt time.Time    `json:"createdAt"`
}

type SessionResponse struct {
	Session  Session   `json:"session"`
	Messages []Message `json:"messages,omitempty"`
	Events   []Event   `json:"events,omitempty"`
}

type SendMessageRequest struct {
	Content string `json:"content"`
}

type SendMessageResponse struct {
	Session Session `json:"session"`
	Events  []Event `json:"events,omitempty"`
}

type ApprovalRequest struct {
	Decision harness.Decision `json:"decision"`
}
