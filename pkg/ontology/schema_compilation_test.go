package ontology

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadSchema_IdentifierValidationOrder(t *testing.T) {
	cases := []struct {
		name, declaration, field, message string
	}{
		{"note enum before source", `@node(paths: ["notes/*.md"])`, `key: Status @field(sourceKind: INLINE)`, "requires a scalar String field"},
		{"note type before source", `@node(paths: ["notes/*.md"])`, `key: Int @field(sourceKind: INLINE)`, "requires a String or ID field (got Int)"},
		{"note list before source", `@node(paths: ["notes/*.md"])`, `key: [String!] @field(sourceKind: INLINE)`, "cannot be a list field"},
		{"note inline", `@node(paths: ["notes/*.md"])`, `key: String @field(sourceKind: INLINE)`, "requires sourceKind: FRONTMATTER"},
		{"section item title", `implements Section`, `key: String @field(sourceKind: ITEM_TITLE)`, "requires a frontmatter or inline field"},
		{"embedded item title", `implements Section @node(locator: EMBEDDED)`, `key: String @field(sourceKind: ITEM_TITLE)`, "requires a frontmatter or inline field"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeOntologySchema(t, root, fmt.Sprintf("enum Status { ready }\ntype Entry %s { %s @identifier }", tc.declaration, tc.field))
			_, err := LoadSchema(root)
			require.ErrorContains(t, err, "@identifier on Entry.key "+tc.message)
		})
	}
}

func TestLoadSchema_PatternConstraintDiagnostics(t *testing.T) {
	cases := []struct{ name, args, message string }{
		{"empty", `pattern: ""`, "pattern or notPattern is required"},
		{"invalid pattern", `pattern: "["`, "pattern is not a valid regexp:"},
		{"invalid exclusion", `notPattern: "["`, "notPattern is not a valid regexp:"},
		{"pattern before exclusion", `pattern: "[", notPattern: "("`, "pattern is not a valid regexp:"},
	}
	for _, tc := range cases {
		for _, directive := range []string{"title", "format"} {
			t.Run(directive+"/"+tc.name, func(t *testing.T) {
				root := t.TempDir()
				typeDirective, fieldDirective, owner := "", "", "Entry"
				if directive == "title" {
					typeDirective = "@title(" + tc.args + ")"
				} else {
					fieldDirective = "@format(" + tc.args + ")"
					owner += ".summary"
				}
				writeOntologySchema(t, root, fmt.Sprintf(`type Entry @node(paths: ["notes/*.md"]) %s { summary: String %s }`, typeDirective, fieldDirective))
				_, err := LoadSchema(root)
				require.ErrorContains(t, err, "invalid @"+directive+" directive on "+owner+": "+tc.message)
			})
		}
	}
}

func TestLoadSchema_SectionPreludePreservesQuotedBraces(t *testing.T) {
	for _, template := range []string{`"{{title}} \"quoted\" {body}"`, `"""{{title}}
{body} [detail]"""`} {
		t.Run(template, func(t *testing.T) {
			root := t.TempDir()
			writeOntologySchema(t, root, fmt.Sprintf(`
type Entry implements Section
  @node(locator: EMBEDDED)
  @preview(template: %s)
  @guidance(meaning: "{{title}}", authoring: "spec { id title summary }")
  @requiresWhen(field: "status", equals: "ready", require: [{field: "summary"}]) {
  status: String @field
  summary: String @field @guidance(meaning: "Authored {braces} stay intact")
}
`, template))
			schema, err := LoadSchema(root)
			require.NoError(t, err)
			entry := schema.Types["Entry"]
			require.Equal(t, []string{"title"}, entry.Preview.Placeholders)
			require.Contains(t, entry.Preview.Template, "{body}")
			require.Equal(t, "{{title}}", entry.Guidance.Meaning)
			require.Equal(t, "spec { id title summary }", entry.Guidance.Authoring)
			require.NotNil(t, entry.ByName["status"])
			require.Contains(t, entry.Implements, "Section")
			require.Equal(t, "Authored {braces} stay intact", entry.ByName["summary"].Guidance.Meaning)
			require.Equal(t, []RequiredFieldCondition{{Field: "summary"}}, entry.RequiresWhen[0].Require)
		})
	}
}
