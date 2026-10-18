package schemadoc

import (
	"github.com/stretchr/testify/require"

	"testing"
)

func TestRenderMarkdown_InjectsNextIDWhenLookupProvided(t *testing.T) {
	schema := testCustomSchema(t, `
type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  id: String! @field(source: "id") @identifier(preferred: true, prefix: "SPEC")
  aliases: [String!] @field
}
`)
	calls := 0
	lookup := IDLookup(func(typeName string) (string, bool, error) {
		calls++
		require.Equal(t, "Spec", typeName)
		return "SPEC-0042", true, nil
	})

	text, err := RenderGuideMarkdown(schema, GuideOptions{Types: []string{"Spec"}, LookupID: lookup})
	require.NoError(t, err)
	require.Equal(t, 1, calls, "lookup should fire once per primary type")
	require.Contains(t, text, "### ID Allocation")
	require.Contains(t, text, "allocation strategy: `SEQUENTIAL`")
	require.Contains(t, text, "next available id: `SPEC-0042`")
	require.Contains(t, text, "rzm agent next-id --type Spec --count <N>")
	require.Contains(t, text, "rzm agent validate all")
	require.Contains(t, text, "includes identifiers and ontology")
	require.Contains(t, text, "[[SPEC-0042]]")
	require.Contains(t, text, "id: SPEC-0042\n")
	require.Contains(t, text, "  - SPEC-0042\n")
}

func TestRenderMarkdown_OmitsIDAllocationWhenTypeHasNoFormat(t *testing.T) {
	schema := testCustomSchema(t, `
type LegacySpec @node(paths: ["legacy/*.md"]) {
  summary: String!
  id: String! @field(source: "id") @identifier(preferred: true)
}
`)
	lookup := IDLookup(func(typeName string) (string, bool, error) {
		t.Fatalf("lookup must not fire for types without IdentifierFormat (called for %q)", typeName)
		return "", false, nil
	})
	text, err := RenderGuideMarkdown(schema, GuideOptions{Types: []string{"LegacySpec"}, LookupID: lookup})
	require.NoError(t, err)
	require.NotContains(t, text, "### ID Allocation")
	require.Contains(t, text, "id:\n")
}

func TestRenderMarkdown_NextIDLookupErrorIsNonFatal(t *testing.T) {
	schema := testCustomSchema(t, `
type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  id: String! @field(source: "id") @identifier(preferred: true, prefix: "SPEC")
}
`)
	lookup := IDLookup(func(string) (string, bool, error) {
		return "", false, errExample
	})
	text, err := RenderGuideMarkdown(schema, GuideOptions{Types: []string{"Spec"}, LookupID: lookup})
	require.NoError(t, err)
	require.Contains(t, text, "### ID Allocation")
	require.Contains(t, text, "rzm agent next-id --type Spec")
	require.Contains(t, text, "id:\n")
	require.NotContains(t, text, "next available id:")
}

func TestRenderMarkdown_DateTimeIDAllocationRequiresProspectivePath(t *testing.T) {
	schema := testCustomSchema(t, `
type EffortNote @node(paths: ["efforts/*.md"]) {
  id: String! @field(source: "id") @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
  aliases: [String!] @field
}
`)

	text, err := RenderGuideMarkdown(schema, GuideOptions{Types: []string{"EffortNote"}})
	require.NoError(t, err)
	require.Contains(t, text, "allocation strategy: `DATETIME`")
	require.Contains(t, text, "rzm agent next-id --type EffortNote --path <prospective-vault-relative-path>")
	require.NotContains(t, text, "rzm agent next-id --type EffortNote --count <N>")
	require.NotContains(t, text, `@identifier {"pad":4`)
}
