package query

import (
	"context"
	"fmt"
	"strings"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestSectionRefPreservesProjectedIdentityAcrossDirectAndIndexedReads(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Story implements Section @node(locator: EMBEDDED) { status: String @field }
type Stories implements Section { stories: [Story!] @contains(level: H3) }
type Spec @node(paths: ["notes/*.md"]) { stories: Stories @contains(level: H2, heading: "Stories") }
`, map[string]string{"notes/spec.md": "---\ntype: Spec\n---\n# Spec\n\n## Stories\n\n### Alpha task\nstatus:: planned\n^alpha-task\n"})
	projection, err := ontology.ProjectNode(t.Context(), obsidian.VaultDefinition{Path: env.root}, &obsidian.Note{}, env.schema, ontology.NodeRef{NotePath: "notes/spec.md", Fragment: "^alpha-task", Kind: ontology.NodeKindEmbedded})
	require.NoError(t, err)
	require.NotEmpty(t, projection.Ref.Structural)

	t.Run("direct heading locator", func(t *testing.T) {
		prepared, errs := Prepare(env.execSchema, `{
 node(ref: "notes/spec.md#^alpha-task") { ref { kind notePath fragment nodeId structural typeName } }
 resolved: resolve(ref: "notes/spec.md#^alpha-task") { found ref { kind notePath fragment nodeId structural typeName } }
}`)
		require.Empty(t, errs)
		result := Execute(t.Context(), env.deps(nil), env.schema, prepared)
		require.Empty(t, result.Errors)
		ref := result.Data["node"].(map[string]any)["ref"].(map[string]any)
		require.Equal(t, projection.Ref.Structural, ref["structural"])
		require.Equal(t, projection.Ref.NodeID, ref["nodeId"])
		require.Equal(t, projection.Ref.Fragment, ref["fragment"])
		require.Equal(t, "EMBEDDED", ref["kind"])
		resolved := result.Data["resolved"].(map[string]any)
		require.Equal(t, true, resolved["found"])
		require.Equal(t, ref, resolved["ref"])
	})
	t.Run("indexed ref-only does not read content", func(t *testing.T) {
		prepared, errs := Prepare(env.execSchema, `{ story(first: 10) { ref { kind notePath fragment nodeId structural typeName } } }`)
		require.Empty(t, errs)
		deps := env.deps(nil)
		deps.NoteReader = contentFailingQueryNoteReader{inner: &obsidian.Note{}}
		deps.Service = nil
		result := Execute(t.Context(), deps, env.schema, prepared)
		require.Empty(t, result.Errors)
		rows := result.Data["story"].([]any)
		require.Len(t, rows, 1)
		ref := rows[0].(map[string]any)["ref"].(map[string]any)
		require.Equal(t, projection.Ref.Structural, ref["structural"])
	})
}

func TestSectionRefProjectionReadsStayBoundedPerHost(t *testing.T) {
	for _, count := range []int{1, 10, 40} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			var content strings.Builder
			content.WriteString("---\ntype: Spec\n---\n# Spec\n\n## Stories\n\n")
			for i := 0; i < count; i++ {
				fmt.Fprintf(&content, "### Task %d\nstatus:: planned\n^task-%d\n\n", i, i)
			}
			env := newCustomQueryTestEnv(t, `type Story implements Section @node(locator: EMBEDDED) { status: String @field }
type Stories implements Section { stories: [Story!] @contains(level: H3) }
type Spec @node(paths: ["notes/*.md"]) { stories: Stories @contains(level: H2, heading: "Stories") }`, map[string]string{"notes/spec.md": content.String()})
			for _, selection := range []string{"title", "ref { structural }"} {
				deps := env.deps(nil)
				calls := 0
				original := deps.ExactNoteMetadataRows
				deps.ExactNoteMetadataRows = func(ctx context.Context, paths []string) (map[string]semdb.NoteMetadataRow, error) {
					calls++
					return original(ctx, paths)
				}
				reader := &boundedQueryReader{NoteReader: &obsidian.Note{}}
				deps.NoteReader = reader
				deps.Service = nil
				prepared, errs := Prepare(env.execSchema, `{ spec(path: "notes/spec.md") { stories { stories { `+selection+` } } } }`)
				require.Empty(t, errs)
				result := Execute(t.Context(), deps, env.schema, prepared)
				require.Empty(t, result.Errors)
				t.Logf("children=%d selection=%s metadata=%d sourceReads=%d", count, selection, calls, reader.contents)
				require.Equal(t, 1, calls, "metadata lookups must stay bounded per host")
				require.LessOrEqual(t, reader.contents, 2, "source reads must stay bounded per host")
			}
		})
	}
}
