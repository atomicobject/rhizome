package ontology

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestPatchFrontmatterFieldSourceEscapesStringValuesWithoutChangingMeaning(t *testing.T) {
	field := &Field{Name: "summary", Source: "summary", Kind: FieldKindScalar, TypeName: "String"}
	values := []string{
		"a: b # still part of the value",
		"true",
		"line one\nline two",
	}

	for _, value := range values {
		t.Run(strings.ReplaceAll(value, "\n", "\\n"), func(t *testing.T) {
			content := "---\nsummary: original\nkeep: untouched # comment\n---\nBody\n"
			updated, ok, err := patchFrontmatterFieldSource(content, field, []string{value}, false)
			require.NoError(t, err)
			require.True(t, ok)
			require.Contains(t, updated, "keep: untouched # comment")
			require.Contains(t, updated, "---\nBody\n")

			frontmatter := decodeFrontmatterForTest(t, updated)
			require.Equal(t, value, frontmatter["summary"])
		})
	}

	updated, ok, err := patchFrontmatterFieldSource("---\nkeep: untouched # comment\n---\nBody\n", field, []string{"looks: yaml # but is text"}, false)
	require.NoError(t, err)
	require.True(t, ok)
	require.Contains(t, updated, "keep: untouched # comment")
	require.Equal(t, "looks: yaml # but is text", decodeFrontmatterForTest(t, updated)["summary"])
}

func TestPatchFrontmatterFieldSourceWritesSequencesForNewAndEmptyLists(t *testing.T) {
	field := &Field{Name: "aliases", Source: "aliases", Kind: FieldKindScalar, TypeName: "String", List: true}

	updated, ok, err := patchFrontmatterFieldSource("---\ntitle: Keep\n---\nBody\n", field, []string{"one", "two"}, false)
	require.NoError(t, err)
	require.True(t, ok)
	require.Contains(t, updated, "aliases: [one, two]\n")
	frontmatter := decodeFrontmatterForTest(t, updated)
	require.Equal(t, []interface{}{"one", "two"}, frontmatter["aliases"])

	updated, ok, err = patchFrontmatterFieldSource("---\ntitle: Keep\n---\nBody\n", field, []string{"a,b", "x: y # text"}, false)
	require.NoError(t, err)
	require.True(t, ok)
	frontmatter = decodeFrontmatterForTest(t, updated)
	require.Equal(t, []interface{}{"a,b", "x: y # text"}, frontmatter["aliases"])

	block := "---\naliases:\n  - one\n  - two\nkeep: untouched\n---\nBody\n"
	updated, ok, err = patchFrontmatterFieldSource(block, field, nil, false)
	require.NoError(t, err)
	require.True(t, ok)
	require.Contains(t, updated, "aliases: []\n")
	require.Contains(t, updated, "keep: untouched\n")
	frontmatter = decodeFrontmatterForTest(t, updated)
	require.Equal(t, []interface{}{}, frontmatter["aliases"])
}

func TestPatchFrontmatterFieldSourceRecognizesQuotedKeys(t *testing.T) {
	field := &Field{Name: "summary", Source: "summary", Kind: FieldKindScalar, TypeName: "String"}
	for _, key := range []string{`"summary"`, `'summary'`} {
		t.Run(key, func(t *testing.T) {
			content := "---\n" + key + ": original\nkeep: untouched\n---\nBody\n"
			updated, ok, err := patchFrontmatterFieldSource(content, field, []string{"updated"}, false)
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, 1, strings.Count(updated, key+":"))
			require.Contains(t, updated, key+": updated")
			require.NotContains(t, updated, "summary: updated")
			require.Equal(t, "updated", decodeFrontmatterForTest(t, updated)["summary"])
		})
	}
}

func TestPatchFrontmatterFieldSourcePreservesIndentedTopLevelMapping(t *testing.T) {
	field := &Field{Name: "summary", Source: "summary", Kind: FieldKindScalar, TypeName: "String"}
	content := "---\n  summary: old\n  keep: untouched\n---\nBody\n"

	updated, ok, err := patchFrontmatterFieldSource(content, field, []string{"updated"}, false)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 1, strings.Count(updated, "summary:"))
	require.Contains(t, updated, "  summary: updated\n  keep: untouched\n")
	require.Equal(t, "updated", decodeFrontmatterForTest(t, updated)["summary"])
}

func TestPatchFrontmatterFieldSourcePreservesMultilineMeaningFromFoldedScalar(t *testing.T) {
	field := &Field{Name: "summary", Source: "summary", Kind: FieldKindScalar, TypeName: "String"}
	content := "---\nsummary: >-\n  old value\nkeep: untouched\n---\nBody\n"

	updated, ok, err := patchFrontmatterFieldSource(content, field, []string{"first\nsecond"}, false)
	require.NoError(t, err)
	require.True(t, ok)
	require.Contains(t, updated, "summary: |-\n  first\n  second\nkeep: untouched\n")
	require.Equal(t, "first\nsecond", decodeFrontmatterForTest(t, updated)["summary"])
}

func TestPatchFrontmatterListFailsClosedForBlockScalarSource(t *testing.T) {
	content := "---\ntags: |-\n  old\n---\n"
	field := &Field{Name: "tags", Kind: FieldKindScalar, TypeName: "String", SourceKind: FieldSourceFrontmatter, List: true}

	updated, ok, err := patchFrontmatterFieldSource(content, field, []string{"first", "second"}, false)

	require.ErrorContains(t, err, "block scalar cannot represent list field tags")
	require.False(t, ok)
	require.Equal(t, content, updated)
}

func TestPatchFrontmatterBlockScalarRespectsExplicitIndentIndicator(t *testing.T) {
	content := "---\nsummary: |2-\n  first\n    indented\n---\n"
	field := &Field{Name: "summary", Kind: FieldKindScalar, TypeName: "String", SourceKind: FieldSourceFrontmatter}

	updated, ok, err := patchFrontmatterFieldSource(content, field, []string{"new"}, false)

	require.NoError(t, err)
	require.True(t, ok)
	require.Contains(t, updated, "summary: |2-\n  new\n")
	require.Equal(t, "new", decodeFrontmatterForTest(t, updated)["summary"])
}

func TestPatchFrontmatterBlockScalarPreservesLessIndentedStandaloneComment(t *testing.T) {
	content := "---\nsummary: |2-\n  first\n # keep standalone\nkeep: untouched\n---\n"
	field := &Field{Name: "summary", Kind: FieldKindScalar, TypeName: "String", SourceKind: FieldSourceFrontmatter}

	updated, ok, err := patchFrontmatterFieldSource(content, field, []string{"new"}, false)

	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "---\nsummary: |2-\n  new\n # keep standalone\nkeep: untouched\n---\n", updated)
}

func TestUnsetFrontmatterBlockScalarPreservesLessIndentedStandaloneComment(t *testing.T) {
	content := "---\nsummary: |2-\n  first\n # keep standalone\nkeep: untouched\n---\n"
	field := &Field{Name: "summary", Kind: FieldKindScalar, TypeName: "String", SourceKind: FieldSourceFrontmatter}

	updated, err := unsetFrontmatterField(content, field)

	require.NoError(t, err)
	require.Equal(t, "---\n # keep standalone\nkeep: untouched\n---\n", updated)
}

func TestUnsetFrontmatterFlowListPreservesIndentedStandaloneComment(t *testing.T) {
	content := "---\ntags: [alpha]\n # keep standalone\nactive: false\n---\n"
	field := &Field{Name: "tags", Kind: FieldKindScalar, TypeName: "String", SourceKind: FieldSourceFrontmatter, List: true}

	updated, err := unsetFrontmatterField(content, field)

	require.NoError(t, err)
	require.Equal(t, "---\n # keep standalone\nactive: false\n---\n", updated)
}

func TestUnsetFrontmatterBlockListPreservesLessIndentedStandaloneComment(t *testing.T) {
	content := "---\ntags:\n  - alpha\n # keep standalone\nactive: false\n---\n"
	field := &Field{Name: "tags", Kind: FieldKindScalar, TypeName: "String", SourceKind: FieldSourceFrontmatter, List: true}

	updated, err := unsetFrontmatterField(content, field)

	require.NoError(t, err)
	require.Equal(t, "---\n # keep standalone\nactive: false\n---\n", updated)
}

func TestPatchFrontmatterFieldSourceRejectsFlowMappingInsteadOfAppendingDuplicate(t *testing.T) {
	field := &Field{Name: "summary", Source: "summary", Kind: FieldKindScalar, TypeName: "String"}
	content := "---\n{summary: old, keep: untouched}\n---\nBody\n"

	updated, ok, err := patchFrontmatterFieldSource(content, field, []string{"updated"}, false)
	require.ErrorContains(t, err, "flow-style frontmatter mappings cannot be patched safely")
	require.False(t, ok)
	require.Equal(t, content, updated)
	require.Equal(t, "old", decodeFrontmatterForTest(t, updated)["summary"])

	updated, err = unsetFrontmatterField(content, field)
	require.ErrorContains(t, err, "existing frontmatter field has an unsupported source shape")
	require.Equal(t, content, updated)
}

func TestPatchFrontmatterFieldSourceReplacesEmptyScalarAndPreservesInlineComment(t *testing.T) {
	field := &Field{Name: "summary", Source: "summary", Kind: FieldKindScalar, TypeName: "String"}
	content := "---\nsummary: # keep this comment\nkeep: untouched\n---\nBody\n"

	updated, ok, err := patchFrontmatterFieldSource(content, field, []string{"updated"}, false)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "---\nsummary: updated # keep this comment\nkeep: untouched\n---\nBody\n", updated)
	require.Equal(t, "updated", decodeFrontmatterForTest(t, updated)["summary"])
}

func TestPatchFrontmatterFieldSourceFailsClosedForUnsupportedSpan(t *testing.T) {
	field := &Field{Name: "summary", Source: "summary", Kind: FieldKindScalar, TypeName: "String"}
	content := "---\nsummary:\n  nested: value\nkeep: untouched\n---\nBody\n"

	updated, ok, err := patchFrontmatterFieldSource(content, field, []string{"updated"}, false)
	require.Error(t, err)
	require.False(t, ok)
	require.Equal(t, content, updated)
	_, err = setFrontmatterField(content, field, []string{"updated"}, false)
	require.Error(t, err)
}

func decodeFrontmatterForTest(t *testing.T, content string) map[string]interface{} {
	t.Helper()
	range_ := frontmatterRange(content)
	require.True(t, range_.Valid(len(content)))
	raw := content[range_.Start:range_.End]
	lines := strings.Split(raw, "\n")
	require.GreaterOrEqual(t, len(lines), 2)
	var frontmatter map[string]interface{}
	require.NoError(t, yaml.Unmarshal([]byte(strings.Join(lines[1:len(lines)-1], "\n")), &frontmatter))
	return frontmatter
}
