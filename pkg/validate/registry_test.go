package validate

import (
	"context"
	"strings"
	"testing"

	initactions "github.com/atomicobject/rhizome/pkg/app/cli/init"
	"github.com/atomicobject/rhizome/pkg/ontology"
	validationcatalog "github.com/atomicobject/rhizome/pkg/validate/catalog"
	"github.com/stretchr/testify/require"
)

func TestCheckRegistryMatchesSharedIdentityCatalog(t *testing.T) {
	catalogChecks := validationcatalog.Checks()
	require.Len(t, catalogChecks, len(checkRegistry))
	seen := make(map[string]struct{}, len(checkRegistry))
	seenNames := make(map[string]string)
	for _, identity := range catalogChecks {
		for _, name := range append([]string{identity.ID, identity.Name}, identity.Aliases...) {
			normalized := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), "-", "_")
			if prior, exists := seenNames[normalized]; exists {
				require.Equal(t, prior, identity.ID, "validation selector %q maps to multiple checks", name)
				continue
			}
			seenNames[normalized] = identity.ID
		}
	}
	for _, registration := range checkRegistry {
		identity, ok := validationcatalog.Lookup(registration.Name)
		require.True(t, ok, registration.Name)
		require.Equal(t, identity.Name, registration.CLIName)
		require.Equal(t, identity.Aliases, registration.Aliases)
		_, duplicate := seen[identity.ID]
		require.False(t, duplicate, identity.ID)
		seen[identity.ID] = struct{}{}
	}
}

func TestCheckRegistryDeclaresCompletePublicMetadata(t *testing.T) {
	require.NoError(t, validateCheckRegistry(checkRegistry))

	descriptors := CheckDescriptors()
	require.NotEmpty(t, descriptors)
	for _, descriptor := range descriptors {
		require.NotEmpty(t, descriptor.Name)
		require.NotEmpty(t, descriptor.CLIName)
		require.NotEmpty(t, descriptor.Description)
		require.NotEmpty(t, descriptor.Suites)
		require.NotEmpty(t, descriptor.Applicability)
		require.NotEmpty(t, descriptor.ProjectionDomains)
		require.NotEmpty(t, descriptor.ExecutionSurfaces)
		require.NotEmpty(t, descriptor.PrerequisiteMode)
		require.NotEmpty(t, descriptor.MissingPrerequisiteOutcome)
		require.NotEmpty(t, descriptor.Remediation.Kind)

		canonical, ok := CanonicalCheck(descriptor.CLIName)
		require.True(t, ok, descriptor.CLIName)
		require.Equal(t, descriptor.Name, canonical)
	}
}

func TestCheckRegistryHasStableCoreAndSuiteOrder(t *testing.T) {
	core := []string{CheckOntology, CheckIdentifiers, CheckBrokenLinks}
	all := []string{
		CheckOntology,
		CheckIdentifiers,
		CheckBrokenLinks,
		CheckLinkHygiene,
		CheckQueryRecipes,
		CheckViews,
		CheckSkillOverlays,
		CheckCodeFrontmatter,
		CheckCodeAnchors,
		CheckCompanionDocs,
	}
	audit := []string{CheckFrozenScopeDrift, CheckFragileExternal, CheckOrphanBlockIDs, CheckPlaceholderLinks}

	require.Equal(t, core, DefaultChecks)
	require.Equal(t, core, builtInSuiteChecks(SuiteDefault))
	require.Equal(t, all, builtInSuiteChecks(SuiteAll))
	require.Equal(t, audit, builtInSuiteChecks(SuiteAudit))
	for _, maintenanceCheck := range audit {
		require.NotContains(t, all, maintenanceCheck)
	}
}

func TestIdentifiersOwnsLegacyAliases(t *testing.T) {
	for _, alias := range []string{"identifiers", "identifier", "aliases", "alias"} {
		canonical, ok := CanonicalCheck(alias)
		require.True(t, ok, alias)
		require.Equal(t, CheckIdentifiers, canonical)
	}
	require.Equal(t, CheckIdentifiers, CheckAliases)
}

func TestCheckDescriptorsReturnsIndependentSlices(t *testing.T) {
	descriptors := CheckDescriptors()
	descriptors[0].Aliases[0] = "mutated"
	descriptors[0].Suites[0] = "mutated"
	descriptors[0].ProjectionDomains[0] = "mutated"
	descriptors[0].ExecutionSurfaces[0] = "mutated"

	fresh := CheckDescriptors()[0]
	require.NotEqual(t, "mutated", fresh.Aliases[0])
	require.NotEqual(t, CheckSuite("mutated"), fresh.Suites[0])
	require.NotEqual(t, ProjectionDomain("mutated"), fresh.ProjectionDomains[0])
	require.NotEqual(t, ExecutionSurface("mutated"), fresh.ExecutionSurfaces[0])
}

func TestCheckRegistryRejectsIncompleteDuplicateAndAmbiguousDescriptors(t *testing.T) {
	runner := func(context.Context, RunContext, Options, *ontology.Runtime, error) CheckResult { return CheckResult{} }
	base := registeredCheck{
		CheckDescriptor: CheckDescriptor{
			Name:                       "one",
			CLIName:                    "one",
			Description:                "one",
			Category:                   CheckCategoryConfig,
			Suites:                     []CheckSuite{SuiteAll},
			Applicability:              ApplicabilityAlways,
			ProjectionDomains:          []ProjectionDomain{ProjectionValidation},
			ExecutionSurfaces:          []ExecutionSurface{SurfaceLocal, SurfaceCI},
			PrerequisiteMode:           PrerequisiteAutoManaged,
			MissingPrerequisiteOutcome: PrerequisiteBlocked,
			Remediation:                RemediationSupport{Kind: RemediationNone},
		},
		RuntimeRequirement: runtimeRequirementNone,
		Run:                runner,
	}

	tests := []struct {
		name     string
		registry []registeredCheck
		want     string
	}{
		{name: "incomplete", registry: []registeredCheck{{}}, want: "incomplete validation check descriptor"},
		{name: "identity", registry: []registeredCheck{base, base}, want: "duplicate validation check"},
		{name: "reserved selector", registry: []registeredCheck{withRegistryAlias(base, "default")}, want: "reserved validation selector"},
		{name: "invalid suite", registry: []registeredCheck{withRegistrySuites(base, []CheckSuite{"mystery"})}, want: "unknown suite"},
		{name: "all and audit overlap", registry: []registeredCheck{withRegistrySuites(base, []CheckSuite{SuiteAll, SuiteAudit})}, want: "cannot belong to both all and audit"},
		{name: "invalid runtime requirement", registry: []registeredCheck{withRuntimeRequirement(base, runtimeRequirement("mystery"))}, want: "unknown runtime requirement"},
		{name: "ontology runtime without ontology domain", registry: []registeredCheck{withRuntimeRequirement(base, runtimeRequirementOntology)}, want: "requires ontology projection domain"},
		{
			name:     "code projection declared auto managed",
			registry: []registeredCheck{withRegistryPrerequisites(base, []ProjectionDomain{ProjectionValidation, ProjectionCode}, PrerequisiteAutoManaged, "")},
			want:     "code projection requires prerequisite mode",
		},
		{
			name:     "non-code projection declared external",
			registry: []registeredCheck{withRegistryPrerequisites(base, []ProjectionDomain{ProjectionValidation}, PrerequisiteExternal, "rzm index")},
			want:     "non-code projections require prerequisite mode",
		},
		{
			name: "alias",
			registry: []registeredCheck{base, {
				CheckDescriptor: CheckDescriptor{
					Name:                       "two",
					CLIName:                    "two",
					Description:                "two",
					Category:                   CheckCategoryConfig,
					Aliases:                    []string{"one"},
					Suites:                     []CheckSuite{SuiteAll},
					Applicability:              ApplicabilityAlways,
					ProjectionDomains:          []ProjectionDomain{ProjectionValidation},
					ExecutionSurfaces:          []ExecutionSurface{SurfaceLocal, SurfaceCI},
					PrerequisiteMode:           PrerequisiteAutoManaged,
					MissingPrerequisiteOutcome: PrerequisiteBlocked,
					Remediation:                RemediationSupport{Kind: RemediationNone},
				},
				RuntimeRequirement: runtimeRequirementNone,
				Run:                runner,
			}},
			want: "belongs to both",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCheckRegistry(tt.registry)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func withRegistryAlias(registration registeredCheck, alias string) registeredCheck {
	registration.Aliases = []string{alias}
	return registration
}

func withRegistrySuites(registration registeredCheck, suites []CheckSuite) registeredCheck {
	registration.Suites = suites
	return registration
}

func withRuntimeRequirement(registration registeredCheck, requirement runtimeRequirement) registeredCheck {
	registration.RuntimeRequirement = requirement
	return registration
}

func withRegistryPrerequisites(registration registeredCheck, domains []ProjectionDomain, mode PrerequisiteMode, command string) registeredCheck {
	registration.ProjectionDomains = domains
	registration.PrerequisiteMode = mode
	registration.PreparationCommand = command
	return registration
}

func TestRegistryRuntimeRequirementDrivesSharedOntologyAcquisition(t *testing.T) {
	require.True(t, registryNeedsOntologyRuntime([]string{CheckOntology}))
	require.True(t, registryNeedsOntologyRuntime([]string{CheckIdentifiers}))
	require.True(t, registryNeedsOntologyRuntime([]string{CheckFrozenScopeDrift}))
	require.False(t, registryNeedsOntologyRuntime([]string{CheckBrokenLinks, CheckQueryRecipes, CheckOrphanBlockIDs}))
}

func TestSkillOverlayCheckPreservesDeterministicIssuesForCentralizedTruncation(t *testing.T) {
	manifest := initactions.SkillOverlayManifest{Issues: []initactions.SkillOverlayIssue{
		{Code: "z", Path: "b", Message: "third"},
		{Code: "b", Path: "a", Message: "second"},
		{Code: "a", Path: "a", Message: "first"},
	}}

	result := skillOverlayCheckResult(manifest)

	require.Equal(t, 3, result.IssueCount)
	require.Len(t, result.Issues, 3)
	require.Equal(t, "a", result.Issues[0].Code)
	require.Equal(t, "b", result.Issues[1].Code)
	finalized := finalizeCheckResult(CheckSkillOverlays, result, 0, 2)
	require.Len(t, finalized.Issues, 2)
	require.Len(t, finalized.allIssueKeys, 3)
}
