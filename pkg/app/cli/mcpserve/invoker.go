// Package mcpserve owns the CLI application workflow for a persistent MCP
// process. The transport adapter remains in pkg/app/mcpserve.
package mcpserve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	transport "github.com/atomicobject/rhizome/pkg/app/mcpserve"
	"github.com/atomicobject/rhizome/pkg/app/oneshotruntime"
	"github.com/atomicobject/rhizome/pkg/fileio"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// Dependencies are the command-composed seams needed to build request-scoped
// agent runtimes without coupling this workflow to Cobra globals.
type Dependencies struct {
	VaultDefinition func(context.Context) (obsidian.VaultDefinition, error)
	RuntimeFree     func() (agentapi.Config, error)
	Prepare         func(context.Context, string, oneshotruntime.OperationID, map[string]any) (agentapi.Config, func(), error)
	PrepareBound    func(context.Context, string, oneshotruntime.OperationID, map[string]any) (agentapi.Config, func(), error)
}

// NewInvoker snapshots the vault definition and config, then returns a
// dispatcher that constructs and closes runtime resources for every call.
func NewInvoker(ctx context.Context, readWrite bool, dependencies Dependencies) (transport.Dispatcher, error) {
	if dependencies.VaultDefinition == nil || dependencies.RuntimeFree == nil || dependencies.Prepare == nil {
		return nil, fmt.Errorf("MCP invoker dependencies are incomplete")
	}

	definition, err := dependencies.VaultDefinition(ctx)
	if err != nil {
		return nil, err
	}
	vaultPaths, err := paths.NewVaultPaths(definition.BasePath())
	if err != nil {
		return nil, err
	}
	if err := vaultPaths.RequireCurrentWorkingDirectoryAtRoot(); err != nil {
		if errors.Is(err, paths.ErrWorkingDirectoryNotVaultRoot) {
			return nil, fmt.Errorf("MCP serve must run from its configured vault root %q", vaultPaths.Root())
		}
		return nil, err
	}
	definitionSnapshot, err := json.Marshal(definition)
	if err != nil {
		return nil, err
	}
	configPath, err := vaultPaths.Abs(paths.RelPath(".rhizome/config.yml"))
	if err != nil {
		return nil, err
	}
	configSnapshot, err := readConfig(configPath.String())
	if err != nil {
		return nil, err
	}

	return func(callCtx context.Context, name string, input map[string]any) ([]byte, error) {
		current, err := dependencies.VaultDefinition(callCtx)
		if err != nil {
			return nil, err
		}
		currentDefinition, err := json.Marshal(current)
		if err != nil {
			return nil, err
		}
		currentConfig, err := readConfig(configPath.String())
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(definitionSnapshot, currentDefinition) || !bytes.Equal(configSnapshot, currentConfig) {
			return nil, fmt.Errorf("vault configuration changed; close and reconnect before further calls")
		}

		operationID, err := RuntimeOperationID(name, input)
		if err != nil {
			return nil, err
		}
		if name == "capabilities" {
			cfg, err := dependencies.RuntimeFree()
			if err != nil {
				return nil, err
			}
			cfg.ReadWrite = readWrite
			return agentapi.CallJSON(callCtx, cfg, name, input)
		}

		prepare := dependencies.Prepare
		if declaration, ok := oneshotruntime.DefaultRegistry().Declaration(operationID); ok && declaration.PlannerBinding == oneshotruntime.PlannerBindingQueryRecipeRun {
			if dependencies.PrepareBound == nil {
				return nil, fmt.Errorf("MCP tool %q requires its bound runtime planner", name)
			}
			prepare = dependencies.PrepareBound
		}
		cfg, cleanup, err := prepare(callCtx, name, operationID, input)
		if err != nil {
			return nil, err
		}
		if cleanup != nil {
			defer cleanup()
		}
		cfg.ReadWrite = readWrite
		return agentapi.CallJSON(callCtx, cfg, name, input)
	}, nil
}

// RuntimeOperationID resolves a shared tool through the code-mode catalog's
// concrete CLI leaves. Multi-action tools select the leaf named by their op.
func RuntimeOperationID(toolName string, input map[string]any) (oneshotruntime.OperationID, error) {
	if toolName == "capabilities" {
		operationID, ok := oneshotruntime.DefaultRegistry().OperationIDForFront("agent surface")
		if !ok {
			return "", fmt.Errorf("MCP tool %q has no registered runtime operation", toolName)
		}
		return operationID, nil
	}

	descriptor, ok := agentapi.CodeOperationDescriptor(toolName)
	if !ok || descriptor.Surfaces&agentapi.SurfaceAgentAPI == 0 || descriptor.Handler == nil {
		return "", fmt.Errorf("MCP tool %q has no code-mode operation descriptor", toolName)
	}
	leaves := descriptor.CodeCLILeaves
	if len(leaves) > 1 {
		op, _ := input["op"].(string)
		op = strings.TrimSpace(op)
		if op == "" {
			return "", fmt.Errorf("op is required")
		}
		matched := leaves[:0]
		for _, leaf := range leaves {
			if strings.HasSuffix(leaf, " "+op) {
				matched = append(matched, leaf)
			}
		}
		leaves = matched
	}
	if len(leaves) != 1 {
		return "", fmt.Errorf("MCP tool %q does not resolve to one runtime operation", toolName)
	}
	operationID, ok := oneshotruntime.DefaultRegistry().OperationIDForFront(leaves[0])
	if !ok {
		return "", fmt.Errorf("MCP tool %q leaf %q has no registered runtime operation", toolName, leaves[0])
	}
	return operationID, nil
}

func readConfig(path string) ([]byte, error) {
	data, err := fileio.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return data, err
}
