package init

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/skilloverlay"
	"github.com/stretchr/testify/require"
)

func TestLoadAllSkillTemplatesStripsAgenticEngineeringSlotMarkers(t *testing.T) {
	skills, err := loadAllSkillTemplates([]string{templateAgenticEngineering})
	require.NoError(t, err)

	for _, name := range []string{"agentic-engineering", "ingest-transcript"} {
		for _, file := range requireSkillTemplate(t, skills, name).Files {
			body := string(file.Content)
			require.NotContains(t, body, "rzm:skill-slot")
			require.NotContains(t, body, "/rzm:skill-slot")
		}
	}
}

func TestLoadAllSkillTemplatesAppliesComplexDomainOverlays(t *testing.T) {
	skills, manifest, err := loadAllSkillTemplatesWithReport([]string{templateAgenticEngineering, templateComplexDomain, templateActionItems})
	require.NoError(t, err)

	for _, name := range []string{"agentic-engineering", "ingest-transcript"} {
		for _, file := range requireSkillTemplate(t, skills, name).Files {
			body := string(file.Content)
			require.NotContains(t, body, "rzm:skill-slot")
			require.NotContains(t, body, "/rzm:skill-slot")
		}
	}

	router := requireSkillTemplate(t, skills, "agentic-engineering")
	specify := string(requireSkillTemplateFile(t, router, "references/specification.md").Content)
	require.Contains(t, specify, "domain-topic-survey")
	require.Contains(t, specify, "candidate requirements that should not yet become delivery commitments")

	plan := string(requireSkillTemplateFile(t, router, "references/planning.md").Content)
	require.Contains(t, plan, "spec-domain-context-pack")
	require.Contains(t, plan, "complex-domain.uncovered-requirements")

	implement := string(requireSkillTemplateFile(t, router, "references/implementation.md").Content)
	require.Contains(t, implement, "requirement-trace-pack")
	require.Contains(t, implement, "domain-backport")
	require.Contains(t, implement, "misunderstood domain rules")

	compounding := string(requireSkillTemplateFile(t, router, "references/compounding.md").Content)
	require.Contains(t, compounding, "coverage and stale-source views")

	require.ElementsMatch(t, []string{
		"agentic-engineering:alignment.additional-context",
		"agentic-engineering:reconciliation.additional-targets",
		"agentic-engineering:routing.additional-compounding",
		"agentic-engineering:scope.additional-context",
		"agentic-engineering:inputs.additional-context",
		"agentic-engineering:docs.additional-traceability",
		"ingest-transcript:source.additional-preflight",
		"ingest-transcript:synthesis.additional-tracks",
		"agentic-engineering:integration-map.additional-dimensions",
		"agentic-engineering:architecture-decisions.additional-checks",
		"agentic-engineering:context.after-discovery",
		"agentic-engineering:context.constraint-extraction",
	}, uniqueManifestEntryTargets(manifest))
}

func requireSkillTemplate(t *testing.T, skills []skilloverlay.Skill, name string) skilloverlay.Skill {
	t.Helper()
	for _, skill := range skills {
		if skill.Name == name {
			return skill
		}
	}
	t.Fatalf("expected skill template %q in %#v", name, skills)
	return skilloverlay.Skill{}
}

func requireSkillTemplateFile(t *testing.T, skill skilloverlay.Skill, filePath string) skilloverlay.File {
	t.Helper()
	for _, file := range skill.Files {
		if file.Path == filePath {
			return file
		}
	}
	t.Fatalf("expected skill template %q file %q in %#v", skill.Name, filePath, skill.Files)
	return skilloverlay.File{}
}

func manifestEntryTargets(manifest skilloverlay.Manifest) []string {
	out := make([]string, 0, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		out = append(out, entry.TargetSkill+":"+entry.Slot)
	}
	return out
}

func uniqueManifestEntryTargets(manifest skilloverlay.Manifest) []string {
	seen := make(map[string]struct{}, len(manifest.Entries))
	var targets []string
	for _, target := range manifestEntryTargets(manifest) {
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		targets = append(targets, target)
	}
	return targets
}
