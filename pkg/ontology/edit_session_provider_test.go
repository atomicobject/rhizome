package ontology

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/html"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderAwareEditSessionPlansTypedHTMLRootMetadataWithoutTouchingBody(t *testing.T) {
	root, schema, runtime := providerEditFixture(t)
	content := "<!doctype html>\r\n<html><head>\r\n<script id=\"rhizome-metadata\" type=\"application/json\">{\"type\":\"Project\",\"unknown\":123456789012345678901234567890,\"active\":false}</script>\r\n</head><body><script>window.state = 1</script></body></html>"
	writeOntologyNote(t, root, "notes/report.html", content)

	session := NewProviderAwareEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, runtime)
	ref := NodeRef{NotePath: "notes/report.html", Kind: NodeKindNote, TypeName: "Project"}
	require.NoError(t, session.SetScalarField(ref, "active", "true"))
	require.NoError(t, session.SetScalarListField(ref, "tags", []string{"alpha", "beta"}))
	require.NoError(t, session.SetLinkField(ref, "owner", []string{"people/alice.md"}))

	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Files, 1)
	updated := plan.Files[0].UpdatedContentPreview
	assert.Contains(t, updated, `"active": true`)
	assert.Contains(t, updated, `"owner": "people/alice.md"`)
	assert.Contains(t, updated, `"tags": [`)
	assert.Equal(t, []any{"alpha", "beta"}, htmlPreviewMetadata(t, updated)["tags"])
	assert.Contains(t, updated, `"unknown": 123456789012345678901234567890`)
	bodyStart := strings.Index(content, "{")
	closeStart := strings.Index(content, "</script>")
	updatedCloseStart := strings.Index(updated, "</script>")
	require.GreaterOrEqual(t, bodyStart, 0)
	require.GreaterOrEqual(t, closeStart, 0)
	require.GreaterOrEqual(t, updatedCloseStart, 0)
	assert.Equal(t, content[:bodyStart], updated[:bodyStart])
	assert.Equal(t, content[closeStart:], updated[updatedCloseStart:])
	assert.Equal(t, content, readOntologyTestNote(t, root, "notes/report.html"), "preview must not write")
}

func TestProviderAwareEditSessionInsertsMissingHTMLMetadataOnlyInPreview(t *testing.T) {
	root, schema, runtime := providerEditFixture(t)
	content := "\ufeff<!doctype html>\r\n<html><body><h1>Report</h1></body></html>"
	writeOntologyNote(t, root, "notes/report.html", content)
	session := NewProviderAwareEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, runtime)
	ref := NodeRef{NotePath: "notes/report.html", Kind: NodeKindNote}
	require.NoError(t, session.SetScalarField(ref, "type", "Project"))
	require.NoError(t, session.SetScalarField(ref, "title", "Prototype report"))
	require.NoError(t, session.SetRootMetadataList(ref, "tags", []string{"prototype", "report"}))
	require.NoError(t, session.SetRootMetadataList(ref, "aliases", []string{"HTML report"}))

	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Files, 1)
	assert.Contains(t, plan.Files[0].UpdatedContentPreview, `<script id="rhizome-metadata" type="application/json">`)
	assert.Contains(t, plan.Files[0].UpdatedContentPreview, `"title": "Prototype report"`)
	assert.Contains(t, plan.Files[0].UpdatedContentPreview, `"tags": [`)
	assert.Contains(t, plan.Files[0].UpdatedContentPreview, `"aliases": [`)
	metadata := htmlPreviewMetadata(t, plan.Files[0].UpdatedContentPreview)
	assert.Equal(t, []any{"prototype", "report"}, metadata["tags"])
	assert.Equal(t, []any{"HTML report"}, metadata["aliases"])
	assert.Contains(t, plan.Files[0].UpdatedContentPreview, "<body><h1>Report</h1></body>")
	assert.Equal(t, content, readOntologyTestNote(t, root, "notes/report.html"))
	_, err = session.Commit(context.Background())
	assert.ErrorContains(t, err, "journaled application adapter")
	assert.Equal(t, content, readOntologyTestNote(t, root, "notes/report.html"))
}

func TestProviderAwareEditSessionFailsClosedForDamagedMetadataAndStructuralHTMLWrites(t *testing.T) {
	root, schema, runtime := providerEditFixture(t)
	ref := NodeRef{NotePath: "notes/report.html", Kind: NodeKindNote, TypeName: "Project"}

	cases := map[string]string{
		"malformed JSON":       `<html><head><script id="rhizome-metadata" type="application/json">{"type":</script></head><body>keep</body></html>`,
		"duplicate blocks":     `<html><head><script id="rhizome-metadata" type="application/json">{"type":"Project"}</script><script id="rhizome-metadata" type="application/json">{"type":"Project"}</script></head><body>keep</body></html>`,
		"ambiguous attributes": `<html><head><script id="rhizome-metadata" id="other" type="application/json">{"type":"Project"}</script></head><body>keep</body></html>`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			writeOntologyNote(t, root, "notes/report.html", content)
			metadata := NewProviderAwareEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, runtime)
			require.NoError(t, metadata.SetScalarField(ref, "active", "true"))
			_, err := metadata.Preview(context.Background())
			require.Error(t, err)
			assert.Equal(t, content, readOntologyTestNote(t, root, "notes/report.html"))
		})
	}

	duplicate := cases["duplicate blocks"]
	writeOntologyNote(t, root, "notes/report.html", duplicate)
	structural := NewProviderAwareEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, runtime)
	require.NoError(t, structural.SetNarrative(ref, 0, 0, "", "changed"))
	_, err := structural.Preview(context.Background())
	assert.ErrorContains(t, err, "root metadata edits only")
	assert.Equal(t, duplicate, readOntologyTestNote(t, root, "notes/report.html"))
}

func TestProviderAwareEditSessionRebasesBodyDriftAndPreservesExternalChangesForBaseNoops(t *testing.T) {
	root, schema, runtime := providerEditFixture(t)
	base := `<html><head><script id="rhizome-metadata" type="application/json">{"type":"Project","active":false}</script></head><body>one</body></html>`
	writeOntologyNote(t, root, "notes/report.html", base)
	ref := NodeRef{NotePath: "notes/report.html", Kind: NodeKindNote, TypeName: "Project"}

	bodySession := NewProviderAwareEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, runtime)
	require.NoError(t, bodySession.SetScalarField(ref, "active", "true"))
	_, err := bodySession.Preview(context.Background())
	require.NoError(t, err)
	bodyChanged := stringReplaceOnce(t, base, "<body>one</body>", "<body>two</body>")
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes/report.html"), []byte(bodyChanged), 0o644))
	plan, conflicts, err := bodySession.PreviewCurrent(context.Background())
	require.NoError(t, err)
	require.Empty(t, conflicts)
	assert.True(t, plan.Files[0].Rebased)
	assert.Contains(t, plan.Files[0].UpdatedContentPreview, "<body>two</body>")

	metadataSession := NewProviderAwareEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, runtime)
	require.NoError(t, metadataSession.SetScalarField(ref, "active", "false"))
	_, err = metadataSession.Preview(context.Background())
	require.NoError(t, err)
	metadataChanged := stringReplaceOnce(t, bodyChanged, `"active":false`, `"active":true`)
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes/report.html"), []byte(metadataChanged), 0o644))
	plan, conflicts, err = metadataSession.PreviewCurrent(context.Background())
	require.NoError(t, err)
	require.Empty(t, conflicts)
	assert.False(t, plan.Files[0].HasMaterialChange)
	assert.Contains(t, plan.Files[0].UpdatedContentPreview, `"active":true`)
}

func TestProviderAwareEditSessionPlansMarkdownAndHTMLBeforeAnyWrite(t *testing.T) {
	root, schema, runtime := providerEditFixture(t)
	markdownContent := "---\ntype: Project\nactive: false\n---\n\nMarkdown body\n"
	htmlContent := `<html><head><script id="rhizome-metadata" type="application/json">{"type":"Project","active":false}</script></head><body>HTML body</body></html>`
	writeOntologyNote(t, root, "notes/project.md", markdownContent)
	writeOntologyNote(t, root, "notes/report.html", htmlContent)

	session := NewProviderAwareEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, runtime)
	require.NoError(t, session.SetScalarField(NodeRef{NotePath: "notes/project.md", Kind: NodeKindNote, TypeName: "Project"}, "active", "true"))
	require.NoError(t, session.SetScalarField(NodeRef{NotePath: "notes/report.html", Kind: NodeKindNote, TypeName: "Project"}, "active", "true"))

	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Files, 2)
	assert.Contains(t, plan.Files[0].UpdatedContentPreview+plan.Files[1].UpdatedContentPreview, "active: true")
	assert.Contains(t, plan.Files[0].UpdatedContentPreview+plan.Files[1].UpdatedContentPreview, `"active": true`)
	assert.Equal(t, markdownContent, readOntologyTestNote(t, root, "notes/project.md"))
	assert.Equal(t, htmlContent, readOntologyTestNote(t, root, "notes/report.html"))
}

func TestProviderAwareEditSessionUsesExplicitWitnessesForRawRootLists(t *testing.T) {
	root, schema, runtime := providerEditFixture(t)
	base := `<html><head><script id="rhizome-metadata" type="application/json">{"type":"Project","tags":["alpha"]}</script></head><body>keep</body></html>`
	writeOntologyNote(t, root, "notes/report.html", base)
	ref := NodeRef{NotePath: "notes/report.html", Kind: NodeKindNote, TypeName: "Project"}
	session := NewProviderAwareEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, runtime)
	require.NoError(t, session.SetRootMetadataListWithWitness(ref, "tags", []string{"beta"}, []string{"alpha"}, "list", false))
	_, err := session.Preview(context.Background())
	require.NoError(t, err)

	external := stringReplaceOnce(t, base, `"tags":["alpha"]`, `"tags":["external"]`)
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes/report.html"), []byte(external), 0o644))
	_, conflicts, err := session.PreviewCurrent(context.Background())
	require.NoError(t, err)
	require.Len(t, conflicts, 1)
	assert.Equal(t, ConflictKindFieldChanged, conflicts[0].Kind)
	assert.Equal(t, []string{"external"}, conflicts[0].CurrentValues)

	keepMine := NewProviderAwareEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, runtime)
	require.NoError(t, keepMine.RestoreBases(session.BaseDocuments()))
	require.NoError(t, keepMine.SetRootMetadataListWithWitness(ref, "tags", []string{"beta"}, []string{"external"}, "list", false))
	plan, conflicts, err := keepMine.PreviewCurrent(context.Background())
	require.NoError(t, err)
	require.Empty(t, conflicts)
	require.Len(t, plan.Files, 1)
	assert.Contains(t, plan.Files[0].UpdatedContentPreview, `"tags": [
    "beta"
  ]`)
	assert.Contains(t, plan.Files[0].UpdatedContentPreview, `<body>keep</body>`)
}

func TestProviderAwareEditSessionTreatsMissingAuthoredTitleAsUnsetWitness(t *testing.T) {
	root, schema, runtime := providerEditFixture(t)
	base := `<html><head></head><body><h1>Derived title</h1></body></html>`
	writeOntologyNote(t, root, "notes/report.html", base)
	ref := NodeRef{NotePath: "notes/report.html", Kind: NodeKindNote}
	session := NewProviderAwareEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, runtime)
	require.NoError(t, session.SetFieldValueWithWitness(ref, "title", []string{"Authored title"}, nil, "unset", false, false, false))

	plan, conflicts, err := session.PreviewCurrent(context.Background())
	require.NoError(t, err)
	require.Empty(t, conflicts)
	require.Len(t, plan.Files, 1)
	assert.Contains(t, plan.Files[0].UpdatedContentPreview, `"title": "Authored title"`)
	assert.Contains(t, plan.Files[0].UpdatedContentPreview, `<body><h1>Derived title</h1></body>`)
}

func TestProviderAwareEditSessionDistinguishesEmptyAndUnsetRootLists(t *testing.T) {
	root, schema, runtime := providerEditFixture(t)
	base := `<html><head><script id="rhizome-metadata" type="application/json">{"type":"Project","tags":["alpha"]}</script></head><body>keep</body></html>`
	writeOntologyNote(t, root, "notes/report.html", base)
	ref := NodeRef{NotePath: "notes/report.html", Kind: NodeKindNote, TypeName: "Project"}

	empty := NewProviderAwareEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, runtime)
	require.NoError(t, empty.SetRootMetadataList(ref, "tags", nil))
	plan, err := empty.Preview(context.Background())
	require.NoError(t, err)
	assert.Contains(t, plan.Files[0].UpdatedContentPreview, `"tags": []`)

	unset := NewProviderAwareEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, runtime)
	require.NoError(t, unset.UnsetRootMetadataList(ref, "tags"))
	plan, err = unset.Preview(context.Background())
	require.NoError(t, err)
	assert.NotContains(t, plan.Files[0].UpdatedContentPreview, `"tags"`)
}

func TestProviderAwareEditSessionDistinguishesEmptyAndUnsetMarkdownRootLists(t *testing.T) {
	root, schema, runtime := providerEditFixture(t)
	base := "---\ntype: Project\ntags: [alpha]\n---\n\nKeep\n"
	writeOntologyNote(t, root, "notes/report.md", base)
	ref := NodeRef{NotePath: "notes/report.md", Kind: NodeKindNote, TypeName: "Project"}

	empty := NewProviderAwareEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, runtime)
	require.NoError(t, empty.SetRootMetadataList(ref, "tags", nil))
	plan, err := empty.Preview(context.Background())
	require.NoError(t, err)
	assert.Contains(t, plan.Files[0].UpdatedContentPreview, "tags: []")

	unset := NewProviderAwareEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, runtime)
	require.NoError(t, unset.UnsetRootMetadataList(ref, "tags"))
	plan, err = unset.Preview(context.Background())
	require.NoError(t, err)
	assert.NotContains(t, plan.Files[0].UpdatedContentPreview, "tags:")
	assert.Contains(t, plan.Files[0].UpdatedContentPreview, "\n\nKeep\n")
}

func providerEditFixture(t *testing.T) (string, *Schema, noteformat.Runtime) {
	t.Helper()
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Person @node(paths: ["people/*.md"]) { name: String @field }
type Project @node(paths: ["notes/*.html", "notes/*.md"]) {
  active: Boolean @field
  tags: [String!] @field
  owner: Person @link
}
`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	registry, err := noteformat.NewRegistry(markdown.New(), html.New())
	require.NoError(t, err)
	runtime, err := noteformat.NewRuntime(registry, markdown.New(), html.New())
	require.NoError(t, err)
	return root, schema, runtime
}

func readOntologyTestNote(t *testing.T, root, path string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	require.NoError(t, err)
	return string(content)
}

func htmlPreviewMetadata(t *testing.T, content string) map[string]any {
	t.Helper()
	start := strings.Index(content, `<script id="rhizome-metadata" type="application/json">`)
	require.GreaterOrEqual(t, start, 0)
	start += len(`<script id="rhizome-metadata" type="application/json">`)
	end := strings.Index(content[start:], "</script>")
	require.GreaterOrEqual(t, end, 0)
	var metadata map[string]any
	decoder := json.NewDecoder(strings.NewReader(content[start : start+end]))
	decoder.UseNumber()
	require.NoError(t, decoder.Decode(&metadata))
	return metadata
}

func stringReplaceOnce(t *testing.T, value, old, replacement string) string {
	t.Helper()
	updated := strings.Replace(value, old, replacement, 1)
	require.NotEqual(t, value, updated)
	return updated
}
