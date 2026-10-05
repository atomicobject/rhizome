//go:build !windows

package cmd

import (
	"fmt"
	"os"
	"strings"
	"syscall"
)

// Replace the launcher so terminal and supervisor signals reach the actual
// command once, and its PID and standard streams retain their original owner.
func executeRepoBinary(target string, args []string) int {
	env := os.Environ()
	for i := 0; i < len(env); {
		if strings.HasPrefix(env[i], repoDelegatedEnv+"=") {
			env = append(env[:i], env[i+1:]...)
		} else {
			i++
		}
	}
	err := syscall.Exec(target, append([]string{target}, args...), append(env, repoDelegatedEnv+"=1"))
	fmt.Fprintf(os.Stderr, "Whoops. There was an error delegating to repo Rhizome binary '%s': %v\n", target, err)
	return 1
}
