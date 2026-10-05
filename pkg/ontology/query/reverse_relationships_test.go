package query

import (
	"context"
	"fmt"
	"strings"
	"testing"

	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology/guide"
	"github.com/stretchr/testify/require"
)

func TestReverseRelationshipAndLinkFilterSemantics(t *testing.T) {
	const schema = `
type Opportunity @node(paths: ["opportunities/*.md"]) {
  summary: String @field
  evidence: [Evidence!] @reverse(field: "opportunities")
  references: [Evidence!] @reverse(field: "references")
}

type Evidence @node(paths: ["evidence/*.md"]) {
  summary: String @field
  opportunities: [Opportunity!] @link(includeBodyLinks: false, includeBacklinks: false)
  references: [Opportunity!] @link(includeBodyLinks: false, includeBacklinks: false)
}
`
	notes := map[string]string{
		"opportunities/rhizome.md": "---\nsummary: Rhizome public use\n---\n",
		"evidence/explicit.md":     "---\nsummary: Positive evidence\nopportunities:\n  - '[[rhizome]]'\n---\n",
		"evidence/other.md":        "---\nsummary: Other relation\nreferences:\n  - '[[rhizome]]'\n---\n",
		"evidence/mention.md":      "---\nsummary: Passing mention\n---\nRhizome public use mentions [[rhizome]] in prose.\n",
	}
	env := newCustomQueryTestEnv(t, schema, notes)
	guideText, err := guide.RenderMarkdown(env.schema, []string{"Opportunity"})
	require.NoError(t, err)
	require.Contains(t, guideText, "reverse of authored link `Evidence.opportunities`")
	require.Contains(t, guideText, "do not write it into note markdown")
	require.NotContains(t, guideText, "inverse_mismatch")
	query := `{ opportunity(path: "opportunities/rhizome.md") { evidence { path } evidenceCount references { path } referencesCount } }`
	prepared, errs := Prepare(env.execSchema, query)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	items := result.Data["opportunity"].([]any)
	require.Len(t, items, 1)
	opportunity := items[0].(map[string]any)
	require.Equal(t, 1, opportunity["evidenceCount"])
	require.Equal(t, 1, opportunity["referencesCount"])
	require.Equal(t, []any{map[string]any{"path": "evidence/explicit.md"}}, opportunity["evidence"])
	require.Equal(t, []any{map[string]any{"path": "evidence/other.md"}}, opportunity["references"])

	for _, tc := range []struct {
		name, selector, filter, wantPath, wantError string
	}{
		{"eq", "", `{field: "opportunities", op: eq, value: "[[rhizome]]"}`, "evidence/explicit.md", ""},
		{"in", "", `{field: "opportunities", op: in, values: ["opportunities/rhizome.md"]}`, "evidence/explicit.md", ""},
		{"path eq", `path: "evidence/explicit.md", `, `{field: "opportunities", op: eq, value: "opportunities/rhizome.md"}`, "evidence/explicit.md", ""},
		{"path in", `path: "evidence/explicit.md", `, `{field: "opportunities", op: in, values: ["[[rhizome]]"]}`, "evidence/explicit.md", ""},
		{"find eq", `find: "explicit", `, `{field: "opportunities", op: eq, value: "opportunities/rhizome.md"}`, "evidence/explicit.md", ""},
		{"find in", `find: "explicit", `, `{field: "opportunities", op: in, values: ["[[rhizome]]"]}`, "evidence/explicit.md", ""},
		{"contains", "", `{field: "opportunities", op: contains, value: "rhizome"}`, "", "operator \"contains\" is unsupported"},
		{"unresolved", "", `{field: "opportunities", op: eq, value: "missing"}`, "", "did not resolve"},
		{"scalar contains", "", `{field: "summary", op: contains, value: "Positive"}`, "evidence/explicit.md", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prepared, errs := Prepare(env.execSchema, `{ evidence(`+tc.selector+`filters: [`+tc.filter+`]) { path } }`)
			require.Empty(t, errs)
			result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
			if tc.wantError != "" {
				require.NotEmpty(t, result.Errors)
				require.Contains(t, result.Errors[0].Message, tc.wantError)
				return
			}
			require.Empty(t, result.Errors)
			require.Equal(t, []any{map[string]any{"path": tc.wantPath}}, result.Data["evidence"])
		})
	}
	require.Contains(t, env.execSchema.SDL, "evidenceCount: Int!")
	require.Contains(t, env.execSchema.SDL, "referencesCount: Int!")
	require.True(t, strings.Contains(env.execSchema.SDL, "evidence(first: Int): [Evidence!]"))
}

func TestLinkFilterResolvesTargetPastFirstCatalogPage(t *testing.T) {
	const schema = `
type Opportunity @node(paths: ["opportunities/*.md"]) { summary: String @field }
type Evidence @node(paths: ["evidence/*.md"]) { opportunities: [Opportunity!] @link }
`
	notes := map[string]string{"evidence/final.md": "---\nopportunities:\n  - '[[opportunities/zz-last]]'\n---\n"}
	for i := 0; i < 101; i++ {
		notes[fmt.Sprintf("opportunities/%03d.md", i)] = "# Earlier opportunity\n"
	}
	notes["opportunities/zz-last.md"] = "# Final opportunity\n"
	env := newCustomQueryTestEnv(t, schema, notes)
	prepared, errs := Prepare(env.execSchema, `{ evidence(filters: [{field: "opportunities", op: eq, value: "opportunities/zz-last.md"}]) { path } }`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	require.Equal(t, []any{map[string]any{"path": "evidence/final.md"}}, result.Data["evidence"])
}

func TestReverseRelationshipPreservesEmbeddedSourceIdentity(t *testing.T) {
	const schema = `
type Opportunity @node(paths: ["opportunities/*.md"]) {
  stories: [ZStory!] @reverse(field: "opportunity")
  reviewStories: [ZStory!] @reverse(field: "reviewOpportunity")
  interfaceStories: [ZStoryInterface!] @reverse(field: "opportunity")
}
interface ZStoryBase implements Section {
  opportunity: Opportunity @link
}
interface ZStoryInterface implements ZStoryBase & Section {
  id: ID!
  notePath: String!
  title: String!
  level: SectionLevel!
  content: String!
  children(first: Int = 20): [Section!]!
  opportunity: Opportunity @link
}
type ZStory implements ZStoryInterface & ZStoryBase & Section @node(locator: EMBEDDED) @source(shape: LIST_ITEM, marker: "#story") {
  opportunity: Opportunity @link
  reviewOpportunity: Opportunity @link
}
`
	notes := map[string]string{
		"opportunities/rhizome.md": "# Rhizome public use\n",
		"notes/stories.md": `# Stories

- First #story
  opportunity:: [[opportunities/rhizome]]
- Second #story
  opportunity:: [[opportunities/rhizome]]
  review-opportunity:: [[opportunities/rhizome]]
- Third #story
  review-opportunity:: [[opportunities/rhizome]]

Passing prose mention: [[opportunities/rhizome]].
`,
	}
	env := newCustomQueryTestEnv(t, schema, notes)
	prepared, errs := Prepare(env.execSchema, `{ opportunity(path: "opportunities/rhizome.md") {
  stories { path notePath resolvedType }
  storiesCount
  reviewStories { path notePath resolvedType }
  reviewStoriesCount
  interfaceStories { path notePath resolvedType }
  interfaceStoriesCount
} }`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	opportunity := result.Data["opportunity"].([]any)[0].(map[string]any)
	for _, field := range []string{"stories", "reviewStories", "interfaceStories"} {
		rows := opportunity[field].([]any)
		require.Len(t, rows, 2, field)
		require.Equal(t, 2, opportunity[field+"Count"], field)
		require.NotEqual(t, rows[0].(map[string]any)["path"], rows[1].(map[string]any)["path"], field)
		for _, row := range rows {
			item := row.(map[string]any)
			require.Equal(t, "notes/stories.md", item["notePath"])
			require.Equal(t, "ZStory", item["resolvedType"])
		}
	}
	storyPaths := []any{opportunity["stories"].([]any)[0].(map[string]any)["path"], opportunity["stories"].([]any)[1].(map[string]any)["path"]}
	reviewPaths := []any{opportunity["reviewStories"].([]any)[0].(map[string]any)["path"], opportunity["reviewStories"].([]any)[1].(map[string]any)["path"]}
	require.NotEqual(t, storyPaths, reviewPaths)
	require.Equal(t, storyPaths, []any{opportunity["interfaceStories"].([]any)[0].(map[string]any)["path"], opportunity["interfaceStories"].([]any)[1].(map[string]any)["path"]})
}

// A list reverse or neighbor field's `first` argument caps the targets read
// per record, in the batched read under a root and on a single record, while
// its <field>Count still counts every target.
func TestRelationListFieldsTakeFirst(t *testing.T) {
	const schema = `
type Area @node(paths: ["areas/*.md"]) {
  name: String
  parent: Area @link
  children: [Area!] @reverse(field: "parent")
  mentions: [Area!] @neighbors(direction: INBOUND, type: "Area")
}
`
	notes := map[string]string{"areas/root.md": "---\ntype: Area\nname: Root\n---\n# Root\n"}
	for i := range 5 {
		notes[fmt.Sprintf("areas/a%d.md", i)] = fmt.Sprintf("---\ntype: Area\nname: A%d\nparent: \"[[root]]\"\n---\n# A%d\n\nSee [[root]].\n", i, i)
	}
	env := newCustomQueryTestEnv(t, schema, notes)
	require.Contains(t, env.execSchema.SDL, "children(first: Int): [Area!]")
	require.Contains(t, env.execSchema.SDL, "mentions(first: Int): [Area!]")

	// Both orders: the batched read keeps each limit's targets apart.
	for name, query := range map[string]string{
		"root":              `{ area(first: 10) { path children(first: 2) { path } childrenCount mentions(first: 3) { path } mentionsCount all: children { path } } }`,
		"root, all first":   `{ area(first: 10) { path all: children { path } children(first: 2) { path } childrenCount mentions(first: 3) { path } mentionsCount } }`,
		"single":            `{ area(path: "areas/root.md") { path children(first: 2) { path } childrenCount mentions(first: 3) { path } mentionsCount all: children { path } } }`,
		"single, all first": `{ area(path: "areas/root.md") { path all: children { path } children(first: 2) { path } childrenCount mentions(first: 3) { path } mentionsCount } }`,
	} {
		t.Run(name, func(t *testing.T) {
			prepared, errs := Prepare(env.execSchema, query)
			require.Empty(t, errs)
			result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
			require.Empty(t, result.Errors)
			var root map[string]any
			for _, item := range result.Data["area"].([]any) {
				if row := item.(map[string]any); row["path"] == "areas/root.md" {
					root = row
				}
			}
			require.NotNil(t, root)
			require.Len(t, root["children"], 2)
			require.Equal(t, 5, root["childrenCount"])
			require.Len(t, root["mentions"], 3)
			require.Equal(t, 5, root["mentionsCount"])
			require.Len(t, root["all"], 5, "without first, every target")
		})
	}
}

type scopeCountingStore struct {
	*codeanchorsqlite.Store
	scopes int
}

func (s *scopeCountingStore) GetValidationScopeSummaries(ctx context.Context, request codeanchorsqlite.ValidationScopeSummaryRequest) (codeanchorsqlite.ValidationScopeSummaryResponse, error) {
	s.scopes += len(request.Scopes)
	return s.Store.GetValidationScopeSummaries(ctx, request)
}

// A capped neighbor list hydrates and resolves only its first targets: each
// resolved target's issueCount registers one validation scope, so a hub with
// five neighbors read with first: 2 asks about two.
func TestNeighborListsResolveOnlyFirstTargets(t *testing.T) {
	const schema = `
type Area @node(paths: ["areas/*.md"]) {
  name: String
  mentions: [Area!] @neighbors(direction: INBOUND, type: "Area")
  notes: NotesSection @contains(level: H2, heading: "Notes")
}
type NotesSection implements Section {
  links: [Area!] @neighbors(direction: OUTBOUND, type: "Area")
}
`
	notes := map[string]string{}
	hubLinks := ""
	for i := range 5 {
		notes[fmt.Sprintf("areas/a%d.md", i)] = fmt.Sprintf("---\ntype: Area\nname: A%d\n---\n# A%d\n\nSee [[hub]].\n", i, i)
		hubLinks += fmt.Sprintf(" [[a%d]]", i)
	}
	notes["areas/hub.md"] = "---\ntype: Area\nname: Hub\n---\n# Hub\n\n## Notes\n\nLinks:" + hubLinks + "\n"
	env := newCustomQueryTestEnv(t, schema, notes)
	publishQueryValidationSnapshot(t, env.store, codeanchorsqlite.ValidationSnapshot{
		SelectedChecks: []string{"ontology"}, IssueCount: 1, AffectedNoteCount: 1,
		Checks:      []codeanchorsqlite.ValidationCheckSnapshot{{Check: "ontology", Outcome: codeanchorsqlite.ValidationCheckOutcomeCompleted, IssueCount: 1}},
		Diagnostics: []codeanchorsqlite.ValidationDiagnostic{{IssueKey: "h:1", Check: "ontology", Code: "x", PrimaryPath: "areas/hub.md", AffectedNotePaths: []string{"areas/hub.md"}}},
	})
	for name, tc := range map[string]struct {
		query string
		list  func(map[string]any) any
	}{
		"note":    {`{ area(path: "areas/hub.md") { mentions(first: 2) { issueCount } } }`, func(hub map[string]any) any { return hub["mentions"] }},
		"section": {`{ area(path: "areas/hub.md") { notes { links(first: 2) { issueCount } } } }`, func(hub map[string]any) any { return hub["notes"].(map[string]any)["links"] }},
	} {
		t.Run(name, func(t *testing.T) {
			prepared, errs := Prepare(env.execSchema, tc.query)
			require.Empty(t, errs)
			deps := env.deps(nil)
			store := &scopeCountingStore{Store: env.store}
			deps.Store = store
			result := Execute(context.Background(), deps, env.schema, prepared)
			require.Empty(t, result.Errors)
			hub := result.Data["area"].([]any)[0].(map[string]any)
			require.Len(t, tc.list(hub), 2)
			require.Equal(t, 2, store.scopes, "only the first two targets resolve")
		})
	}
}
