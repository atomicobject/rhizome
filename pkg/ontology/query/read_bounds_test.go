package query

import (
	"context"
	"fmt"

	"strings"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type readBoundsStore struct {
	*semdb.Store
	typeBatches, metadataBatches, unboundedTypeScans int
	propertyReads                                    int
}

func (s *readBoundsStore) OntologyTypesByPaths(ctx context.Context, paths []string) (map[string]semdb.OntologyNoteTypeRow, error) {
	s.typeBatches++
	return s.Store.OntologyTypesByPaths(ctx, paths)
}

func (s *readBoundsStore) CurrentNoteMetadataRowsByPaths(ctx context.Context, paths []string) (map[string]semdb.NoteMetadataRow, error) {
	s.metadataBatches++
	return s.Store.CurrentNoteMetadataRowsByPaths(ctx, paths)
}

func (s *readBoundsStore) OntologyPathsByType(ctx context.Context, name string, limit int) ([]string, error) {
	if limit <= 0 || limit >= 100000 {
		s.unboundedTypeScans++
	}
	return s.Store.OntologyPathsByType(ctx, name, limit)
}

func (s *readBoundsStore) CurrentNotePropertyValues(ctx context.Context, paths []string, names []string, source semdb.NotePropertySource) ([]semdb.NotePropertyValueRow, error) {
	s.propertyReads++
	return s.Store.CurrentNotePropertyValues(ctx, paths, names, source)
}

func TestExecuteTypedRootsAndNodeBatchesHaveBoundedReads(t *testing.T) {
	notes := map[string]string{}
	refs := make([]string, 40)
	for i := range refs {
		path := fmt.Sprintf("notes/%02d.md", i)
		refs[i] = fmt.Sprintf("%q", path)
		notes[path] = fmt.Sprintf("---\nname: Item %d\n---\n", i)
	}
	env := newCustomQueryTestEnv(t, `type Item @node(paths: ["notes/*.md"]) { name: String @field }`, notes)
	for _, query := range []string{
		`{ item(first: 40) { path name } }`,
		`{ item(first: 40, filters: [{field: "name", op: contains, value: "Item"}]) { path name } }`,
		`{ nodes(refs: [` + strings.Join(refs, ",") + `]) { items { node { ... on Item { path name } } } } }`,
	} {
		t.Run(query[:min(50, len(query))], func(t *testing.T) {
			store := &readBoundsStore{Store: env.store}
			deps := env.deps(nil)
			deps.Store = store
			deps.ExactNoteMetadataRows = nil
			prepared, errs := Prepare(env.execSchema, query)
			require.Empty(t, errs)
			result := Execute(context.Background(), deps, env.schema, prepared)
			require.Empty(t, result.Errors)
			if rows, ok := result.Data["item"]; ok {
				require.Len(t, rows, 40)
			} else {
				require.Len(t, result.Data["nodes"].(map[string]any)["items"], 40)
			}
			require.LessOrEqual(t, store.typeBatches, 2)
			require.LessOrEqual(t, store.metadataBatches, 2)
		})
	}
}

func TestExecuteUnknownIdentifierDoesNotReadNoteBodies(t *testing.T) {
	notes := map[string]string{}
	for i := 0; i < 40; i++ {
		notes[fmt.Sprintf("notes/%02d.md", i)] = fmt.Sprintf("---\nid: SPEC-%04d\n---\n- [ ] Child #item\n  id:: CHILD-%d\n", i, i)
	}
	env := newCustomQueryTestEnv(t, `type Spec @node(paths: ["notes/*.md"]) { id: ID! @field @identifier(preferred: true) }
type Item implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#item") { id: ID! @field @identifier(preferred: true, derivedSuffix: "ITEM") }`, notes)
	reader := &boundedQueryReader{NoteReader: &obsidian.Note{}}
	store := &readBoundsStore{Store: env.store}
	deps := env.deps(nil)
	deps.NoteReader, deps.Store = reader, store
	prepared, errs := Prepare(env.execSchema, `{ node(ref: "SPEC-9999") { nodeKind } }`)
	require.Empty(t, errs)
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	require.Nil(t, result.Data["node"])
	require.Zero(t, reader.contents)
	require.Zero(t, store.unboundedTypeScans)
	require.Zero(t, store.propertyReads, "missing identifiers must use indexed field predicates without loading candidate note properties")
}

func TestExecuteSectionNeighborsDoNotReadUnrelatedNotes(t *testing.T) {
	notes := map[string]string{
		"specs/target.md":   "## Stories\n\n### Story\n^story\n\nBody.\n",
		"efforts/linked.md": "Targets [[specs/target#^story]].\n",
	}
	for i := 0; i < 30; i++ {
		notes[fmt.Sprintf("efforts/unrelated-%02d.md", i)] = "No outgoing links.\n"
	}
	env := newCustomQueryTestEnv(t, `type Effort @node(paths: ["efforts/*.md"]) { name: String @field }
type Story implements Section { efforts: [Effort!] @neighbors(direction: INBOUND, type: "Effort", scope: SUBTREE) }
type Stories implements Section { stories: [Story!] @contains(level: H3) }
type Spec @node(paths: ["specs/*.md"]) { stories: Stories @contains(level: H2, heading: "Stories") }`, notes)
	reader := &inventoryQueryReader{NoteReader: &obsidian.Note{}, contents: map[string]int{}}
	for path := range notes {
		reader.paths = append(reader.paths, path)
	}
	deps := env.deps(nil)
	deps.NoteReader = reader
	prepared, errs := Prepare(env.execSchema, `{ spec(path: "specs/target.md") { stories { stories { efforts { path } } } } }`)
	require.Empty(t, errs)
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	story := result.Data["spec"].([]any)[0].(map[string]any)["stories"].(map[string]any)["stories"].([]any)[0].(map[string]any)
	require.Equal(t, []any{map[string]any{"path": "efforts/linked.md"}}, story["efforts"])
	for path, reads := range reader.contents {
		if strings.Contains(path, "unrelated") {
			require.Zero(t, reads, path)
		}
	}
}
