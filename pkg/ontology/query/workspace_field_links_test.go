package query

import (
	"context"
	"sync/atomic"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

const fieldLinksSchema = `
type Opportunity @node(paths: ["notes/opportunities/*.md"]) {
  title: String @field
}

type Story implements Section @node(locator: EMBEDDED) {
  opportunity: Opportunity @link
}
type Stories implements Section { stories: [Story!] @contains(level: H3) }

type Project @node(paths: ["notes/projects/*.md"]) {
  name: String @field
  opportunities: [Opportunity!] @link
  stories: Stories @contains(level: H2, heading: "Stories")
}
`

const fieldLinksProject = `---
type: Project
name: Apollo
opportunities:
  - "[[IP opportunity - AI in the products we build]]"
  - notes/opportunities/second.md
  - "[[missing-opportunity|Missing]]"
---

## Stories

### First story
opportunity:: [[second]]
^story-one
`

func newFieldLinksTestEnv(t *testing.T) *queryTestEnv {
	t.Helper()
	return newCustomQueryTestEnv(t, fieldLinksSchema, map[string]string{
		"notes/projects/apollo.md": fieldLinksProject,
		"notes/opportunities/IP opportunity - AI in the products we build.md": `---
type: Opportunity
title: AI in the products we build
---
`,
		"notes/opportunities/second.md": `---
type: Opportunity
title: Second opportunity
---
`,
		"notes/opportunities/third.md": `---
type: Opportunity
title: Third opportunity
---
`,
	})
}

type fieldLink struct{ value, title, notePath string }

func workspaceLinksByField(t *testing.T, fields []any) map[string][]fieldLink {
	t.Helper()
	out := map[string][]fieldLink{}
	for _, raw := range fields {
		field := raw.(map[string]any)
		links := make([]fieldLink, 0)
		for _, rawLink := range field["links"].([]any) {
			item := rawLink.(map[string]any)
			next := fieldLink{value: item["value"].(string)}
			if item["resolved"] == true {
				next.title = item["title"].(string)
				next.notePath = item["ref"].(map[string]any)["notePath"].(string)
			} else {
				require.Nil(t, item["title"])
				require.Nil(t, item["ref"])
			}
			links = append(links, next)
		}
		out[field["name"].(string)] = links
	}
	return out
}

const fieldLinksQuery = `query FieldLinks($note: String!) {
  node(ref: $note) {
    workspace {
      fields { name links { value resolved title ref { notePath } } }
      bodies { ref { kind } fields { name links { value resolved title ref { notePath } } } }
    }
  }
}`

// countingFieldLinkStore counts the indexed field-value reads behind workspace links.
type countingFieldLinkStore struct {
	*codeanchorsqlite.Store
	fieldValueReads atomic.Int64
}

func (s *countingFieldLinkStore) OntologyNodeFieldValuesByNodeIDs(ctx context.Context, nodeIDs []string, fields []string) ([]codeanchor.IntelOntologyNodeFieldValue, error) {
	s.fieldValueReads.Add(1)
	return s.Store.OntologyNodeFieldValuesByNodeIDs(ctx, nodeIDs, fields)
}

func TestExecute_NodeWorkspaceFieldLinksReadIndexedTargetsOnce(t *testing.T) {
	env := newFieldLinksTestEnv(t)
	prepared, errs := PrepareWithVariables(env.execSchema, fieldLinksQuery, map[string]any{"note": "notes/projects/apollo.md"})
	require.Empty(t, errs)

	store := &countingFieldLinkStore{Store: env.store}
	reader := &countingNoteReader{inner: &obsidian.Note{}}
	deps := env.deps(nil)
	deps.Store = store
	deps.NoteReader = reader
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)

	workspace := result.Data["node"].(map[string]any)["workspace"].(map[string]any)
	links := workspaceLinksByField(t, workspace["fields"].([]any))
	require.Empty(t, links["name"], "non-relation fields carry no links")
	wantOpportunities := []fieldLink{
		{"[[IP opportunity - AI in the products we build]]", "AI in the products we build", "notes/opportunities/IP opportunity - AI in the products we build.md"},
		{"notes/opportunities/second.md", "Second opportunity", "notes/opportunities/second.md"},
		{"[[missing-opportunity|Missing]]", "", ""},
	}
	require.Equal(t, wantOpportunities, links["opportunities"])

	var storyLinks []fieldLink
	for _, raw := range workspace["bodies"].([]any) {
		body := raw.(map[string]any)
		if body["ref"].(map[string]any)["kind"] == "EMBEDDED" {
			storyLinks = workspaceLinksByField(t, body["fields"].([]any))["opportunity"]
		}
	}
	require.Equal(t, []fieldLink{{"[[second]]", "Second opportunity", "notes/opportunities/second.md"}}, storyLinks)

	require.EqualValues(t, 1, store.fieldValueReads.Load(), "fields and bodies share one indexed field-value read")
	require.Zero(t, reader.notesListCalls.Load(), "indexed values never rebuild the vault-wide note-path cache")
}

func TestExecute_NodeWorkspaceFieldLinksResolveStagedValues(t *testing.T) {
	env := newFieldLinksTestEnv(t)
	prepared, errs := PrepareWithVariables(env.execSchema, fieldLinksQuery, map[string]any{"note": "notes/projects/apollo.md"})
	require.Empty(t, errs)

	deps := env.deps(nil)
	deps.ReadOverlay = &ReadOverlay{
		SourceFormat: "markdown",
		UpdatedContentByPath: map[string]string{
			"notes/projects/apollo.md": `---
type: Project
name: Apollo
opportunities:
  - "[[third]]"
---
`,
		},
		TouchedPaths: []string{"notes/projects/apollo.md"},
		Status:       "dirty",
	}
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	workspace := result.Data["node"].(map[string]any)["workspace"].(map[string]any)
	require.Equal(t,
		[]fieldLink{{"[[third]]", "Third opportunity", "notes/opportunities/third.md"}},
		workspaceLinksByField(t, workspace["fields"].([]any))["opportunities"])
}

func TestExecute_NodeWorkspaceParentTitle(t *testing.T) {
	env := newFieldLinksTestEnv(t)
	prepared, errs := PrepareWithVariables(env.execSchema, `query Parent($ref: String!) {
  node(ref: $ref) { workspace { parentRef { notePath } parentTitle } }
}`, map[string]any{"ref": "notes/projects/apollo.md#^story-one"})
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	workspace := result.Data["node"].(map[string]any)["workspace"].(map[string]any)
	require.Equal(t, "notes/projects/apollo.md", workspace["parentRef"].(map[string]any)["notePath"])
	require.Equal(t, "Apollo", workspace["parentTitle"])
}
