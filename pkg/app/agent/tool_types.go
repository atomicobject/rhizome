package agent

// ToolSpec describes a tool exposed to an agent planner.
type ToolSpec struct {
	Name        string
	Description string
	ArgsSchema  string
}

// ToolCall captures a requested tool invocation.
type ToolCall struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}
