package codex

import "encoding/json"

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return e.Message }

type initializeParams struct {
	ClientInfo clientInfo `json:"clientInfo"`
}

type clientInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Version string `json:"version"`
}

type initializeResponse struct {
	UserAgent string `json:"userAgent"`
}

type threadParams struct {
	ThreadID              string `json:"threadId,omitempty"`
	ExcludeTurns          bool   `json:"excludeTurns,omitempty"`
	Model                 string `json:"model,omitempty"`
	Cwd                   string `json:"cwd,omitempty"`
	ApprovalPolicy        string `json:"approvalPolicy,omitempty"`
	Sandbox               string `json:"sandbox,omitempty"`
	DeveloperInstructions string `json:"developerInstructions,omitempty"`
}

type threadResponse struct {
	Thread struct {
		ID string `json:"id"`
	} `json:"thread"`
}

type turnStartParams struct {
	ThreadID       string      `json:"threadId"`
	Input          []userInput `json:"input"`
	Model          string      `json:"model,omitempty"`
	Effort         string      `json:"effort,omitempty"`
	Cwd            string      `json:"cwd,omitempty"`
	ApprovalPolicy string      `json:"approvalPolicy,omitempty"`
	SandboxPolicy  any         `json:"sandboxPolicy,omitempty"`
}

type userInput struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type turnResponse struct {
	Turn turn `json:"turn"`
}

type turn struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type turnInterruptParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
}

type accountResponse struct {
	Account *struct {
		Type     string `json:"type"`
		Email    string `json:"email"`
		PlanType string `json:"planType"`
	} `json:"account"`
	RequiresOpenAIAuth bool `json:"requiresOpenaiAuth"`
}

type modelListResponse struct {
	Data []struct {
		ID                        string `json:"id"`
		Model                     string `json:"model"`
		DisplayName               string `json:"displayName"`
		IsDefault                 bool   `json:"isDefault"`
		SupportedReasoningEfforts []struct {
			ReasoningEffort string `json:"reasoningEffort"`
		} `json:"supportedReasoningEfforts"`
	} `json:"data"`
	NextCursor string `json:"nextCursor"`
}

type modelListParams struct {
	Cursor string `json:"cursor,omitempty"`
}

type notificationEnvelope struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
	Delta    string `json:"delta"`
	Message  string `json:"message"`
	Turn     turn   `json:"turn"`
	Thread   struct {
		ID string `json:"id"`
	} `json:"thread"`
	Item       threadItem       `json:"item"`
	Changes    []fileChange     `json:"changes"`
	TokenUsage threadTokenUsage `json:"tokenUsage"`
	Error      json.RawMessage  `json:"error"`
	WillRetry  bool             `json:"willRetry"`
}

type threadItem struct {
	ID               string          `json:"id"`
	Type             string          `json:"type"`
	Text             string          `json:"text"`
	Command          string          `json:"command"`
	Cwd              string          `json:"cwd"`
	Status           string          `json:"status"`
	AggregatedOutput string          `json:"aggregatedOutput"`
	Arguments        json.RawMessage `json:"arguments"`
	Result           json.RawMessage `json:"result"`
	ExitCode         *int            `json:"exitCode"`
	Changes          []fileChange    `json:"changes"`
	Server           string          `json:"server"`
	Tool             string          `json:"tool"`
	Name             string          `json:"name"`
}

type fileChange struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	Diff string `json:"diff"`
}

type threadTokenUsage struct {
	Total tokenUsage `json:"total"`
	Last  tokenUsage `json:"last"`
}

type tokenUsage struct {
	InputTokens           int64 `json:"inputTokens"`
	CachedInputTokens     int64 `json:"cachedInputTokens"`
	OutputTokens          int64 `json:"outputTokens"`
	ReasoningOutputTokens int64 `json:"reasoningOutputTokens"`
	TotalTokens           int64 `json:"totalTokens"`
}

type turnError struct {
	Message           string `json:"message"`
	AdditionalDetails string `json:"additionalDetails"`
}

type approvalParams struct {
	ThreadID       string                     `json:"threadId"`
	ConversationID string                     `json:"conversationId"`
	TurnID         string                     `json:"turnId"`
	ItemID         string                     `json:"itemId"`
	CallID         string                     `json:"callId"`
	Command        json.RawMessage            `json:"command"`
	Cwd            string                     `json:"cwd"`
	Reason         string                     `json:"reason"`
	GrantRoot      string                     `json:"grantRoot"`
	Paths          []string                   `json:"paths"`
	FileChanges    map[string]json.RawMessage `json:"fileChanges"`
	Permissions    json.RawMessage            `json:"permissions"`
}
