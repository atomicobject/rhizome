package cmd

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/app/indexing"
	"github.com/atomicobject/rhizome/pkg/app/mcp"
	"github.com/atomicobject/rhizome/pkg/app/oneshotruntime"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// runBackgroundIndex performs the boot catch-up index. It acquires no lock: the
// vault runtime submits it as a lane job and the lane owns .rhizome/index.lock
// and the yield policy (SPEC-0104 US3). ctx is the job's context, so an
// explicit index job or an external priority request cancels this run.
func runBackgroundIndex(ctx context.Context, noteMetadata notemeta.Indexer, vaultPath string, vaultDef obsidian.VaultDefinition, dbg bool, label string) error {
	if dbg {
		log.Printf("%s: starting background index", label)
	}

	bar := newCLIProgressBar(os.Stderr)
	restoreLogs := withProgressLogOutput(bar, os.Stderr, dbg)

	err := indexing.RunUnifiedCore(ctx, indexing.UnifiedOptions{
		VaultPath:    vaultPath,
		VaultDef:     vaultDef,
		NoteMetadata: noteMetadata,
		ProgressBar:  bar,
		Verbose:      dbg,
		Vacuum:       false,
		// Automatic catch-up never rewrites the user's config.yml.
		SkipConfigPersistence: true,
	})

	restoreLogs()
	bar.Close()

	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			if dbg {
				log.Printf("%s: background index yielded: %v", label, ctxErr)
			}
			return ctxErr
		}
		return fmt.Errorf("background index failed: %w", err)
	}
	if dbg {
		log.Printf("%s: background index complete", label)
	} else {
		fmt.Fprintln(os.Stderr, "Watching file system...")
	}
	return nil
}

func buildSuppressTags() []string {
	tags := []string{"no-prompt"}
	if len(suppressTags) > 0 {
		tags = append(tags, suppressTags...)
	}
	if noSuppress {
		return []string{}
	}
	return tags
}

func effectiveAgentBudgetChars(vaultPath string, budgetOverride int) int {
	if budgetOverride > 0 {
		return budgetOverride
	}
	budget := agentCLIBudgetFloor
	if cfgBudget := obsidian.GetBudgetChars(vaultPath); cfgBudget > budget {
		budget = cfgBudget
	}
	return budget
}

func buildAgentConfigWithRequirements(ctx context.Context, budgetOverride int, toolName string, requirements bootstrap.RuntimeRequirements) (agentapi.Config, *bootstrap.LiveRuntime, error) {
	return buildAgentConfigWithOperationRequirements(ctx, budgetOverride, toolName, agentOperationIDForToolName(toolName), requirements)
}

func buildAgentConfigForOperation(ctx context.Context, budgetOverride int, toolName string, operationID oneshotruntime.OperationID) (agentapi.Config, *bootstrap.LiveRuntime, error) {
	if declaration, ok := oneshotruntime.DefaultRegistry().Declaration(operationID); ok && declaration.StaticPlan != nil {
		return buildAgentConfigWithOperationRequirementsAndPlan(ctx, budgetOverride, toolName, operationID, declaration.StaticPlan.RuntimeRequirements(), declaration.StaticPlan)
	}
	if declaration, ok := oneshotruntime.DefaultRegistry().Declaration(operationID); ok && declaration.PlanSource == oneshotruntime.PlanSourceExempt && declaration.Exemption == oneshotruntime.RuntimeExemptionFullRuntimeParity {
		// This exemption is registry-declared until output parity freezes a
		// narrower source-snippet plan. Do not infer it from a command name.
		return buildAgentConfigWithOperationRequirementsAndPlan(ctx, budgetOverride, toolName, operationID, bootstrap.RuntimeRequirements{}, declaration.ExemptionPlan)
	}
	plan, err := oneshotruntime.RequestPlan(operationID, nil)
	if err != nil {
		return agentapi.Config{}, nil, fmt.Errorf("operation %q request plan: %w", operationID, err)
	}
	return buildAgentConfigWithPlan(ctx, budgetOverride, toolName, operationID, plan)
}

// buildAgentConfigForRequestPlan composes the already-normalized request plan.
// IMPORTANT: planning happens after command input is known; command names do
// not decide whether a response needs cache, index, or provider readiness.
func buildAgentConfigForRequestPlan(ctx context.Context, budgetOverride int, toolName string, operationID oneshotruntime.OperationID, plan oneshotruntime.Plan) (agentapi.Config, *bootstrap.LiveRuntime, error) {
	if err := plan.Validate(); err != nil {
		return agentapi.Config{}, nil, err
	}
	if !plan.RequiresRuntime() {
		cfg, err := buildRuntimeFreeAgentConfig(budgetOverride)
		if err != nil {
			return agentapi.Config{}, nil, err
		}
		cfg.IntelStorePolicy = agentOperationIntelStorePolicyForPlan(operationID, &plan)
		return cfg, nil, nil
	}
	return buildAgentConfigWithPlan(ctx, budgetOverride, toolName, operationID, plan)
}

func buildRuntimeFreeAgentConfig(budgetOverride int) (agentapi.Config, error) {
	vault := &obsidian.Vault{Name: vaultName}
	if vaultName == "" {
		name, err := vault.DefaultName()
		if err != nil {
			return agentapi.Config{}, err
		}
		vault.Name = name
	}
	vaultDef, err := vault.Definition()
	if err != nil {
		return agentapi.Config{}, err
	}
	return agentapi.Config{
		Vault:               vault,
		VaultPath:           vaultDef.BasePath(),
		VaultDef:            vaultDef,
		BudgetCharsOverride: effectiveAgentBudgetChars(vaultDef.BasePath(), budgetOverride),
		Debug:               debug,
		SuppressedTags:      buildSuppressTags(),
	}, nil
}

func buildAgentConfigWithPlan(ctx context.Context, budgetOverride int, toolName string, operationID oneshotruntime.OperationID, plan oneshotruntime.Plan) (agentapi.Config, *bootstrap.LiveRuntime, error) {
	return buildAgentConfigWithOperationRequirementsAndPlan(ctx, budgetOverride, toolName, operationID, plan.RuntimeRequirements(), &plan)
}

func buildAgentConfigWithOperationRequirements(ctx context.Context, budgetOverride int, toolName string, operationID oneshotruntime.OperationID, requirements bootstrap.RuntimeRequirements) (agentapi.Config, *bootstrap.LiveRuntime, error) {
	return buildAgentConfigWithOperationRequirementsAndPlan(ctx, budgetOverride, toolName, operationID, requirements, nil)
}

func buildAgentConfigWithOperationRequirementsAndPlan(ctx context.Context, budgetOverride int, toolName string, operationID oneshotruntime.OperationID, requirements bootstrap.RuntimeRequirements, plan *oneshotruntime.Plan) (agentapi.Config, *bootstrap.LiveRuntime, error) {
	readOnlyCodeIndex := requirements.Includes(bootstrap.RuntimeCapabilityCodeIndex)
	if plan != nil {
		// StoreAccess is authoritative for planned requests. In particular, the
		// typed full-runtime parity exemption must retain its live store instead
		// of becoming read-only merely because full requirements include CodeIndex.
		readOnlyCodeIndex = plan.StoreAccess == oneshotruntime.StoreExistingReadOnly
	}
	queryProvidersOnly := agentPlanUsesQueryProvidersOnly(operationID, plan)
	liveOpts := bootstrap.LiveOptions{
		VaultName:         vaultName,
		Debug:             debug,
		SkipCacheWarmup:   true,
		DisableWatchHub:   true,
		DisableLeaderWork: true,
		// Latency-sensitive read-only agent tools do not own full-store
		// integrity validation. Explicit indexing retains the checked open.
		SkipIntelIntegrityCheck: readOnlyCodeIndex,
		ReadOnlyCodeIndex:       readOnlyCodeIndex,
		// A request-derived semantic plan may embed only query text. It must not
		// create, migrate, or repair embedding stores merely because the shared
		// find-connections handler can also run in a live MCP runtime.
		QueryProvidersOnly: queryProvidersOnly,
		Requirements:       requirements,
	}
	if plan != nil && plan.Session == oneshotruntime.SessionNone {
		liveOpts.DisableSessionStore = true
	}
	if plan != nil && plan.Session == oneshotruntime.SessionExistingOnly {
		liveOpts.SessionOnlyExisting = true
	}
	if operationUsesStaticIndexedReadOnlyPlan(operationID) {
		liveOpts = bootstrap.IndexedReadOnlyRuntimeOptions(liveOpts)
	}
	rt, awaitPlan, err := buildAgentOneShotRuntime(ctx, operationID, liveOpts)
	if err != nil {
		return agentapi.Config{}, nil, err
	}
	if plan != nil && planHasReadiness(*plan, oneshotruntime.ReadinessSession) {
		// Session dedupe is response shaping only. Its existing-only writer may be
		// absent or fail to open; preserve output and let the tracker degrade.
		_ = rt.WaitForSession(ctx)
	}
	needsWarmCache := planHasReadinessValue(plan, oneshotruntime.ReadinessSearch) ||
		planHasReadinessValue(plan, oneshotruntime.ReadinessNoteCache) ||
		(plan == nil && requirements.Includes(bootstrap.RuntimeCapabilitySearch))
	usesMetadataIndex := planHasReadinessValue(plan, oneshotruntime.ReadinessCodeIndex) ||
		(plan == nil && requirements.Includes(bootstrap.RuntimeCapabilityCodeIndex))
	// Unlike the long-lived serve process, `rzm agent` is a one-shot local command.
	// It pays the readiness cost up front for tools whose output depends on
	// fresh cache/search state, then leaves semantic/code capabilities to the
	// LiveRuntime view below so handlers can still produce targeted
	// unavailable errors when optional indexes are missing.
	if needsWarmCache {
		phase := agentRuntimeTimingPhase(toolName, indexingperf.AgentStartPhaseRuntimeSearch, indexingperf.SemanticQueryPhaseRuntimeSearch)
		phaseCtx := indexingperf.WithPhase(ctx, phase)
		done := indexingperf.StartSpan(phaseCtx, phase)
		err := rt.WaitForSearch(phaseCtx)
		done(err)
		if err != nil {
			rt.Close()
			return agentapi.Config{}, nil, err
		}
	}
	if planHasReadinessValue(plan, oneshotruntime.ReadinessSemantic) ||
		(plan == nil && requirements.Includes(bootstrap.RuntimeCapabilitySemantic)) {
		semanticPhase := agentRuntimeTimingPhase(toolName, indexingperf.AgentStartPhaseRuntimeSemantic, indexingperf.SemanticQueryPhaseRuntimeSemantic)
		semanticCtx := indexingperf.WithPhase(ctx, semanticPhase)
		doneSemantic := indexingperf.StartSpan(semanticCtx, semanticPhase)
		semanticErr := rt.WaitForSemantic(semanticCtx)
		doneSemantic(semanticErr)
		// Semantic providers are optional retrieval lanes. Preserve the existing
		// lexical/indexed degradation when provider setup is unavailable.
		if semanticErr != nil && debug {
			log.Printf("agent semantic provider unavailable for %s: %v", toolName, semanticErr)
		}
	}
	var indexedUnavailable *actions.IndexedContextFreshness
	if usesMetadataIndex {
		phase := agentRuntimeTimingPhase(toolName, indexingperf.AgentStartPhaseRuntimeCode, indexingperf.SemanticQueryPhaseRuntimeCode)
		phaseCtx := indexingperf.WithPhase(ctx, phase)
		done := indexingperf.StartSpan(phaseCtx, phase)
		var err error
		if awaitPlan != nil {
			err = awaitPlan(phaseCtx)
		} else {
			err = rt.WaitForCodeIndex(phaseCtx)
		}
		done(err)
		if err != nil {
			state := indexedContextUnavailableForError(err)
			indexedUnavailable = &state
		} else if agentPlanRequiresCodeIndexFreshness(plan) {
			expectedScopeHash := ""
			if rt.LocalCfg != nil {
				expectedScopeHash = rt.LocalCfg.ScopeConfigHash()
			}
			freshness := indexedSemanticQueryFreshness(ctx, expectedScopeHash, rt.IntelStore())
			if freshness.State != actions.IndexedContextAvailable {
				indexedUnavailable = &freshness
			}
		}
		if err != nil && debug {
			log.Printf("agent metadata index unavailable for %s: %v", toolName, err)
		}
	}
	if cacheSvc := rt.Cache(); cacheSvc != nil && needsWarmCache {
		phase := agentRuntimeTimingPhase(toolName, indexingperf.AgentStartPhaseNoteCrawl, indexingperf.SemanticQueryPhaseNoteCache)
		phaseCtx := indexingperf.WithPhase(ctx, phase)
		done := indexingperf.StartSpan(phaseCtx, phase)
		err := cacheSvc.EnsureReady(phaseCtx)
		done(err)
		if err != nil {
			rt.Close()
			return agentapi.Config{}, nil, err
		}
	}

	intelStore := rt.IntelStore()

	cfg := agentapi.Config{
		Vault:                        rt.Vault,
		VaultPath:                    rt.VaultPath,
		VaultDef:                     rt.VaultDef,
		BudgetCharsOverride:          effectiveAgentBudgetChars(rt.VaultPath, budgetOverride),
		Debug:                        debug,
		SuppressedTags:               buildSuppressTags(),
		Cache:                        rt.Cache(),
		IntelStore:                   intelStore,
		Runtime:                      rt,
		NoteMetadata:                 rt.NoteMetadataIndexer(),
		IndexedContextUnavailable:    indexedUnavailable,
		IndexedReadOnlyFileContext:   toolName == "file_context",
		IndexedReadOnlySemanticQuery: toolName == "semantic_query",
		IntelStorePolicy:             agentOperationIntelStorePolicyForPlan(operationID, plan),
	}
	return cfg, rt, nil
}

func agentPlanRequiresCodeIndexFreshness(plan *oneshotruntime.Plan) bool {
	return plan != nil && plan.CodeIndexFreshness == oneshotruntime.CodeIndexFreshnessCurrent
}

func planHasReadiness(plan oneshotruntime.Plan, want oneshotruntime.Readiness) bool {
	for _, readiness := range plan.Readiness {
		if readiness == want {
			return true
		}
	}
	return false
}

func planHasReadinessValue(plan *oneshotruntime.Plan, want oneshotruntime.Readiness) bool {
	return plan != nil && planHasReadiness(*plan, want)
}

func agentPlanUsesQueryProvidersOnly(operationID oneshotruntime.OperationID, plan *oneshotruntime.Plan) bool {
	if plan == nil {
		plan = declaredPlan(operationID)
	}
	return plan != nil && plan.StoreAccess == oneshotruntime.StoreExistingReadOnly && planHasReadiness(*plan, oneshotruntime.ReadinessSemantic)
}

func agentOperationIDForToolName(toolName string) oneshotruntime.OperationID {
	return oneshotruntime.OperationID("agent." + strings.ReplaceAll(toolName, "_", "-"))
}

func operationUsesStaticIndexedReadOnlyPlan(operationID oneshotruntime.OperationID) bool {
	declaration, ok := oneshotruntime.DefaultRegistry().Declaration(operationID)
	return ok && declaration.StaticPlan != nil && declaration.StaticPlan.StoreAccess == oneshotruntime.StoreExistingReadOnly && declaration.StaticPlan.Session == oneshotruntime.SessionNone
}

func declaredStaticPlan(operationID oneshotruntime.OperationID) *oneshotruntime.Plan {
	declaration, ok := oneshotruntime.DefaultRegistry().Declaration(operationID)
	if !ok {
		return nil
	}
	return declaration.StaticPlan
}

func declaredPlan(operationID oneshotruntime.OperationID) *oneshotruntime.Plan {
	if plan := declaredStaticPlan(operationID); plan != nil {
		return plan
	}
	plan, err := oneshotruntime.RequestPlan(operationID, nil)
	if err != nil {
		return nil
	}
	return &plan
}

func agentOperationIntelStorePolicyForPlan(operationID oneshotruntime.OperationID, plan *oneshotruntime.Plan) mcp.IntelStorePolicy {
	if plan == nil {
		plan = declaredPlan(operationID)
	}
	if plan != nil && plan.StoreAccess == oneshotruntime.StoreExistingReadOnly {
		return mcp.IntelStoreManagedReadOnly
	}
	if plan != nil && plan.NoteState == oneshotruntime.NoteStateLive {
		return mcp.IntelStoreNoFallback
	}
	return mcp.IntelStoreFallbackAllowed
}

func indexedContextUnavailableForError(err error) actions.IndexedContextFreshness {
	state := actions.IndexedContextFreshness{
		State: actions.IndexedContextMissing, WarningCode: "indexed-context-missing", Remediation: "rzm index",
	}
	var future *migration.ErrFutureSchema
	var drift *migration.ErrSchemaDrift
	var metadataErr embeddings.MetadataError
	if errors.As(err, &future) || errors.As(err, &drift) || errors.As(err, &metadataErr) {
		state = actions.IndexedContextFreshness{
			State: actions.IndexedContextIncompatible, WarningCode: "indexed-context-incompatible", Remediation: "rzm index --rebuild",
		}
	}
	return state
}

func indexedSemanticQueryFreshness(ctx context.Context, expectedScopeHash string, store *semdb.Store) actions.IndexedContextFreshness {
	evidence := actions.IndexedContextFreshnessEvidence{SchemaCompatible: true}
	if store == nil {
		return actions.EvaluateIndexedContextFreshness(evidence)
	}

	snapshot, err := store.CodeIndexPrerequisiteSnapshot(ctx)
	if err != nil {
		return actions.EvaluateIndexedContextFreshness(evidence)
	}

	evidence.HasIndexerVersion = snapshot.IndexerVersionPresent
	evidence.IndexerVersion = snapshot.IndexerVersion
	evidence.ExpectedVersion = codeanchor.IndexerVersion
	evidence.HasScopeHash = snapshot.ScopeConfigHashPresent
	evidence.ScopeHash = snapshot.ScopeConfigHash
	evidence.ExpectedScopeHash = expectedScopeHash
	return actions.EvaluateIndexedContextFreshness(evidence)
}

func agentRuntimeTimingPhase(toolName, defaultPhase, semanticQueryPhase string) string {
	if toolName == "semantic_query" {
		return semanticQueryPhase
	}
	return defaultPhase
}
