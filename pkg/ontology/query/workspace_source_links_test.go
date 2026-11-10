package query

import (
	"context"
	"fmt"
	"strings"

	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

func TestExecuteWorkspaceMarkdownAnchorsKeepCanonicalSourcePath(t *testing.T) {
	for _, tc := range []struct{ name, source, decoy string }{
		{"literal percent escape", "notes/Budget%20USD.md", "notes/Budget USD.md"},
		{"literal hash", "notes/Budget#USD.md", "notes/Budget.md"},
		{"space", "notes/Budget USD.md", "notes/Budget.md"},
		{"ordinary nested path", "notes/BudgetUSD.md", "notes/Budget.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newCustomQueryTestEnv(t, `
type Focus implements Section {}
type Source @node(paths: ["notes/*.md"]) {
  focus: Focus @contains(level: H2, heading: "Focus")
  block: Focus @contains(level: H2, heading: "Block")
}
`, map[string]string{
				tc.source: "[heading](#Focus) [block](#^source-block)\n\n## Focus\n\nContent\n\n## Block\n\nParagraph\n^source-block\n",
				tc.decoy:  "## Focus\n\nDecoy\n\n## Block\n\nParagraph\n^source-block\n",
			})
			prepared, errs := PrepareWithVariables(env.execSchema, `query($path: String!) {
  note(path: $path) { path workspace { sourceLinks(first: 10) {
    authoredTarget target resolved resolvedRef { notePath }
  } } }
}`, map[string]any{"path": tc.source})
			require.Empty(t, errs)
			result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
			require.Empty(t, result.Errors)
			note := result.Data["note"].(map[string]any)
			require.Equal(t, tc.source, note["path"])
			links := note["workspace"].(map[string]any)["sourceLinks"].([]any)
			require.Len(t, links, 2)
			for i, fragment := range []string{"Focus", "^source-block"} {
				link := links[i].(map[string]any)
				require.Equal(t, "#"+fragment, link["authoredTarget"])
				require.Equal(t, tc.source+"#"+fragment, link["target"])
				require.Equal(t, true, link["resolved"])
				ref := link["resolvedRef"].(map[string]any)
				require.Equal(t, tc.source, ref["notePath"], "a decoy with the same fragments must not replace the canonical source")
			}
		})
	}
}

func TestWorkspaceSourceLinkPagination(t *testing.T) {
	for _, count := range []int{0, 99, 100, 101, 200, 201} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			var body strings.Builder
			body.WriteString("## Focus\n\n")
			for i := range count {
				fmt.Fprintf(&body, "[[target|Occurrence %d]]\n", i)
			}
			body.WriteString("\n## Sibling\n\n[[excluded]]\n")
			env := newCustomQueryTestEnv(t, `type Focus implements Section {} type Source @node(paths: ["notes/source.md"]) { focus: Focus @contains(level: H2, heading: "Focus") }`, map[string]string{
				"notes/source.md": body.String(), "notes/target.md": "# Target\n",
			})
			for _, bounds := range [][2]int{{100, 0}, {100, 99}, {100, 100}, {100, 200}, {100, 201}, {100, 2147483647}, {0, 0}, {-1, 0}, {1, 99}, {101, 0}, {2147483647, 100}} {
				first, offset := bounds[0], bounds[1]
				prepared, errs := PrepareWithVariables(env.execSchema, `query($first: Int!, $offset: Int!) {
  source { focus { workspace {
    total: sourceLinkCount
    page: sourceLinks(first: $first, offset: $offset) { text authoredTarget resolved }
  } } }
}`, map[string]any{"first": first, "offset": offset})
				require.Empty(t, errs)
				result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
				require.Empty(t, result.Errors)
				workspace := result.Data["source"].([]any)[0].(map[string]any)["focus"].(map[string]any)["workspace"].(map[string]any)
				require.Equal(t, count, workspace["total"])
				expected := max(0, min(count-offset, first, 100))
				page := workspace["page"].([]any)
				require.Len(t, page, expected, "first=%d offset=%d", first, offset)
				for i, raw := range page {
					item := raw.(map[string]any)
					require.Equal(t, fmt.Sprintf("Occurrence %d", offset+i), item["text"])
					require.Equal(t, "target", item["authoredTarget"])
					require.Equal(t, true, item["resolved"])
				}
			}
		})
	}
}

func TestWorkspaceSourceLinkNegativeOffsetRejected(t *testing.T) {
	env := newCustomQueryTestEnv(t, `type Source @node(paths: ["notes/source.md"]) { title: String }`, map[string]string{"notes/source.md": "[[missing]]"})
	for _, query := range []string{
		`{ node(ref: "notes/source.md") { workspace { sourceLinks(offset: -1) { text } } } }`,
		`query($offset: Int!) { node(ref: "notes/source.md") { workspace { page: sourceLinks(first: 0, offset: $offset) { text } } } }`,
	} {
		_, errs := PrepareWithVariables(env.execSchema, query, map[string]any{"offset": -1})
		require.NotEmpty(t, errs)
		require.Contains(t, errs[0].Message, "offset must be >= 0")
	}
}

func TestWorkspaceSourceLinkOverlayAndOccurrenceCount(t *testing.T) {
	env := newCustomQueryTestEnv(t, `type Source @node(paths: ["notes/source.md"]) { title: String }`, map[string]string{
		"notes/source.md": "[[old]]", "notes/target.md": "---\naliases: [alias]\n---\n# Target\n",
	})
	deps := env.deps(nil)
	deps.ReadOverlay = &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{
		"notes/source.md": "[[alias]] [[missing]] ![[image.png]] [[alias]] [anchor](#Focus)\n\n## Focus\n\nBody\n",
	}, Status: "dirty"}
	prepared, errs := Prepare(env.execSchema, `{ node(ref: "notes/source.md") { workspace {
  sourceLinkCount
  page: sourceLinks(first: 4, offset: 1) { authoredTarget resolved embed target }
} } }`)
	require.Empty(t, errs)
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	workspace := result.Data["node"].(map[string]any)["workspace"].(map[string]any)
	require.Equal(t, 5, workspace["sourceLinkCount"])
	page := workspace["page"].([]any)
	require.Len(t, page, 4)
	for i, target := range []string{"missing", "image.png", "alias", "#Focus"} {
		require.Equal(t, target, page[i].(map[string]any)["authoredTarget"])
	}
	require.Equal(t, false, page[0].(map[string]any)["resolved"])
	require.Equal(t, true, page[1].(map[string]any)["embed"])
	require.Equal(t, "notes/target.md", page[2].(map[string]any)["target"])
	require.Equal(t, "notes/source.md#Focus", page[3].(map[string]any)["target"])
}

type sourceLinkHydrationStore struct {
	*semdb.Store
	propertyPaths []string
}

func (s *sourceLinkHydrationStore) CurrentNotePropertyValues(ctx context.Context, paths, names []string, source semdb.NotePropertySource) ([]semdb.NotePropertyValueRow, error) {
	s.propertyPaths = append(s.propertyPaths, paths...)
	return s.Store.CurrentNotePropertyValues(ctx, paths, names, source)
}

func TestWorkspaceSourceLinkCountDoesNotHydrateTargets(t *testing.T) {
	env := newCustomQueryTestEnv(t, `type Source @node(paths: ["notes/source.md"]) { title: String }`, map[string]string{
		"notes/source.md": "[[first]] [[second]] [[missing]] ![[image.png]] [[first]]", "notes/first.md": "# First", "notes/second.md": "# Second",
	})
	store := &sourceLinkHydrationStore{Store: env.store}
	deps := env.deps(nil)
	deps.Store = store
	for _, tc := range []struct {
		selection string
		targets   []string
	}{
		{`sourceLinkCount`, nil},
		{`sourceLinkCount sourceLinks(first: 1, offset: 1) { resolved title }`, []string{"notes/second.md"}},
	} {
		store.propertyPaths = nil
		prepared, errs := Prepare(env.execSchema, `{node(ref: "notes/source.md") {workspace {`+tc.selection+`}}}`)
		require.Empty(t, errs)
		result := Execute(context.Background(), deps, env.schema, prepared)
		require.Empty(t, result.Errors)
		require.Equal(t, 5, result.Data["node"].(map[string]any)["workspace"].(map[string]any)["sourceLinkCount"])
		var targets []string
		for _, path := range store.propertyPaths {
			if path != "notes/source.md" {
				targets = append(targets, path)
			}
		}
		require.ElementsMatch(t, tc.targets, targets)
	}
}

func TestWorkspaceSourceLinkDiscovery(t *testing.T) {
	env := newCustomQueryTestEnv(t, `type Source @node(paths: ["notes/source.md"]) { title: String }`, map[string]string{"notes/source.md": "Body"})
	discovered, err := DiscoverSchema(env.schema, env.execSchema, SchemaDiscoveryOptions{Type: "NodeWorkspaceProjection"})
	require.NoError(t, err)
	require.Contains(t, discovered.Schema, "sourceLinks(first: Int = 100, offset: Int = 0)")
	require.Contains(t, discovered.Schema, "sourceLinkCount: Int!")
	definition := env.execSchema.Schema.Types["NodeWorkspaceProjection"]
	require.Equal(t, "Int!", definition.Fields.ForName("sourceLinkCount").Type.String())
	args := definition.Fields.ForName("sourceLinks").Arguments
	require.Equal(t, "100", args.ForName("first").DefaultValue.Raw)
	require.Equal(t, "0", args.ForName("offset").DefaultValue.Raw)
	prepared, errs := Prepare(env.execSchema, `{__type(name: "NodeWorkspaceProjection") { fields { name args { name defaultValue } } }}`)
	require.Empty(t, errs)
	result := ExecuteIntrospection(env.execSchema, prepared)
	require.Empty(t, result.Errors)
	fields := result.Data["__type"].(map[string]any)["fields"].([]any)
	found := false
	for _, raw := range fields {
		field := raw.(map[string]any)
		if field["name"] == "sourceLinks" {
			require.Contains(t, field["args"], map[string]any{"name": "offset", "defaultValue": "0"})
			found = true
		}
	}
	require.True(t, found)
}
