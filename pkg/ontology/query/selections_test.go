package query

import (
	"context"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestExecute_NestedFragmentsKeepNodeKindsAndAliasesSeparate(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type UserStory implements Section @node(locator: EMBEDDED) {
  status: String @field
}
type Spec @node(paths: ["notes/*.md"]) {
  stories: [UserStory!] @contains(level: H2)
}
`, map[string]string{
		"notes/spec.md": "---\ntype: Spec\ntitle: Spec One\n---\n\n## Story one\nstatus:: planned\n^story-one\n",
	})
	writeQueryTestNote(t, env.root, "src/app.go", "package src\n\nfunc Run() {}\n")
	require.NoError(t, env.store.ReplaceIntelCodeFile(context.Background(), "src/app.go", []codeanchor.IntelAnchor{{
		AnchorID:    "go:src/app.go:Run",
		Lang:        codeanchor.LangGo,
		Kind:        string(codeanchor.SymFunc),
		Path:        "src/app.go",
		Symbol:      "Run",
		FQN:         "src.Run",
		Fingerprint: "run-fingerprint",
	}}, nil, nil))

	prepared, errs := PrepareWithVariables(env.execSchema, `
query($path: String!) { ... on Query { ...Roots } }
fragment Roots on Query {
  typed: spec(path: $path) { ...TypedNoteFields }
  noteResult: node(ref: "notes/spec.md") { ...Common }
  sectionResult: node(ref: "notes/spec.md#^story-one") { ...Common }
  codeResult: node(ref: "code:src/app.go") { ...Common }
  symbolResult: node(ref: "src/app.go#symbol:src.Run") { ...Common }
}
fragment Common on Node {
  label: title
  ...NoteFields
  ...SectionFields
  ... on CodeFile { fileOnly: nodeKind }
  ... on CodeSymbol { symbolOnly: symbol }
}
fragment NoteFields on NoteNode {
  ... on Spec { noteOnly: resolvedType }
}
fragment TypedNoteFields on NoteNode { path title }
fragment SectionFields on Section {
  ... on UserStory { sectionOnly: status }
}
`, map[string]any{"path": "notes/spec.md"})
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	require.Equal(t, map[string]any{
		"typed":         []any{map[string]any{"path": "notes/spec.md", "title": "Spec One"}},
		"noteResult":    map[string]any{"label": "Spec One", "noteOnly": "Spec"},
		"sectionResult": map[string]any{"label": "Story one", "sectionOnly": "planned"},
		"codeResult":    map[string]any{"label": "app.go", "fileOnly": "CODE_FILE"},
		"symbolResult":  map[string]any{"label": "Run", "symbolOnly": "Run"},
	}, result.Data)
}
