package validate

import (
	"context"
	"fmt"
	"slices"

	"github.com/atomicobject/rhizome/pkg/ontology"
	validationcatalog "github.com/atomicobject/rhizome/pkg/validate/catalog"
)

// CheckCategory groups checks by the kind of repository contract they audit.
type CheckCategory string

const (
	CheckCategoryLinks     CheckCategory = "links"
	CheckCategorySchema    CheckCategory = "schema"
	CheckCategoryCode      CheckCategory = "code"
	CheckCategoryConfig    CheckCategory = "config"
	CheckCategoryLifecycle CheckCategory = "lifecycle"
)

// CheckSuite is a selectable built-in validation suite.
type CheckSuite string

const (
	SuiteDefault CheckSuite = "default"
	SuiteAll     CheckSuite = "all"
	SuiteAudit   CheckSuite = "audit"
)

// CheckApplicability declares which vault feature makes a check relevant.
type CheckApplicability string

const (
	ApplicabilityAlways   CheckApplicability = "always"
	ApplicabilityOntology CheckApplicability = "ontology_configured"
	ApplicabilityCode     CheckApplicability = "code_configured"
	// ApplicabilityCodeAnchors additionally requires at least one enabled
	// code-anchor language with configured roots; without roots indexing cannot
	// produce anchors, so `rzm index` is not a meaningful preparation step.
	ApplicabilityCodeAnchors  CheckApplicability = "code_anchor_roots_configured"
	ApplicabilityQueryRecipes CheckApplicability = "query_recipes_configured"
	ApplicabilityViews        CheckApplicability = "views_configured"
	ApplicabilityEfforts      CheckApplicability = "efforts_present"
)

// ProjectionDomain identifies persisted data a check reads. Validation is the
// lightweight note/link projection; code and ontology are separate domains.
type ProjectionDomain string

const (
	ProjectionValidation ProjectionDomain = "validation"
	ProjectionOntology   ProjectionDomain = "ontology"
	ProjectionCode       ProjectionDomain = "code"
)

// ExecutionSurface identifies a supported validation caller.
type ExecutionSurface string

const (
	SurfaceLocal ExecutionSurface = "local"
	SurfaceCI    ExecutionSurface = "ci"
	SurfaceAgent ExecutionSurface = "agent"
)

// MissingPrerequisiteOutcome describes the result when an applicable check
// cannot acquire its required persisted projection.
type MissingPrerequisiteOutcome string

const (
	PrerequisiteNone          MissingPrerequisiteOutcome = "none"
	PrerequisiteNotApplicable MissingPrerequisiteOutcome = "not_applicable"
	PrerequisiteBlocked       MissingPrerequisiteOutcome = "blocked"
)

// PrerequisiteMode distinguishes projections validation can refresh itself
// from persisted project data that must already exist.
type PrerequisiteMode string

const (
	PrerequisiteAutoManaged PrerequisiteMode = "auto_managed"
	PrerequisiteExternal    PrerequisiteMode = "external_persisted"
)

// RemediationKind describes whether validation can produce a repair plan.
type RemediationKind string

const (
	RemediationNone RemediationKind = "none"
	RemediationPlan RemediationKind = "plan"
)

// RemediationSupport is public catalog metadata for repair support.
type RemediationSupport struct {
	Kind    RemediationKind `json:"kind"`
	Command string          `json:"command,omitempty"`
}

// CheckDescriptor is the public metadata for one validation check. The
// registry is the single source of truth for identity, suites, applicability,
// prerequisites, remediation support, CLI descriptions, and dispatch.
type CheckDescriptor struct {
	Name                       string                     `json:"id"`
	CLIName                    string                     `json:"name"`
	Description                string                     `json:"description"`
	Category                   CheckCategory              `json:"category"`
	Aliases                    []string                   `json:"aliases,omitempty"`
	Suites                     []CheckSuite               `json:"suites"`
	Applicability              CheckApplicability         `json:"applicability"`
	ProjectionDomains          []ProjectionDomain         `json:"projectionDomains"`
	ExecutionSurfaces          []ExecutionSurface         `json:"executionSurfaces"`
	PrerequisiteMode           PrerequisiteMode           `json:"prerequisiteMode"`
	MissingPrerequisiteOutcome MissingPrerequisiteOutcome `json:"missingPrerequisiteOutcome"`
	PreparationCommand         string                     `json:"preparationCommand,omitempty"`
	Remediation                RemediationSupport         `json:"remediation"`
}

type checkRunner func(context.Context, RunContext, ScanOptions, *ontology.Runtime) CheckResult

type checkNeeds struct {
	Sources   bool
	Ontology  bool
	CodeStore bool
}

type registeredCheck struct {
	CheckDescriptor
	Needs checkNeeds
	Run   checkRunner
}

var (
	coreSuites  = []CheckSuite{SuiteDefault, SuiteAll}
	allSuites   = []CheckSuite{SuiteAll}
	auditSuites = []CheckSuite{SuiteAudit}
	allSurfaces = []ExecutionSurface{SurfaceLocal, SurfaceCI, SurfaceAgent}
)

var checkRegistry = []registeredCheck{
	checkRegistration(CheckDescriptor{
		Name: CheckOntology, Description: "ontology schema and typed notes", Category: CheckCategorySchema,
		Suites: coreSuites, Applicability: ApplicabilityOntology,
		Remediation: remediationPlan("ontology"),
	}, checkNeeds{Sources: true, Ontology: true}, runOntologyRegistered),
	checkRegistration(CheckDescriptor{
		Name: CheckIdentifiers, Description: "preferred identifier integrity", Category: CheckCategorySchema,
		Suites: coreSuites, Applicability: ApplicabilityOntology,
		Remediation: remediationPlan("identifiers"),
	}, checkNeeds{Sources: true, Ontology: true}, runIdentifiersRegistered),
	checkRegistration(CheckDescriptor{
		Name: CheckBrokenLinks, Description: "broken internal note, heading, and block targets", Category: CheckCategoryLinks,
		Suites: coreSuites, Applicability: ApplicabilityAlways,
		Remediation: remediationPlan("broken-links"),
	}, checkNeeds{Sources: true}, func(ctx context.Context, runCtx RunContext, opts ScanOptions, _ *ontology.Runtime) CheckResult {
		return RunBrokenLinksContext(ctx, runCtx, opts)
	}),
	checkRegistration(CheckDescriptor{
		Name: CheckLinkHygiene, Description: "internal link hygiene", Category: CheckCategoryLinks,
		Suites: allSuites, Applicability: ApplicabilityAlways,
		Remediation: remediationPlan("link-hygiene"),
	}, checkNeeds{Sources: true}, func(_ context.Context, runCtx RunContext, _ ScanOptions, _ *ontology.Runtime) CheckResult {
		return RunLinkHygiene(runCtx)
	}),
	checkRegistration(CheckDescriptor{
		Name: CheckQueryRecipes, Description: "saved ontology query recipes", Category: CheckCategoryConfig,
		Suites: allSuites, Applicability: ApplicabilityQueryRecipes,
		Remediation: remediationPlan("query-recipes"),
	}, checkNeeds{Ontology: true}, func(ctx context.Context, runCtx RunContext, _ ScanOptions, _ *ontology.Runtime) CheckResult {
		return RunQueryRecipes(ctx, runCtx)
	}),
	checkRegistration(CheckDescriptor{
		Name: CheckViews, Description: "configured views", Category: CheckCategoryConfig,
		Suites: allSuites, Applicability: ApplicabilityViews,
		Remediation: RemediationSupport{Kind: RemediationNone},
	}, checkNeeds{Ontology: true}, func(ctx context.Context, runCtx RunContext, _ ScanOptions, _ *ontology.Runtime) CheckResult {
		return RunViews(ctx, runCtx)
	}),
	checkRegistration(CheckDescriptor{
		Name: CheckSkillOverlays, Description: "bundled skill overlays", Category: CheckCategoryConfig,
		Suites: allSuites, Applicability: ApplicabilityAlways,
		Remediation: RemediationSupport{Kind: RemediationNone},
	}, checkNeeds{}, func(_ context.Context, runCtx RunContext, _ ScanOptions, _ *ontology.Runtime) CheckResult {
		return RunSkillOverlays(runCtx)
	}),
	checkRegistration(CheckDescriptor{
		Name: CheckCodeFrontmatter, Description: "code documentation frontmatter", Category: CheckCategoryCode,
		Suites: allSuites, Applicability: ApplicabilityCode,
		Remediation: remediationPlan("code-frontmatter"),
	}, checkNeeds{Sources: true}, func(ctx context.Context, runCtx RunContext, _ ScanOptions, _ *ontology.Runtime) CheckResult {
		return RunCodeFrontmatter(ctx, runCtx)
	}),
	checkRegistration(CheckDescriptor{
		Name: CheckCodeAnchors, Description: "code anchor matches", Category: CheckCategoryCode,
		Suites: allSuites, Applicability: ApplicabilityCodeAnchors,
		Remediation: remediationPlan("code-anchors"),
	}, checkNeeds{CodeStore: true}, runCodeAnchorsRegistered),
	checkRegistration(CheckDescriptor{
		Name: CheckCompanionDocs, Description: "subsystem companion documentation", Category: CheckCategoryCode,
		Suites: allSuites, Applicability: ApplicabilityCode,
		Remediation: remediationPlan("companion-docs"),
	}, checkNeeds{}, func(_ context.Context, runCtx RunContext, _ ScanOptions, _ *ontology.Runtime) CheckResult {
		return RunCompanionDocs(runCtx)
	}),
	checkRegistration(CheckDescriptor{
		Name: CheckFrozenScopeDrift, Description: "active effort frozen-scope drift", Category: CheckCategoryLifecycle,
		Suites: auditSuites, Applicability: ApplicabilityEfforts,
		Remediation: RemediationSupport{Kind: RemediationNone},
	}, checkNeeds{Sources: true, Ontology: true}, runFrozenScopeDriftRegistered),
	checkRegistration(CheckDescriptor{
		Name: CheckFragileExternal, Description: "fragile external heading links", Category: CheckCategoryLinks,
		Suites: auditSuites, Applicability: ApplicabilityAlways,
		Remediation: remediationPlan("fragile-external"),
	}, checkNeeds{Sources: true}, func(ctx context.Context, runCtx RunContext, opts ScanOptions, _ *ontology.Runtime) CheckResult {
		return RunFragileExternal(ctx, runCtx, opts)
	}),
	checkRegistration(CheckDescriptor{
		Name: CheckOrphanBlockIDs, Description: "orphaned block identifiers", Category: CheckCategoryLinks,
		Suites: auditSuites, Applicability: ApplicabilityOntology,
		Remediation: remediationPlan("orphan-block-ids"),
	}, checkNeeds{Sources: true, Ontology: true}, func(ctx context.Context, runCtx RunContext, _ ScanOptions, _ *ontology.Runtime) CheckResult {
		return RunOrphanBlockIDs(ctx, runCtx)
	}),
	checkRegistration(CheckDescriptor{
		Name: CheckPlaceholderLinks, Description: "placeholder links to notes that never existed", Category: CheckCategoryLinks,
		Suites: auditSuites, Applicability: ApplicabilityAlways,
		Remediation: RemediationSupport{Kind: RemediationNone},
	}, checkNeeds{Sources: true}, func(ctx context.Context, runCtx RunContext, opts ScanOptions, _ *ontology.Runtime) CheckResult {
		return RunPlaceholderLinks(ctx, runCtx, opts)
	}),
}

func remediationPlan(check string) RemediationSupport {
	return RemediationSupport{Kind: RemediationPlan, Command: "rzm validate fix " + check}
}

func checkRegistration(descriptor CheckDescriptor, needs checkNeeds, run checkRunner) registeredCheck {
	identity, ok := validationcatalog.Lookup(descriptor.Name)
	if !ok {
		panic("validation check identity is missing from catalog: " + descriptor.Name)
	}
	descriptor.Name = identity.ID
	descriptor.CLIName = identity.Name
	descriptor.Aliases = slices.Clone(identity.Aliases)
	descriptor.ProjectionDomains = []ProjectionDomain{ProjectionValidation}
	descriptor.ExecutionSurfaces = allSurfaces
	descriptor.PrerequisiteMode = PrerequisiteAutoManaged
	descriptor.MissingPrerequisiteOutcome = PrerequisiteBlocked
	if needs.Ontology {
		descriptor.ProjectionDomains = append(descriptor.ProjectionDomains, ProjectionOntology)
	}
	if needs.CodeStore {
		descriptor.ProjectionDomains = append(descriptor.ProjectionDomains, ProjectionCode)
		descriptor.ExecutionSurfaces = []ExecutionSurface{SurfaceLocal, SurfaceAgent}
		descriptor.PrerequisiteMode = PrerequisiteExternal
		descriptor.PreparationCommand = "rzm index"
	}
	return registeredCheck{CheckDescriptor: descriptor, Needs: needs, Run: run}
}

func runOntologyRegistered(ctx context.Context, runCtx RunContext, _ ScanOptions, runtime *ontology.Runtime) CheckResult {
	return runOntologyWithRuntime(ctx, runCtx, runtime)
}

func runIdentifiersRegistered(ctx context.Context, runCtx RunContext, _ ScanOptions, runtime *ontology.Runtime) CheckResult {
	return RunIdentifiersWithRuntime(ctx, runCtx, runtime)
}

func runFrozenScopeDriftRegistered(ctx context.Context, runCtx RunContext, _ ScanOptions, runtime *ontology.Runtime) CheckResult {
	return RunFrozenScopeDriftWithRuntime(ctx, runCtx, runtime)
}

func runCodeAnchorsRegistered(ctx context.Context, runCtx RunContext, _ ScanOptions, runtime *ontology.Runtime) CheckResult {
	return RunCodeAnchorsWithStore(ctx, runCtx, runtime.Store)
}

func sharedRuntimeError(name string, err error) CheckResult {
	return CheckResult{Name: name, Error: fmt.Sprintf("shared ontology runtime: %v", err)}
}

// CheckDescriptors returns the registered checks in their stable display order.
func CheckDescriptors() []CheckDescriptor {
	descriptors := make([]CheckDescriptor, 0, len(checkRegistry))
	for _, registration := range checkRegistry {
		descriptor := registration.CheckDescriptor
		descriptor.Aliases = slices.Clone(descriptor.Aliases)
		descriptor.Suites = slices.Clone(descriptor.Suites)
		descriptor.ProjectionDomains = slices.Clone(descriptor.ProjectionDomains)
		descriptor.ExecutionSurfaces = slices.Clone(descriptor.ExecutionSurfaces)
		descriptors = append(descriptors, descriptor)
	}
	return descriptors
}

func defaultCheckNames() []string {
	return builtInSuiteChecks(SuiteDefault)
}

func builtInSuiteChecks(suite CheckSuite) []string {
	names := make([]string, 0, len(checkRegistry))
	for _, registration := range checkRegistry {
		if slices.Contains(registration.Suites, suite) {
			names = append(names, registration.Name)
		}
	}
	return names
}

func lookupCheck(name string) (registeredCheck, bool) {
	for _, registration := range checkRegistry {
		if registration.Name == name {
			return registration, true
		}
	}
	return registeredCheck{}, false
}

func registryNeedsOntologyRuntime(checks []string) bool {
	for _, name := range checks {
		registration, ok := lookupCheck(name)
		if ok && registration.Needs.Ontology {
			return true
		}
	}
	return false
}

func validateCheckRegistry(registry []registeredCheck) error {
	names := make(map[string]struct{}, len(registry))
	aliases := make(map[string]string)
	for _, registration := range registry {
		if slices.Contains(registration.Suites, SuiteDefault) && !slices.Contains(registration.Suites, SuiteAll) {
			return fmt.Errorf("default validation check %q must belong to all", registration.Name)
		}
		inAll := slices.Contains(registration.Suites, SuiteAll)
		inAudit := slices.Contains(registration.Suites, SuiteAudit)
		if inAll && inAudit {
			return fmt.Errorf("validation check %q cannot belong to both all and audit", registration.Name)
		}
		if !inAll && !inAudit {
			return fmt.Errorf("validation check %q must belong to all or audit", registration.Name)
		}
		if _, exists := names[registration.Name]; exists {
			return fmt.Errorf("duplicate validation check %q", registration.Name)
		}
		names[registration.Name] = struct{}{}
		for _, alias := range append([]string{registration.Name, registration.CLIName}, registration.Aliases...) {
			normalized := validationcatalog.Normalize(alias)
			if normalized == "" {
				return fmt.Errorf("validation check %q has an empty alias", registration.Name)
			}
			if normalized == string(SuiteDefault) || normalized == string(SuiteAll) || normalized == string(SuiteAudit) {
				return fmt.Errorf("validation check %q uses reserved validation selector %q", registration.Name, alias)
			}
			if owner, exists := aliases[normalized]; exists && owner != registration.Name {
				return fmt.Errorf("validation check alias %q belongs to both %q and %q", alias, owner, registration.Name)
			}
			aliases[normalized] = registration.Name
		}
	}
	return nil
}

func init() {
	if err := validateCheckRegistry(checkRegistry); err != nil {
		panic(err)
	}
}
