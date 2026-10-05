package init

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestExpandAgentTemplateEmbeds_ExpandsOutsideCode(t *testing.T) {
	in := "before\n![[Ontology overview]]\nafter\n"
	out, err := expandAgentTemplateEmbeds(in)
	require.NoError(t, err)
	require.NotContains(t, out, "![[Ontology overview]]")
	require.Contains(t, out, "## Ontology overview")
}

func TestExpandAgentTemplateEmbeds_IgnoresInlineCodeSpans(t *testing.T) {
	in := "`![[Ontology overview]]`"
	out, err := expandAgentTemplateEmbeds(in)
	require.NoError(t, err)
	require.Equal(t, in, out)
}

func TestExpandAgentTemplateEmbeds_IgnoresFencedCodeBlocks(t *testing.T) {
	in := strings.Join([]string{
		"```text",
		"![[Ontology overview]]",
		"```",
		"",
	}, "\n")
	out, err := expandAgentTemplateEmbeds(in)
	require.NoError(t, err)
	require.Equal(t, in, out)
}

func TestExpandAgentTemplateEmbeds_ExpandsAfterFences(t *testing.T) {
	in := strings.Join([]string{
		"before",
		"```md",
		"![[Ontology overview]]",
		"```",
		"after",
		"![[Ontology overview]]",
		"",
	}, "\n")
	out, err := expandAgentTemplateEmbeds(in)
	require.NoError(t, err)

	require.Contains(t, out, "## Ontology overview")
	require.Equal(t, 1, strings.Count(out, "![[Ontology overview]]"))
}

func TestAgenticEngineeringSkillTopology(t *testing.T) {
	skills, err := loadStarterSkillTemplates(templateAgenticEngineering)
	require.NoError(t, err)

	actual := make([]string, 0, len(skills))
	for _, skill := range skills {
		actual = append(actual, skill.Name)
	}
	sort.Strings(actual)
	require.Equal(t, []string{"agentic-engineering", "foundation-review", "ingest-transcript"}, actual)

	router := requireSkillTemplate(t, skills, "agentic-engineering")
	body := string(requireSkillTemplateFile(t, router, "SKILL.md").Content)
	referenceLines := 0
	for _, ref := range []string{
		"workflow-state.md", "specification.md", "effort-setup.md", "planning.md",
		"implementation.md", "quality-gates.md", "alignment.md", "reconciliation.md",
		"compounding.md", "closure.md", "closure-report-contract.md",
	} {
		require.Contains(t, body, "references/"+ref)
		content := requireSkillTemplateFile(t, router, "references/"+ref).Content
		require.NotEmpty(t, content)
		referenceLines += len(strings.Split(string(content), "\n"))
	}
	planning := requireSkillTemplateFile(t, router, "references/planning.md")
	require.Contains(t, string(planning.Content), "references/batch-plan-template.md")
	batchTemplate := requireSkillTemplateFile(t, router, "references/batch-plan-template.md")
	require.NotEmpty(t, batchTemplate.Content)
	require.LessOrEqual(t, len(strings.Split(body, "\n")), 55, "router should remain a compact dispatcher")
	require.LessOrEqual(t, referenceLines, 200, "router resources should stay lazy while allowing phase-keyed dual-form effort guidance")
	require.Contains(t, body, "An approved plan is authorization")
	require.Contains(t, body, "say which reference caused it")
	for _, universal := range []string{"rzm agent start", "semantic-query", "file-context", "note move"} {
		require.NotContains(t, body, universal)
	}
}

func TestAgenticEngineeringEffortIdentifiersUseDatetimeContract(t *testing.T) {
	files, err := loadStarterOntologyTemplates(templateAgenticEngineering)
	require.NoError(t, err)
	require.Len(t, files, 1)
	schema := string(files[0].Content)
	require.Contains(t, schema, `@identifier(strategy: DATETIME, preferred: true, prefix: "EFF")`)
	require.NotContains(t, schema, `@identifier(strategy: SEQUENTIAL, preferred: true, prefix: "EFF")`)

	guide, err := os.ReadFile(filepath.Join("templates", "starters", templateAgenticEngineering, "repo", "docs", "reference", "guides", "spec-driven-query-bundle.md"))
	require.NoError(t, err)
	require.Contains(t, string(guide), "# DATETIME: choose the prospective local-stamped path first")
	require.Contains(t, string(guide), "next-id --type EffortNote --path")
}

func TestAgenticEngineeringPhaseResourcesWireWorkflowLeverage(t *testing.T) {
	skills, err := loadStarterSkillTemplates(templateAgenticEngineering)
	require.NoError(t, err)
	router := requireSkillTemplate(t, skills, "agentic-engineering")

	var bundle strings.Builder
	for _, file := range router.Files {
		bundle.Write(file.Content)
		bundle.WriteByte('\n')
	}
	body := bundle.String()
	for _, id := range []string{
		"effort-execution-context",
		"frozen-spec-index-pack",
		"frozen-spec-detail-pack",
		"story-acceptance-pack",
		"closure-drift-pack",
		"runtime-code-evidence-pack",
		"runtime-note-code-evidence-pack",
		"topic-typed-survey",
	} {
		require.Containsf(t, body, "--id "+id, "router resources orphan workflow recipe %s", id)
	}
	require.Contains(t, body, "validate frozen-scope-drift")
}

func TestAgenticEngineeringPhasesConsultConcernDocsContextually(t *testing.T) {
	router := requireSkillTemplate(t, mustLoadStarterSkills(t), "agentic-engineering")
	reads := map[string][]string{
		"workflow-state.md": {"docs/engineering/README.md", "docs/engineering/review-and-approval.md"},
		"specification.md":  {"docs/engineering/documentation.md", "docs/engineering/review-and-approval.md"},
		"planning.md":       {"docs/engineering/architecture.md", "docs/engineering/testing-policy.md", "docs/engineering/documentation.md", "docs/engineering/release.md"},
		"implementation.md": {"docs/engineering/testing-policy.md", "docs/engineering/quality-gates.md", "docs/engineering/architecture.md", "docs/engineering/documentation.md"},
		"quality-gates.md":  {"docs/engineering/quality-gates.md", "docs/engineering/release.md"},
		"alignment.md":      {"docs/engineering/testing-policy.md", "docs/engineering/documentation.md"},
		"reconciliation.md": {"docs/engineering/documentation.md"},
		"closure.md":        {"docs/engineering/quality-gates.md", "docs/engineering/release.md", "docs/engineering/review-and-approval.md"},
	}
	for ref, docs := range reads {
		body := string(requireSkillTemplateFile(t, router, "references/"+ref).Content)
		for _, doc := range docs {
			require.Containsf(t, body, doc, "%s should consult %s", ref, doc)
		}
		require.NotContainsf(t, body, "docs/engineering/workflow.md", "%s references a retired phase doc", ref)
		require.NotContainsf(t, body, "docs/engineering/efforts.md", "%s references a retired phase doc", ref)
		require.NotContainsf(t, body, "## Stop", "%s should describe decision boundaries, not stops", ref)
	}
}

func TestAgenticEngineeringWorkflowSkillsAreThinAndClosed(t *testing.T) {
	for _, name := range []string{"foundation-review", "ingest-transcript"} {
		body := skillBodyFromTemplates(t, templateAgenticEngineering, name)
		require.LessOrEqualf(t, len(strings.Split(body, "\n")), 58, "workflow skill %s should remain small", name)
		for _, section := range []string{"## Deliverable", "## Authority", "## Decision boundaries"} {
			require.Containsf(t, body, section, "workflow skill %s lacks %s", name, section)
		}
		require.NotContainsf(t, body, "## Stop", "workflow skill %s should describe decision boundaries", name)
		require.NotContainsf(t, body, "../agentic-engineering", "workflow skill %s may not address a sibling bundle", name)
	}

	router := requireSkillTemplate(t, mustLoadStarterSkills(t), "agentic-engineering")
	requireSkillTemplateFile(t, router, "references/spec-template.md")
	requireSkillTemplateFile(t, router, "references/traceability.md")
	ingest := skillBodyFromTemplates(t, templateAgenticEngineering, "ingest-transcript")
	for _, want := range []string{
		"docs/reference/guides/ontology-driven-transcript-ingestion.md",
		"preflight duplicate sources",
		"survey the live ontology and existing notes",
		"prepare the proposed durable writes",
		"preserve provenance",
		"update and index the affected context",
	} {
		require.Contains(t, ingest, want)
	}
}

func TestAgenticEngineeringClosureIsLazyAndSelfContained(t *testing.T) {
	routerBody := skillBodyFromTemplates(t, templateAgenticEngineering, "agentic-engineering")
	require.Contains(t, routerBody, "references/closure.md")
	require.Contains(t, routerBody, "references/closure-report-contract.md")

	router := requireSkillTemplate(t, mustLoadStarterSkills(t), "agentic-engineering")
	closure := string(requireSkillTemplateFile(t, router, "references/closure.md").Content)
	contract := string(requireSkillTemplateFile(t, router, "references/closure-report-contract.md").Content)
	require.Contains(t, closure, "complete closure directly")
	require.Contains(t, closure, "Group decisions and outcomes")
	require.Contains(t, closure, "status: complete")
	require.Contains(t, contract, "# <Specialist> Closure Report")
	require.Contains(t, contract, "Decisions required")
}

func TestAgenticEngineeringCoreOwnsUniversalLeverage(t *testing.T) {
	core, err := loadSkillTemplates()
	require.NoError(t, err)
	rhizome := requireSkillTemplate(t, core, "rhizome")
	body := string(requireSkillTemplateFile(t, rhizome, "SKILL.md").Content)
	require.Contains(t, body, "references/reports-and-health.md")

	bindings := string(requireSkillTemplateFile(t, rhizome, "references/documentation-bindings.md").Content)
	for _, want := range []string{"doc-stack", "CONTEXT.md", "retrieval critique"} {
		require.Contains(t, bindings, want)
	}
	reports := string(requireSkillTemplateFile(t, rhizome, "references/reports-and-health.md").Content)
	for _, want := range []string{"similarity", "complexity", "hotspot", "dependency direction"} {
		require.Contains(t, reports, want)
	}
}

func TestActionItemsStarterOwnsAssumptionTracker(t *testing.T) {
	globalSkills, err := loadSkillTemplates()
	require.NoError(t, err)
	for _, skill := range globalSkills {
		require.NotEqual(t, "assumption-tracker", skill.Name)
	}
	assessor := string(requireSkillTemplateFile(t, requireSkillTemplate(t, globalSkills, "legacy-codebase-assessor"), "SKILL.md").Content)
	require.Contains(t, assessor, "check whether the active harness exposes an installed `assumption-tracker` skill")
	require.Contains(t, assessor, "optional assumption phase requires the `action-items` starter")
	clientHarness := string(requireSkillTemplateFile(t, requireSkillTemplate(t, globalSkills, "client-harness-builder"), "SKILL.md").Content)
	require.Contains(t, clientHarness, "When the active harness exposes `assumption-tracker`")
	require.Contains(t, clientHarness, "Without action-items, assessment/documentation hands off directly here")

	actionItemSkills, err := loadStarterSkillTemplates(templateActionItems)
	require.NoError(t, err)
	requireSkillTemplate(t, actionItemSkills, "assumption-tracker")
}

func mustLoadStarterSkills(t *testing.T) []skillTemplate {
	t.Helper()
	skills, err := loadStarterSkillTemplates(templateAgenticEngineering)
	require.NoError(t, err)
	return skills
}

func TestMarkdownSkillTemplatesWarnOnEscapedWikilinkExamples(t *testing.T) {
	paths := []string{
		filepath.Join("templates", "skills", "markdown", "rhizome", "SKILL.md"),
		filepath.Join("templates", "skills", "markdown", "rhizome", "references", "structured-markdown.md"),
		filepath.Join("templates", "skills", "markdown", "rhizome", "references", "skill-authoring.md"),
	}
	for _, p := range paths {
		body, err := os.ReadFile(p)
		require.NoError(t, err)
		text := string(body)
		if strings.Contains(text, "\\[\\[") {
			require.Containsf(t, text, "without the backslashes", "%s uses escaped wikilink examples without a copy-safety warning", p)
		}
	}
}

func TestMarkdownSkillTemplatesUseSetupContracts(t *testing.T) {
	ontologySkillPath := filepath.Join("templates", "skills", "markdown", "rhizome", "SKILL.md")
	ontologySkill, err := os.ReadFile(ontologySkillPath)
	require.NoError(t, err)
	require.LessOrEqual(t, len(strings.Split(string(ontologySkill), "\n")), 80, "rhizome SKILL.md should route to references instead of inlining command manuals")
	require.Contains(t, string(ontologySkill), "## Classify, then route")
	require.Contains(t, string(ontologySkill), "references/ontology-authoring.md")

	schemaAuthoringPath := filepath.Join("templates", "skills", "markdown", "rhizome", "references", "ontology-authoring.md")
	schemaAuthoring, err := os.ReadFile(schemaAuthoringPath)
	require.NoError(t, err)
	require.NotContains(t, string(schemaAuthoring), "@answerCard")
	require.Contains(t, string(schemaAuthoring), "high-risk route")
	require.Contains(t, string(schemaAuthoring), "content migration")
}

func TestCoreSkillCreatorStaysTokenDense(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("templates", "skills", "markdown", "rhizome", "references", "skill-authoring.md"))
	require.NoError(t, err)
	require.LessOrEqual(t, len(strings.Split(string(body), "\n")), 40)
	require.Contains(t, string(body), "Rhizome integration is opt-in")
	require.Contains(t, string(body), "general skill-authoring guidance")
}

func TestSkillTemplateReferencesAndRecipesAreReachable(t *testing.T) {
	recipes := map[string]bool{}
	for _, template := range []string{templateCore, templateActionItems, templateAgenticEngineering, templateComplexDomain} {
		for id := range starterQueryRecipeIDs(t, template) {
			recipes[id] = true
		}
	}

	coreSkills, err := loadSkillTemplates()
	require.NoError(t, err)
	evidence := requireSkillTemplateFile(t, requireSkillTemplate(t, coreSkills, coreRhizomeSkillName), "references/evidence-and-composition.md")
	require.True(t, evidence.IsMarkdown)
	require.NotEmpty(t, strings.TrimSpace(string(evidence.Content)))
	requireReachableSkillBundle(t, coreSkills, recipes)

	for _, template := range []string{templateActionItems, templateAgenticEngineering, templateComplexDomain} {
		skills, err := loadStarterSkillTemplates(template)
		require.NoError(t, err)
		requireReachableSkillBundle(t, skills, recipes)
	}
}

func requireReachableSkillBundle(t *testing.T, skills []skillTemplate, recipes map[string]bool) {
	t.Helper()
	for _, skill := range skills {
		require.LessOrEqual(t, len(skill.Name), 64)
		body := normalizeTemplateNewlines(string(requireSkillTemplateFile(t, skill, "SKILL.md").Content))
		require.Contains(t, body, "\nname: "+skill.Name+"\n")
		description := frontmatterValue(body, "description")
		require.NotEmpty(t, description, "skill %s should have a description", skill.Name)
		require.LessOrEqualf(t, len(description), 1024, "skill %s description exceeds discovery limit", skill.Name)
		for _, file := range skill.Files {
			fileBody := normalizeTemplateNewlines(string(file.Content))
			for _, reference := range referencedSkillTemplatePaths(fileBody) {
				requireSkillTemplateFile(t, skill, reference)
			}
			for _, id := range recipeIDsReferencedIn(fileBody) {
				require.Truef(t, recipes[id], "skill %s file %s references unknown query recipe %q", skill.Name, file.Path, id)
			}
		}
	}
}

func normalizeTemplateNewlines(body string) string {
	return strings.ReplaceAll(body, "\r\n", "\n")
}

func referencedSkillTemplatePaths(body string) []string {
	pattern := regexp.MustCompile(`references/[A-Za-z0-9_./-]+`)
	seen := map[string]struct{}{}
	var paths []string
	for _, match := range pattern.FindAllString(body, -1) {
		if strings.HasSuffix(match, "/") {
			continue
		}
		if _, ok := seen[match]; ok {
			continue
		}
		seen[match] = struct{}{}
		paths = append(paths, match)
	}
	sort.Strings(paths)
	return paths
}

func skillBodyFromTemplates(t *testing.T, template, name string) string {
	t.Helper()
	skills, err := loadStarterSkillTemplates(template)
	require.NoError(t, err)
	return string(requireSkillTemplateFile(t, requireSkillTemplate(t, skills, name), "SKILL.md").Content)
}

func starterQueryRecipeIDs(t *testing.T, template string) map[string]bool {
	t.Helper()
	files, err := loadStarterQueryRecipeTemplates(template)
	require.NoError(t, err)
	out := map[string]bool{}
	for _, file := range files {
		for _, line := range strings.Split(string(file.Content), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "id: ") {
				out[strings.TrimSpace(strings.TrimPrefix(trimmed, "id: "))] = true
			}
		}
	}
	return out
}

func recipeIDsReferencedIn(body string) []string {
	var ids []string
	for _, line := range strings.Split(body, "\n") {
		for {
			idx := strings.Index(line, "--id ")
			if idx == -1 {
				break
			}
			rest := strings.TrimSpace(line[idx+len("--id "):])
			if rest == "" {
				break
			}
			fields := strings.Fields(rest)
			if len(fields) == 0 {
				break
			}
			ids = append(ids, strings.Trim(fields[0], "`'\".,;:()[]"))
			line = rest[len(fields[0]):]
		}
	}
	return ids
}

func frontmatterValue(body, key string) string {
	fm, err := obsidian.ExtractFrontmatter(body)
	if err != nil || fm == nil {
		return ""
	}
	if value, ok := fm[key].(string); ok {
		return value
	}
	return ""
}

func TestCoreStarterOwnsSharedRuntimeQueryRecipes(t *testing.T) {
	repoRoot := filepath.Clean("../../../..")
	coreRecipes, err := os.ReadFile(filepath.Join(repoRoot, "pkg", "app", "cli", "init", "templates", "starters", "core", "rhizome", "query-recipes", "core.yaml"))
	require.NoError(t, err)
	for _, id := range []string{"runtime-authoring-context", "runtime-schema-capability-pack"} {
		require.Contains(t, string(coreRecipes), "id: "+id)
	}
	require.NotContains(t, string(coreRecipes), "id: assessment-assumptions")

	actionItemRecipes, err := os.ReadFile(filepath.Join(repoRoot, "pkg", "app", "cli", "init", "templates", "starters", "action-items", "rhizome", "query-recipes", "action-items.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(actionItemRecipes), "id: assessment-assumptions")

	for _, pathParts := range [][]string{
		{"pkg", "app", "cli", "init", "templates", "starters", "agentic-engineering", "rhizome", "query-recipes", "spec-driven.yaml"},
	} {
		recipePath := filepath.Join(append([]string{repoRoot}, pathParts...)...)
		body, err := os.ReadFile(recipePath)
		require.NoError(t, err)
		for _, id := range []string{"runtime-authoring-context", "runtime-schema-capability-pack"} {
			require.NotContains(t, string(body), "id: "+id, recipePath)
		}
	}
}

// TestStarterDirectoriesGroupAssetsByDestination pins the starter source
// layout: loaders read only these entries, so anything else would be silently
// dropped from installs.
func TestStarterDirectoriesGroupAssetsByDestination(t *testing.T) {
	allowed := map[string][]string{
		".":       {"agents", "repo", "rhizome", "template.yaml"},
		"agents":  {"AGENTS.md", "skill-overlays", "skills"},
		"rhizome": {"ontology", "query-recipes", "views"},
	}
	for _, template := range []string{templateCore, templateActionItems, templateAgenticEngineering, templateComplexDomain} {
		t.Run(template, func(t *testing.T) {
			for dir, names := range allowed {
				entries, err := agentHelperTemplates.ReadDir(path.Join("templates", "starters", template, dir))
				require.NoError(t, err)
				for _, entry := range entries {
					require.Contains(t, names, entry.Name(), "unexpected starter entry %s/%s", dir, entry.Name())
				}
			}
			files, err := loadStarterTemplates(template)
			require.NoError(t, err)
			require.NotEmpty(t, files, "starter %q must retain its repo docs", template)
		})
	}
}

// TestStarterOntologySchemasAreTemplateNamed keeps files collision-safe while
// allowing agentic-engineering's specification-domain schema to retain its
// accurate historical `spec-driven.graphql` filename.
func TestStarterOntologySchemasAreTemplateNamed(t *testing.T) {
	for _, template := range []string{templateAgenticEngineering} {
		template := template
		t.Run(template, func(t *testing.T) {
			files, err := loadStarterOntologyTemplates(template)
			require.NoError(t, err)
			require.NotEmpty(t, files, "%s template should ship at least one .graphql", template)

			for _, file := range files {
				require.False(t, strings.EqualFold(file.Path, "schema.graphql"),
					"starter %q must rename schema.graphql to a template-specific name to avoid cross-template collisions", template)
				if template == templateAgenticEngineering {
					require.Equal(t, "spec-driven.graphql", file.Path,
						"the ontology remains specification-domain accurate after the starter rename")
					continue
				}
				require.True(t, strings.HasPrefix(file.Path, template),
					"starter %q ontology file %q should be prefixed with the template name", template, file.Path)
			}
		})
	}
}

// TestStarterTemplateScaffoldsHaveNoCrossTemplateConflicts walks every
// destination path each starter would write, grouped by the relative path
// inside the project, and fails when two templates target the same path with
// different bytes. Identical-byte overlap is allowed (and de-duplicated) so
// shared boilerplate stays fine. This is the safety net the user asked for:
// catches collisions like `.rhizome/ontology/schema.graphql` before they let
// one template silently overwrite another.
func TestStarterTemplateScaffoldsHaveNoCrossTemplateConflicts(t *testing.T) {
	type plannedFile struct {
		template string
		content  []byte
	}
	plans := make(map[string][]plannedFile)

	registry, err := loadStarterTemplateMetadataRegistry()
	require.NoError(t, err)
	var templates []string
	for template := range registry {
		templates = append(templates, template)
	}
	sort.Strings(templates)
	for _, template := range templates {
		ontologyFiles, err := loadStarterOntologyTemplates(template)
		require.NoError(t, err)
		for _, file := range ontologyFiles {
			target := path.Join(".rhizome", "ontology", file.Path)
			plans[target] = append(plans[target], plannedFile{template: template, content: file.Content})
		}

		recipeFiles, err := loadStarterQueryRecipeTemplates(template)
		require.NoError(t, err)
		for _, file := range recipeFiles {
			target := path.Join(".rhizome", "query-recipes", file.Path)
			plans[target] = append(plans[target], plannedFile{template: template, content: file.Content})
		}

		viewFiles, err := loadStarterViewTemplates(template)
		require.NoError(t, err)
		for _, file := range viewFiles {
			target := path.Join(".rhizome", "views", file.Path)
			plans[target] = append(plans[target], plannedFile{template: template, content: file.Content})
		}

		docFiles, err := loadStarterTemplates(template)
		require.NoError(t, err)
		for _, file := range docFiles {
			target := file.Path
			plans[target] = append(plans[target], plannedFile{template: template, content: file.Content})
		}
	}

	var conflicts []string
	for target, planned := range plans {
		if len(planned) < 2 {
			continue
		}
		baseline := planned[0].content
		for _, other := range planned[1:] {
			if !bytes.Equal(baseline, other.content) {
				conflicts = append(conflicts, fmt.Sprintf(
					"%s: templates %s and %s write different content",
					target, planned[0].template, other.template,
				))
				break
			}
		}
	}
	sort.Strings(conflicts)
	require.Empty(t, conflicts,
		"templates may not target the same path with differing content; either rename per template or unify the file:\n  %s",
		strings.Join(conflicts, "\n  "),
	)
}

func TestCoreRhizomeSkillRoutesToDirectFocusedReferences(t *testing.T) {
	skills, err := loadSkillTemplates()
	require.NoError(t, err)

	skillBody := func(name string) string {
		t.Helper()
		for _, skill := range skills {
			if skill.Name != name {
				continue
			}
			for _, file := range skill.Files {
				if file.Path == "SKILL.md" {
					return string(file.Content)
				}
			}
			t.Fatalf("skill %q missing SKILL.md", name)
		}
		t.Fatalf("core skill %q not found", name)
		return ""
	}

	body := skillBody("rhizome")
	require.Contains(t, body, "Classify, then route")
	require.Contains(t, body, "rzm agent start --intent")
	require.Contains(t, body, "only when the selected route calls `rzm agent`")
	require.Contains(t, body, "Markdown note/attachment moves")
	require.Contains(t, body, "sole top-level exception, `note-move`")
	require.Contains(t, body, "A more-specific workflow skill owns")
	require.NotContains(t, body, "agent start --profile")
	require.NotContains(t, body, "agent start --ontology")

	var rhizomeSkill skillTemplate
	foundRhizome := false
	for _, skill := range skills {
		if strings.HasPrefix(skill.Name, "rhizome") {
			require.Equal(t, "rhizome", skill.Name, "retired core Rhizome skills must not remain installed")
		}
		if skill.Name == "rhizome" {
			rhizomeSkill = skill
			foundRhizome = true
		}
	}
	require.True(t, foundRhizome)
	expectedRefs := []string{
		"code-mode.md",
		"configuration.md",
		"diagnostics.md",
		"documentation-bindings.md",
		"evidence-and-composition.md",
		"file-context.md",
		"index-scope.md",
		"indexing-and-freshness.md",
		"installation-and-integration.md",
		"markdown-mutations.md",
		"onboarding.md",
		"ontology-authoring.md",
		"ontology-usage.md",
		"reports-and-health.md",
		"search-and-code-evidence.md",
		"sessions.md",
		"skill-authoring.md",
		"skill-scripts.md",
		"structured-markdown.md",
		"validation-and-repair.md",
		"views.md",
	}
	var actualRefs []string
	for _, file := range rhizomeSkill.Files {
		content := string(file.Content)
		require.NotContains(t, content, "![[", file.Path)
		if strings.HasPrefix(file.Path, "references/") {
			actualRefs = append(actualRefs, strings.TrimPrefix(file.Path, "references/"))
		}
		if file.Path == "references/installation-and-integration.md" {
			require.Contains(t, content, "`next` is intentionally empty")
			require.Contains(t, content, "executing `agentSurfaceRepair.initCandidate` as an argv array")
			require.Contains(t, content, "`present` is not a runtime integration claim")
			require.NotContains(t, content, "<project-root>/bin/rzm init")
		}
		if file.Path == "references/search-and-code-evidence.md" {
			require.Contains(t, content, "`--include-content false`")
			require.Contains(t, content, "`--include-content true --input \"<literal-path>\"`")
		}
	}
	sort.Strings(actualRefs)
	require.Equal(t, expectedRefs, actualRefs)
}

func TestRhizomeManagedBlockIsLeanIntegrationRouter(t *testing.T) {
	repoRoot := filepath.Clean("../../../..")
	body, err := os.ReadFile(filepath.Join(repoRoot, "docs", "rhizome-md-templates", "RHIZOME.md"))
	require.NoError(t, err)
	text := string(body)

	require.Contains(t, text, "If an installed `rhizome` skill is available to the active harness")
	require.Contains(t, text, "A more-specific workflow skill owns its deliverable")
	require.Contains(t, text, "Rhizome enablement is opt-in")
	require.Contains(t, text, "Markdown note/attachment moves")
	require.Contains(t, text, "If only the managed `rhizome` skill is missing")
	require.Contains(t, text, "init --agents <agents>")
	require.Contains(t, text, "Never silently substitute generic shell behavior")
	require.NotContains(t, text, "note/file moves")
	require.NotContains(t, text, "rzm agent start --profile")
	require.NotContains(t, text, "rhizome-onboard")
	require.NotContains(t, text, "rhizome-note-authoring")
	require.NotContains(t, text, "rhizome-ontology")
	require.NotContains(t, text, "rhizome-skill-creator")
}

func TestRepositoryLiveAgentSurfacesDoNotRouteToRetiredCoreSkills(t *testing.T) {
	repoRoot := filepath.Clean("../../../..")
	liveRoots := []string{
		filepath.Join(repoRoot, ".agents", "skills"),
		filepath.Join(repoRoot, ".claude", "skills"),
	}

	for _, root := range liveRoots {
		err := filepath.WalkDir(root, func(filePath string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			body, err := os.ReadFile(filePath)
			if err != nil {
				return err
			}
			for _, retired := range retiredCoreSkillNames {
				require.NotContains(t, string(body), retired, "retired core route remains live in %s", filePath)
			}
			return nil
		})
		require.NoError(t, err)
	}
}
