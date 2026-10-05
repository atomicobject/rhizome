package validate

import (
	"context"
	"fmt"
	"slices"
	"strings"

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

type checkRunner func(context.Context, RunContext, Options, *ontology.Runtime, error) CheckResult

// runtimeRequirement declares which shared runtime the suite orchestrator must
// acquire before invoking a check. It stays internal because callers consume
// projection-domain metadata, not implementation wiring.
type runtimeRequirement string

const (
	runtimeRequirementNone     runtimeRequirement = "none"
	runtimeRequirementOntology runtimeRequirement = "ontology"
)

type registeredCheck struct {
	CheckDescriptor
	RuntimeRequirement runtimeRequirement
	Run                checkRunner
}

var (
	coreSuites  = []CheckSuite{SuiteDefault, SuiteAll}
	allSuites   = []CheckSuite{SuiteAll}
	auditSuites = []CheckSuite{SuiteAudit}
	allSurfaces = []ExecutionSurface{SurfaceLocal, SurfaceCI, SurfaceAgent}
)

var checkRegistry = []registeredCheck{
	ontologyCheckRegistration(CheckDescriptor{
		Name: CheckOntology, Description: "ontology schema and typed notes", Category: CheckCategorySchema,
		Suites: coreSuites, Applicability: ApplicabilityOntology,
		ProjectionDomains: []ProjectionDomain{ProjectionValidation, ProjectionOntology}, ExecutionSurfaces: allSurfaces,
		PrerequisiteMode: PrerequisiteAutoManaged, MissingPrerequisiteOutcome: PrerequisiteBlocked,
		Remediation: remediationPlan("ontology"),
	}, runOntologyRegistered),
	ontologyCheckRegistration(CheckDescriptor{
		Name: CheckIdentifiers, Description: "preferred identifier integrity", Category: CheckCategorySchema,
		Suites: coreSuites, Applicability: ApplicabilityOntology,
		ProjectionDomains: []ProjectionDomain{ProjectionValidation, ProjectionOntology}, ExecutionSurfaces: allSurfaces,
		PrerequisiteMode: PrerequisiteAutoManaged, MissingPrerequisiteOutcome: PrerequisiteBlocked,
		Remediation: remediationPlan("identifiers"),
	}, runIdentifiersRegistered),
	checkRegistration(CheckDescriptor{
		Name: CheckBrokenLinks, Description: "broken internal note, heading, and block targets", Category: CheckCategoryLinks,
		Suites: coreSuites, Applicability: ApplicabilityAlways,
		ProjectionDomains: []ProjectionDomain{ProjectionValidation}, ExecutionSurfaces: allSurfaces,
		PrerequisiteMode: PrerequisiteAutoManaged, MissingPrerequisiteOutcome: PrerequisiteBlocked,
		Remediation: remediationPlan("broken-links"),
	}, func(ctx context.Context, runCtx RunContext, opts Options, _ *ontology.Runtime, _ error) CheckResult {
		return RunBrokenLinksContext(ctx, runCtx, opts)
	}),
	checkRegistration(CheckDescriptor{
		Name: CheckLinkHygiene, Description: "internal link hygiene", Category: CheckCategoryLinks,
		Suites: allSuites, Applicability: ApplicabilityAlways,
		ProjectionDomains: []ProjectionDomain{ProjectionValidation}, ExecutionSurfaces: allSurfaces,
		PrerequisiteMode: PrerequisiteAutoManaged, MissingPrerequisiteOutcome: PrerequisiteBlocked, Remediation: remediationPlan("link-hygiene"),
	}, func(_ context.Context, runCtx RunContext, _ Options, _ *ontology.Runtime, _ error) CheckResult {
		return RunLinkHygiene(runCtx)
	}),
	checkRegistration(CheckDescriptor{
		Name: CheckQueryRecipes, Description: "saved ontology query recipes", Category: CheckCategoryConfig,
		Suites: allSuites, Applicability: ApplicabilityQueryRecipes,
		ProjectionDomains: []ProjectionDomain{ProjectionValidation, ProjectionOntology}, ExecutionSurfaces: allSurfaces,
		PrerequisiteMode: PrerequisiteAutoManaged, MissingPrerequisiteOutcome: PrerequisiteBlocked, Remediation: remediationPlan("query-recipes"),
	}, func(ctx context.Context, runCtx RunContext, _ Options, _ *ontology.Runtime, _ error) CheckResult {
		return RunQueryRecipes(ctx, runCtx)
	}),
	checkRegistration(CheckDescriptor{
		Name: CheckViews, Description: "configured views", Category: CheckCategoryConfig,
		Suites: allSuites, Applicability: ApplicabilityViews,
		ProjectionDomains: []ProjectionDomain{ProjectionValidation, ProjectionOntology}, ExecutionSurfaces: allSurfaces,
		PrerequisiteMode: PrerequisiteAutoManaged, MissingPrerequisiteOutcome: PrerequisiteBlocked, Remediation: RemediationSupport{Kind: RemediationNone},
	}, func(ctx context.Context, runCtx RunContext, _ Options, _ *ontology.Runtime, _ error) CheckResult {
		return RunViews(ctx, runCtx)
	}),
	checkRegistration(CheckDescriptor{
		Name: CheckSkillOverlays, Description: "bundled skill overlays", Category: CheckCategoryConfig,
		Suites: allSuites, Applicability: ApplicabilityAlways,
		ProjectionDomains: []ProjectionDomain{ProjectionValidation}, ExecutionSurfaces: allSurfaces,
		PrerequisiteMode: PrerequisiteAutoManaged, MissingPrerequisiteOutcome: PrerequisiteBlocked, Remediation: RemediationSupport{Kind: RemediationNone},
	}, func(_ context.Context, runCtx RunContext, _ Options, _ *ontology.Runtime, _ error) CheckResult {
		return RunSkillOverlays(runCtx)
	}),
	checkRegistration(CheckDescriptor{
		Name: CheckCodeFrontmatter, Description: "code documentation frontmatter", Category: CheckCategoryCode,
		Suites: allSuites, Applicability: ApplicabilityCode,
		ProjectionDomains: []ProjectionDomain{ProjectionValidation}, ExecutionSurfaces: allSurfaces,
		PrerequisiteMode: PrerequisiteAutoManaged, MissingPrerequisiteOutcome: PrerequisiteBlocked, Remediation: remediationPlan("code-frontmatter"),
	}, func(ctx context.Context, runCtx RunContext, _ Options, _ *ontology.Runtime, _ error) CheckResult {
		return RunCodeFrontmatter(ctx, runCtx)
	}),
	checkRegistration(CheckDescriptor{
		Name: CheckCodeAnchors, Description: "code anchor matches", Category: CheckCategoryCode,
		Suites: allSuites, Applicability: ApplicabilityCodeAnchors,
		ProjectionDomains: []ProjectionDomain{ProjectionValidation, ProjectionCode}, ExecutionSurfaces: []ExecutionSurface{SurfaceLocal, SurfaceAgent},
		PrerequisiteMode: PrerequisiteExternal, MissingPrerequisiteOutcome: PrerequisiteBlocked, PreparationCommand: "rzm index", Remediation: remediationPlan("code-anchors"),
	}, runCodeAnchorsRegistered),
	checkRegistration(CheckDescriptor{
		Name: CheckCompanionDocs, Description: "subsystem companion documentation", Category: CheckCategoryCode,
		Suites: allSuites, Applicability: ApplicabilityCode,
		ProjectionDomains: []ProjectionDomain{ProjectionValidation}, ExecutionSurfaces: allSurfaces,
		PrerequisiteMode: PrerequisiteAutoManaged, MissingPrerequisiteOutcome: PrerequisiteBlocked, Remediation: remediationPlan("companion-docs"),
	}, func(_ context.Context, runCtx RunContext, _ Options, _ *ontology.Runtime, _ error) CheckResult {
		return RunCompanionDocs(runCtx)
	}),
	ontologyCheckRegistration(CheckDescriptor{
		Name: CheckFrozenScopeDrift, Description: "active effort frozen-scope drift", Category: CheckCategoryLifecycle,
		Suites: auditSuites, Applicability: ApplicabilityEfforts,
		ProjectionDomains: []ProjectionDomain{ProjectionValidation, ProjectionOntology}, ExecutionSurfaces: allSurfaces,
		PrerequisiteMode: PrerequisiteAutoManaged, MissingPrerequisiteOutcome: PrerequisiteBlocked, Remediation: RemediationSupport{Kind: RemediationNone},
	}, runFrozenScopeDriftRegistered),
	checkRegistration(CheckDescriptor{
		Name: CheckFragileExternal, Description: "fragile external heading links", Category: CheckCategoryLinks,
		Suites: auditSuites, Applicability: ApplicabilityAlways,
		ProjectionDomains: []ProjectionDomain{ProjectionValidation}, ExecutionSurfaces: allSurfaces,
		PrerequisiteMode: PrerequisiteAutoManaged, MissingPrerequisiteOutcome: PrerequisiteBlocked, Remediation: remediationPlan("fragile-external"),
	}, func(ctx context.Context, runCtx RunContext, opts Options, _ *ontology.Runtime, _ error) CheckResult {
		return RunFragileExternal(ctx, runCtx, opts)
	}),
	checkRegistration(CheckDescriptor{
		Name: CheckOrphanBlockIDs, Description: "orphaned block identifiers", Category: CheckCategoryLinks,
		Suites: auditSuites, Applicability: ApplicabilityOntology,
		ProjectionDomains: []ProjectionDomain{ProjectionValidation, ProjectionOntology}, ExecutionSurfaces: allSurfaces,
		PrerequisiteMode: PrerequisiteAutoManaged, MissingPrerequisiteOutcome: PrerequisiteBlocked, Remediation: remediationPlan("orphan-block-ids"),
	}, func(ctx context.Context, runCtx RunContext, _ Options, _ *ontology.Runtime, _ error) CheckResult {
		return RunOrphanBlockIDs(ctx, runCtx)
	}),
	checkRegistration(CheckDescriptor{
		Name: CheckPlaceholderLinks, Description: "placeholder links to notes that never existed", Category: CheckCategoryLinks,
		Suites: auditSuites, Applicability: ApplicabilityAlways,
		ProjectionDomains: []ProjectionDomain{ProjectionValidation}, ExecutionSurfaces: allSurfaces,
		PrerequisiteMode: PrerequisiteAutoManaged, MissingPrerequisiteOutcome: PrerequisiteBlocked, Remediation: RemediationSupport{Kind: RemediationNone},
	}, func(ctx context.Context, runCtx RunContext, opts Options, _ *ontology.Runtime, _ error) CheckResult {
		return RunPlaceholderLinks(ctx, runCtx, opts)
	}),
}

func remediationPlan(check string) RemediationSupport {
	return RemediationSupport{Kind: RemediationPlan, Command: "rzm validate fix " + check}
}

func checkRegistration(descriptor CheckDescriptor, run checkRunner) registeredCheck {
	identity, ok := validationcatalog.Lookup(descriptor.Name)
	if !ok {
		panic("validation check identity is missing from catalog: " + descriptor.Name)
	}
	descriptor.Name = identity.ID
	descriptor.CLIName = identity.Name
	descriptor.Aliases = slices.Clone(identity.Aliases)
	return registeredCheck{CheckDescriptor: descriptor, RuntimeRequirement: runtimeRequirementNone, Run: run}
}

func ontologyCheckRegistration(descriptor CheckDescriptor, run checkRunner) registeredCheck {
	registration := checkRegistration(descriptor, run)
	registration.RuntimeRequirement = runtimeRequirementOntology
	return registration
}

func runOntologyRegistered(ctx context.Context, runCtx RunContext, _ Options, runtime *ontology.Runtime, runtimeErr error) CheckResult {
	if runtime != nil {
		return runOntologyWithRuntime(ctx, runCtx, runtime)
	}
	if runtimeErr != nil {
		return sharedRuntimeError(CheckOntology, runtimeErr)
	}
	return RunOntology(ctx, runCtx)
}

func runIdentifiersRegistered(ctx context.Context, runCtx RunContext, _ Options, runtime *ontology.Runtime, runtimeErr error) CheckResult {
	if runtime != nil {
		return RunIdentifiersWithRuntime(ctx, runCtx, runtime)
	}
	if runtimeErr != nil {
		return sharedRuntimeError(CheckIdentifiers, runtimeErr)
	}
	return RunIdentifiers(ctx, runCtx)
}

func runFrozenScopeDriftRegistered(ctx context.Context, runCtx RunContext, _ Options, runtime *ontology.Runtime, runtimeErr error) CheckResult {
	if runtime != nil {
		return RunFrozenScopeDriftWithRuntime(ctx, runCtx, runtime)
	}
	if runtimeErr != nil {
		return sharedRuntimeError(CheckFrozenScopeDrift, runtimeErr)
	}
	return RunFrozenScopeDrift(ctx, runCtx)
}

func runCodeAnchorsRegistered(ctx context.Context, runCtx RunContext, _ Options, runtime *ontology.Runtime, _ error) CheckResult {
	if runtime != nil && runtime.Store != nil {
		return RunCodeAnchorsWithStore(ctx, runCtx, runtime.Store)
	}
	return RunCodeAnchors(ctx, runCtx)
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

// CheckHelpText renders the shared CLI check list from the registry.
func CheckHelpText() string {
	items := make([]string, 0, len(checkRegistry))
	for _, registration := range checkRegistry {
		item := registration.CLIName
		if !slices.Contains(registration.Suites, SuiteAll) {
			item += " (audit)"
		}
		items = append(items, item)
	}
	return "exactly one validation selector: default, all, audit, or one check: " + strings.Join(items, ", ")
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
		if ok && registration.RuntimeRequirement == runtimeRequirementOntology {
			return true
		}
	}
	return false
}

func validateCheckRegistry(registry []registeredCheck) error {
	names := make(map[string]struct{}, len(registry))
	aliases := make(map[string]string)
	for _, registration := range registry {
		if registration.Name == "" || registration.CLIName == "" || registration.Description == "" || registration.Category == "" ||
			len(registration.Suites) == 0 || registration.Applicability == "" || len(registration.ProjectionDomains) == 0 ||
			len(registration.ExecutionSurfaces) == 0 || registration.PrerequisiteMode == "" || registration.MissingPrerequisiteOutcome == "" ||
			registration.Remediation.Kind == "" || registration.RuntimeRequirement == "" || registration.Run == nil {
			return fmt.Errorf("incomplete validation check descriptor %q", registration.Name)
		}
		if registration.PrerequisiteMode == PrerequisiteExternal && registration.PreparationCommand == "" {
			return fmt.Errorf("blocked validation check %q must declare a preparation command", registration.Name)
		}
		if registration.PrerequisiteMode == PrerequisiteAutoManaged && registration.PreparationCommand != "" {
			return fmt.Errorf("auto-managed validation check %q must not declare a preparation command", registration.Name)
		}
		if !validCategory(registration.Category) {
			return fmt.Errorf("validation check %q has unknown category %q", registration.Name, registration.Category)
		}
		if !validApplicability(registration.Applicability) {
			return fmt.Errorf("validation check %q has unknown applicability %q", registration.Name, registration.Applicability)
		}
		if !validPrerequisiteOutcome(registration.MissingPrerequisiteOutcome) {
			return fmt.Errorf("validation check %q has unknown missing-prerequisite outcome %q", registration.Name, registration.MissingPrerequisiteOutcome)
		}
		if registration.PrerequisiteMode != PrerequisiteAutoManaged && registration.PrerequisiteMode != PrerequisiteExternal {
			return fmt.Errorf("validation check %q has unknown prerequisite mode %q", registration.Name, registration.PrerequisiteMode)
		}
		if registration.Remediation.Kind != RemediationNone && registration.Remediation.Kind != RemediationPlan {
			return fmt.Errorf("validation check %q has unknown remediation kind %q", registration.Name, registration.Remediation.Kind)
		}
		if err := validateDescriptorValues(registration); err != nil {
			return err
		}
		if message := prerequisiteMetadataError(registration.CheckDescriptor); message != "" {
			return fmt.Errorf("validation check %q %s", registration.Name, message)
		}
		if registration.RuntimeRequirement != runtimeRequirementNone && registration.RuntimeRequirement != runtimeRequirementOntology {
			return fmt.Errorf("validation check %q has unknown runtime requirement %q", registration.Name, registration.RuntimeRequirement)
		}
		if registration.RuntimeRequirement == runtimeRequirementOntology && !slices.Contains(registration.ProjectionDomains, ProjectionOntology) {
			return fmt.Errorf("validation check %q ontology runtime requires ontology projection domain", registration.Name)
		}
		if registration.Remediation.Kind == RemediationPlan && registration.Remediation.Command == "" {
			return fmt.Errorf("validation check %q remediation plan must declare a command", registration.Name)
		}
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
			normalized := normalizeCheckName(alias)
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

func validateDescriptorValues(registration registeredCheck) error {
	seenSuites := make(map[CheckSuite]struct{}, len(registration.Suites))
	for _, suite := range registration.Suites {
		if suite != SuiteDefault && suite != SuiteAll && suite != SuiteAudit {
			return fmt.Errorf("validation check %q has unknown suite %q", registration.Name, suite)
		}
		if _, duplicate := seenSuites[suite]; duplicate {
			return fmt.Errorf("validation check %q repeats suite %q", registration.Name, suite)
		}
		seenSuites[suite] = struct{}{}
	}
	seenDomains := make(map[ProjectionDomain]struct{}, len(registration.ProjectionDomains))
	for _, domain := range registration.ProjectionDomains {
		if domain != ProjectionValidation && domain != ProjectionOntology && domain != ProjectionCode {
			return fmt.Errorf("validation check %q has unknown projection domain %q", registration.Name, domain)
		}
		if _, duplicate := seenDomains[domain]; duplicate {
			return fmt.Errorf("validation check %q repeats projection domain %q", registration.Name, domain)
		}
		seenDomains[domain] = struct{}{}
	}
	seenSurfaces := make(map[ExecutionSurface]struct{}, len(registration.ExecutionSurfaces))
	for _, surface := range registration.ExecutionSurfaces {
		if surface != SurfaceLocal && surface != SurfaceCI && surface != SurfaceAgent {
			return fmt.Errorf("validation check %q has unknown execution surface %q", registration.Name, surface)
		}
		if _, duplicate := seenSurfaces[surface]; duplicate {
			return fmt.Errorf("validation check %q repeats execution surface %q", registration.Name, surface)
		}
		seenSurfaces[surface] = struct{}{}
	}
	return nil
}

func validCategory(category CheckCategory) bool {
	return category == CheckCategoryLinks || category == CheckCategorySchema || category == CheckCategoryCode ||
		category == CheckCategoryConfig || category == CheckCategoryLifecycle
}

func validApplicability(applicability CheckApplicability) bool {
	return applicability == ApplicabilityAlways || applicability == ApplicabilityOntology || applicability == ApplicabilityCode ||
		applicability == ApplicabilityCodeAnchors ||
		applicability == ApplicabilityQueryRecipes || applicability == ApplicabilityViews || applicability == ApplicabilityEfforts
}

func validPrerequisiteOutcome(outcome MissingPrerequisiteOutcome) bool {
	return outcome == PrerequisiteNone || outcome == PrerequisiteNotApplicable || outcome == PrerequisiteBlocked
}

func init() {
	if err := validateCheckRegistry(checkRegistry); err != nil {
		panic(err)
	}
}
