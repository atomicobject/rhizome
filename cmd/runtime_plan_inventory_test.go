package cmd

import (
	"sort"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/oneshotruntime"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestRuntimePlanInventoryCoversExecutableAgentCommands(t *testing.T) {
	registry := oneshotruntime.DefaultRegistry()
	actual := executableCommandPaths(agentCmd)
	missing := make([]string, 0)
	for _, path := range actual {
		if !registry.HasFront(path) {
			missing = append(missing, path)
		}
	}
	assert.Empty(t, missing)

	declared := registry.FrontsWithPrefix("agent ")
	extra := make([]string, 0)
	actualSet := make(map[string]struct{}, len(actual))
	for _, path := range actual {
		actualSet[path] = struct{}{}
	}
	for _, path := range declared {
		if _, ok := actualSet[path]; !ok {
			extra = append(extra, path)
		}
	}
	assert.Empty(t, extra)

	for _, command := range executableCommands(agentCmd) {
		path := runtimePlanCommandPath(command)
		want, ok := registry.OperationIDForFront(path)
		assert.Truef(t, ok, "missing registry operation for %q", path)
		assert.Equal(t, string(want), command.Annotations[runtimePlanOperationAnnotation])
	}
}

func TestRuntimePlanInventoryCoversAgentCatalogParents(t *testing.T) {
	registry := oneshotruntime.DefaultRegistry()
	for _, descriptor := range agentapi.AgentCLICommandDescriptors() {
		prefix := "agent " + descriptor.AgentCommand
		assert.Truef(t, registry.HasFrontOrDescendant(prefix), "missing catalog command %q", descriptor.AgentCommand)
	}
	assert.True(t, registry.HasFront("agent surface"))
}

func TestRuntimePlanInventoryRootFrontsResolveToCommands(t *testing.T) {
	registry := oneshotruntime.DefaultRegistry()
	actual := map[string]struct{}{}
	for _, command := range allCommands(rootCmd) {
		actual[runtimePlanCommandPath(command)] = struct{}{}
	}
	missing := make([]string, 0)
	for _, path := range registry.FrontsWithoutPrefix("agent ") {
		if _, ok := actual[path]; !ok {
			missing = append(missing, path)
		}
	}
	assert.Empty(t, missing)
}

func executableCommandPaths(root *cobra.Command) []string {
	commands := executableCommands(root)
	paths := make([]string, 0)
	for _, command := range commands {
		paths = append(paths, runtimePlanCommandPath(command))
	}
	sort.Strings(paths)
	return paths
}

func executableCommands(root *cobra.Command) []*cobra.Command {
	commands := make([]*cobra.Command, 0)
	var walk func(*cobra.Command)
	walk = func(command *cobra.Command) {
		if command != root && (command.Run != nil || command.RunE != nil) {
			commands = append(commands, command)
		}
		for _, child := range command.Commands() {
			walk(child)
		}
	}
	walk(root)
	return commands
}

func allCommands(root *cobra.Command) []*cobra.Command {
	commands := make([]*cobra.Command, 0)
	var walk func(*cobra.Command)
	walk = func(command *cobra.Command) {
		if command != root {
			commands = append(commands, command)
		}
		for _, child := range command.Commands() {
			walk(child)
		}
	}
	walk(root)
	return commands
}
