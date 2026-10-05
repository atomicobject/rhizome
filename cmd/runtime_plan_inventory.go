package cmd

import (
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/oneshotruntime"
	"github.com/spf13/cobra"
)

const runtimePlanOperationAnnotation = "rhizome.io/one-shot-operation"

// bindAgentRuntimePlanOperations attaches the typed inventory identity to the
// executable Cobra node. Later rollout phases consume this identity instead of
// inferring runtime policy from handler or command names.
func bindAgentRuntimePlanOperations(root *cobra.Command) {
	registry := oneshotruntime.DefaultRegistry()
	if err := registry.Validate(); err != nil {
		panic(fmt.Sprintf("invalid one-shot runtime registry: %v", err))
	}
	var walk func(*cobra.Command)
	walk = func(command *cobra.Command) {
		if command != root && (command.Run != nil || command.RunE != nil) {
			path := runtimePlanCommandPath(command)
			operationID, ok := registry.OperationIDForFront(path)
			if !ok {
				panic(fmt.Sprintf("one-shot runtime registry missing executable front %q", path))
			}
			if command.Annotations == nil {
				command.Annotations = map[string]string{}
			}
			command.Annotations[runtimePlanOperationAnnotation] = string(operationID)
		}
		for _, child := range command.Commands() {
			walk(child)
		}
	}
	walk(root)
}

func runtimePlanCommandPath(command *cobra.Command) string {
	path := strings.TrimSpace(command.CommandPath())
	return strings.TrimPrefix(path, command.Root().Name()+" ")
}
