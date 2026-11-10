package query

import (
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit/recordfixture"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/stretchr/testify/require"
)

const capabilitySelection = `capabilities { canEdit canEditFields canEditCollections canNavigateChildren canSubscribe readOnlyReason }`

// SPEC-0136 R8: a capability-only workspace selection builds no workspace and
// reports exactly what the full workspace reports, across Markdown roots,
// embedded and section nodes, empty collections, owned files, and
// provider-backed records.
func TestCapabilityOnlyWorkspaceMatchesFullWorkspace(t *testing.T) {
	markdown := newCustomQueryTestEnv(t, `
type Story implements Section @node(locator: EMBEDDED) {
  status: String @field
}
type StoriesSection implements Section {
  stories: [Story!] @contains(level: H3)
}
type Spec @node(paths: ["notes/*.md"]) {
  summary: String
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}
type Plain @node(paths: ["plain/*.md"]) {
  summary: String
}
`, map[string]string{
		".rhizome/source-ownership.yml": "sources:\n  - owner: Fixture tool\n    reason: Change it with the fixture tool.\n    paths: [notes/owned.md]\n",
		"notes/spec.md":                 "---\nsummary: Spec\n---\n\n## Stories\n\n### First\nstatus:: planned\n^story-one\n",
		"notes/empty.md":                "---\nsummary: No stories\n---\n\n## Stories\n",
		"notes/owned.md":                "---\nsummary: Owned\n---\n\n## Stories\n\n### Owned story\nstatus:: done\n^owned-story\n",
		"plain/bare.md":                 "Body only.\n",
	})
	for _, ref := range []string{"notes/spec.md", "notes/spec.md#^story-one", "notes/empty.md", "notes/owned.md", "notes/owned.md#^owned-story", "plain/bare.md"} {
		t.Run(ref, func(t *testing.T) {
			assertCapabilityOnlyMatchesFull(t, markdown.execSchema, markdown.schema, markdown.deps(nil), `node(ref: "`+ref+`")`, "node")
		})
	}

	files := recordfixture.Files()
	files[".rhizome/source-ownership.yml"] = "sources:\n  - owner: Fixture author\n    reason: Change this with the owning tool.\n    paths: [account.yaml]\n"
	records := newConfiguredQueryTestEnv(t, recordfixture.SDL, files, recordfixture.Includes)
	for _, root := range []string{`service(sort:[{field:"id"}])`, "grouped", "accountEntry"} {
		t.Run(root, func(t *testing.T) {
			assertCapabilityOnlyMatchesFull(t, records.execSchema, records.schema, records.deps(nil), root, rootResponseKey(root))
		})
	}
}

func assertCapabilityOnlyMatchesFull(t *testing.T, execSchema *ExecutableSchema, schema *ontoschema.Schema, deps Deps, root, key string) {
	t.Helper()
	builds := 0
	restore := buildQueryWorkspace
	buildQueryWorkspace = func(schema *ontoschema.Schema, projection *ontology.NodeProjection) *ontology.NodeWorkspace {
		builds++
		return restore(schema, projection)
	}
	t.Cleanup(func() { buildQueryWorkspace = restore })
	run := func(selection string) any {
		prepared, errs := Prepare(execSchema, `{ `+root+` { workspace { `+selection+` } } }`)
		require.Empty(t, errs)
		result := Execute(t.Context(), deps, schema, prepared)
		require.Empty(t, result.Errors)
		return result.Data[key]
	}
	capabilityOnly := run(capabilitySelection)
	require.Zero(t, builds, "a capability-only selection must not build the workspace")
	full := run("version " + capabilitySelection)
	require.NotZero(t, builds)
	require.NotEmpty(t, capabilityOnly)
	require.Equal(t, stripWorkspaceVersion(full), capabilityOnly)
}

func rootResponseKey(root string) string {
	for i, r := range root {
		if r == '(' {
			return root[:i]
		}
	}
	return root
}

func stripWorkspaceVersion(value any) any {
	switch current := value.(type) {
	case []any:
		out := make([]any, len(current))
		for i, item := range current {
			out[i] = stripWorkspaceVersion(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(current))
		for k, v := range current {
			if k != "version" {
				out[k] = stripWorkspaceVersion(v)
			}
		}
		return out
	}
	return value
}
