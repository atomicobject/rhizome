package oneshotruntime

import (
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
)

type OperationID string

type CompositionOwner string

const (
	CompositionNone              CompositionOwner = "none"
	CompositionOneShotRuntime    CompositionOwner = "one_shot_runtime"
	CompositionValidationProduct CompositionOwner = "validation_product"
	CompositionBoundedService    CompositionOwner = "bounded_direct_service"
	CompositionLongLivedRuntime  CompositionOwner = "long_lived_runtime"
)

type PlanSource string

const (
	PlanSourceStatic         PlanSource = "static"
	PlanSourceRequestDerived PlanSource = "request_derived"
	PlanSourceExempt         PlanSource = "explicit_exemption"
)

// PlannerBinding identifies the typed implementation that derives a
// request-dependent one-shot plan. GenericRequestPlan is the only binding
// routed through RequestPlan; the remaining values name their dedicated
// planners so registry validation does not mistake them for generic branches.
type PlannerBinding string

const (
	PlannerBindingNone                   PlannerBinding = ""
	PlannerBindingGenericRequestPlan     PlannerBinding = "generic_request_plan"
	PlannerBindingAgentStart             PlannerBinding = "agent_start"
	PlannerBindingOntologyQuery          PlannerBinding = "ontology_query"
	PlannerBindingQueryRecipeRun         PlannerBinding = "query_recipe_run"
	PlannerBindingOntologyAuthoringGuide PlannerBinding = "ontology_authoring_guide"
	PlannerBindingNextID                 PlannerBinding = "next_id"
	PlannerBindingCurrentUserValidate    PlannerBinding = "current_user_validate"
)

// RuntimeExemption names a deliberately preserved composition path. It is
// limited to fronts whose output parity has not yet frozen a narrower plan.
type RuntimeExemption string

const (
	RuntimeExemptionNone              RuntimeExemption = ""
	RuntimeExemptionFullRuntimeParity RuntimeExemption = "full_runtime_parity"
)

type FrontRef struct {
	Path string
}

type Declaration struct {
	ID             OperationID
	Fronts         []FrontRef
	Owner          CompositionOwner
	PlanSource     PlanSource
	PlannerBinding PlannerBinding
	Exemption      RuntimeExemption
	Authority      Authority
	StaticPlan     *Plan
	ExemptionPlan  *Plan
}

type Registry struct {
	declarations []Declaration
}

func NewRegistry(declarations ...Declaration) Registry {
	return Registry{declarations: append([]Declaration(nil), declarations...)}
}

func (r Registry) Validate() error {
	operations := make(map[OperationID]struct{}, len(r.declarations))
	fronts := map[string]OperationID{}
	for _, declaration := range r.declarations {
		if declaration.ID == "" {
			return fmt.Errorf("operation id is required")
		}
		if _, duplicate := operations[declaration.ID]; duplicate {
			return fmt.Errorf("duplicate operation id %q", declaration.ID)
		}
		operations[declaration.ID] = struct{}{}
		if !knownCompositionOwner(declaration.Owner) {
			return fmt.Errorf("operation %q has unknown composition owner %q", declaration.ID, declaration.Owner)
		}
		if declaration.PlanSource != PlanSourceStatic && declaration.PlanSource != PlanSourceRequestDerived && declaration.PlanSource != PlanSourceExempt {
			return fmt.Errorf("operation %q has unknown plan source %q", declaration.ID, declaration.PlanSource)
		}
		if !knownAuthority(declaration.Authority) {
			return fmt.Errorf("operation %q has unknown authority %q", declaration.ID, declaration.Authority)
		}
		if len(declaration.Fronts) == 0 {
			return fmt.Errorf("operation %q requires at least one front", declaration.ID)
		}
		for _, front := range declaration.Fronts {
			path := strings.TrimSpace(front.Path)
			if path == "" {
				return fmt.Errorf("operation %q has an empty front path", declaration.ID)
			}
			if prior, duplicate := fronts[path]; duplicate {
				return fmt.Errorf("duplicate front path %q for %q and %q", path, prior, declaration.ID)
			}
			fronts[path] = declaration.ID
		}
		if declaration.Owner == CompositionOneShotRuntime && declaration.PlanSource == PlanSourceStatic && declaration.StaticPlan == nil {
			return fmt.Errorf("static one-shot operation requires a plan: %s", declaration.ID)
		}
		if declaration.Owner == CompositionOneShotRuntime && declaration.PlanSource == PlanSourceRequestDerived {
			if !knownPlannerBinding(declaration.PlannerBinding) || declaration.PlannerBinding == PlannerBindingNone {
				return fmt.Errorf("request-derived one-shot operation requires a planner binding: %s", declaration.ID)
			}
			if declaration.PlannerBinding == PlannerBindingGenericRequestPlan && !HasGenericRequestPlan(declaration.ID) {
				return fmt.Errorf("generic request-plan binding has no request plan: %s", declaration.ID)
			}
		}
		if declaration.PlanSource != PlanSourceRequestDerived && declaration.PlannerBinding != PlannerBindingNone {
			return fmt.Errorf("non-request-derived operation %q cannot declare a planner binding", declaration.ID)
		}
		if declaration.PlanSource == PlanSourceRequestDerived && declaration.StaticPlan != nil {
			return fmt.Errorf("request-derived operation %q cannot declare a static plan", declaration.ID)
		}
		if declaration.PlanSource == PlanSourceExempt && declaration.Exemption == RuntimeExemptionNone {
			return fmt.Errorf("exempt operation %q requires an exemption", declaration.ID)
		}
		if declaration.PlanSource == PlanSourceExempt && declaration.Exemption != RuntimeExemptionFullRuntimeParity {
			return fmt.Errorf("exempt operation %q has unknown exemption %q", declaration.ID, declaration.Exemption)
		}
		if declaration.PlanSource != PlanSourceExempt && declaration.Exemption != RuntimeExemptionNone {
			return fmt.Errorf("non-exempt operation %q cannot declare an exemption", declaration.ID)
		}
		if declaration.PlanSource == PlanSourceExempt && declaration.StaticPlan != nil {
			return fmt.Errorf("exempt operation %q cannot declare a static plan", declaration.ID)
		}
		if declaration.PlanSource == PlanSourceExempt && declaration.ExemptionPlan == nil {
			return fmt.Errorf("exempt operation %q requires an exemption plan", declaration.ID)
		}
		if declaration.PlanSource != PlanSourceExempt && declaration.ExemptionPlan != nil {
			return fmt.Errorf("non-exempt operation %q cannot declare an exemption plan", declaration.ID)
		}
		if declaration.StaticPlan != nil {
			if declaration.StaticPlan.FullRuntime {
				return fmt.Errorf("static operation %q cannot declare full runtime; use an explicit exemption", declaration.ID)
			}
			if err := declaration.StaticPlan.ValidateForAuthority(declaration.Authority); err != nil {
				return fmt.Errorf("operation %q plan: %w", declaration.ID, err)
			}
		}
		if declaration.ExemptionPlan != nil {
			if declaration.Exemption == RuntimeExemptionFullRuntimeParity && !declaration.ExemptionPlan.FullRuntime {
				return fmt.Errorf("full-runtime parity exemption %q requires a full-runtime plan", declaration.ID)
			}
			if err := declaration.ExemptionPlan.ValidateForAuthority(declaration.Authority); err != nil {
				return fmt.Errorf("operation %q exemption plan: %w", declaration.ID, err)
			}
		}
	}
	return nil
}

func (r Registry) HasFront(path string) bool {
	for _, declaration := range r.declarations {
		for _, front := range declaration.Fronts {
			if front.Path == path {
				return true
			}
		}
	}
	return false
}

func (r Registry) OperationIDForFront(path string) (OperationID, bool) {
	for _, declaration := range r.declarations {
		for _, front := range declaration.Fronts {
			if front.Path == path {
				return declaration.ID, true
			}
		}
	}
	return "", false
}

// Declaration returns a defensive copy of one operation declaration. Callers
// may use it for composition without being able to mutate the registry's
// authoritative static plan.
func (r Registry) Declaration(id OperationID) (Declaration, bool) {
	for _, declaration := range r.declarations {
		if declaration.ID != id {
			continue
		}
		copy := declaration
		copy.Fronts = append([]FrontRef(nil), declaration.Fronts...)
		if declaration.StaticPlan != nil {
			plan := *declaration.StaticPlan
			plan.Capabilities = append([]bootstrap.RuntimeCapability(nil), plan.Capabilities...)
			plan.Readiness = append([]Readiness(nil), plan.Readiness...)
			copy.StaticPlan = &plan
		}
		if declaration.ExemptionPlan != nil {
			plan := *declaration.ExemptionPlan
			plan.Capabilities = append([]bootstrap.RuntimeCapability(nil), plan.Capabilities...)
			plan.Readiness = append([]Readiness(nil), plan.Readiness...)
			copy.ExemptionPlan = &plan
		}
		return copy, true
	}
	return Declaration{}, false
}

func (r Registry) HasFrontOrDescendant(prefix string) bool {
	for _, path := range r.FrontsWithPrefix(prefix) {
		if path == prefix || strings.HasPrefix(path, prefix+" ") {
			return true
		}
	}
	return false
}

func (r Registry) FrontsWithPrefix(prefix string) []string {
	paths := make([]string, 0)
	for _, declaration := range r.declarations {
		for _, front := range declaration.Fronts {
			if front.Path == prefix || strings.HasPrefix(front.Path, prefix+" ") {
				paths = append(paths, front.Path)
			}
		}
	}
	sort.Strings(paths)
	return paths
}

func (r Registry) FrontsWithoutPrefix(prefix string) []string {
	paths := make([]string, 0)
	for _, declaration := range r.declarations {
		for _, front := range declaration.Fronts {
			if front.Path != prefix && !strings.HasPrefix(front.Path, prefix) {
				paths = append(paths, front.Path)
			}
		}
	}
	sort.Strings(paths)
	return paths
}

func DefaultRegistry() Registry {
	empty := func() *Plan { return &Plan{} }
	code := func() *Plan {
		return &Plan{
			Capabilities:         []bootstrap.RuntimeCapability{bootstrap.RuntimeCapabilityCodeIndex},
			Readiness:            []Readiness{ReadinessCodeIndex},
			StoreAccess:          StoreExistingReadOnly,
			CodeIndexFreshness:   CodeIndexFreshnessCurrent,
			Diagnostics:          DiagnosticsEnabled,
			DiagnosticsNamespace: "one_shot.code_index",
			Unavailable:          UnavailableStructured,
		}
	}
	fileContext := func() *Plan {
		plan := *code()
		plan.Session = SessionExistingOnly
		plan.Readiness = append(plan.Readiness, ReadinessSession)
		return &plan
	}
	static := func(id, front string, owner CompositionOwner, authority Authority, plan *Plan) Declaration {
		return Declaration{ID: OperationID(id), Fronts: []FrontRef{{Path: front}}, Owner: owner, PlanSource: PlanSourceStatic, Authority: authority, StaticPlan: plan}
	}
	request := func(id, front string, owner CompositionOwner, authority Authority) Declaration {
		return Declaration{ID: OperationID(id), Fronts: []FrontRef{{Path: front}}, Owner: owner, PlanSource: PlanSourceRequestDerived, Authority: authority}
	}
	boundRequest := func(id, front string, owner CompositionOwner, authority Authority, binding PlannerBinding) Declaration {
		declaration := request(id, front, owner, authority)
		declaration.PlannerBinding = binding
		return declaration
	}
	exempt := func(id, front string, owner CompositionOwner, authority Authority, exemption RuntimeExemption, plan *Plan) Declaration {
		return Declaration{ID: OperationID(id), Fronts: []FrontRef{{Path: front}}, Owner: owner, PlanSource: PlanSourceExempt, Authority: authority, Exemption: exemption, ExemptionPlan: plan}
	}

	return NewRegistry(
		static("agent.surface", "agent surface", CompositionNone, AuthorityReadOnly, empty()),
		static("agent.code.surface", "agent code surface", CompositionNone, AuthorityReadOnly, empty()),
		static("agent.code.describe", "agent code describe", CompositionNone, AuthorityReadOnly, empty()),
		static("agent.code.execute", "agent code execute", CompositionBoundedService, AuthorityWriteCapable, empty()),
		static("agent.code.generate", "agent code generate", CompositionNone, AuthorityWriteCapable, empty()),
		request("agent.code.serve", "agent code serve", CompositionLongLivedRuntime, AuthorityWriteCapable),
		boundRequest("agent.start", "agent start", CompositionOneShotRuntime, AuthorityReadOnly, PlannerBindingAgentStart),
		request("agent.validate.run", "agent validate", CompositionValidationProduct, AuthorityReadOnly),
		static("agent.validate.list", "agent validate list", CompositionNone, AuthorityReadOnly, empty()),
		request("agent.validate.fix", "agent validate fix", CompositionValidationProduct, AuthorityWriteCapable),
		boundRequest("agent.node-link", "agent node-link", CompositionOneShotRuntime, AuthorityPlanOnly, PlannerBindingGenericRequestPlan),
		request("agent.note-rename-heading", "agent note-rename-heading", CompositionBoundedService, AuthorityPlanOnly),
		static("agent.ontology-query-schema", "agent ontology-query-schema", CompositionNone, AuthorityReadOnly, empty()),
		boundRequest("agent.ontology-query", "agent ontology-query", CompositionOneShotRuntime, AuthorityReadOnly, PlannerBindingOntologyQuery),
		static("agent.query-recipe.list", "agent query-recipe list", CompositionNone, AuthorityReadOnly, empty()),
		static("agent.query-recipe.validate", "agent query-recipe validate", CompositionNone, AuthorityReadOnly, empty()),
		boundRequest("agent.query-recipe.run", "agent query-recipe run", CompositionOneShotRuntime, AuthorityReadOnly, PlannerBindingQueryRecipeRun),
		static("agent.view.list", "agent view list", CompositionBoundedService, AuthorityReadOnly, empty()),
		static("agent.view.show", "agent view show", CompositionBoundedService, AuthorityReadOnly, empty()),
		static("agent.view.validate", "agent view validate", CompositionBoundedService, AuthorityReadOnly, empty()),
		request("agent.view.run", "agent view run", CompositionBoundedService, AuthorityReadOnly),
		static("agent.view.eject", "agent view eject", CompositionBoundedService, AuthorityWriteCapable, empty()),
		static("agent.ontology-reference", "agent ontology-reference", CompositionNone, AuthorityReadOnly, empty()),
		boundRequest("agent.ontology-authoring-guide", "agent ontology-authoring-guide", CompositionOneShotRuntime, AuthorityReadOnly, PlannerBindingOntologyAuthoringGuide),
		request("agent.ontology-inspect", "agent ontology-inspect", CompositionBoundedService, AuthorityReadOnly),
		boundRequest("agent.next-id", "agent next-id", CompositionOneShotRuntime, AuthorityReadOnly, PlannerBindingNextID),
		static("agent.current-user.help", "agent current-user", CompositionNone, AuthorityReadOnly, empty()),
		static("agent.current-user.show", "agent current-user show", CompositionNone, AuthorityReadOnly, empty()),
		static("agent.current-user.set", "agent current-user set", CompositionBoundedService, AuthorityWriteCapable, empty()),
		boundRequest("agent.current-user.validate", "agent current-user validate", CompositionOneShotRuntime, AuthorityReadOnly, PlannerBindingCurrentUserValidate),
		boundRequest("agent.files", "agent files", CompositionOneShotRuntime, AuthorityReadOnly, PlannerBindingGenericRequestPlan),
		boundRequest("agent.list-tags", "agent list-tags", CompositionOneShotRuntime, AuthorityReadOnly, PlannerBindingGenericRequestPlan),
		boundRequest("agent.list-properties", "agent list-properties", CompositionOneShotRuntime, AuthorityReadOnly, PlannerBindingGenericRequestPlan),
		boundRequest("agent.community-list", "agent community-list", CompositionOneShotRuntime, AuthorityReadOnly, PlannerBindingGenericRequestPlan),
		boundRequest("agent.report", "agent report", CompositionOneShotRuntime, AuthorityReadOnly, PlannerBindingGenericRequestPlan),
		static("agent.file-context", "agent file-context", CompositionOneShotRuntime, AuthorityReadOnly, fileContext()),
		boundRequest("agent.vault-context", "agent vault-context", CompositionOneShotRuntime, AuthorityReadOnly, PlannerBindingGenericRequestPlan),
		boundRequest("agent.semantic-query", "agent semantic-query", CompositionOneShotRuntime, AuthorityReadOnly, PlannerBindingGenericRequestPlan),
		static("agent.code-symbol", "agent code-symbol", CompositionOneShotRuntime, AuthorityReadOnly, code()),
		static("agent.code-references", "agent code-references", CompositionOneShotRuntime, AuthorityReadOnly, code()),
		static("agent.check-paths", "agent check-paths", CompositionOneShotRuntime, AuthorityReadOnly, empty()),
		static("agent.evaluate-batch", "agent evaluate-batch", CompositionOneShotRuntime, AuthorityReadOnly, empty()),
		static("agent.evaluate", "agent evaluate", CompositionOneShotRuntime, AuthorityReadOnly, empty()),
		static("agent.external-references", "agent external-references", CompositionOneShotRuntime, AuthorityReadOnly, code()),
		exempt("agent.code-symbol-context", "agent code-symbol-context", CompositionOneShotRuntime, AuthorityReadOnly, RuntimeExemptionFullRuntimeParity, &Plan{
			FullRuntime:  true,
			Capabilities: []bootstrap.RuntimeCapability{bootstrap.RuntimeCapabilitySearch, bootstrap.RuntimeCapabilityCodeIndex},
			Readiness:    []Readiness{ReadinessSearch, ReadinessNoteCache, ReadinessCodeIndex},
			StoreAccess:  StoreLiveReadWrite,
			NoteState:    NoteStateCacheSnapshot,
			Session:      SessionLiveWrite,
			Unavailable:  UnavailableFail,
		}),
		boundRequest("agent.find-connections", "agent find-connections", CompositionOneShotRuntime, AuthorityReadOnly, PlannerBindingGenericRequestPlan),
		static("agent.graph-path", "agent graph-path", CompositionOneShotRuntime, AuthorityReadOnly, code()),
		static("agent.code-rationale", "agent code-rationale", CompositionOneShotRuntime, AuthorityReadOnly, code()),
		boundRequest("agent.vault-health", "agent vault-health", CompositionOneShotRuntime, AuthorityReadOnly, PlannerBindingGenericRequestPlan),

		request("root.search", "search", CompositionBoundedService, AuthorityReadOnly),
		static("root.list", "list", CompositionBoundedService, AuthorityReadOnly, empty()),
		static("root.note-tags-list", "note tags list", CompositionBoundedService, AuthorityReadOnly, empty()),
		static("root.note-properties-list", "note properties list", CompositionBoundedService, AuthorityReadOnly, empty()),
		request("root.note-move", "note move", CompositionBoundedService, AuthorityWriteCapable),
		request("root.note-rename-heading", "note rename-heading", CompositionBoundedService, AuthorityWriteCapable),
		request("root.graph-path", "graph path", CompositionBoundedService, AuthorityReadOnly),
		request("root.code-rationale", "code rationale", CompositionBoundedService, AuthorityReadOnly),
		static("root.query-recipe-list", "query-recipe list", CompositionNone, AuthorityReadOnly, empty()),
		static("root.query-recipe-validate", "query-recipe validate", CompositionNone, AuthorityReadOnly, empty()),
		static("root.query-recipe-show", "query-recipe show", CompositionNone, AuthorityReadOnly, empty()),
		boundRequest("root.query-recipe-run", "query-recipe run", CompositionOneShotRuntime, AuthorityReadOnly, PlannerBindingQueryRecipeRun),
		boundRequest("root.ontology-query", "ontology query", CompositionOneShotRuntime, AuthorityReadOnly, PlannerBindingOntologyQuery),
		static("root.ontology-query-schema", "ontology query-schema", CompositionNone, AuthorityReadOnly, empty()),
		static("root.ontology-reference", "ontology reference", CompositionNone, AuthorityReadOnly, empty()),
		request("root.ontology-authoring-guide", "ontology authoring-guide", CompositionBoundedService, AuthorityReadOnly),
		request("root.ontology-inspect", "ontology inspect", CompositionBoundedService, AuthorityReadOnly),
		request("root.ontology-validate", "ontology validate", CompositionBoundedService, AuthorityReadOnly),
		request("root.ontology-walk", "ontology walk", CompositionBoundedService, AuthorityReadOnly),
		static("root.view-list", "view list", CompositionBoundedService, AuthorityReadOnly, empty()),
		static("root.view-show", "view show", CompositionBoundedService, AuthorityReadOnly, empty()),
		static("root.view-validate", "view validate", CompositionBoundedService, AuthorityReadOnly, empty()),
		request("root.view-run", "view run", CompositionBoundedService, AuthorityReadOnly),
		static("root.view-eject", "view eject", CompositionBoundedService, AuthorityWriteCapable, empty()),
		request("root.validate", "validate", CompositionValidationProduct, AuthorityReadOnly),
		static("root.validate-list", "validate list", CompositionValidationProduct, AuthorityReadOnly, empty()),
		request("root.validate-fix", "validate fix", CompositionValidationProduct, AuthorityWriteCapable),
		request("root.ci", "ci", CompositionValidationProduct, AuthorityReadOnly),
		request("root.graph-file-context", "graph file-context", CompositionBoundedService, AuthorityWriteCapable),
		request("root.graph-vault-context", "graph vault-context", CompositionBoundedService, AuthorityReadOnly),
		static("root.index", "index", CompositionBoundedService, AuthorityWriteCapable, empty()),
		static("root.code-index", "code index", CompositionBoundedService, AuthorityWriteCapable, empty()),
		static("root.serve", "serve", CompositionLongLivedRuntime, AuthorityWriteCapable, empty()),
		static("root.start", "start", CompositionLongLivedRuntime, AuthorityWriteCapable, empty()),
	)
}

func knownCompositionOwner(owner CompositionOwner) bool {
	switch owner {
	case CompositionNone, CompositionOneShotRuntime, CompositionValidationProduct, CompositionBoundedService, CompositionLongLivedRuntime:
		return true
	default:
		return false
	}
}

func knownPlannerBinding(binding PlannerBinding) bool {
	switch binding {
	case PlannerBindingNone,
		PlannerBindingGenericRequestPlan,
		PlannerBindingAgentStart,
		PlannerBindingOntologyQuery,
		PlannerBindingQueryRecipeRun,
		PlannerBindingOntologyAuthoringGuide,
		PlannerBindingNextID,
		PlannerBindingCurrentUserValidate:
		return true
	default:
		return false
	}
}
