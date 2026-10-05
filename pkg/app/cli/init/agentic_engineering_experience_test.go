package init

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestAgenticEngineeringEffortRecipesExecuteLifecycleEvidence(t *testing.T) {
	const (
		blankPath    = "docs/efforts/2026-09-07-10-00-blank-approval.md"
		approvedPath = "docs/efforts/2026-09-07-10-01-approved.md"
	)

	root, schema, execSchema, store := setupAgenticEngineeringRecipeVault(t, map[string]string{
		blankPath:    agenticEngineeringEffortNote("EFF-2026-09-07-10-00", "Blank approval", "", "blank"),
		approvedPath: agenticEngineeringEffortNote("EFF-2026-09-07-10-01", "Approved plan", "Drew Colthorp", "approved"),
	})

	recipePath := filepath.Join(root, ".rhizome", "query-recipes", "spec-driven.yaml")
	recipes, issues := queryrecipe.LoadPath(recipePath)
	require.Empty(t, issues)
	byID := make(map[string]queryrecipe.Recipe, len(recipes))
	for _, recipe := range recipes {
		byID[recipe.ID] = recipe
	}

	deps := ontologyquery.Deps{
		VaultDef:   obsidian.VaultDefinition{Root: root, Path: root, Includes: []string{"**/*.md", "**/*.html"}},
		NoteReader: &obsidian.Note{},
		Store:      store,
		Service:    ontology.NewService(obsidian.VaultDefinition{Root: root, Path: root, Includes: []string{"**/*.md", "**/*.html"}}, &obsidian.Note{}, store, schema),
	}

	for _, recipeID := range []string{"effort-execution-context", "closure-drift-pack"} {
		recipe, ok := byID[recipeID]
		require.Truef(t, ok, "recipe %s is missing", recipeID)

		for _, tc := range []struct {
			name          string
			path          string
			approval      any
			requiredWords []string
		}{
			{
				name:          "blank approval remains pending",
				path:          blankPath,
				approval:      nil,
				requiredWords: []string{"blank plan", "blank checklist", "blank execution"},
			},
			{
				name:          "populated approval and evidence are returned",
				path:          approvedPath,
				approval:      "Drew Colthorp",
				requiredWords: []string{"approved plan", "approved checklist", "approved execution"},
			},
		} {
			t.Run(recipeID+"/"+tc.name, func(t *testing.T) {
				compiled, compileIssues := queryrecipe.Compile(recipe, execSchema, map[string]string{"path": tc.path})
				require.Empty(t, compileIssues)
				require.NotNil(t, compiled)

				result := ontologyquery.Execute(context.Background(), deps, schema, compiled.Prepared)
				require.Empty(t, result.Errors)

				effort, ok := result.Data["note"].(map[string]any)
				require.True(t, ok)
				require.Equal(t, tc.path, effort["path"])
				require.Equal(t, tc.approval, effort["planApprovedBy"])

				require.Contains(t, narrativeContent(t, effort, "plan"), tc.requiredWords[0])
				require.Contains(t, narrativeContent(t, effort, "closureChecklist"), tc.requiredWords[1])
				require.Contains(t, narrativeContent(t, effort, "executionNotes"), tc.requiredWords[2])
			})
		}
	}
}

func TestAgenticEngineeringWorkspaceRecipesReturnExplicitSources(t *testing.T) {
	const folder = "docs/efforts/2026-09-13-10-00-folder/"
	const entry = folder + "2026-09-13-10-00-effort.html"
	root, schema, execSchema, store := setupAgenticEngineeringRecipeVault(t, map[string]string{
		entry:                              `<html><head><title>Folder effort</title><script id="rhizome-metadata" type="application/json">{"type":"EffortWorkspace","id":"EFF-2026-09-13-10-00","aliases":["EFF-2026-09-13-10-00"],"governing-specs":[],"name":"Folder effort","summary":"Workspace","status":"planned","created-at":"2026-09-13T10:00:00Z","implementation-plan":"` + folder + `plan.html","work-log":"` + folder + `work-log.md","materials":["` + folder + `materials/report.md"]}</script></head><body>Frozen intent</body></html>`,
		folder + "plan.html":               `<html><head><title>Implementation plan</title></head><body>Plan source</body></html>`,
		folder + "work-log.md":             "# Work log\n\n## Spec Set (Frozen)\nFrozen spec revision abc123\n\n## Stories In Scope (Frozen)\nSelected story US1\n\n## Plan Approval\nDrew approved plan.html at revision def456 on 2026-09-13T14:00:01Z\n\n## Original Intended Delivery\nIntended outcome\n\n## Actual Delivered\nVerified delivered outcome\n\n## Deviations\nNo known deviations\n\n## Closure Checklist\nGate evidence passed\n\n## Status\nNext unfinished task\n\n## Execution Notes\n2026-09-13T14:01:02Z [validation] Execution evidence\n",
		folder + "materials/report.md":     "# Report\nSelected evidence\n",
		folder + "materials/unselected.md": "# Unselected\nNot membership\n",
	})
	recipes, issues := queryrecipe.LoadPath(filepath.Join(root, ".rhizome", "query-recipes", "spec-driven.yaml"))
	require.Empty(t, issues)
	required := requiredWorkspaceRecipes(t, recipes)
	deps := ontologyquery.Deps{VaultDef: obsidian.VaultDefinition{Root: root, Path: root, Includes: []string{"**/*.md", "**/*.html"}}, NoteReader: &obsidian.Note{}, Store: store, Service: ontology.NewService(obsidian.VaultDefinition{Root: root, Path: root, Includes: []string{"**/*.md", "**/*.html"}}, &obsidian.Note{}, store, schema)}
	formats, err := builtin.NewRuntime()
	require.NoError(t, err)
	deps.NoteFormats = formats
	for _, recipe := range required {
		t.Run(recipe.ID, func(t *testing.T) {
			compiled, issues := queryrecipe.Compile(recipe, execSchema, map[string]string{"path": entry})
			require.Empty(t, issues)
			result := ontologyquery.Execute(context.Background(), deps, schema, compiled.Prepared)
			require.Empty(t, result.Errors)
			effort, ok := result.Data["note"].(map[string]any)
			require.True(t, ok)
			require.Equal(t, entry, effort["path"])
			require.Equal(t, "planned", effort["status"])
			for field, path := range map[string]string{"implementationPlan": folder + "plan.html", "workLog": folder + "work-log.md"} {
				component, ok := effort[field].(map[string]any)
				require.True(t, ok, "%s: %#v", field, effort[field])
				require.Equal(t, path, component["path"])
				require.NotEmpty(t, component["title"])
				ref, ok := component["ref"].(map[string]any)
				require.True(t, ok)
				require.Equal(t, path, ref["notePath"])
				require.NotEmpty(t, ref["ref"])
			}
			log := effort["workLog"].(map[string]any)
			for field, evidence := range map[string]string{
				"specSetFrozen": "abc123", "storiesInScopeFrozen": "US1",
				"planApproval": "def456", "originalIntendedDelivery": "Intended outcome",
				"actualDelivered": "Verified delivered outcome", "deviations": "No known deviations",
				"closureChecklist": "Gate evidence passed", "statusSection": "Next unfinished task",
				"executionNotes": "2026-09-13T14:01:02Z",
			} {
				require.Contains(t, narrativeContent(t, log, field), evidence)
			}
			materials, ok := effort["materials"].([]any)
			require.True(t, ok)
			require.Len(t, materials, 1)
			require.Equal(t, folder+"materials/report.md", materials[0].(map[string]any)["path"])
		})
	}
}

func TestAgenticEngineeringWorkspaceMissingLifecycleEvidenceRemainsAbsent(t *testing.T) {
	const folder = "docs/efforts/2026-09-13-10-00-folder/"
	const entry = folder + "2026-09-13-10-00-effort.html"
	root, schema, execSchema, store := setupAgenticEngineeringRecipeVault(t, map[string]string{
		entry:                  `<html><head><title>Pending effort</title><script id="rhizome-metadata" type="application/json">{"type":"EffortWorkspace","id":"EFF-2026-09-13-10-00","aliases":["EFF-2026-09-13-10-00"],"governing-specs":[],"name":"Pending effort","summary":"Workspace","status":"planned","created-at":"2026-09-13T10:00:00Z","implementation-plan":"` + folder + `plan.html","work-log":"` + folder + `work-log.md"}</script></head><body>Unapproved</body></html>`,
		folder + "plan.html":   `<html><head><title>Plan</title></head><body>Proposed plan</body></html>`,
		folder + "work-log.md": "# Work log\nNo lifecycle snapshots yet.\n",
	})
	recipes, issues := queryrecipe.LoadPath(filepath.Join(root, ".rhizome", "query-recipes", "spec-driven.yaml"))
	require.Empty(t, issues)
	required := requiredWorkspaceRecipes(t, recipes)
	vault := obsidian.VaultDefinition{Root: root, Path: root, Includes: []string{"**/*.md", "**/*.html"}}
	formats, err := builtin.NewRuntime()
	require.NoError(t, err)
	deps := ontologyquery.Deps{VaultDef: vault, NoteReader: &obsidian.Note{}, Store: store, Service: ontology.NewService(vault, &obsidian.Note{}, store, schema), NoteFormats: formats}
	for _, recipe := range required {
		t.Run(recipe.ID, func(t *testing.T) {
			compiled, issues := queryrecipe.Compile(recipe, execSchema, map[string]string{"path": entry})
			require.Empty(t, issues)
			result := ontologyquery.Execute(context.Background(), deps, schema, compiled.Prepared)
			require.Empty(t, result.Errors)
			effort := result.Data["note"].(map[string]any)
			require.Nil(t, effort["planApprovedBy"])
			log := effort["workLog"].(map[string]any)
			require.Equal(t, folder+"work-log.md", log["path"])
			for _, field := range []string{"specSetFrozen", "storiesInScopeFrozen", "planApproval", "originalIntendedDelivery", "actualDelivered", "deviations", "closureChecklist", "statusSection", "executionNotes"} {
				require.Nil(t, log[field], field)
			}
		})
	}
}

func requiredWorkspaceRecipes(t *testing.T, recipes []queryrecipe.Recipe) []queryrecipe.Recipe {
	t.Helper()
	byID := make(map[string]queryrecipe.Recipe, len(recipes))
	for _, recipe := range recipes {
		byID[recipe.ID] = recipe
	}
	selected := make([]queryrecipe.Recipe, 0, 2)
	for _, id := range []string{"effort-execution-context", "closure-drift-pack"} {
		recipe, ok := byID[id]
		require.True(t, ok, "required recipe %s missing", id)
		selected = append(selected, recipe)
	}
	return selected
}

func TestAgenticEngineeringExperienceHooksStripAndTargetExtensions(t *testing.T) {
	skills, manifest, err := loadAllSkillTemplatesWithReport([]string{templateAgenticEngineering})
	require.NoError(t, err)

	for _, target := range []string{
		"agentic-engineering:alignment.additional-context",
		"agentic-engineering:reconciliation.additional-targets",
	} {
		entry, ok := findSkillOverlayManifestEntry(manifest, target)
		require.Truef(t, ok, "empty AE-only hook %s should be reported", target)
		require.Equal(t, "stripped_empty_slot", entry.Outcome)
	}

	for _, filePath := range []string{"references/alignment.md", "references/reconciliation.md"} {
		file := requireSkillTemplateFile(t, requireSkillTemplate(t, skills, "agentic-engineering"), filePath)
		require.NotContains(t, string(file.Content), "rzm:skill-slot")
	}

	rawSkills, err := loadStarterSkillTemplates(templateAgenticEngineering)
	require.NoError(t, err)
	rendered, _, err := applySkillOverlays(rawSkills, []loadedSkillOverlay{{
		Template:      "experience-test",
		TemplateOrder: 0,
		Path:          "experience-test.yaml",
		Overlay: skillOverlay{
			APIVersion:  skillOverlayAPIVersion,
			ID:          "experience-hooks",
			TargetSkill: "agentic-engineering",
			Fragments: []skillOverlayFragment{
				{
					File:    "references/alignment.md",
					Slot:    "alignment.additional-context",
					Op:      skillOverlayOpAppend,
					Content: "Alignment extension fragment.",
				},
				{
					File:    "references/reconciliation.md",
					Slot:    "reconciliation.additional-targets",
					Op:      skillOverlayOpAppend,
					Content: "Reconciliation extension fragment.",
				},
			},
		},
	}}, templateAgenticEngineering)
	require.NoError(t, err)

	router := requireSkillTemplate(t, rendered, "agentic-engineering")
	alignment := string(requireSkillTemplateFile(t, router, "references/alignment.md").Content)
	reconciliation := string(requireSkillTemplateFile(t, router, "references/reconciliation.md").Content)
	require.Contains(t, alignment, "Alignment extension fragment.")
	require.NotContains(t, alignment, "Reconciliation extension fragment.")
	require.Contains(t, reconciliation, "Reconciliation extension fragment.")
	require.NotContains(t, reconciliation, "Alignment extension fragment.")
	require.NotContains(t, alignment, "rzm:skill-slot")
	require.NotContains(t, reconciliation, "rzm:skill-slot")
}

func TestAgenticEngineeringInstalledDocsValidateReferenceFieldsAndEffortLinks(t *testing.T) {
	root := t.TempDir()
	installStarterOntologySchemas(t, root, templateAgenticEngineering)
	docFiles, err := loadStarterTemplates(templateAgenticEngineering)
	require.NoError(t, err)
	require.NotEmpty(t, docFiles)

	var referencePaths []string
	for _, file := range docFiles {
		writeTemplateNote(t, root, file.Path, string(file.Content))
		if strings.HasPrefix(file.Path, "docs/reference/") {
			referencePaths = append(referencePaths, file.Path)
		}
	}
	require.NotEmpty(t, referencePaths)

	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	sources, err := testNoteMetadataIndexer(t).BuildNoteSourceSnapshots(
		context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{},
	)
	require.NoError(t, err)
	build, err := ontology.BuildIndexFromNoteSources(
		context.Background(), obsidian.VaultDefinition{Path: root}, sources, schema, "agentic-engineering-installed-docs",
	)
	require.NoError(t, err)

	for _, path := range referencePaths {
		found := false
		for _, node := range build.Nodes {
			if node.NotePath == path && node.TypeName == "ReferenceDoc" {
				found = true
				break
			}
		}
		require.Truef(t, found, "installed reference doc %s should resolve as ReferenceDoc", path)
		require.Empty(t, starterIssuesForPath(build.ValidationIssues, path, ""),
			"installed reference doc %s should satisfy its required fields", path)
	}

	const effortReadme = "docs/efforts/README.md"
	note := &obsidian.Note{}
	notePaths, err := note.GetNotesList(obsidian.VaultDefinition{Path: root})
	require.NoError(t, err)
	cache := obsidian.BuildNotePathCache(notePaths)
	effortBody, err := note.GetContents(obsidian.VaultDefinition{Path: root}, effortReadme)
	require.NoError(t, err)
	links := obsidian.ExtractMdLinks(effortBody, obsidian.DefaultMdLinkOptions)
	require.NotEmpty(t, links, "installed effort README should retain internal links")
	for _, target := range links {
		if isExternalAgenticEngineeringLink(target) {
			continue
		}
		_, ok := cache.ResolveMdLink(target, effortReadme)
		require.Truef(t, ok, "installed effort README link %q should resolve", target)
	}
}

func setupAgenticEngineeringRecipeVault(t *testing.T, notes map[string]string) (string, *ontology.Schema, *ontologyquery.ExecutableSchema, *codeanchorsqlite.Store) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\", \"**/*.html\"]\n"), 0o644))

	ontologyFiles, err := loadStarterOntologyTemplates(templateAgenticEngineering)
	require.NoError(t, err)
	require.Len(t, ontologyFiles, 1)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", ontologyFiles[0].Path), ontologyFiles[0].Content, 0o644))

	recipeFiles, err := loadStarterQueryRecipeTemplates(templateAgenticEngineering)
	require.NoError(t, err)
	require.Len(t, recipeFiles, 1)
	recipeRoot := filepath.Join(root, ".rhizome", "query-recipes")
	require.NoError(t, os.MkdirAll(recipeRoot, 0o755))
	for _, file := range recipeFiles {
		require.NoError(t, os.WriteFile(filepath.Join(recipeRoot, file.Path), file.Content, 0o644))
	}
	for notePath, body := range notes {
		writeQueryRecipeExperienceNote(t, root, notePath, body)
	}

	return indexAgenticEngineeringRecipeVault(t, root)
}

func TestAgenticEngineeringFeatureAreasNestUnderOneParentArea(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Run(RunOptions{Dir: root, Workflow: templateAgenticEngineering, Stdout: io.Discard, Stderr: io.Discard}))
	writeQueryRecipeExperienceNote(t, root, "docs/feature-areas/platform.md", "---\ntype: FeatureArea\nid: FA-0001\nsummary: Platform\n---\n\n# Platform\n\nIncludes [[views]].\n")
	writeQueryRecipeExperienceNote(t, root, "docs/feature-areas/views.md", "---\ntype: FeatureArea\nid: FA-0002\nsummary: Views\nparent: \"[[platform]]\"\n---\n\n# Views\n")

	_, schema, execSchema, store := indexAgenticEngineeringRecipeVault(t, root)
	parent := ontology.ParentField(schema.Types["FeatureArea"].Fields)
	require.NotNil(t, parent)
	require.Equal(t, "parent", parent.Name)
	specActive := schema.EnumTypes["SpecStatus"].ByName["active"].View
	require.Equal(t, ontology.StageDone, specActive.Stage, "a current contract is not in-motion work")
	require.NotEqual(t, "progress", specActive.Tone)

	vaultDef := obsidian.VaultDefinition{Root: root, Path: root, Includes: []string{"**/*.md", "**/*.html"}}
	deps := ontologyquery.Deps{VaultDef: vaultDef, NoteReader: &obsidian.Note{}, Store: store, Service: ontology.NewService(vaultDef, &obsidian.Note{}, store, schema)}
	prepared, errors := ontologyquery.Prepare(execSchema, `{
  child: note(path: "docs/feature-areas/views.md") { ... on FeatureArea { parent { id } } }
  top: note(path: "docs/feature-areas/platform.md") { ... on FeatureArea { parent { id } } }
 }`)
	require.Empty(t, errors)
	result := ontologyquery.Execute(context.Background(), deps, schema, prepared)
	require.Empty(t, result.Errors)
	require.Equal(t, map[string]any{"id": "FA-0001"}, result.Data["child"].(map[string]any)["parent"])
	require.Nil(t, result.Data["top"].(map[string]any)["parent"])

	edges, err := store.OntologyEdgesForPaths(context.Background(), []string{"docs/feature-areas/platform.md"}, true, "parent", 0)
	require.NoError(t, err)
	require.Len(t, edges, 1, "body links and backlinks must not appear as parent edges")
	require.Equal(t, "docs/feature-areas/views.md", edges[0].SrcPath)
	require.Equal(t, "docs/feature-areas/platform.md", edges[0].DstPath)
}

func indexAgenticEngineeringRecipeVault(t *testing.T, root string) (string, *ontology.Schema, *ontologyquery.ExecutableSchema, *codeanchorsqlite.Store) {
	t.Helper()
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	execSchema, err := ontologyquery.BuildExecutableSchema(schema)
	require.NoError(t, err)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	vaultDef := obsidian.VaultDefinition{Root: root, Path: root, Includes: []string{"**/*.md", "**/*.html"}}
	indexer := testNoteMetadataIndexer(t)
	_, err = indexer.EnsureIndexed(ctx, vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	build, err := ontology.BuildIndexWithStore(ctx, indexer, vaultDef, &obsidian.Note{}, store, schema, "agentic-engineering-experience")
	require.NoError(t, err)
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, codeanchorsqlite.OntologySnapshot{
		Assessments:  build.AssessmentRows,
		NoteStates:   build.NoteStates,
		NoteTypes:    build.NoteTypes,
		Edges:        build.Edges,
		TypePolicies: build.TypePolicies,
		SchemaState: codeanchorsqlite.OntologySchemaState{
			SchemaHash: schema.Hash,
			NotesHash:  build.NotesHash,
			LoadedAt:   1,
			Ready:      true,
		},
	}))
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths:        build.NodePaths,
		Nodes:            build.Nodes,
		FieldValues:      build.NodeFieldValues,
		LinkDependencies: build.NodeLinkDeps,
	}))

	return root, schema, execSchema, store
}

func isExternalAgenticEngineeringLink(target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	for _, prefix := range []string{"http://", "https://", "mailto:", "tel:", "ftp://", "file://"} {
		if strings.HasPrefix(target, prefix) {
			return true
		}
	}
	return false
}

func agenticEngineeringEffortNote(id, name, approval, marker string) string {
	approvalLine := ""
	if strings.TrimSpace(approval) != "" {
		approvalLine = "plan-approved-by: " + approval + "\n"
	}
	return "---\n" +
		"type: EffortNote\n" +
		"id: " + id + "\n" +
		"name: " + name + "\n" +
		"created-at: 2026-09-07T10:00:00Z\n" +
		approvalLine +
		"status: planned\n" +
		"summary: Disposable recipe execution fixture.\n" +
		"aliases:\n" +
		"  - " + id + "\n" +
		"---\n\n" +
		"# " + name + "\n\n" +
		"## Scope\n\n" +
		"Scope for the " + marker + " fixture.\n\n" +
		"## Spec Set (Frozen)\n\n" +
		"No governing spec is needed for this disposable fixture.\n\n" +
		"## Stories In Scope (Frozen)\n\n" +
		"No stories are needed for this disposable fixture.\n\n" +
		"## Spec Coverage Checklist\n\n" +
		"- [ ] " + marker + " coverage.\n\n" +
		"## Plan\n\n" +
		"The " + marker + " plan is decision complete.\n\n" +
		"## Original Intended Delivery\n\n" +
		"The " + marker + " intended delivery.\n\n" +
		"## Actual Delivered\n\n" +
		"The " + marker + " actual delivery.\n\n" +
		"## Execution Notes\n\n" +
		"- 2026-09-07T10:00Z [validation] The " + marker + " execution evidence.\n\n" +
		"## Deviations\n\n" +
		"- None for the " + marker + " fixture.\n\n" +
		"## Closure Checklist\n\n" +
		"- [ ] The " + marker + " checklist remains pending.\n\n" +
		"## Compounding Follow-ups\n\n" +
		"- None for the " + marker + " fixture.\n\n" +
		"## Status\n\n" +
		"Planned for recipe execution testing.\n"
}

func writeQueryRecipeExperienceNote(t *testing.T, root, notePath, body string) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(notePath))
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, []byte(body), 0o644))
}

func narrativeContent(t *testing.T, effort map[string]any, field string) string {
	t.Helper()
	section, ok := effort[field].(map[string]any)
	require.Truef(t, ok, "expected narrative section %s, got %#v", field, effort[field])
	content, ok := section["content"].(string)
	require.Truef(t, ok, "expected narrative content %s, got %#v", field, section["content"])
	return content
}

func findSkillOverlayManifestEntry(manifest SkillOverlayManifest, target string) (SkillOverlayManifestEntry, bool) {
	parts := strings.SplitN(target, ":", 2)
	if len(parts) != 2 {
		return SkillOverlayManifestEntry{}, false
	}
	for _, entry := range manifest.Entries {
		if entry.TargetSkill == parts[0] && entry.Slot == parts[1] {
			return entry, true
		}
	}
	return SkillOverlayManifestEntry{}, false
}
