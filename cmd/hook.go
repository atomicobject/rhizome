package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/cli/hook"
	initactions "github.com/atomicobject/rhizome/pkg/app/cli/init"
)

// IsHookInvocation reports whether args run the hook dispatcher, which takes
// an early path: no .env loading, binary bootstrap, update, or Cobra setup.
func IsHookInvocation(args []string) bool {
	return len(args) > 0 && args[0] == "hook"
}

// runHook is `rzm hook <harness> <event>` and `rzm hook --check`. Once it
// starts it exits 0: a hook never blocks or fails the host's tool call.
//
// Docs: [[agent-surface-integration-modes#^SPEC-0045-US5-AC7]]
func runHook(args []string) int {
	if len(args) == 1 && args[0] == "--check" {
		fmt.Printf("rzm-hook %d\n", hook.ProtocolVersion)
		return 0
	}
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: rzm hook <claude|codex> <session-start|subagent-start|post-tool-use>")
		return 0
	}
	// A last guard below the host's timeout, should file I/O hang past the
	// dispatcher's own deadline.
	time.AfterFunc(hook.Deadline+300*time.Millisecond, func() { os.Exit(0) })
	cwd, _ := os.Getwd()
	hook.Run(context.Background(), hook.Options{
		Harness:  args[0],
		Point:    args[1],
		Stdin:    os.Stdin,
		Stdout:   os.Stdout,
		Cwd:      cwd,
		Handlers: initactions.HookHandlers,
	})
	return 0
}
