package schema

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadSchema_IdentifierValidationOrder(t *testing.T) {
	cases := []struct {
		name, declaration, field, code string
	}{
		{"note enum before source", `@node(paths: ["notes/*.md"])`, `key: Status @field(sourceKind: INLINE)`, "identifier_field_kind"},
		{"note type before source", `@node(paths: ["notes/*.md"])`, `key: Int @field(sourceKind: INLINE)`, "identifier_field_type"},
		{"note list before source", `@node(paths: ["notes/*.md"])`, `key: [String!] @field(sourceKind: INLINE)`, "identifier_list"},
		{"note inline", `@node(paths: ["notes/*.md"])`, `key: String @field(sourceKind: INLINE)`, "identifier_frontmatter_source"},
		{"section item title", `implements Section`, `key: String @field(sourceKind: ITEM_TITLE)`, "identifier_source_kind"},
		{"embedded item title", `implements Section @node(locator: EMBEDDED)`, `key: String @field(sourceKind: ITEM_TITLE)`, "identifier_source_kind"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeOntologySchema(t, root, fmt.Sprintf("enum Status { ready }\ntype Entry %s { %s @identifier }", tc.declaration, tc.field))
			_, err := LoadSchema(root)
			rule := requireCompileError(t, err, tc.code)
			require.Equal(t, "Entry", rule.TypeName)
			require.Equal(t, "key", rule.FieldName)
		})
	}
}

func TestLoadSchema_PatternConstraintDiagnostics(t *testing.T) {
	cases := []struct{ name, args, code string }{
		{"empty", `pattern: ""`, "pattern_missing"},
		{"invalid pattern", `pattern: "["`, "invalid_pattern"},
		{"invalid exclusion", `notPattern: "["`, "invalid_not_pattern"},
		{"pattern before exclusion", `pattern: "[", notPattern: "("`, "invalid_pattern"},
	}
	for _, tc := range cases {
		for _, directive := range []string{"title", "format"} {
			t.Run(directive+"/"+tc.name, func(t *testing.T) {
				root := t.TempDir()
				typeDirective, fieldDirective, fieldName := "", "", ""
				if directive == "title" {
					typeDirective = "@title(" + tc.args + ")"
				} else {
					fieldDirective = "@format(" + tc.args + ")"
					fieldName = "summary"
				}
				writeOntologySchema(t, root, fmt.Sprintf(`type Entry @node(paths: ["notes/*.md"]) %s { summary: String %s }`, typeDirective, fieldDirective))
				_, err := LoadSchema(root)
				rule := requireCompileError(t, err, tc.code)
				require.Equal(t, "Entry", rule.TypeName)
				require.Equal(t, fieldName, rule.FieldName)
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
