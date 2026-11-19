package init

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The effort gates of SPEC-0038 US13. A team turns each one on by listing it in
// docs/engineering/review-and-approval.md.
var gateSkillNames = []string{"effort-research", "effort-discuss", "plan-check", "phase-code-review"}

const gatePolicyDoc = "docs/engineering/review-and-approval.md"

var gateDocsPath = regexp.MustCompile(`docs/[A-Za-z0-9_./-]+`)

func gateSkillDescription(t *testing.T, body string) string {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if rest, ok := strings.CutPrefix(line, "description: "); ok {
			return rest
		}
	}
	t.Fatalf("no single-line description in %q", body[:min(len(body), 120)])
	return ""
}

func starterTemplateText(t *testing.T, parts ...string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(append([]string{"templates", "starters", templateAgenticEngineering}, parts...)...))
	require.NoError(t, err)
	return string(data)
}

// requireOneLineWith fails unless a single line of body contains every want.
func requireOneLineWith(t *testing.T, body, label string, wants ...string) {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		all := true
		for _, want := range wants {
			all = all && strings.Contains(line, want)
		}
		if all {
			return
		}
	}
	t.Fatalf("%s has no single line containing all of %q", label, wants)
}

func TestGateSkillsArePortableAndBounded(t *testing.T) {
	skills := mustLoadStarterSkills(t)
	withDomain, _, err := loadAllSkillTemplatesWithReport([]string{templateAgenticEngineering, templateComplexDomain, templateActionItems})
	require.NoError(t, err)

	neighbors := map[string][]string{
		"effort-research":   {"`agentic-engineering specify`", "`foundation-review`"},
		"effort-discuss":    {"`agentic-engineering specify`", "`foundation-review`"},
		"plan-check":        {"`foundation-review`"},
		"phase-code-review": {"`foundation-review`", "pull request"},
	}
	for _, name := range gateSkillNames {
		body := string(requireSkillTemplateFile(t, requireSkillTemplate(t, skills, name), "SKILL.md").Content)

		for _, banned := range []string{"docs/specs/", "AGENTS.md", "alignment review", "complex effort", "Complex effort"} {
			require.NotContainsf(t, body, banned, "%s names something a downstream repository does not have", name)
		}
		for _, retired := range retiredStarterSkillNames {
			require.NotContainsf(t, body, "`"+retired+"`", "%s points at the retired skill %s", name, retired)
		}
		for _, docPath := range gateDocsPath.FindAllString(body, -1) {
			require.Truef(t, strings.HasPrefix(docPath, "docs/engineering/"), "%s names %s, outside docs/engineering/", name, docPath)
		}

		description := gateSkillDescription(t, body)
		require.Containsf(t, description, gatePolicyDoc, "%s must say local policy turns it on", name)
		_, notFor, found := strings.Cut(description, "Not for")
		require.Truef(t, found, "%s description needs a Not for clause", name)
		for _, neighbor := range neighbors[name] {
			require.Containsf(t, notFor, neighbor, "%s Not for clause must name %s", name, neighbor)
		}

		for _, want := range []string{"adoption", "created-at", "git log -S", "workspace", "linked plan document"} {
			require.Containsf(t, body, want, "%s lacks %q", name, want)
		}

		rendered := string(requireSkillTemplateFile(t, requireSkillTemplate(t, withDomain, name), "SKILL.md").Content)
		require.Equalf(t, body, rendered, "%s must render unchanged when complex-domain is installed", name)
	}

	review := skillBodyFromTemplates(t, templateAgenticEngineering, "phase-code-review")
	for _, want := range []string{
		"adoption commit", "requires-human-verification", "merge-base",
		"one revertible change per finding", "commit policy", "degraded: no fresh-context reviewer",
	} {
		require.Contains(t, review, want)
	}
	require.NotContains(t, review, "sign-off before the phase proceeds")
	require.Contains(t, skillBodyFromTemplates(t, templateAgenticEngineering, "plan-check"), "degraded: no fresh-context reviewer")
}

func TestGateSkillsAreTurnedOnByLocalPolicy(t *testing.T) {
	policy := starterTemplateText(t, "repo", "docs", "engineering", "review-and-approval.md")
	require.Contains(t, policy, "## Gates")
	require.NotContains(t, policy, "It is not a standing rule after every phase.")

	router := skillBodyFromTemplates(t, templateAgenticEngineering, "agentic-engineering")
	block := starterTemplateText(t, "agents", "AGENTS.md")
	require.Contains(t, block, "part of the approved work")
	for _, name := range gateSkillNames {
		require.Containsf(t, policy, "`"+name+"`", "the policy template must list %s", name)
		requireOneLineWith(t, router, "the router", "`"+name+"`", gatePolicyDoc)
		requireOneLineWith(t, block, "the managed block", "`"+name+"`", "review policy lists it")
	}

	skill := requireSkillTemplate(t, mustLoadStarterSkills(t), "agentic-engineering")
	entryChecks := map[string][]string{
		"planning.md":       {"`effort-research`", "`effort-discuss`", "`plan-check`"},
		"implementation.md": {"`phase-code-review`"},
		"closure.md":        {"`phase-code-review`"},
	}
	for ref, gates := range entryChecks {
		body := string(requireSkillTemplateFile(t, skill, "references/"+ref).Content)
		requireOneLineWith(t, body, ref, append(gates, gatePolicyDoc, "no recorded outcome", "do not repeat")...)
	}
	for _, ref := range []string{"planning.md", "closure.md"} {
		body := string(requireSkillTemplateFile(t, skill, "references/"+ref).Content)
		require.Containsf(t, body, "linked plan document", "%s must say where a workspace effort keeps gate output", ref)
	}
	closure := string(requireSkillTemplateFile(t, skill, "references/closure.md").Content)
	for _, want := range []string{"requires-human-verification", "force-past", "discussion pass"} {
		require.Contains(t, closure, want)
	}

	harness := starterTemplateText(t, "repo", "docs", "reference", "guides", "harness-capabilities.md")
	requireOneLineWith(t, harness, "the harness guide", "absence alone needs no ceremony", "outcome line")
}

// A team may already have its own skill under one of the gate names. Init asks
// about it and can keep it, in either skill folder.
func TestSyncAsksBeforeReplacingATeamSkillNamedLikeAGate(t *testing.T) {
	templates := []string{templateCore, templateAgenticEngineering}
	for _, surface := range []string{".agents/skills", ".claude/skills"} {
		for _, name := range gateSkillNames {
			rel := surface + "/" + name + "/SKILL.md"
			const mine = "---\nname: team\n---\nour own gate\n"

			t.Run(rel+" asked and kept", func(t *testing.T) {
				root := t.TempDir()
				writeTestFile(t, root, rel, mine)
				ui := &scriptedUI{unknown: "keep"}
				syncForTest(t, root, templates, ui)
				require.Equal(t, []string{"unknown:" + rel}, ui.asked)
				require.Equal(t, mine, readTestFile(t, root, rel))
			})
			t.Run(rel+" kept without a terminal", func(t *testing.T) {
				root := t.TempDir()
				writeTestFile(t, root, rel, mine)
				report := syncForTest(t, root, templates, nil)
				require.Equal(t, mine, readTestFile(t, root, rel))
				require.Contains(t, report.Kept, rel)
			})
		}
	}
}
