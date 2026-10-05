package agentstart

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/agentcode"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/app/noteownership"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type Response struct {
	SessionID      string                              `json:"sessionId,omitempty"`
	DurationMs     int64                               `json:"durationMs"`
	SurfaceCommand string                              `json:"surfaceCommand,omitempty"`
	Notes          []string                            `json:"notes,omitempty"`
	Code           agentcode.SurfaceResponse           `json:"code"`
	VaultContext   agentapi.ContextTextResponse        `json:"vaultContext"`
	Ontology       *actions.OntologyVaultSummary       `json:"ontology,omitempty"`
	OntologyError  string                              `json:"ontologyError,omitempty"`
	Diagnostics    *indexingperf.AgentStartDiagnostics `json:"diagnostics,omitempty"`
}

type Request struct {
	Profile, Intent               string
	ContextFiles, Files           []string
	BudgetChars, SubmoduleDepth   int
	SkipAnchors, SkipEmbeds       bool
	IncludeTags, RecencyCascade   bool
	GraphSummary, IncludeOntology bool
	Timings                       bool
	SessionID                     string
}

type Dependencies struct {
	ResolveVault func(context.Context) (obsidian.VaultDefinition, error)
	Formats      func() (noteformat.Runtime, error)
	BuildConfig  func(context.Context, int, bootstrap.RuntimeRequirements) (agentapi.Config, func(), error)
}

func Execute(ctx context.Context, deps Dependencies, request Request) (Response, error) {
	started := time.Now()
	var collector *indexingperf.Collector
	if request.Timings {
		collector = indexingperf.New()
		ctx = indexingperf.WithCollector(ctx, collector)
	}
	vaultDef, err := deps.ResolveVault(ctx)
	if err != nil {
		return Response{}, err
	}
	formats, err := deps.Formats()
	if err != nil {
		return Response{}, err
	}
	plan, err := PlanRequest(vaultDef, formats, RequestInput{Profile: request.Profile, ContextFiles: request.ContextFiles, Files: request.Files, IncludeOntology: request.IncludeOntology, GraphSummary: request.GraphSummary})
	if err != nil {
		return Response{}, err
	}
	cfg, cleanup, err := deps.BuildConfig(ctx, request.BudgetChars, plan.Requirements)
	if err != nil {
		return Response{}, err
	}
	if cleanup != nil {
		defer cleanup()
	}
	args := map[string]any{"requestScope": string(plan.Scope), "includeOntology": request.IncludeOntology, "skipAnchors": request.SkipAnchors, "skipEmbeds": request.SkipEmbeds, "includeTags": request.IncludeTags, "recencyCascade": request.RecencyCascade, "graphSummary": request.GraphSummary}
	setString(args, "sessionId", request.SessionID)
	setString(args, "profile", request.Profile)
	setString(args, "intent", request.Intent)
	setStrings(args, "contextFiles", request.ContextFiles)
	setStrings(args, "files", request.Files)
	if request.SubmoduleDepth != 0 {
		args["submoduleDepth"] = request.SubmoduleDepth
	}
	var vaultResult agentapi.ContextTextResponse
	var vaultErr error
	var ontologySummary *actions.OntologyVaultSummary
	var ontologyErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		phaseCtx := indexingperf.WithPhase(ctx, indexingperf.AgentStartPhaseVaultContext)
		done := indexingperf.StartSpan(phaseCtx, indexingperf.AgentStartPhaseVaultContext)
		raw, callErr := agentapi.CallJSON(phaseCtx, cfg, "vault_context", args)
		done(callErr)
		if callErr != nil {
			vaultErr = callErr
			return
		}
		vaultErr = json.Unmarshal(raw, &vaultResult)
	}()
	if request.IncludeOntology {
		wg.Add(1)
		go func() { defer wg.Done(); ontologySummary, ontologyErr = BuildOntologySummary(ctx, cfg) }()
	}
	wg.Wait()
	if vaultErr != nil {
		return Response{}, vaultErr
	}
	code := agentcode.Surface()
	response := Response{SessionID: vaultResult.SessionID, DurationMs: time.Since(started).Milliseconds(), SurfaceCommand: "rzm agent surface", Notes: []string{"Run `rzm agent surface` only when you need detailed command flags, examples, or subcommands."}, Code: code, VaultContext: vaultResult, Ontology: ontologySummary}
	if ontologyErr != nil {
		response.OntologyError = ontologyErr.Error()
	}
	if collector != nil {
		collector.RecordSpanWindow(indexingperf.AgentStartPhaseTotal, started, time.Now(), nil)
		diagnostics := collector.AgentStartDiagnostics()
		response.Diagnostics = &diagnostics
	}
	return response, nil
}

type RequestInput struct {
	Profile                       string
	ContextFiles, Files           []string
	IncludeOntology, GraphSummary bool
}
type RequestPlan struct {
	Scope        actions.VaultContextRequestScope
	Requirements bootstrap.RuntimeRequirements
}

func PlanRequest(vaultDef obsidian.VaultDefinition, formats noteformat.Runtime, input RequestInput) (RequestPlan, error) {
	rich := strings.EqualFold(strings.TrimSpace(input.Profile), string(actions.ContextProfileVault)) || input.IncludeOntology || input.GraphSummary || containsNonEmpty(input.ContextFiles)
	if !rich {
		var err error
		rich, err = containsSourceReadableNoteTarget(vaultDef, formats, input.Files)
		if err != nil {
			return RequestPlan{}, fmt.Errorf("classify agent start files: %w", err)
		}
	}
	if !rich {
		return RequestPlan{Scope: actions.VaultContextRequestScopeMinimalBootstrap, Requirements: bootstrap.RequireRuntimeCapabilities()}, nil
	}
	return RequestPlan{Scope: actions.VaultContextRequestScopeIndexedBootstrap, Requirements: bootstrap.RequireRuntimeCapabilities(bootstrap.RuntimeCapabilityCodeIndex)}, nil
}

func containsSourceReadableNoteTarget(vaultDef obsidian.VaultDefinition, formats noteformat.Runtime, files []string) (bool, error) {
	if !containsNonEmpty(files) {
		return false, nil
	}
	vaultPaths, err := paths.NewVaultPaths(vaultDef.BasePath())
	if err != nil || vaultPaths.Root() == "" {
		return false, fmt.Errorf("vault base path is required")
	}
	selector, err := noteownership.CompileSelector(noteownership.SelectorInput{VaultDefinition: vaultDef, Registry: formats.Registry()})
	if err != nil {
		return false, err
	}
	for _, target := range files {
		absolute := paths.AbsFromInputWithVaultPaths(vaultPaths, vaultPaths.Root(), target)
		if absolute == "" {
			continue
		}
		if info, statErr := os.Stat(absolute.String()); statErr == nil && info.IsDir() {
			continue
		}
		rel, relErr := vaultPaths.RelStrict(absolute.String())
		if relErr != nil || rel == "" {
			continue
		}
		selection, selectErr := selector.Select(rel)
		if selectErr != nil {
			return false, selectErr
		}
		if selection.Owner != notediscovery.Note {
			continue
		}
		provider, ok := formats.Provider(selection.Provider)
		if ok && formats.CanProject(provider.Descriptor().ID) && provider.Descriptor().Capabilities.Has(noteformat.CapabilitySourceReading) {
			return true, nil
		}
	}
	return false, nil
}

func BuildOntologySummary(ctx context.Context, cfg agentapi.Config) (*actions.OntologyVaultSummary, error) {
	freshnessCtx := indexingperf.WithPhase(ctx, indexingperf.AgentStartPhaseOntologyFreshness)
	doneFreshness := indexingperf.StartSpan(freshnessCtx, indexingperf.AgentStartPhaseOntologyFreshness)
	store := cfg.GetIntelStore()
	if store == nil {
		doneFreshness(nil)
		return nil, nil
	}
	indexed, err := store.IndexedOntologySummary(freshnessCtx, 30, 3)
	doneFreshness(err)
	if err != nil {
		return nil, err
	}
	summaryCtx := indexingperf.WithPhase(ctx, indexingperf.AgentStartPhaseOntologySummary)
	doneSummary := indexingperf.StartSpan(summaryCtx, indexingperf.AgentStartPhaseOntologySummary)
	summary := &actions.OntologyVaultSummary{Available: indexed.Available, Ready: indexed.Ready, SchemaHash: indexed.SchemaHash, TotalNotes: indexed.TotalNotes, TypedNotes: indexed.TypedNotes, UntypedNotes: indexed.UntypedNotes}
	for _, count := range indexed.TypeCounts {
		summary.TypeCounts = append(summary.TypeCounts, actions.OntologyTypeCount{TypeName: count.TypeName, Count: count.Count, Examples: count.Examples})
	}
	doneSummary(nil)
	return summary, nil
}

func containsNonEmpty(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}
func setString(out map[string]any, key, value string) {
	if strings.TrimSpace(value) != "" {
		out[key] = value
	}
}
func setStrings(out map[string]any, key string, values []string) {
	if len(values) > 0 {
		out[key] = values
	}
}
