package graph

import (
	"context"
	"errors"
	"github.com/atomicobject/rhizome/pkg/paths"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockNoteReader struct {
	mock.Mock
}

func (m *MockNoteReader) GetContents(def obsidian.VaultDefinition, notePath string) (string, error) {
	args := m.Called(def, notePath)
	return args.String(0), args.Error(1)
}

func (m *MockNoteReader) GetNotesList(def obsidian.VaultDefinition) ([]string, error) {
	args := m.Called(def)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockNoteReader) GetModTime(def obsidian.VaultDefinition, notePath string) (time.Time, error) {
	args := m.Called(def, notePath)
	return args.Get(0).(time.Time), args.Error(1)
}

func (m *MockNoteReader) Title(path string) (string, bool) {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base)), true
}

type snapshotNoteReader struct {
	entries []obsidian.NoteEntry
}

func (n *snapshotNoteReader) GetContents(obsidian.VaultDefinition, string) (string, error) {
	return "", errors.New("unexpected content read")
}

func (n *snapshotNoteReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	out := make([]string, 0, len(n.entries))
	for _, entry := range n.entries {
		out = append(out, entry.Path)
	}
	return out, nil
}

func (n *snapshotNoteReader) GetModTime(obsidian.VaultDefinition, string) (time.Time, error) {
	return time.Time{}, nil
}

func (n *snapshotNoteReader) Title(path string) (string, bool) {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base)), true
}

func (n *snapshotNoteReader) NoteEntriesSnapshot(context.Context) ([]obsidian.NoteEntry, error) {
	out := make([]obsidian.NoteEntry, 0, len(n.entries))
	for _, entry := range n.entries {
		out = append(out, entry)
	}
	return out, nil
}

func TestCollectBacklinksHonorsSkipOptions(t *testing.T) {
	vaultPath := filepath.Join("..", "..", "mocks", "vaults", "backlinks")
	vaultDef := obsidian.VaultDefinition{Name: "backlinks", Path: vaultPath}
	note := &obsidian.Note{}
	targets := []string{"target.md"}

	backlinksSkipNone, err := CollectBacklinks(vaultDef, note, targets, markdownsyntax.WikilinkOptions{SkipAnchors: false, SkipEmbeds: false}, nil)
	assert.NoError(t, err)
	// Expect all variants present
	if assert.Contains(t, backlinksSkipNone, "target.md") {
		assert.Len(t, backlinksSkipNone["target.md"], 7)
	}

	backlinksSkipAnchors, err := CollectBacklinks(vaultDef, note, targets, markdownsyntax.WikilinkOptions{SkipAnchors: true, SkipEmbeds: false}, nil)
	assert.NoError(t, err)
	if assert.Contains(t, backlinksSkipAnchors, "target.md") {
		for _, bl := range backlinksSkipAnchors["target.md"] {
			assert.NotEqual(t, markdownsyntax.BacklinkTypeHeading, bl.LinkType)
			assert.NotEqual(t, markdownsyntax.BacklinkTypeBlock, bl.LinkType)
		}
	}

	backlinksSkipEmbeds, err := CollectBacklinks(vaultDef, note, targets, markdownsyntax.WikilinkOptions{SkipAnchors: false, SkipEmbeds: true}, nil)
	assert.NoError(t, err)
	if assert.Contains(t, backlinksSkipEmbeds, "target.md") {
		for _, bl := range backlinksSkipEmbeds["target.md"] {
			assert.NotEqual(t, markdownsyntax.BacklinkTypeEmbed, bl.LinkType)
		}
	}
}
func TestCollectBacklinks(t *testing.T) {
	vaultPath := filepath.Join("..", "..", "mocks", "vaults", "backlinks")
	vaultDef := obsidian.VaultDefinition{Name: "backlinks", Path: vaultPath}
	note := &obsidian.Note{}

	targets := []string{"target.md", "nolinks.md"}

	backlinks, err := CollectBacklinks(vaultDef, note, targets, markdownsyntax.WikilinkOptions{SkipAnchors: false, SkipEmbeds: false}, nil)
	assert.NoError(t, err)

	expected := map[string]map[string]markdownsyntax.BacklinkType{
		"target.md": {
			"ref-alias.md":             markdownsyntax.BacklinkTypeAlias,
			"ref-basic.md":             markdownsyntax.BacklinkTypeBasic,
			"ref-block.md#^blockid":    markdownsyntax.BacklinkTypeBlock,
			"ref-duplicate.md":         markdownsyntax.BacklinkTypeBasic,
			"ref-duplicate.md#Heading": markdownsyntax.BacklinkTypeHeading,
			"ref-embed.md":             markdownsyntax.BacklinkTypeEmbed,
			"ref-heading.md#Heading":   markdownsyntax.BacklinkTypeHeading,
		},
		"nolinks.md": {},
	}

	for target, expectedRefs := range expected {
		assert.Contains(t, backlinks, target)
		resultRefs := make(map[string]markdownsyntax.BacklinkType)
		for _, backlink := range backlinks[target] {
			key := paths.Normalize(backlink.Referrer).String()
			if fragment := strings.TrimSpace(backlink.Fragment); fragment != "" {
				key += "#" + fragment
			}
			resultRefs[key] = backlink.LinkType
		}

		assert.Equal(t, len(expectedRefs), len(resultRefs))
		for referrer, linkType := range expectedRefs {
			assert.Equal(t, linkType, resultRefs[referrer], "target %s referrer %s", target, referrer)
		}
	}
}

func TestCollectBacklinksWithMarkdownLinks(t *testing.T) {
	mockNote := &MockNoteReader{}
	vaultDef := obsidian.VaultDefinition{Name: "test", Path: "/test/vault", Links: obsidian.LinkTypeBoth}

	mockNote.On("GetNotesList", mock.Anything).Return([]string{"target.md", "ref-wiki.md", "ref-md.md"}, nil)
	mockNote.On("GetContents", mock.Anything, "target.md").Return("Target content", nil)
	mockNote.On("GetContents", mock.Anything, "ref-wiki.md").Return("Links via [[target]]", nil)
	mockNote.On("GetContents", mock.Anything, "ref-md.md").Return("Links via [target](target.md)", nil)

	targets := []string{"target.md"}
	backlinks, err := CollectBacklinks(vaultDef, mockNote, targets, markdownsyntax.WikilinkOptions{}, nil)

	assert.NoError(t, err)
	assert.Contains(t, backlinks, "target.md")
	assert.Len(t, backlinks["target.md"], 2)

	referrers := make(map[string]bool)
	for _, bl := range backlinks["target.md"] {
		referrers[bl.Referrer] = true
	}
	assert.True(t, referrers["ref-wiki.md"], "Should find wikilink referrer")
	assert.True(t, referrers["ref-md.md"], "Should find markdown link referrer")
}

func TestCollectBacklinksMarkdownOnly(t *testing.T) {
	mockNote := &MockNoteReader{}
	// markdown-only vault
	vaultDef := obsidian.VaultDefinition{Name: "test", Path: "/test/vault", Links: obsidian.LinkTypeMarkdown}

	mockNote.On("GetNotesList", mock.Anything).Return([]string{"target.md", "ref-wiki.md", "ref-md.md"}, nil)
	mockNote.On("GetContents", mock.Anything, "target.md").Return("Target content", nil)
	mockNote.On("GetContents", mock.Anything, "ref-wiki.md").Return("Links via [[target]]", nil)
	mockNote.On("GetContents", mock.Anything, "ref-md.md").Return("Links via [target](target.md)", nil)

	targets := []string{"target.md"}
	backlinks, err := CollectBacklinks(vaultDef, mockNote, targets, markdownsyntax.WikilinkOptions{}, nil)

	assert.NoError(t, err)
	assert.Contains(t, backlinks, "target.md")
	// Should only find markdown link, not wikilink
	assert.Len(t, backlinks["target.md"], 1)
	assert.Equal(t, "ref-md.md", backlinks["target.md"][0].Referrer)
}

func TestCollectBacklinksUsesSnapshotEntriesAndSuppressedTags(t *testing.T) {
	vaultDef := obsidian.VaultDefinition{Name: "test", Path: "/test/vault", Links: obsidian.LinkTypeBoth}
	note := &snapshotNoteReader{
		entries: []obsidian.NoteEntry{
			{Path: "target.md", Content: "Target"},
			{Path: "ref-wiki.md", Content: "Links via [[target|alias]]"},
			{Path: "ref-md.md", Content: "Links via [target](target.md#heading)"},
			{Path: "ref-hidden.md", Content: "Links via ![](target.md)", Tags: []string{"no-prompt"}},
		},
	}

	backlinks, err := CollectBacklinks(vaultDef, note, []string{"target.md"}, markdownsyntax.WikilinkOptions{}, []string{"no-prompt"})
	assert.NoError(t, err)
	assert.Contains(t, backlinks, "target.md")
	assert.Len(t, backlinks["target.md"], 2)

	typeMap := make(map[string]markdownsyntax.BacklinkType)
	for _, backlink := range backlinks["target.md"] {
		typeMap[backlink.Referrer] = backlink.LinkType
	}
	assert.Equal(t, markdownsyntax.BacklinkTypeAlias, typeMap["ref-wiki.md"])
	assert.Equal(t, markdownsyntax.BacklinkTypeHeading, typeMap["ref-md.md"])
	_, hidden := typeMap["ref-hidden.md"]
	assert.False(t, hidden)
}

func TestCollectBacklinksKeepsDistinctFragmentsFromSameReferrer(t *testing.T) {
	mockNote := &MockNoteReader{}
	vaultDef := obsidian.VaultDefinition{Name: "test", Path: "/test/vault", Links: obsidian.LinkTypeBoth}

	mockNote.On("GetNotesList", mock.Anything).Return([]string{"target.md", "ref.md"}, nil)
	mockNote.On("GetContents", mock.Anything, "target.md").Return("Target content", nil)
	mockNote.On("GetContents", mock.Anything, "ref.md").Return("Links [one](target.md#alpha) and [two](target.md#beta)", nil)

	backlinks, err := CollectBacklinks(vaultDef, mockNote, []string{"target.md"}, markdownsyntax.WikilinkOptions{}, nil)
	assert.NoError(t, err)
	assert.Contains(t, backlinks, "target.md")
	assert.Len(t, backlinks["target.md"], 2)

	fragments := make([]string, 0, len(backlinks["target.md"]))
	for _, backlink := range backlinks["target.md"] {
		fragments = append(fragments, backlink.Fragment)
	}
	assert.ElementsMatch(t, []string{"alpha", "beta"}, fragments)
}
