package web

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestApplyWorkspaceAssessmentStatus(t *testing.T) {
	resp := NodeWorkspaceResponse{
		Fields: []ontology.NodeFieldState{
			{Name: "summary"},
			{Name: "stories"},
		},
		Collections: []ontology.NodeCollectionState{
			{Name: "stories"},
		},
	}
	assessment := &ontology.NoteAssessment{
		Issues: []ontology.ValidationIssue{{Code: "missing_required_field"}},
		Fields: []ontology.FieldAssessment{
			{
				Name:   "summary",
				Issues: []ontology.ValidationIssue{{Code: "required"}},
			},
			{
				Name:   "stories",
				Issues: []ontology.ValidationIssue{{Code: "min_items"}, {Code: "invalid_target"}},
			},
		},
	}

	applyWorkspaceAssessmentStatus(&resp, assessment)

	require.Equal(t, 4, resp.Status.Validation.IssueCount)
	require.True(t, resp.Status.HasWarnings)
	require.Equal(t, 1, resp.Fields[0].Status.Validation.IssueCount)
	require.True(t, resp.Fields[0].Status.HasWarnings)
	require.Equal(t, 2, resp.Fields[1].Status.Validation.IssueCount)
	require.Equal(t, 2, resp.Collections[0].Status.Validation.IssueCount)
	require.True(t, resp.Collections[0].Status.HasWarnings)
}

func TestNewServerUsesCacheBackedNoteReaderWhenAvailable(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`type Doc @node(paths: ["*.md"]) { title: String @field }`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("---\naliases: [old-alias]\n---\n# Old\n"), 0o644))
	cacheSvc, err := cache.NewService(root, cache.Options{})
	require.NoError(t, err)
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "web-test.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	srv, err := NewServer(t.Context(), Config{
		Vault:        &obsidian.Vault{Name: "test"},
		VaultDef:     obsidian.VaultDefinition{Name: "test", Path: root, Links: obsidian.LinkTypeBoth},
		VaultPath:    root,
		Cache:        cacheSvc,
		Runtime:      &Runtime{IntelStore: store},
		NoteMetadata: testNoteMetadataIndexer(t),
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = srv.Close()
	})

	service, _, err := srv.ontologyContext()
	require.NoError(t, err)
	require.NotNil(t, service)
	content, err := service.NoteReader.GetContents(srv.cfg.VaultDef, "note.md")
	require.NoError(t, err)
	require.Contains(t, content, "# Old")

	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("---\naliases: [new-alias]\n---\n# New\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "added.md"), []byte("# Added\n"), 0o644))
	cacheSvc.MarkStale()
	deps := srv.ontologyQueryDeps(service, nil)
	// The recrawl runs in the background; reads keep serving the last index.
	require.Eventually(t, func() bool {
		content, err = deps.NoteReader.GetContents(srv.cfg.VaultDef, "note.md")
		require.NoError(t, err)
		return strings.Contains(content, "# New")
	}, 5*time.Second, 5*time.Millisecond)
	paths, err := deps.NoteReader.GetNotesList(srv.cfg.VaultDef)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"added.md", "note.md"}, paths)
	provider := deps.NoteReader.(interface {
		NoteEntriesSnapshot(context.Context) ([]obsidian.NoteEntry, error)
	})
	entries, err := provider.NoteEntriesSnapshot(context.Background())
	require.NoError(t, err)
	require.Len(t, entries, 2)
	for _, entry := range entries {
		if entry.Path == "note.md" {
			require.Equal(t, []interface{}{"new-alias"}, entry.Frontmatter["aliases"])
		}
	}

	require.NoError(t, os.Rename(filepath.Join(root, "added.md"), filepath.Join(t.TempDir(), "added.md")))
	cacheSvc.MarkStale()
	require.Eventually(t, func() bool {
		paths, err = service.NoteReader.GetNotesList(srv.cfg.VaultDef)
		require.NoError(t, err)
		return len(paths) == 1
	}, 5*time.Second, 5*time.Millisecond)
	require.Equal(t, []string{"note.md"}, paths)
}

type fixedWebNoteReader struct{ content string }

func (r fixedWebNoteReader) GetContents(obsidian.VaultDefinition, string) (string, error) {
	return r.content, nil
}
func (fixedWebNoteReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) { return nil, nil }
func (fixedWebNoteReader) GetModTime(obsidian.VaultDefinition, string) (time.Time, error) {
	return time.Time{}, nil
}
func (fixedWebNoteReader) Title(string) (string, bool) { return "", false }

func TestOntologyQueryDepsPreservesExplicitReaderAndRawFallback(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("# Disk\n"), 0o644))
	srv := &Server{cfg: Config{VaultDef: obsidian.VaultDefinition{Path: root}}, runtime: &Runtime{}}
	explicit := fixedWebNoteReader{content: "# Explicit\n"}

	deps := srv.ontologyQueryDeps(nil, explicit)
	content, err := deps.NoteReader.GetContents(srv.cfg.VaultDef, "note.md")
	require.NoError(t, err)
	require.Equal(t, "# Explicit\n", content)

	deps = srv.ontologyQueryDeps(nil, nil)
	content, err = deps.NoteReader.GetContents(srv.cfg.VaultDef, "note.md")
	require.NoError(t, err)
	require.Equal(t, "# Disk\n", content)
}

func TestNodeProjectionCacheReusesParsedNoteForEmbeddedNavigation(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type UserStory implements Section @node(locator: EMBEDDED) {
  status: String @field
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["specs/*/spec.md"]) {
  summary: String!
  userStoriesSection: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "specs", "demo"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "specs", "demo", "spec.md"), []byte(`---
type: Spec
summary: Demo
---

# Demo

## User Stories

### Story A
status:: ready
^story-a
`), 0o644))

	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	reader := &projectionCountingNoteReader{root: root}
	cache := newNodeProjectionCache(8)
	ctx := context.Background()
	vaultDef := obsidian.VaultDefinition{Name: "test", Path: root, Links: obsidian.LinkTypeBoth}

	note, err := cache.Projection(ctx, vaultDef, reader, schema, ontology.NodeRef{
		NotePath: "specs/demo/spec.md",
		Kind:     ontology.NodeKindNote,
	})
	require.NoError(t, err)
	require.Equal(t, "Spec", note.ResolvedType)

	story, err := cache.Projection(ctx, vaultDef, reader, schema, ontology.NodeRef{
		NotePath: "specs/demo/spec.md",
		Fragment: "^story-a",
		Kind:     ontology.NodeKindEmbedded,
	})
	require.NoError(t, err)
	require.Equal(t, "UserStory", story.ResolvedType)
	require.Equal(t, 1, reader.contentsCalls())
}

func TestFastNodeWorkspaceLocatorBuildsEmbeddedBlockLink(t *testing.T) {
	srv := &Server{
		cfg: Config{
			VaultDef:     obsidian.VaultDefinition{Name: "test", Path: t.TempDir(), Links: obsidian.LinkTypeBoth},
			NoteMetadata: testNoteMetadataIndexer(t),
		},
	}
	srv.noteCache = obsidian.BuildNotePathCache([]string{"specs/demo/spec.md"})

	locator, ok := srv.fastNodeWorkspaceLocator(t.Context(), ontology.NodeRef{
		NotePath: "specs/demo/spec.md",
		Fragment: "^story-a",
		NodeID:   "specs/demo/spec.md#^story-a",
		Kind:     ontology.NodeKindEmbedded,
	}, "Story A")

	require.True(t, ok)
	require.Equal(t, ontology.NodeLocatorLinkable, locator.Status)
	require.Equal(t, "specs/demo/spec.md#^story-a", locator.SourceLocator)
	require.NotNil(t, locator.LinkTarget)
	require.Equal(t, "specs/demo/spec.md#^story-a", locator.LinkTarget.Markdown)
	require.Equal(t, "[[spec#^story-a]]", locator.LinkTarget.Wikilink)
	require.Equal(t, "story-a", locator.LinkTarget.BlockID)
}

func TestFastNodeWorkspaceLocatorHandlesUnsupportedSections(t *testing.T) {
	srv := &Server{}

	locator, ok := srv.fastNodeWorkspaceLocator(t.Context(), ontology.NodeRef{
		NotePath: "specs/demo/spec.md",
		Fragment: "Stories",
		NodeID:   "specs/demo/spec.md#Stories",
		Kind:     ontology.NodeKindSection,
	}, "Stories")

	require.True(t, ok)
	require.Equal(t, ontology.NodeLocatorUnsupported, locator.Status)
	require.Equal(t, ontology.NodeKindSection, locator.Kind)
	require.Equal(t, "specs/demo/spec.md#Stories", locator.SourceLocator)
	require.Nil(t, locator.LinkTarget)
	require.NotEmpty(t, locator.Diagnostics)
	require.Equal(t, "unsupported_node_kind", locator.Diagnostics[0].Code)
}

type projectionCountingNoteReader struct {
	root     string
	mu       sync.Mutex
	contents int
}

func (r *projectionCountingNoteReader) GetContents(_ obsidian.VaultDefinition, noteName string) (string, error) {
	r.mu.Lock()
	r.contents++
	r.mu.Unlock()
	body, err := os.ReadFile(filepath.Join(r.root, filepath.FromSlash(noteName)))
	return string(body), err
}

func (r *projectionCountingNoteReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	return nil, nil
}

func (r *projectionCountingNoteReader) GetModTime(_ obsidian.VaultDefinition, noteName string) (time.Time, error) {
	info, err := os.Stat(filepath.Join(r.root, filepath.FromSlash(noteName)))
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

func (r *projectionCountingNoteReader) Title(path string) (string, bool) {
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), true
}

func (r *projectionCountingNoteReader) contentsCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.contents
}

func TestParentWorkspaceGroupLinksToEmbeddedNodeParent(t *testing.T) {
	parentRef := ontology.NodeRef{
		NotePath: "specs/demo/spec.md",
		Fragment: "^stories",
		NodeID:   "specs/demo/spec.md#^stories",
		Kind:     ontology.NodeKindEmbedded,
	}
	rendered := RenderedFileResponse{
		Sections: []RenderedSection{{
			ID:      "specs/demo/spec.md#^stories",
			Title:   "User stories",
			Locator: "EMBEDDED",
		}},
	}

	group, ok := parentWorkspaceGroup(&parentRef, rendered)

	require.True(t, ok)
	require.Equal(t, "parent", group.Key)
	require.Equal(t, "Parent", group.Label)
	require.Len(t, group.Items, 1)
	require.Equal(t, "specs/demo/spec.md#^stories", group.Items[0].Path)
	require.Equal(t, "User stories", group.Items[0].Title)
	require.Equal(t, "note", group.Items[0].Kind)
	require.Equal(t, "^stories", group.Items[0].Anchor)
}

func TestOntologyNodeRefFromOpInfersEmbeddedKind(t *testing.T) {
	tests := []struct {
		name string
		op   OntologyEditOp
		want ontology.NodeRef
	}{
		{
			name: "path plus block node id",
			op:   OntologyEditOp{Kind: "setField", Path: "specs/demo/spec.md", NodeID: "specs/demo/spec.md#^story-a", Field: "status", Value: "done"},
			want: ontology.NodeRef{NotePath: "specs/demo/spec.md", Fragment: "^story-a", Kind: ontology.NodeKindEmbedded, NodeID: "specs/demo/spec.md#^story-a"},
		},
		{
			name: "item fragment path with arbitrary node id",
			op:   OntologyEditOp{Kind: "setField", Path: "meetings/planning.md#item-42", NodeID: "item-node", Field: "status", Value: "done"},
			want: ontology.NodeRef{NotePath: "meetings/planning.md", Fragment: "item-42", Kind: ontology.NodeKindEmbedded, NodeID: "item-node"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ref, err := ontologyNodeRefFromOp(test.op)
			require.NoError(t, err)
			require.Equal(t, test.want.NotePath, ref.NotePath)
			require.Equal(t, test.want.Fragment, ref.Fragment)
			require.Equal(t, test.want.Kind, ref.Kind)
			require.Equal(t, test.want.NodeID, ref.NodeID)
		})
	}
}

func TestNodeWorkspace_PrefersNoteHeadingForIdentityTitle(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type Spec
  @node(paths: ["specs/*.md"], label: "Spec", keyField: "summary") {
  summary: String!
}
`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "specs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "specs", "demo.md"), []byte(`---
type: Spec
summary: Summary should not become the pane title
---

# Real note title

Body.
`), 0o644))

	fixture := fixtureVault{
		root:     root,
		vault:    &obsidian.Vault{Name: "test"},
		vaultDef: obsidian.VaultDefinition{Name: "test", Path: root, Links: obsidian.LinkTypeBoth},
	}
	srv := newFixtureServer(t, fixture, nil)

	workspace, err := srv.nodeWorkspace(t.Context(), ontology.NodeRef{
		NotePath: "specs/demo.md",
		Kind:     ontology.NodeKindNote,
	}, defaultNodeWorkspaceIncludes())
	require.NoError(t, err)
	require.Equal(t, "Real note title", workspace.Node.Title)
	require.Equal(t, "Real note title", workspace.Content.Title)
}

func TestNodeWorkspace_MultipleH1sUseFilenameForIdentityTitle(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type Spec
  @node(paths: ["specs/*.md"], label: "Spec", keyField: "summary") {
  summary: String!
}
`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "specs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "specs", "filename-title.md"), []byte(`---
type: Spec
summary: Summary should not become the pane title
---

# Decision

First decision.

# Decision

Second decision.
`), 0o644))

	fixture := fixtureVault{
		root:     root,
		vault:    &obsidian.Vault{Name: "test"},
		vaultDef: obsidian.VaultDefinition{Name: "test", Path: root, Links: obsidian.LinkTypeBoth},
	}
	srv := newFixtureServer(t, fixture, nil)

	workspace, err := srv.nodeWorkspace(t.Context(), ontology.NodeRef{
		NotePath: "specs/filename-title.md",
		Kind:     ontology.NodeKindNote,
	}, defaultNodeWorkspaceIncludes())
	require.NoError(t, err)
	require.Equal(t, "filename-title", workspace.Node.Title)
	require.Equal(t, "filename-title", workspace.Content.Title)
}

func TestNodeWorkspace_UndeclaredSectionsCarryEditableBodyAndNesting(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type Spec @node(paths: ["specs/*.md"]) {
  summary: String
}
`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "plain.md"), []byte(`# Plain

Intro.

## Alpha

Alpha body.

### Alpha One

Alpha one body.

## Beta

Beta body.
`), 0o644))

	fixture := fixtureVault{
		root:     root,
		vault:    &obsidian.Vault{Name: "test"},
		vaultDef: obsidian.VaultDefinition{Name: "test", Path: root, Links: obsidian.LinkTypeBoth},
	}
	srv := newFixtureServer(t, fixture, nil)

	workspace, err := srv.nodeWorkspace(t.Context(), ontology.NodeRef{
		NotePath: "notes/plain.md",
		Kind:     ontology.NodeKindNote,
	}, defaultNodeWorkspaceIncludes())
	require.NoError(t, err)
	byTitle := map[string]WorkspaceNodeResponse{}
	for _, node := range workspace.Nodes {
		if node.Kind == WorkspaceNodeKindSection && node.Data != nil {
			byTitle[node.Data.Title] = node
		}
	}
	alpha, one := byTitle["Alpha"], byTitle["Alpha One"]
	require.Equal(t, alpha.ID, one.ParentID)
	require.Contains(t, alpha.ChildIDs, one.ID)

	// Session responses carry this REST workspace, so it must match GraphQL:
	// undeclared headings edit in place, nested ones included.
	require.Len(t, alpha.Body, 2)
	require.Equal(t, ontology.NodeBodyBlockKindNarrative, alpha.Body[0].Kind)
	require.Equal(t, one.Ref.NodeID, alpha.Body[1].ChildRef.NodeID)
	require.Len(t, one.Body, 1)
	require.Equal(t, "\nAlpha one body.", one.Body[0].Markdown)
	beta := byTitle["Beta"]
	require.Len(t, beta.Body, 1)
	require.Contains(t, beta.Body[0].Markdown, "Beta body.")

	var focused WorkspaceNodeResponse
	for _, node := range workspace.Nodes {
		if node.ID == workspace.FocusedNodeID {
			focused = node
		}
	}
	require.NotEmpty(t, workspace.FocusedNodeID)
	require.NotEmpty(t, focused.Body, "the focused note carries its own body alongside undeclared sections")
}
