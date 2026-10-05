package init

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBaseSkillBundleRemainsStableAcrossStarterComposition(t *testing.T) {
	base, err := loadSkillTemplates()
	require.NoError(t, err)

	cases := []struct {
		name     string
		starters []string
	}{
		{name: "no starter"},
		{name: "agentic engineering", starters: []string{templateAgenticEngineering}},
		{name: "agentic engineering plus complex domain", starters: []string{templateAgenticEngineering, templateComplexDomain}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			composed, _, err := loadAllSkillTemplatesWithReport(tc.starters)
			require.NoError(t, err)
			if len(tc.starters) == 0 {
				requireSkillTemplateSetsEqual(t, base, composed)
			}
			requireBaseSkillTemplatesEqual(t, base, composed)
		})
	}
}

func TestBaseRhizomeBundleDoesNotAssumeStarterContracts(t *testing.T) {
	base, err := loadSkillTemplates()
	require.NoError(t, err)
	rhizome := requireSkillTemplate(t, base, coreRhizomeSkillName)

	// Keep this denylist limited to identifiers that name a starter, ontology
	// type, or saved recipe. Generic words such as "artifact", "project",
	// "source", and "recipe" are valid in universal mechanics and belong out
	// of this guard.
	forbidden := []string{
		"action-items",
		"agentic-engineering",
		"complex-domain",
		"project-kb",
		"ActionItem",
		"DomainConcept",
		"DomainProcess",
		"EffortNote",
		"EvidenceClaim",
		"PromptRecipe",
		"ProjectUpdate",
		"CurationRun",
		"Requirement",
		"RequirementSource",
		"UserWorkflow",
	}
	for _, template := range []string{templateCore, templateActionItems, templateAgenticEngineering, templateComplexDomain} {
		for id := range starterQueryRecipeIDs(t, template) {
			forbidden = append(forbidden, id)
		}
	}
	sort.Strings(forbidden)

	for _, file := range rhizome.Files {
		body := string(file.Content)
		for _, term := range forbidden {
			require.Falsef(t, baseContractTokenPresent(body, term), "base skill %s/%s names starter-owned contract %q", rhizome.Name, file.Path, term)
		}
	}
}

func baseContractTokenPresent(body, token string) bool {
	for offset := 0; offset <= len(body)-len(token); {
		relative := strings.Index(body[offset:], token)
		if relative < 0 {
			return false
		}
		start := offset + relative
		end := start + len(token)
		if (start == 0 || !baseContractIdentifierChar(body[start-1])) &&
			(end == len(body) || !baseContractIdentifierChar(body[end])) {
			return true
		}
		offset = end
	}
	return false
}

func baseContractIdentifierChar(char byte) bool {
	return char == '_' ||
		(char >= 'a' && char <= 'z') ||
		(char >= 'A' && char <= 'Z') ||
		(char >= '0' && char <= '9')
}

func requireBaseSkillTemplatesEqual(t *testing.T, expected, actual []skillTemplate) {
	t.Helper()
	for _, want := range expected {
		got := requireSkillTemplate(t, actual, want.Name)
		require.Equal(t, want.Files, got.Files, "base skill %q changed during starter composition", want.Name)
	}
}

func requireSkillTemplateSetsEqual(t *testing.T, expected, actual []skillTemplate) {
	t.Helper()
	require.Len(t, actual, len(expected), "no-starter composition should contain exactly the base skill set")

	expectedNames := make([]string, 0, len(expected))
	actualNames := make([]string, 0, len(actual))
	for _, skill := range expected {
		expectedNames = append(expectedNames, skill.Name)
	}
	for _, skill := range actual {
		actualNames = append(actualNames, skill.Name)
	}
	require.ElementsMatch(t, expectedNames, actualNames)
}
