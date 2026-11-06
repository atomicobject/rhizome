package harness

import (
	"fmt"
	"strings"
)

// Environment parses the server's explicit KEY=value entries; the last value wins.
func (s MCPServer) Environment() (map[string]string, error) {
	env := make(map[string]string, len(s.Env))
	for _, entry := range s.Env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("invalid environment entry %q", entry)
		}
		env[key] = value
	}
	return env, nil
}
