package cmd

import (
	"context"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/app/oneshotruntime"
	"github.com/spf13/cobra"
)

func agentOperationIDFromCommand(cmd *cobra.Command) (oneshotruntime.OperationID, error) {
	if cmd == nil || cmd.Annotations == nil {
		return "", fmt.Errorf("one-shot runtime operation is not bound")
	}
	operationID := oneshotruntime.OperationID(cmd.Annotations[runtimePlanOperationAnnotation])
	if _, ok := oneshotruntime.DefaultRegistry().Declaration(operationID); !ok {
		return "", fmt.Errorf("unknown one-shot runtime operation %q", operationID)
	}
	return operationID, nil
}

// assertRuntimeFreeAgentOperation runs static runtime-free declarations through
// the same typed boundary. BuildAndAwait rejects accidental construction by
// returning before it calls a factory for an empty plan.
func assertRuntimeFreeAgentOperation(cmd *cobra.Command) error {
	operationID, err := agentOperationIDFromCommand(cmd)
	if err != nil {
		return err
	}
	declaration, ok := oneshotruntime.DefaultRegistry().Declaration(operationID)
	if !ok || declaration.StaticPlan == nil {
		return fmt.Errorf("operation %q does not declare a static runtime-free plan", operationID)
	}
	if declaration.StaticPlan.RequiresRuntime() {
		return fmt.Errorf("operation %q is runtime-backed", operationID)
	}
	_, err = oneshotruntime.BuildAndAwait(cmd.Context(), *declaration.StaticPlan, nil)
	return err
}

// buildAgentOneShotRuntime applies a static registry plan through the one-shot
// boundary. Request-derived operations retain their established composition
// until their plan is frozen in a later phase.
func buildAgentOneShotRuntime(ctx context.Context, operationID oneshotruntime.OperationID, opts bootstrap.LiveOptions) (*bootstrap.LiveRuntime, func(context.Context) error, error) {
	declaration, ok := oneshotruntime.DefaultRegistry().Declaration(operationID)
	if !ok {
		return nil, nil, fmt.Errorf("unknown one-shot runtime operation %q", operationID)
	}
	if declaration.Owner != oneshotruntime.CompositionOneShotRuntime || declaration.StaticPlan == nil {
		runtime, err := bootstrap.NewLiveRuntime(ctx, opts)
		return runtime, nil, err
	}

	plan := *declaration.StaticPlan
	runtime, err := oneshotruntime.Build(ctx, plan, func(buildCtx context.Context, requirements bootstrap.RuntimeRequirements) (oneshotruntime.Runtime, error) {
		opts.Requirements = requirements
		// Read-only bootstrap normally retains its narrow existing-only session
		// handle. Static plan policy removes it for exact index operations.
		opts.DisableSessionStore = plan.Session == oneshotruntime.SessionNone
		live, err := bootstrap.NewLiveRuntime(buildCtx, opts)
		if err != nil {
			return nil, err
		}
		return &liveRuntimePlanAdapter{LiveRuntime: live}, nil
	})
	if err != nil {
		return nil, nil, err
	}
	adapter, ok := runtime.(*liveRuntimePlanAdapter)
	if !ok || adapter.LiveRuntime == nil {
		if runtime != nil {
			runtime.Close()
		}
		return nil, nil, fmt.Errorf("one-shot runtime factory returned an incompatible runtime")
	}
	return adapter.LiveRuntime, func(waitCtx context.Context) error {
		return oneshotruntime.Await(waitCtx, runtime, plan)
	}, nil
}

type liveRuntimePlanAdapter struct {
	*bootstrap.LiveRuntime
}

func (a *liveRuntimePlanAdapter) Close() {
	_ = a.LiveRuntime.Close()
}

func (a *liveRuntimePlanAdapter) WaitForNoteCache(ctx context.Context) error {
	if err := a.LiveRuntime.WaitForSearch(ctx); err != nil {
		return err
	}
	if cache := a.LiveRuntime.Cache(); cache != nil {
		return cache.EnsureReady(ctx)
	}
	return fmt.Errorf("note cache unavailable")
}

func (a *liveRuntimePlanAdapter) WaitForSession(ctx context.Context) error {
	return a.LiveRuntime.WaitForSession(ctx)
}
