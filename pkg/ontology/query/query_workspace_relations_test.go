package query

import (
	"context"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestExecute_NodeWorkspaceProjectionIncludesDocumentAndCodeRelations(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Project @node(paths: ["notes/source.md"]) {
  name: String!
}
`, map[string]string{
		"notes/source.md": `---
name: Source
---
See [[connected]].
`,
		"notes/connected.md": "# Connected\n",
		"notes/backlink.md":  "See [[source]].\n",
	})
	ctx := context.Background()
	require.NoError(t, env.store.ReplaceDocLinksForPath(ctx, "pkg/roadmap.go", []codeanchor.DocLink{{
		SrcType: "code",
		SrcPath: "pkg/roadmap.go",
		DstKind: "note",
		DstPath: "notes/source.md",
	}}))

	prepared, errs := PrepareWithVariables(env.execSchema, `query Workspace($ref: String!) {
  node(ref: $ref) {
    workspace {
      relationGroups { key items { ref { ref kind } provenance direction } }
    }
  }
}`, map[string]any{"ref": "notes/source.md"})
	require.Empty(t, errs)

	result := Execute(ctx, env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	workspace := result.Data["node"].(map[string]any)["workspace"].(map[string]any)
	groups := workspace["relationGroups"].([]any)
	byKey := make(map[string][]any, len(groups))
	for _, raw := range groups {
		group := raw.(map[string]any)
		byKey[group["key"].(string)] = group["items"].([]any)
	}
	require.Equal(t, "notes/connected.md", byKey["connected"][0].(map[string]any)["ref"].(map[string]any)["ref"], "ordinary outbound wikilinks remain visible")
	require.Equal(t, "notes/backlink.md", byKey["backlinks"][0].(map[string]any)["ref"].(map[string]any)["ref"], "ordinary inbound wikilinks remain visible")
	require.Equal(t, "OUTBOUND", byKey["connected"][0].(map[string]any)["direction"])
	require.Equal(t, "INBOUND", byKey["backlinks"][0].(map[string]any)["direction"])
	require.NotEmpty(t, byKey["code"], "note-code links remain visible")
	codeRef := byKey["code"][0].(map[string]any)["ref"].(map[string]any)
	require.Equal(t, "CODE_FILE", codeRef["kind"])
	require.Equal(t, "pkg/roadmap.go", codeRef["ref"])
}

func TestExecute_NodeWorkspaceRelationDirectionFollowsTheAuthoredLink(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Topic @node(paths: ["notes/*.md"]) {
  name: String
}
`, map[string]string{
		"notes/source.md": "# Source\n\nSee [[target]].\n",
		"notes/target.md": "# Target\n",
	})

	// The index mirrors each body link with a synthetic "backlink" edge; both
	// halves must report the authored direction so a one-way link never reads
	// as mutual.
	directions := func(ref, other string) map[string]bool {
		prepared, errs := PrepareWithVariables(env.execSchema, `query Workspace($ref: String!) {
  node(ref: $ref) { workspace { relationGroups { key items { ref { ref } direction } } } }
}`, map[string]any{"ref": ref})
		require.Empty(t, errs)
		result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
		require.Empty(t, result.Errors)
		out := map[string]bool{}
		groups := result.Data["node"].(map[string]any)["workspace"].(map[string]any)["relationGroups"].([]any)
		for _, raw := range groups {
			for _, rawItem := range raw.(map[string]any)["items"].([]any) {
				item := rawItem.(map[string]any)
				if item["ref"].(map[string]any)["ref"] == other {
					out[item["direction"].(string)] = true
				}
			}
		}
		return out
	}

	require.Equal(t, map[string]bool{"OUTBOUND": true}, directions("notes/source.md", "notes/target.md"))
	require.Equal(t, map[string]bool{"INBOUND": true}, directions("notes/target.md", "notes/source.md"))
}

func TestExecute_NodeWorkspaceRelationGroupsTitleFragmentTargetsFromTheNode(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Person @node(paths: ["notes/people/*.md"]) {
  displayName: String @field
}

type Plan @node(paths: ["notes/plan.md"]) {
  assignee: Person! @link
}

type Task implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#task", paths: ["notes/**/*.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
  assignee: Person! @link
}
`, map[string]string{
		"notes/people/ada.md": "---\ndisplayName: Ada Lovelace\n---\n",
		"notes/plan.md":       "---\nassignee: '[[ada]]'\n---\n# Household plan\n\n- [ ] Order extra firewood #task assignee:: [[ada]]\n",
	})

	prepared, errs := PrepareWithVariables(env.execSchema, `query Workspace($ref: String!) {
  node(ref: $ref) {
    workspace {
      relationGroups { key items { ref { ref kind notePath fragment } title relationName } }
    }
  }
}`, map[string]any{"ref": "notes/people/ada.md"})
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	workspace := result.Data["node"].(map[string]any)["workspace"].(map[string]any)
	groups := workspace["relationGroups"].([]any)
	var structural []any
	for _, raw := range groups {
		group := raw.(map[string]any)
		if group["key"] == "structural" {
			structural = group["items"].([]any)
		}
	}
	require.Len(t, structural, 2, "both document and embedded assignee relations are structural")
	var item map[string]any
	for _, raw := range structural {
		candidate := raw.(map[string]any)
		if candidate["ref"].(map[string]any)["kind"] == "NOTE" {
			require.Equal(t, "Household plan", candidate["title"], "an embedded title must not replace its document title")
		} else {
			item = candidate
		}
	}
	require.NotNil(t, item)
	ref := item["ref"].(map[string]any)
	require.Equal(t, "EMBEDDED", ref["kind"], "the target keeps its embedded node kind")
	require.Equal(t, "notes/plan.md", ref["notePath"], "the target note path stays separable from the fragment")
	require.NotEmpty(t, ref["fragment"])
	require.Equal(t, "assignee", item["relationName"])
	require.Equal(
		t,
		"Order extra firewood",
		item["title"],
		"a fragment target is titled from its own node, not from the raw fragment",
	)
	require.NotEqual(t, ref["fragment"], item["title"])
}

func TestExecute_NodeWorkspaceRelationGroupsTitleNoteTargetsFromTheNode(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Project @node(paths: ["notes/project.md"]) {
  plan: Material! @link
}

type Material @node(paths: ["notes/materials/*.md"]) {
  summary: String
}
`, map[string]string{
		"notes/project.md": `---
plan: "[[materials/opaque-slug]]"
---
# Project
`,
		"notes/materials/opaque-slug.md": `---
summary: Delivery plan
---
# Human-readable plan title
`,
	})

	prepared, errs := PrepareWithVariables(env.execSchema, `query Workspace($ref: String!) {
  node(ref: $ref) {
    workspace {
      relationGroups { key items { ref { ref kind notePath } title relationName } }
    }
  }
}`, map[string]any{"ref": "notes/project.md"})
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	workspace := result.Data["node"].(map[string]any)["workspace"].(map[string]any)
	groups := workspace["relationGroups"].([]any)
	var structural []any
	for _, raw := range groups {
		group := raw.(map[string]any)
		if group["key"] == "structural" {
			structural = group["items"].([]any)
		}
	}
	require.Len(t, structural, 1)
	item := structural[0].(map[string]any)
	ref := item["ref"].(map[string]any)
	require.Equal(t, "NOTE", ref["kind"])
	require.Equal(t, "notes/materials/opaque-slug.md", ref["notePath"])
	require.Equal(t, "plan", item["relationName"])
	require.Equal(t, "Human-readable plan title", item["title"])
}

func TestExecute_NodeWorkspaceRelationGroupsBuildSchemaDeclaredNavigation(t *testing.T) {
	const sharedPlan = "# Shared delivery plan\n\n## Details\n\nImplementation detail.\n"
	env := newCustomQueryTestEnv(t, `
type Workspace @node(paths: ["notes/workspaces/*.md"]) @display(singular: "Effort", plural: "Efforts") {
  plan: Material! @link @workspaceMember(label: "Implementation plan")
  workLog: Material! @link @workspaceMember(label: "Work log")
  materials: [Material!] @link @workspaceMember
  governing: Material! @link
}

type DetailsSection implements Section {
}

type Material @node(paths: ["notes/materials/*.md"]) {
  summary: String
  details: DetailsSection @contains(level: H2, heading: "Details")
}
`, map[string]string{
		"notes/workspaces/alpha.md": `---
plan: "[[../materials/shared-plan]]"
work-log: "[[../materials/alpha-log]]"
materials:
  - "[[../materials/alpha-report]]"
governing: "[[../materials/governing]]"
---
# Alpha effort
`,
		"notes/workspaces/beta.md": `---
plan: "[[../materials/shared-plan]]"
work-log: "[[../materials/beta-log]]"
materials: []
governing: "[[../materials/governing]]"
---
# Beta effort
`,
		"notes/materials/shared-plan.md":  sharedPlan,
		"notes/materials/alpha-log.md":    "# Alpha execution log\n",
		"notes/materials/alpha-report.md": "# Verification report\n",
		"notes/materials/beta-log.md":     "# Beta execution log\n",
		"notes/materials/governing.md":    "# Governing contract\n",
	})

	query := `query Workspace($ref: String!) {
  node(ref: $ref) {
    workspace {
      relationGroups {
        key label ownerTitle navigation
        items { ref { notePath } title targetTitle relationName current }
      }
    }
  }
}`
	execute := func(ref string) []any {
		prepared, errs := PrepareWithVariables(env.execSchema, query, map[string]any{"ref": ref})
		require.Empty(t, errs)
		result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
		require.Empty(t, result.Errors)
		workspace := result.Data["node"].(map[string]any)["workspace"].(map[string]any)
		return workspace["relationGroups"].([]any)
	}

	ownerGroups := execute("notes/workspaces/alpha.md")
	var navigation, structural map[string]any
	for _, raw := range ownerGroups {
		group := raw.(map[string]any)
		if group["navigation"] == true {
			navigation = group
		}
		if group["key"] == "structural" {
			structural = group
		}
	}
	require.NotNil(t, navigation)
	require.NotNil(t, structural, "the unpromoted governing relation remains structural context")
	require.Equal(t, true, navigation["navigation"])
	require.Equal(t, "In this effort", navigation["label"])
	require.Equal(t, "Alpha effort", navigation["ownerTitle"])
	items := navigation["items"].([]any)
	require.Equal(t, []string{"Overview", "Implementation plan", "Work log", "Verification report"}, []string{
		items[0].(map[string]any)["title"].(string),
		items[1].(map[string]any)["title"].(string),
		items[2].(map[string]any)["title"].(string),
		items[3].(map[string]any)["title"].(string),
	})
	require.Equal(t, true, items[0].(map[string]any)["current"])
	require.Equal(t, "Shared delivery plan", items[1].(map[string]any)["targetTitle"])
	require.Equal(t, "structural", structural["key"])
	require.Equal(t, "Governing contract", structural["items"].([]any)[0].(map[string]any)["title"])

	childGroups := execute("notes/materials/shared-plan.md")
	ownerTitles := make([]string, 0, 2)
	for _, raw := range childGroups {
		group := raw.(map[string]any)
		if group["navigation"] != true {
			continue
		}
		require.Equal(t, true, group["navigation"])
		ownerTitles = append(ownerTitles, group["ownerTitle"].(string))
		var currentItems int
		for _, rawItem := range group["items"].([]any) {
			item := rawItem.(map[string]any)
			if item["current"] == true {
				currentItems++
				require.Equal(t, "Implementation plan", item["title"])
			}
		}
		require.Equal(t, 1, currentItems)
	}
	require.Len(t, ownerTitles, 2, "the shared material exposes each explicit owner separately")
	require.Equal(t, []string{"Alpha effort", "Beta effort"}, ownerTitles)

	sections := ontology.ParseSections("notes/materials/shared-plan.md", sharedPlan)
	require.Len(t, sections, 1)
	require.Len(t, sections[0].Children, 1)
	sectionGroups := execute(sections[0].Children[0].ID)
	var sectionOwners []string
	for _, raw := range sectionGroups {
		group := raw.(map[string]any)
		if group["navigation"] != true {
			continue
		}
		sectionOwners = append(sectionOwners, group["ownerTitle"].(string))
		var currentItems int
		for _, rawItem := range group["items"].([]any) {
			item := rawItem.(map[string]any)
			if item["current"] == true {
				currentItems++
				require.Equal(t, "Implementation plan", item["title"])
			}
		}
		require.Equal(t, 1, currentItems, "focusing a section keeps its containing member current")
	}
	require.Equal(t, ownerTitles, sectionOwners, "section focus preserves the document's workspace navigation")
}
