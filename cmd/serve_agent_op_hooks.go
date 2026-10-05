package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	appserve "github.com/atomicobject/rhizome/pkg/app/cli/serve"
	"github.com/atomicobject/rhizome/pkg/app/oneshotruntime"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// agentOpConfigGenerationKey returns the runtime's configuration generation to
// the code-mode host inside the outcome diagnostic, because the control route
// answers with a bare CallOutcome. The host removes the key before the outcome
// reaches the generated client, so SPEC-0092's wire shape is unchanged.
const agentOpConfigGenerationKey = "rzmConfigGeneration"

// agentOpHost executes catalog operations against the runtime's live
// configuration. It owns no runtime state of its own: every call reads the
// current snapshot so a replaced index or reopened store is observed.
type agentOpHost struct {
	rt         *bootstrap.LiveRuntime
	configPath string

	mu         sync.Mutex
	generation uint64
	definition []byte
	config     []byte
	reads      *actions.CodeModeRuntimeReads
}

// registerAgentOpHooks wires catalog operation execution for code-mode hosts
// onto the runtime's live configuration (SPEC-0104 US4).
func registerAgentOpHooks(hooks *appserve.ControlHooks, rt *bootstrap.LiveRuntime) {
	if hooks == nil || rt == nil {
		return
	}
	host := &agentOpHost{
		rt: rt, configPath: filepath.Join(rt.VaultPath, obsidian.RhizomeDirName, "config.yml"),
		reads: &actions.CodeModeRuntimeReads{Runtime: liveCodeModeReads{rt: rt}},
	}
	hooks.AgentOp = host.call
}

// call never returns a transport error for a domain failure: the route turns a
// non-nil error into HTTP 500, which the host reads as "runtime unusable".
func (h *agentOpHost) call(ctx context.Context, name string, req appruntime.AgentOpRequest) (outcome agentapi.CallOutcome, callErr error) {
	ctx, finish := observeAgentRequest(ctx, name, "runtime_http")
	defer func() {
		if panicked := recover(); panicked != nil {
			finish(false, errors.New("diagnostic boundary panicked"))
			panic(panicked)
		}
		finish(outcome.OK, callErr)
	}()
	return withConfigGeneration(h.execute(ctx, name, req), h.configGeneration()), nil
}

func (h *agentOpHost) execute(ctx context.Context, name string, req appruntime.AgentOpRequest) agentapi.CallOutcome {
	descriptor, ok := agentapi.CodeOperationDescriptor(name)
	switch {
	case !ok:
		return codeModeFailure(fmt.Errorf("code operation %q is not part of the catalog", name))
	case descriptor.CodeLocal != nil && descriptor.CodeRuntimeRead != nil && descriptor.CodeRuntimeRead(req.Input):
		return codeModeWorkflowOutcome(h.reads.Call(ctx, name, req.Input))
	case descriptor.CodeLocal != nil:
		// Local-only operations execute in the code-mode host process. A call
		// that reaches the runtime was misrouted; say so instead of degrading.
		return codeModeFailure(fmt.Errorf(`{"code":"code_mode_local_only","message":"agent operation %s executes in the code-mode host process, not the vault runtime"}`, name))
	case descriptor.Mutation != agentapi.MutationNever && !req.ReadWrite:
		return codeModeFailure(errors.New(`{"code":"write_requires_read_write","message":"this operation mutates and needs a read-write code-mode connection"}`))
	case descriptor.Handler == nil:
		return codeModeFailure(fmt.Errorf("code operation %q has no application handler", name))
	}
	input := req.Input
	if input == nil {
		input = map[string]any{}
	}
	if req.SessionID != "" {
		input["sessionId"] = req.SessionID
	}
	// Preserve the agent CLI's plan-only boundaries. Connection write authority
	// does not introduce additional MCP-only mutation modes, so the runtime
	// grants no more than the stdio connection's --read-write flag allows.
	if (name == "node_link" && input["ensure"] == "apply") ||
		(name == "file_context" && input["ensureLinkTargets"] == "apply") {
		return codeModeFailure(errors.New(`{"code":"apply_requires_read_write","message":"this agent operation is plan-only; use its documented write-capable mutation surface"}`))
	}
	budget := 0
	if value, ok := input["budgetChars"].(float64); ok {
		budget = int(value)
	}
	if name == "semantic_query" && input["timings"] == true {
		ctx = indexingperf.WithCollector(ctx, indexingperf.NewSemanticQueryCollector())
		ctx = search.WithTimings(ctx, &search.Timings{})
	}
	cfg, err := h.agentConfig(ctx, name, budget, input)
	if err != nil {
		return codeModeFailure(err)
	}
	payload, err := agentapi.CallJSON(ctx, cfg, name, input)
	if err != nil {
		return codeModeFailure(err)
	}
	// Payload is the structured result; repeating it as stdout text doubles what the agent reads.
	return agentapi.CallOutcome{OK: true, Payload: json.RawMessage(payload)}
}

// agentConfig composes the same request-derived configuration the one-shot host
// builds, but over the runtime's already-initialized capabilities. Readiness is
// awaited with the request context so a cold capability cannot pin the handler.
func (h *agentOpHost) agentConfig(ctx context.Context, name string, budget int, input map[string]any) (agentapi.Config, error) {
	operationID := agentOperationIDForToolName(name)
	plan, err := agentOpRequestPlan(ctx, operationID, input)
	if err != nil {
		return agentapi.Config{}, err
	}
	indexedUnavailable, err := h.awaitPlanReadiness(ctx, name, plan)
	if err != nil {
		return agentapi.Config{}, err
	}
	rt := h.rt
	snapshot := rt.Snapshot()
	return agentapi.Config{
		Vault:               rt.Vault,
		VaultPath:           rt.VaultPath,
		VaultDef:            rt.VaultDef,
		BudgetCharsOverride: effectiveAgentBudgetChars(rt.VaultPath, budget),
		Debug:               debug,
		SuppressedTags:      buildSuppressTags(),
		Cache:               h.cacheForPlan(ctx, plan),
		NoteMetadata:        rt.NoteMetadataIndexer(),
		Embeddings:          snapshot.NoteIndex,
		EmbedProvider:       snapshot.NoteProvider,
		EmbeddingsOn:        snapshot.NoteIndex != nil && snapshot.NoteProvider != nil,
		CodeEmbeddings:      snapshot.CodeIndex,
		CodeEmbedProvider:   snapshot.CodeProvider,
		CodeEmbeddingsOn:    snapshot.CodeIndex != nil && snapshot.CodeProvider != nil,
		IntelStore:          snapshot.IntelStore,
		SessionStore:        rt.SessionDedupeStore(),
		Runtime:             rt,
		// These keep runtime-executed results identical to the in-process
		// fallback for the same operation; they are not a latency policy.
		IndexedContextUnavailable:    indexedUnavailable,
		IndexedReadOnlyFileContext:   name == "file_context",
		IndexedReadOnlySemanticQuery: name == "semantic_query",
		IntelStorePolicy:             agentOperationIntelStorePolicyForPlan(operationID, plan),
	}, nil
}

// cacheForPlan mirrors the one-shot host's note-state policy. Only a
// cache-snapshot plan builds a note cache there, crawled fresh for the request;
// every other note state (live, selected files, none) reads source straight
// from disk. A live watcher cache can lag a completed external edit, and
// SPEC-0092 requires source reads to observe it, so the runtime hands out its
// cache only for snapshot plans and only after forcing a reconcile with disk.
func (h *agentOpHost) cacheForPlan(ctx context.Context, plan *oneshotruntime.Plan) *cache.Service {
	cacheSvc := h.rt.Cache()
	if cacheSvc == nil || plan == nil || plan.NoteState != oneshotruntime.NoteStateCacheSnapshot {
		return nil
	}
	if err := resyncCache(ctx, cacheSvc); err != nil && debug {
		log.Printf("agent op: cache resync: %v", err)
	}
	return cacheSvc
}

// resyncCache forces one full reconcile with disk and returns when it has
// completed, or when ctx ends.
func resyncCache(ctx context.Context, cacheSvc *cache.Service) error {
	cacheSvc.MarkStale()
	for {
		result, err := cacheSvc.RefreshWithResult(ctx)
		if err != nil {
			return err
		}
		if result.Resynced {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// agentOpRequestPlan mirrors prepareAgentJSONTool's planning: input is
// normalized first, because command input — not the operation name — decides
// which capabilities a response needs. A nil plan means full requirements.
func agentOpRequestPlan(ctx context.Context, operationID oneshotruntime.OperationID, input map[string]any) (*oneshotruntime.Plan, error) {
	if err := normalizeAgentRequestPlanArgs(ctx, operationID, input); err != nil {
		return nil, err
	}
	declaration, declared := oneshotruntime.DefaultRegistry().Declaration(operationID)
	if declared && declaration.StaticPlan != nil {
		return declaration.StaticPlan, nil
	}
	plan, err := oneshotruntime.RequestPlan(operationID, input)
	if err == nil {
		if err := plan.Validate(); err != nil {
			return nil, err
		}
		return &plan, nil
	}
	if errors.Is(err, oneshotruntime.ErrRequestPlanNotDeclared) && declared && declaration.PlanSource == oneshotruntime.PlanSourceExempt {
		return declaration.ExemptionPlan, nil
	}
	return nil, fmt.Errorf("operation %q has no runtime plan or exemption: %w", operationID, err)
}

func (h *agentOpHost) awaitPlanReadiness(ctx context.Context, name string, plan *oneshotruntime.Plan) (*actions.IndexedContextFreshness, error) {
	rt := h.rt
	if plan != nil && planHasReadiness(*plan, oneshotruntime.ReadinessSession) {
		// Session dedupe is response shaping only; let it degrade.
		_ = rt.WaitForSession(ctx)
	}
	needsWarmCache := plan == nil || planHasReadinessValue(plan, oneshotruntime.ReadinessSearch) || planHasReadinessValue(plan, oneshotruntime.ReadinessNoteCache)
	if needsWarmCache {
		phase := agentRuntimeTimingPhase(name, indexingperf.AgentStartPhaseRuntimeSearch, indexingperf.SemanticQueryPhaseRuntimeSearch)
		phaseCtx := indexingperf.WithPhase(ctx, phase)
		done := indexingperf.StartSpan(phaseCtx, phase)
		err := rt.WaitForSearch(phaseCtx)
		done(err)
		if err != nil {
			return nil, err
		}
	}
	if plan == nil || planHasReadinessValue(plan, oneshotruntime.ReadinessSemantic) {
		phase := agentRuntimeTimingPhase(name, indexingperf.AgentStartPhaseRuntimeSemantic, indexingperf.SemanticQueryPhaseRuntimeSemantic)
		phaseCtx := indexingperf.WithPhase(ctx, phase)
		done := indexingperf.StartSpan(phaseCtx, phase)
		err := rt.WaitForSemantic(phaseCtx)
		done(err)
		// Semantic providers are optional retrieval lanes; preserve lexical degradation.
		if err != nil && debug {
			log.Printf("runtime agent operation %s: semantic provider unavailable: %v", name, err)
		}
	}
	var indexedUnavailable *actions.IndexedContextFreshness
	if plan == nil || planHasReadinessValue(plan, oneshotruntime.ReadinessCodeIndex) {
		phase := agentRuntimeTimingPhase(name, indexingperf.AgentStartPhaseRuntimeCode, indexingperf.SemanticQueryPhaseRuntimeCode)
		phaseCtx := indexingperf.WithPhase(ctx, phase)
		done := indexingperf.StartSpan(phaseCtx, phase)
		err := rt.WaitForCodeIndex(phaseCtx)
		done(err)
		switch {
		case err != nil:
			state := indexedContextUnavailableForError(err)
			indexedUnavailable = &state
		case agentPlanRequiresCodeIndexFreshness(plan):
			// Read the current config, not the one loaded at boot: an edit to
			// scope followed by a delegated index must not leave runtime-hosted
			// reads reporting a stale index until the runtime restarts.
			expectedScopeHash := ""
			if cfg, loadErr := obsidian.LoadLocalConfig(rt.VaultPath); loadErr == nil && cfg != nil {
				expectedScopeHash = cfg.ScopeConfigHash()
			} else if rt.LocalCfg != nil {
				expectedScopeHash = rt.LocalCfg.ScopeConfigHash()
			}
			if freshness := indexedSemanticQueryFreshness(ctx, expectedScopeHash, rt.IntelStore()); freshness.State != actions.IndexedContextAvailable {
				indexedUnavailable = &freshness
			}
		}
	}
	if cacheSvc := rt.Cache(); cacheSvc != nil && needsWarmCache {
		phase := agentRuntimeTimingPhase(name, indexingperf.AgentStartPhaseNoteCrawl, indexingperf.SemanticQueryPhaseNoteCache)
		phaseCtx := indexingperf.WithPhase(ctx, phase)
		done := indexingperf.StartSpan(phaseCtx, phase)
		err := cacheSvc.EnsureReady(phaseCtx)
		done(err)
		if err != nil {
			return nil, err
		}
	}
	return indexedUnavailable, ctx.Err()
}

// configGeneration bumps whenever the vault definition or repository config
// this runtime serves changes, so a long-lived code-mode connection learns that
// its start-of-connection assumptions no longer hold.
func (h *agentOpHost) configGeneration() uint64 {
	var definition []byte
	if h.rt.Vault != nil {
		if def, err := h.rt.Vault.Definition(); err == nil {
			definition, _ = json.Marshal(def)
		}
	}
	config, _ := readCodeModeConfig(h.configPath)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.generation == 0 || !bytes.Equal(definition, h.definition) || !bytes.Equal(config, h.config) {
		h.generation++
		h.definition, h.config = definition, config
	}
	return h.generation
}

func withConfigGeneration(outcome agentapi.CallOutcome, generation uint64) agentapi.CallOutcome {
	diagnostic := map[string]any{}
	switch existing := outcome.Diagnostic.(type) {
	case nil:
	case map[string]any:
		for key, value := range existing {
			diagnostic[key] = value
		}
	default:
		// An unexpected diagnostic shape stays intact; the host keeps the
		// generation it already knows rather than losing the diagnostic.
		return outcome
	}
	diagnostic[agentOpConfigGenerationKey] = generation
	outcome.Diagnostic = diagnostic
	return outcome
}
