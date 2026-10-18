package validate

import (
	"encoding/json"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	htmlformat "github.com/atomicobject/rhizome/pkg/noteformat/html"
	markdownformat "github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestProductionLinkChecksUseRegisteredTargetsAndProviderAuthoredBlockSpans(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		".rhizome/config.yml": "notes:\n  includes: [\"**/*.md\", \"**/*.html\", \"**/*.note\"]\nvalidation:\n  brokenLinks:\n    placeholders: strict\n",
		"source.md":           "---\nexample: '[[target#^orphan]]'\n---\n[registered note](missing.note)\n[attachment](missing.pdf)\n[HTML section](source.html#section)\n",
		"target.md":           "# Target\n\nKept paragraph\n^kept\n\nUnreferenced paragraph\n^orphan\n",
		// These are real HTML-authored links even inside Markdown-looking delimiters.
		"source.html": "---\n<a href=\"target.md#^kept\">keep</a>\n---\n<html><body><h2 id=\"section\">Section</h2><pre>[[phantom]]</pre></body></html>",
	}))
	markdown, html := markdownformat.New(), htmlformat.New()
	registry, err := noteformat.NewRegistry(markdown, html, validationRegisteredNoteProvider{})
	require.NoError(t, err)
	formats, err := noteformat.NewRuntime(registry, markdown, html)
	require.NoError(t, err)
	metadata, err := notemeta.NewIndexer(formats)
	require.NoError(t, err)
	run := RunContext{
		VaultPath:  root,
		VaultDef:   obsidian.VaultDefinition{Root: root, Includes: []string{"**/*.md", "**/*.html", "**/*.note"}, Links: obsidian.LinkTypeBoth},
		NoteReader: &obsidian.Note{}, NoteMetadata: metadata,
	}
	result, _, err := RunSuiteOncePrepared(t.Context(), ScanOptions{Checks: []string{CheckBrokenLinks}, RunContext: &run}, nil, nil)
	require.NoError(t, err)
	require.Zero(t, result.ErrorCount)
	require.Len(t, result.Checks, 1)
	require.Equal(t, 1, result.Checks[0].IssueCount)
	require.Len(t, result.Checks[0].Issues, 1)
	require.Equal(t, "missing.note", result.Checks[0].Issues[0].Target)
	require.Equal(t, "source.md", result.Checks[0].Issues[0].Path)

	orphans := RunOrphanBlockIDs(t.Context(), run)
	require.Empty(t, orphans.Error)
	require.Len(t, orphans.Issues, 1)
	require.Equal(t, "target.md", orphans.Issues[0].Path)
	var data OrphanBlockIDData
	require.NoError(t, json.Unmarshal(orphans.Issues[0].Data, &data))
	require.Equal(t, "orphan", data.BlockID, "HTML references count; Markdown frontmatter examples do not")

	inventory, err := validationLinksFor(t.Context(), run)
	require.NoError(t, err)
	require.Equal(t, []FixEdit{{
		Kind: FixKindRewriteLinkGroup, NotePath: "source.html",
		OldTarget: "target.md#^kept", NewTarget: "target.md#^replacement",
	}}, blockIDReferenceEdits(inventory, "target.md", "kept", "replacement"))
	require.Empty(t, blockIDReferenceEdits(inventory, "target.md", "orphan", "replacement"), "metadata examples cannot become authored rewrite edits")
}

type validationRegisteredNoteProvider struct{}

func (validationRegisteredNoteProvider) Descriptor() noteformat.Descriptor {
	return noteformat.Descriptor{
		ID: "registered-note", Extensions: []string{".note"}, ProviderVersion: "test-v1", ProjectionVersion: "test-v1",
		OwnershipPolicy: noteformat.OwnershipExplicitInclude,
		Capabilities:    noteformat.MustCapabilities(noteformat.CapabilitySourceReading),
	}
}
