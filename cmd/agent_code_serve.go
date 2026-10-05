package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/agentcode"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/fileio"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/version"
	"github.com/spf13/cobra"
)

func newAgentCodeServeCmd() *cobra.Command {
	var readWrite bool
	command := &cobra.Command{
		Use: "serve", Short: "Serve selected agent operations in one JSON-RPC stdio process", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			call, err := newAgentCodeInvoker(command.Context(), readWrite)
			if err != nil {
				return agentCodeError(err)
			}
			if err := agentcode.Serve(command.Context(), command.InOrStdin(), command.OutOrStdout(), call); err != nil {
				return agentCodeError(err)
			}
			return nil
		},
	}
	command.Flags().BoolVar(&readWrite, "read-write", false, "allow supported explicit write/apply operations for this connection")
	return command
}

// The process is persistent; application snapshots remain request-scoped. This
// avoids stale note/config/index objects and preserves each operation's effects.
func newAgentCodeInvoker(ctx context.Context, readWrite bool) (func(context.Context, string, map[string]any) (agentcode.CallOutcome, error), error) {
	def, err := vaultDefOrDefaultContext(ctx)
	if err != nil {
		return nil, err
	}
	root, err := filepath.EvalSymlinks(def.BasePath())
	if err != nil {
		return nil, err
	}
	cwd, err := commandEnvFromContext(ctx).Getwd()
	if err != nil {
		return nil, err
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil || filepath.Clean(cwd) != filepath.Clean(root) {
		return nil, fmt.Errorf("code serve must run from its configured vault root %q", root)
	}
	definition, err := json.Marshal(def)
	if err != nil {
		return nil, err
	}
	configPath := filepath.Join(root, ".rhizome", "config.yml")
	config, err := readCodeModeConfig(configPath)
	if err != nil {
		return nil, err
	}
	resources := agentapi.NewCodeResources()
	forwarder := newAgentOpForwarder(root)
	// changed reports a configuration change since connect as the outcome every
	// later call returns.
	changed := func(ctx context.Context) (agentcode.CallOutcome, bool) {
		current, err := vaultDefOrDefaultContext(ctx)
		if err != nil {
			return codeModeFailure(err), true
		}
		currentDefinition, err := json.Marshal(current)
		if err != nil {
			return codeModeFailure(err), true
		}
		currentConfig, err := readCodeModeConfig(configPath)
		if err != nil {
			return codeModeFailure(err), true
		}
		if !bytes.Equal(definition, currentDefinition) {
			return codeModeConfigurationChanged("vault definition"), true
		}
		if !bytes.Equal(config, currentConfig) {
			return codeModeConfigurationChanged(".rhizome/config.yml"), true
		}
		return agentcode.CallOutcome{}, false
	}
	// After this connection writes, its reads run here, where the freshness
	// refresh observes the write without waiting for the runtime's watcher.
	var wrote atomic.Bool
	// runtimeRead serves a catalog-admitted read in the vault runtime under
	// shared access, so independent reads overlap. handled=false sends the call
	// to the in-process path (SPEC-0115).
	runtimeRead := func(ctx context.Context, name string, input map[string]any) (agentcode.CallOutcome, bool) {
		release, err := resources.AcquireAccess(ctx, agentapi.CodeVaultRead)
		if err != nil {
			return codeModeFailure(err), true
		}
		defer release()
		if outcome, changed := changed(ctx); changed {
			return outcome, true
		}
		outcome, handled := forwarder.call(ctx, name, input, readWrite)
		if !handled || actions.IsCodeModeRuntimeReadPending(outcome.Diagnostic) {
			return agentcode.CallOutcome{}, false
		}
		return outcome, true
	}
	return func(ctx context.Context, name string, input map[string]any) (outcome agentcode.CallOutcome, callErr error) {
		ctx, finish := observeAgentRequest(ctx, name, "code_stdio")
		defer func() {
			if panicked := recover(); panicked != nil {
				finish(false, errors.New("diagnostic boundary panicked"))
				panic(panicked)
			}
			finish(outcome.OK, callErr)
		}()
		descriptor, _ := agentapi.CodeOperationDescriptor(name)
		if descriptor.CodeRuntimeRead != nil && descriptor.CodeRuntimeRead(input) && !wrote.Load() {
			if outcome, handled := runtimeRead(ctx, name, input); handled {
				return outcome, nil
			}
		} else if readWrite && descriptor.Mutation != agentapi.MutationNever {
			wrote.Store(true)
		}
		release, err := resources.Acquire(ctx, name)
		if err != nil {
			return codeModeFailure(err), nil
		}
		defer release()
		if outcome, changed := changed(ctx); changed {
			return outcome, nil
		}
		// Local-only operations never leave this process, so the catalog decides
		// the execution host before anything is forwarded.
		if result, handled := callAgentCodeLocal(ctx, name, input, readWrite); handled {
			return result, nil
		}
		if descriptor, ok := agentapi.CodeOperationDescriptor(name); ok && descriptor.CodeAccess == agentapi.CodeIndependent {
			return callAgentCodeOperation(ctx, name, input, readWrite), nil
		}
		if outcome, handled := forwarder.call(ctx, name, input, readWrite); handled {
			return outcome, nil
		}
		return callAgentCodeOperation(ctx, name, input, readWrite), nil
	}, nil
}

func codeModeConfigurationChanged(what string) agentcode.CallOutcome {
	return codeModeFailure(fmt.Errorf(`{"code":"code_mode_configuration_changed","message":"vault configuration changed (%s); close and reconnect before further calls"}`, what))
}

// agentOpForwarder routes catalog operations to the vault runtime (SPEC-0104
// US4). Calls may overlap, so mutable connection state is synchronized without
// holding the lock across the runtime request.
// Once the runtime proves unusable the whole connection stays in-process:
// alternating hosts would make freshness and write effects unexplainable.
type agentOpForwarder struct {
	mu           sync.Mutex
	client       *appruntime.Client
	options      appruntime.EnsureOptions
	initializing chan struct{}
	generation   uint64
	reason       string
	reported     bool
}

func newAgentOpForwarder(vaultPath string) *agentOpForwarder {
	forwarder := &agentOpForwarder{}
	executable, err := os.Executable()
	if err != nil {
		forwarder.reason = err.Error()
		return forwarder
	}
	_, localCfg, _ := obsidian.FindLocalConfig(vaultPath)
	forwarder.options = appruntime.EnsureOptions{
		VaultPath:  vaultPath,
		Executable: executable,
		BuildID:    appruntime.BuildID(version.Version, executable),
		Autostart:  appruntime.AutostartEnabled(localCfg),
		Wait:       true,
		Budget:     appruntime.SpawnBudget,
	}
	return forwarder
}

func agentOpFallbackReason(err error) string {
	switch {
	case err == nil:
		return "no vault runtime client"
	case errors.Is(err, appruntime.ErrAutostartDisabled):
		return "vault runtime auto-start is disabled"
	default:
		return err.Error()
	}
}

// call reports handled=false when the caller must execute in-process instead.
func (f *agentOpForwarder) call(ctx context.Context, name string, input map[string]any, readWrite bool) (agentcode.CallOutcome, bool) {
	client, err := f.clientForCall(ctx)
	if err != nil {
		// A canceled first attempt belongs to this request. Later calls may
		// still attach to the runtime, so do not make fallback permanent.
		return codeModeFailure(err), true
	}
	if client == nil {
		f.mu.Lock()
		f.reportLocked()
		f.mu.Unlock()
		return agentcode.CallOutcome{}, false
	}
	outcome, generation, err := postAgentOp(ctx, client, name, appruntime.AgentOpRequest{
		Input: input, ReadWrite: readWrite, SessionID: codeModeString(input, "sessionId"),
	})
	if err != nil {
		if ctx.Err() != nil {
			// Cancellation and deadlines belong to this call, not to the runtime.
			return codeModeFailure(err), true
		}
		f.fallback(client, err.Error())
		return agentcode.CallOutcome{}, false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if generation != 0 && f.generation != 0 && generation != f.generation {
		return codeModeConfigurationChanged("runtime configuration generation"), true
	}
	f.generation = generation
	return outcome, true
}

func (f *agentOpForwarder) clientForCall(ctx context.Context) (*appruntime.Client, error) {
	for {
		f.mu.Lock()
		if f.client != nil || f.reason != "" {
			client := f.client
			f.mu.Unlock()
			return client, nil
		}
		if waiting := f.initializing; waiting != nil {
			f.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-waiting:
			}
			continue
		}
		waiting := make(chan struct{})
		f.initializing = waiting
		options := f.options
		f.mu.Unlock()

		result, err := appruntime.Ensure(ctx, options)
		f.mu.Lock()
		if ctx.Err() == nil {
			if err != nil || result.Client == nil {
				f.reason = agentOpFallbackReason(err)
			} else {
				f.client = result.Client
			}
		}
		f.initializing = nil
		close(waiting)
		client := f.client
		f.mu.Unlock()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return client, nil
	}
}

func (f *agentOpForwarder) fallback(client *appruntime.Client, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.client != client {
		return
	}
	f.client, f.reason = nil, reason
	f.reportLocked()
}

func (f *agentOpForwarder) reportLocked() {
	if f.reported || f.reason == "" {
		return
	}
	f.reported = true
	fmt.Fprintf(os.Stderr, "rzm agent code serve: executing operations in this process (%s)\n", f.reason)
}

// postAgentOp decodes the runtime's outcome with raw payload and diagnostic so
// forwarded results keep the exact JSON an in-process call would have produced.
func postAgentOp(ctx context.Context, client *appruntime.Client, name string, payload appruntime.AgentOpRequest) (agentcode.CallOutcome, uint64, error) {
	request, err := client.NewRequest(ctx, http.MethodPost, appruntime.AgentOpsPathPrefix+name, payload)
	if err != nil {
		return agentcode.CallOutcome{}, 0, err
	}
	response, err := client.HTTP.Do(request)
	if err != nil {
		return agentcode.CallOutcome{}, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return agentcode.CallOutcome{}, 0, fmt.Errorf("vault runtime answered %s for agent operation %s", response.Status, name)
	}
	var wire struct {
		OK         bool            `json:"ok"`
		ExitCode   int             `json:"exitCode"`
		Payload    json.RawMessage `json:"payload"`
		Stdout     string          `json:"stdout"`
		Stderr     string          `json:"stderr"`
		Diagnostic json.RawMessage `json:"diagnostic"`
	}
	if err := json.NewDecoder(response.Body).Decode(&wire); err != nil {
		return agentcode.CallOutcome{}, 0, err
	}
	outcome := agentcode.CallOutcome{OK: wire.OK, ExitCode: wire.ExitCode, Stdout: wire.Stdout, Stderr: wire.Stderr}
	if len(wire.Payload) > 0 {
		outcome.Payload = wire.Payload
	}
	diagnostic, generation := takeConfigGeneration(wire.Diagnostic)
	outcome.Diagnostic = diagnostic
	return outcome, generation, nil
}

// takeConfigGeneration strips the runtime's configuration generation from the
// diagnostic so the generated client sees the same shape either host produces.
func takeConfigGeneration(raw json.RawMessage) (any, uint64) {
	if len(raw) == 0 {
		return nil, 0
	}
	var object map[string]any
	if json.Unmarshal(raw, &object) != nil || object == nil {
		var other any
		if json.Unmarshal(raw, &other) != nil {
			return nil, 0
		}
		return other, 0
	}
	generation := uint64(0)
	if value, ok := object[agentOpConfigGenerationKey].(float64); ok && value > 0 {
		generation = uint64(value)
	}
	delete(object, agentOpConfigGenerationKey)
	if len(object) == 0 {
		return nil, generation
	}
	return object, generation
}

func readCodeModeConfig(path string) ([]byte, error) {
	data, err := fileio.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return data, err
}

func callAgentCodeOperation(ctx context.Context, name string, input map[string]any, readWrite bool) agentcode.CallOutcome {
	if result, handled := callAgentCodeLocal(ctx, name, input, readWrite); handled {
		return result
	}
	descriptor, ok := agentapi.CodeOperationDescriptor(name)
	if !ok || descriptor.Handler == nil {
		return codeModeFailure(fmt.Errorf("code operation %q has no application handler", name))
	}
	// Preserve the agent CLI's plan-only boundaries. Connection write authority
	// does not introduce additional MCP-only mutation modes.
	if (name == "node_link" && input["ensure"] == "apply") ||
		(name == "file_context" && input["ensureLinkTargets"] == "apply") {
		return codeModeFailure(fmt.Errorf(`{"code":"apply_requires_read_write","message":"this agent operation is plan-only; use its documented write-capable mutation surface"}`))
	}
	budget := 0
	if value, ok := input["budgetChars"].(float64); ok {
		budget = int(value)
	}
	if name == "semantic_query" && input["timings"] == true {
		ctx = indexingperf.WithCollector(ctx, indexingperf.NewSemanticQueryCollector())
		ctx = search.WithTimings(ctx, &search.Timings{})
	}
	cfg, runtime, err := prepareAgentJSONTool(ctx, budget, name, agentOperationIDForToolName(name), input)
	if err != nil {
		return codeModeFailure(err)
	}
	if runtime != nil {
		defer runtime.Close()
	}
	payload, err := agentapi.CallJSON(ctx, cfg, name, input)
	if err != nil {
		return codeModeFailure(err)
	}
	// Payload is the structured result; repeating it as stdout text doubles what the agent reads.
	return agentcode.CallOutcome{OK: true, Payload: json.RawMessage(payload)}
}

func codeModeFailure(err error) agentcode.CallOutcome {
	var diagnostic any
	if json.Unmarshal([]byte(err.Error()), &diagnostic) != nil {
		diagnostic = map[string]any{"error": err.Error()}
	}
	data, _ := json.Marshal(diagnostic)
	return agentcode.CallOutcome{ExitCode: 1, Stderr: string(data) + "\n", Diagnostic: diagnostic}
}
