package agentapi

// SurfaceCommand describes reflected CLI syntax; command behavior remains in the catalog and application services.
type SurfaceCommand struct {
	Name        string           `json:"name"`
	Use         string           `json:"use"`
	Source      string           `json:"source"`
	Category    string           `json:"category,omitempty"`
	ToolName    string           `json:"toolName,omitempty"`
	Short       string           `json:"short,omitempty"`
	Examples    []string         `json:"examples,omitempty"`
	Flags       []SurfaceFlag    `json:"flags,omitempty"`
	Subcommands []SurfaceCommand `json:"subcommands,omitempty"`
}

type SurfaceFlag struct {
	Name       string `json:"name"`
	Shorthand  string `json:"shorthand,omitempty"`
	Type       string `json:"type"`
	Default    string `json:"default,omitempty"`
	Repeatable bool   `json:"repeatable,omitempty"`
	Usage      string `json:"usage,omitempty"`
}
