package cmd

import (
	"github.com/atomicobject/rhizome/internal/testkit"
	"os"
	"testing"
)

func newRuntimeOptionsForTest(t *testing.T) *vaultCommandOptions {
	return &vaultCommandOptions{root: newRootCommand(commandEnv{Getwd: os.Getwd, Config: testkit.Env(t)}).Command}
}
func newCodeOptionsForTest(t *testing.T) *codeInvocationOptions {
	options := &codeInvocationOptions{vaultCommandOptions: newRuntimeOptionsForTest(t)}
	return options
}
func newGraphOptionsForTest(t *testing.T) *graphInvocationOptions {
	options := &graphInvocationOptions{vaultCommandOptions: newRuntimeOptionsForTest(t)}
	return options
}
func newIndexOptionsForTest(t *testing.T) *indexInvocationOptions {
	options := &indexInvocationOptions{vaultCommandOptions: newRuntimeOptionsForTest(t)}
	options.newWorktreeRunIndex = options.runNewWorktreeIndexThroughRuntime
	return options
}
func newInitOptionsForTest(t *testing.T) *initInvocationOptions {
	options := &initInvocationOptions{indexInvocationOptions: newIndexOptionsForTest(t)}
	return options
}
