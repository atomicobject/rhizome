package cmd

import (
	"context"
	"errors"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/agentcode"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// A start checks connectivity without waiting through the runtime spawn budget.
const agentStartConnectionBudget = 500 * time.Millisecond

func checkAgentStartRuntime(ctx context.Context, vault obsidian.VaultDefinition) agentcode.RuntimeConnection {
	opts, err := vaultRuntimeEnsureOptions(vault.BasePath(), false, nil)
	if err != nil {
		return agentcode.RuntimeConnection{State: "unavailable", Code: "runtime_unavailable", Message: err.Error()}
	}
	return agentStartRuntimeConnection(ctx, opts)
}

func agentStartRuntimeConnection(ctx context.Context, opts appruntime.EnsureOptions) agentcode.RuntimeConnection {
	ctx, cancel := context.WithTimeout(ctx, agentStartConnectionBudget)
	defer cancel()
	opts.Wait = false
	// The request is short, but a successfully spawned child retains the normal
	// background startup lease while it initializes.
	opts.Budget = appruntime.SpawnBudget
	// Connection verification must not tear down an otherwise reachable runtime.
	// The operation that needs a particular build owns replacement.
	opts.BuildID = ""
	opts.Replace = false
	result, err := appruntime.Ensure(ctx, opts)
	switch {
	case errors.Is(err, appruntime.ErrRuntimeAccessDenied):
		return agentcode.RuntimeConnection{State: "access_denied", Code: "runtime_access_denied", Message: err.Error()}
	case errors.Is(err, appruntime.ErrAutostartDisabled):
		return agentcode.RuntimeConnection{State: "disabled", Code: "runtime_autostart_disabled", Message: err.Error()}
	case err != nil:
		return agentcode.RuntimeConnection{State: "unavailable", Code: "runtime_unavailable", Message: err.Error()}
	case result.Client == nil:
		return agentcode.RuntimeConnection{State: "starting", Message: "Runtime startup is in progress; the first runtime-backed operation will wait for readiness."}
	default:
		return agentcode.RuntimeConnection{State: "connected", Ready: result.Health.Ready}
	}
}
