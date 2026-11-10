package query

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// SPEC-0136 R8: a read selecting no body, content, passage, or capability
// field opens no note body.
func TestBodyFreeSelectionsOpenNoNoteBody(t *testing.T) {
	env := newQueryTestEnv(t)
	for _, tc := range []struct {
		name, query string
		opensBodies bool
	}{
		{"scalars and relations", `{ project(first: 50) { path title name ref { notePath typeName } updatedAt issueCount owner { name team { name } } decisionNotes { name } decisions { name } decisionsCount linked(first: 5) { path } backlinked(first: 5) { path } } person(first: 50) { name role projects { name } } }`, false},
		{"single node", `{ node(ref: "notes/projects/roadmap-refresh.md") { ... on Project { name owner { name } } } }`, false},
		{"content", `{ project(first: 50) { name content } }`, true},
		{"capabilities", `{ project(first: 50) { name workspace { capabilities { canEdit } } } }`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &countingNoteReader{inner: &obsidian.Note{}}
			deps := env.deps(nil)
			deps.NoteReader = reader
			prepared, errs := Prepare(env.execSchema, tc.query)
			require.Empty(t, errs)
			result := Execute(t.Context(), deps, env.schema, prepared)
			require.Empty(t, result.Errors)
			if tc.opensBodies {
				require.NotZero(t, reader.bodies.Load())
			} else {
				require.Zero(t, reader.bodies.Load(), "a body-free selection must not open note bodies")
			}
		})
	}
}
