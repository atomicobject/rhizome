// Package shellquote quotes individual arguments for POSIX shell commands.
package shellquote

import "strings"

func Quote(arg string) string {
	if arg == "" {
		return "''"
	}
	for _, r := range arg {
		if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' || r == '/' || r == ':') {
			return "'" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'"
		}
	}
	return arg
}
