package cmd

import (
	"fmt"
	"strings"
)

type agentSelectedSurfaceResponse struct {
	Version  string              `json:"version"`
	Command  string              `json:"command"`
	Mode     string              `json:"mode"`
	Selected agentSurfaceCommand `json:"selected"`
}

func selectAgentSurface(selector string) (any, error) {
	full := buildAgentSurface()
	selector = strings.Join(strings.Fields(selector), " ")
	if selector == "" {
		return full, nil
	}
	selector = strings.TrimPrefix(selector, "rzm agent ")
	selector = strings.TrimPrefix(selector, "agent ")
	parts := strings.Fields(selector)
	candidates := full.Commands
	var selected agentSurfaceCommand
	for _, part := range parts {
		found := false
		for _, candidate := range candidates {
			if candidate.Name == part {
				selected = candidate
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown agent command %q", selector)
		}
		candidates = selected.Subcommands
	}
	return agentSelectedSurfaceResponse{Version: full.Version, Command: full.Command, Mode: full.Mode, Selected: selected}, nil
}
