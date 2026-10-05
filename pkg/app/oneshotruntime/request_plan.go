package oneshotruntime

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/app/mcp"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
)

// ErrRequestPlanNotDeclared marks an operation that remains on its previously
// established composition path. Invalid normalized input is not this error and
// must never silently select a broader legacy runtime.
var ErrRequestPlanNotDeclared = errors.New("request plan is not declared")

const (
	// FilesRequestPlanMaxDepthArg is the decoded max depth supplied by the
	// shared files request normalizer. Raw continuation tokens have no useful
	// planning depth until this value exists.
	FilesRequestPlanMaxDepthArg = "filesRequestPlanMaxDepth"
	// FilesRequestPlanHasPathInputsArg reports whether the normalized files
	// request can enumerate indexed code paths.
	FilesRequestPlanHasPathInputsArg = "filesRequestPlanHasPathInputs"
)

// NormalizeRequestPlanArgs derives neutral planning facts from a shared agent
// request. The command adapter supplies vault paths and the composed note-format
// runtime when they are available; this package owns path resolution and
// filesystem classification so other one-shot fronts can reuse the same policy.
func NormalizeRequestPlanArgs(operationID OperationID, args map[string]any, vaultPaths *paths.VaultPaths, noteFormats *noteformat.Runtime) error {
	if operationID != "agent.files" {
		return nil
	}
	facts, err := mcp.NormalizeFilesRequestPlan(args)
	if err != nil {
		if rawToken, _ := args["continuationToken"].(string); rawToken != "" {
			return fmt.Errorf("invalid continuationToken: %w", err)
		}
		return err
	}
	if facts.HasPathInputs && facts.OnlyFileInputs && filesAreExistingNotes(vaultPaths, noteFormats, facts.FileInputs) {
		facts.HasPathInputs = false
	}
	args[FilesRequestPlanMaxDepthArg] = facts.MaxDepth
	args[FilesRequestPlanHasPathInputsArg] = facts.HasPathInputs
	return nil
}

func filesAreExistingNotes(vaultPaths *paths.VaultPaths, noteFormats *noteformat.Runtime, inputs []string) bool {
	if vaultPaths == nil || vaultPaths.Root() == "" || noteFormats == nil || len(inputs) == 0 {
		return false
	}
	for _, input := range inputs {
		rel, abs, err := paths.ResolveNotePathInputWithVaultPaths(*vaultPaths, input)
		if err != nil {
			return false
		}
		provider, claimed := noteFormats.ProviderForPath(paths.RelPath(rel))
		if !claimed || !noteFormats.CanProject(provider.Descriptor().ID) || !provider.Descriptor().Capabilities.Has(noteformat.CapabilitySourceReading) {
			return false
		}
		info, err := os.Stat(abs.String())
		if err != nil || !info.Mode().IsRegular() {
			return false
		}
	}
	return true
}

// RequestPlan compiles a one-shot plan after command input is normalized. It
// does not dispatch tools or replace the catalog. Its StoreAccess and
// Unavailable fields are composition authority for planned agent paths.
func RequestPlan(operationID OperationID, args map[string]any) (Plan, error) {
	switch operationID {
	case "agent.semantic-query":
		return SemanticQueryPlan(args), nil
	case "agent.files":
		maxDepth := intArg(args, "maxDepth")
		if _, normalized := args[FilesRequestPlanMaxDepthArg]; normalized {
			// Presence is authoritative, including a decoded continuation depth of
			// zero. The shared handler ignores conflicting raw flags for cursors.
			maxDepth = intArg(args, FilesRequestPlanMaxDepthArg)
		}
		if maxDepth > 0 {
			plan := Plan{
				Capabilities:       []bootstrap.RuntimeCapability{bootstrap.RuntimeCapabilitySearch, bootstrap.RuntimeCapabilityCodeIndex, bootstrap.RuntimeCapabilityCodeRefDiscovery},
				Readiness:          []Readiness{ReadinessSearch, ReadinessNoteCache, ReadinessCodeIndex},
				StoreAccess:        StoreExistingReadOnly,
				CodeIndexFreshness: CodeIndexFreshnessCurrent,
				NoteState:          NoteStateCacheSnapshot,
			}
			if hasSessionID(args) {
				plan.Session = SessionExistingOnly
				plan.Readiness = append(plan.Readiness, ReadinessSession)
			}
			// Graph expansion is an enrichment of the live file listing. A missing
			// managed reader must leave the established live-only payload intact.
			plan.Unavailable = UnavailableDegrade
			return plan, nil
		}
		if boolArg(args, FilesRequestPlanHasPathInputsArg) {
			plan := Plan{
				Capabilities:       []bootstrap.RuntimeCapability{bootstrap.RuntimeCapabilityCodeIndex},
				Readiness:          []Readiness{ReadinessCodeIndex},
				StoreAccess:        StoreExistingReadOnly,
				CodeIndexFreshness: CodeIndexFreshnessCurrent,
				NoteState:          NoteStateLive,
				Unavailable:        UnavailableDegrade,
			}
			if hasSessionID(args) {
				plan.Session = SessionExistingOnly
				plan.Readiness = append(plan.Readiness, ReadinessSession)
			}
			return plan, nil
		}
		plan := Plan{NoteState: NoteStateLive, Unavailable: UnavailableDegrade}
		if hasSessionID(args) {
			plan.Session = SessionExistingOnly
			plan.Readiness = []Readiness{ReadinessSession}
		}
		return plan, nil
	case "agent.list-tags":
		// This summary does not consume the session tracker. Do not make an
		// ignored sessionId open or await SQLite state.
		return Plan{NoteState: NoteStateLive, Unavailable: UnavailableDegrade}, nil
	case "agent.list-properties":
		// A current metadata projection preserves the established property-key
		// normalization and scalar/list aggregation. If it is unavailable, the
		// action remains live and must not repair or create indexed state.
		plan := indexedReadPlan()
		plan.CodeIndexFreshness = CodeIndexFreshnessNone
		plan.NoteState = NoteStateLive
		plan.Unavailable = UnavailableDegrade
		return plan, nil
	case "agent.community-list":
		plan := indexedReadPlan()
		// Persisted graph data is an acceleration. Community-list has an existing
		// live graph contract, so a missing/stale managed index must not turn a
		// successful live result into an unavailable error.
		plan.Unavailable = UnavailableDegrade
		return plan, nil
	case "agent.vault-health":
		return Plan{
			Capabilities:       []bootstrap.RuntimeCapability{bootstrap.RuntimeCapabilitySearch, bootstrap.RuntimeCapabilityCodeIndex, bootstrap.RuntimeCapabilityCodeRefDiscovery},
			Readiness:          []Readiness{ReadinessSearch, ReadinessNoteCache, ReadinessCodeIndex},
			NoteState:          NoteStateCacheSnapshot,
			StoreAccess:        StoreExistingReadOnly,
			CodeIndexFreshness: CodeIndexFreshnessCurrent,
			Unavailable:        UnavailableDegrade,
		}, nil
	case "agent.report":
		if !knownReportOperation(stringArg(args, "op")) {
			return Plan{}, fmt.Errorf("op must be one of doc_coverage, complexity, hotspots, rationale_attention, relatedness, code_similarity")
		}
		return indexedReadPlan(), nil
	case "agent.vault-context":
		return vaultContextPlan(args), nil
	case "agent.find-connections":
		plan := indexedReadPlan()
		plan.CodeIndexFreshness = CodeIndexFreshnessNone
		// This handler owns a session tracker. Await its narrow existing-only
		// writer before the handler starts so cross-invocation dedupe is stable.
		if hasSessionID(args) {
			plan.Session = SessionExistingOnly
			plan.Readiness = append(plan.Readiness, ReadinessSession)
		}
		if strings.TrimSpace(stringArg(args, "text")) != "" {
			plan.Capabilities = append(plan.Capabilities, bootstrap.RuntimeCapabilitySemantic)
			plan.Readiness = append(plan.Readiness, ReadinessSemantic)
		}
		return plan, nil
	case "agent.node-link", "agent.note-rename-heading":
		return Plan{NoteState: NoteStateSelectedFiles, Unavailable: UnavailableStructured}, nil
	default:
		return Plan{}, fmt.Errorf("%w for %q", ErrRequestPlanNotDeclared, operationID)
	}
}

// SemanticQueryPlan keeps query embedding and indexed retrieval explicit while
// retaining the request-derived composition path and unavailable behavior
// established for the agent JSON surface.
func SemanticQueryPlan(args map[string]any) Plan {
	plan := Plan{
		Capabilities: []bootstrap.RuntimeCapability{
			bootstrap.RuntimeCapabilitySemantic,
			bootstrap.RuntimeCapabilityCodeIndex,
			bootstrap.RuntimeCapabilityCodeEmbeddings,
		},
		Readiness:            []Readiness{ReadinessSemantic, ReadinessCodeIndex},
		StoreAccess:          StoreExistingReadOnly,
		CodeIndexFreshness:   CodeIndexFreshnessCurrent,
		Unavailable:          UnavailableStructured,
		Diagnostics:          DiagnosticsEnabled,
		DiagnosticsNamespace: "one_shot.semantic_query",
	}
	if hasSessionID(args) {
		plan.Session = SessionExistingOnly
		plan.Readiness = append(plan.Readiness, ReadinessSession)
	}
	return plan
}

// HasGenericRequestPlan reports whether the generic request-plan compiler has
// a typed branch for an operation. Other one-shot planners are declared by a
// typed planner binding in the registry.
func HasGenericRequestPlan(operationID OperationID) bool {
	switch operationID {
	case "agent.node-link", "agent.semantic-query", "agent.files", "agent.list-tags", "agent.list-properties",
		"agent.community-list", "agent.vault-health", "agent.report", "agent.vault-context",
		"agent.find-connections":
		return true
	default:
		return false
	}
}

func indexedReadPlan() Plan {
	return Plan{
		Capabilities:       []bootstrap.RuntimeCapability{bootstrap.RuntimeCapabilityCodeIndex},
		Readiness:          []Readiness{ReadinessCodeIndex},
		StoreAccess:        StoreExistingReadOnly,
		CodeIndexFreshness: CodeIndexFreshnessCurrent,
		Unavailable:        UnavailableStructured,
	}
}

func vaultContextPlan(args map[string]any) Plan {
	switch strings.TrimSpace(stringArg(args, "requestScope")) {
	case "minimal_bootstrap":
		return Plan{NoteState: NoteStateLive, Session: SessionExistingOnly, Readiness: []Readiness{ReadinessSession}, Unavailable: UnavailableDegrade}
	case "indexed_bootstrap":
		plan := indexedReadPlan()
		plan.Session = SessionExistingOnly
		plan.Readiness = append(plan.Readiness, ReadinessSession)
		return plan
	}

	// Standalone vault-context is intentionally rich by default. Normalized
	// profile/options tune rendering, but each rich variant consumes the live
	// note cache and may consume persisted ontology/code evidence: code needs
	// graph + docs, vault needs ontology/graph, and explicit targets add both.
	// The agent start command passes a requestScope explicitly, so omitted scope
	// cannot be treated as the minimal bootstrap shortcut.
	plan := Plan{
		Capabilities:       []bootstrap.RuntimeCapability{bootstrap.RuntimeCapabilitySearch, bootstrap.RuntimeCapabilityCodeIndex, bootstrap.RuntimeCapabilityCodeRefDiscovery},
		Readiness:          []Readiness{ReadinessSearch, ReadinessNoteCache, ReadinessCodeIndex},
		StoreAccess:        StoreExistingReadOnly,
		CodeIndexFreshness: CodeIndexFreshnessCurrent,
		Unavailable:        UnavailableDegrade,
	}
	plan.NoteState = NoteStateCacheSnapshot
	plan.Session = SessionExistingOnly
	plan.Readiness = append(plan.Readiness, ReadinessSession)
	return plan
}

func knownReportOperation(op string) bool {
	switch strings.ReplaceAll(strings.ToLower(strings.TrimSpace(op)), "-", "_") {
	case "doc_coverage", "complexity", "hotspots", "rationale_attention", "relatedness", "code_similarity":
		return true
	default:
		return false
	}
}

func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	value, _ := args[key].(string)
	return value
}

func hasSessionID(args map[string]any) bool {
	return strings.TrimSpace(stringArg(args, "sessionId")) != ""
}

func intArg(args map[string]any, key string) int {
	if args == nil {
		return 0
	}
	switch value := args[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return 0
	}
}

func boolArg(args map[string]any, key string) bool {
	if args == nil {
		return false
	}
	value, _ := args[key].(bool)
	return value
}
