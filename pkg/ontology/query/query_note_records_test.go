package query

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2/ast"
	"sync/atomic"
	"testing"
	"time"
)

func TestResolveNodeFieldDisclosesProviderSourceContract(t *testing.T) {
	executor := &executor{}
	node := &noteRecord{
		Format:                 "html",
		SourceRepresentation:   ontology.SourceRepresentationUTF8,
		EvidenceRepresentation: ontology.EvidenceRepresentationProviderProjection,
		Capabilities:           []noteformat.Capability{noteformat.CapabilitySourceReading, noteformat.CapabilityActiveContentViewing},
	}
	tests := map[string]any{
		"format":                 "html",
		"sourceRepresentation":   "source_utf8",
		"evidenceRepresentation": "provider_projection",
		"sourceCapabilities":     []string{"source_reading", "active_content_viewing"},
	}
	for name, expected := range tests {
		value, ok := executor.resolveNodeField(context.Background(), node, &ast.Field{Name: name}, nil)
		require.True(t, ok, name)
		require.Equal(t, expected, value, name)
	}
}

func TestExecute_CachesNoteRecordsWithinRequest(t *testing.T) {
	env := newQueryTestEnv(t)
	prepared, errs := Prepare(env.execSchema, `
{
  project(path: "notes/projects/roadmap-refresh.md") {
    primaryOwner: owner { title }
    repeatedOwner: owner { title }
  }
}
`)
	require.Empty(t, errs)

	spy := &spyQueryStore{Store: env.store}
	deps := env.deps(nil)
	deps.Store = spy
	deps.ExactNoteMetadataRows = nil

	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	projects := result.Data["project"].([]any)
	require.Len(t, projects, 1)
	project := projects[0].(map[string]any)
	require.Equal(t, "Alice", project["primaryOwner"].(map[string]any)["title"])
	require.Equal(t, "Alice", project["repeatedOwner"].(map[string]any)["title"])
	require.Equal(t, 1, spy.metadataPathLoads["notes/people/alice.md"])
}

func TestExecute_RequiredListFieldMissingReturnsPartialDataWithError(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
  aliases: [String!]! @field(source: "aliases")
}
`, map[string]string{
		"notes/projects/roadmap.md": `---
type: Project
name: Roadmap
---
`,
	})
	prepared, errs := Prepare(env.execSchema, `{ project(path: "notes/projects/roadmap.md") { name aliases } }`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Len(t, result.Errors, 1)
	rows := result.Data["project"].([]any)
	require.Len(t, rows, 1)
	record := rows[0].(map[string]any)
	require.Equal(t, "Roadmap", record["name"])
	_, hasAliases := record["aliases"]
	require.False(t, hasAliases)
}

func TestExecute_MixedCaseSourceAndEmptyListRoundTripFromMetadata(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
  status: String! @field(source: "Status")
  aliases: [String!]! @field(source: "Aliases")
}
`, map[string]string{
		"notes/projects/roadmap.md": `---
type: Project
name: Roadmap
Status: active
Aliases: []
---
`,
	})
	prepared, errs := Prepare(env.execSchema, `{ project(path: "notes/projects/roadmap.md") { status aliases } }`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["project"].([]any)
	require.Len(t, rows, 1)
	record := rows[0].(map[string]any)
	require.Equal(t, "active", record["status"])
	require.Equal(t, []any{}, record["aliases"])
}

func TestExecute_DoesNotParsePathsMissingFromMetadataRows(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Person @node(paths: ["notes/people/*.md"]) {
  name: String!
}

type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
  owner: Person! @link(source: "owner")
}
`, map[string]string{
		"notes/projects/roadmap.md": `---
type: Project
name: Roadmap
owner: notes/people/alice.md
---
`,
		"notes/people/alice.md": `---
type: Person
name: Alice
---
`,
	})
	require.NoError(t, env.store.ApplyNoteMetadataDelta(context.Background(), codeanchorsqlite.NoteMetadataDelta{
		State:        codeanchorsqlite.NoteMetadataState{LoadedAt: 99, Ready: true},
		DeletedPaths: []string{"notes/people/alice.md"},
	}))

	prepared, errs := Prepare(env.execSchema, `{ project(path: "notes/projects/roadmap.md") { name owner { name } } }`)
	require.Empty(t, errs)
	reader := &inventoryQueryReader{NoteReader: &obsidian.Note{}, contents: map[string]int{}}
	deps := env.deps(nil)
	deps.NoteReader = reader
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.NotEmpty(t, result.Errors)
	require.Contains(t, result.Errors[0].Message, "required relation Project.owner is missing")
	require.Equal(t, []string{"project", "owner"}, result.Errors[0].Path)
	projects := result.Data["project"].([]any)
	require.Len(t, projects, 1)
	require.Equal(t, "Roadmap", projects[0].(map[string]any)["name"])
	require.Zero(t, reader.contents["notes/people/alice.md"])
}

func TestExecute_InvalidTypedNotesStillResolveAtRoot(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
}
`, map[string]string{
		"notes/projects/good.md": `---
type: Project
name: Good
---
`,
		"notes/projects/bad.md": `---
type: Project
---
`,
	})
	prepared, errs := Prepare(env.execSchema, `{ project(find: "project") { path name } }`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Len(t, result.Errors, 1)
	rows := result.Data["project"].([]any)
	require.Len(t, rows, 2)
	require.Equal(t, "notes/projects/bad.md", rows[0].(map[string]any)["path"])
	require.Equal(t, "notes/projects/good.md", rows[1].(map[string]any)["path"])
}

type contentFailingQueryNoteReader struct {
	inner obsidian.NoteReader
}

func (r contentFailingQueryNoteReader) GetContents(obsidian.VaultDefinition, string) (string, error) {
	return "", fmt.Errorf("unexpected markdown projection")
}

func (r contentFailingQueryNoteReader) GetNotesList(vault obsidian.VaultDefinition) ([]string, error) {
	return r.inner.GetNotesList(vault)
}

func (r contentFailingQueryNoteReader) GetModTime(vault obsidian.VaultDefinition, noteName string) (time.Time, error) {
	return r.inner.GetModTime(vault, noteName)
}

func (r contentFailingQueryNoteReader) Title(noteName string) (string, bool) {
	return r.inner.Title(noteName)
}

type countingNoteReader struct {
	inner          obsidian.NoteReader
	notesListCalls atomic.Int64
}

func (r *countingNoteReader) GetContents(vault obsidian.VaultDefinition, noteName string) (string, error) {
	return r.inner.GetContents(vault, noteName)
}

func (r *countingNoteReader) GetNotesList(vault obsidian.VaultDefinition) ([]string, error) {
	r.notesListCalls.Add(1)
	return r.inner.GetNotesList(vault)
}

func (r *countingNoteReader) GetModTime(vault obsidian.VaultDefinition, noteName string) (time.Time, error) {
	return r.inner.GetModTime(vault, noteName)
}

func (r *countingNoteReader) Title(noteName string) (string, bool) {
	return r.inner.Title(noteName)
}

type spyQueryStore struct {
	*codeanchorsqlite.Store
	maxMetadataPathsBatch int
	metadataPathLoads     map[string]int
}

func (s *spyQueryStore) CurrentNoteMetadataRowsByPaths(ctx context.Context, paths []string) (map[string]codeanchorsqlite.NoteMetadataRow, error) {
	if len(paths) > s.maxMetadataPathsBatch {
		s.maxMetadataPathsBatch = len(paths)
	}
	if s.metadataPathLoads == nil {
		s.metadataPathLoads = make(map[string]int)
	}
	for _, path := range paths {
		s.metadataPathLoads[path]++
	}
	return s.Store.CurrentNoteMetadataRowsByPaths(ctx, paths)
}
