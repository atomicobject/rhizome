package presentation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestDefaultPacker_ExpandsOntologyNodeBodyWithParentAndContextInclude(t *testing.T) {
	root, schema, story := setupOntologyPresentationFixture(t)
	refJSON, err := json.Marshal(story.Ref)
	require.NoError(t, err)

	packer := &DefaultPacker{
		VaultPath:      root,
		VaultDef:       obsidian.VaultDefinition{Path: root},
		NoteReader:     &obsidian.Note{},
		OntologySchema: schema,
		Intel:          newPresentationOntologyIntel("notes/specs/checkout.md"),
	}
	packed, err := packer.Pack(context.Background(), search.QuerySpec{
		Text:   "faster checkout story",
		Budget: search.Budget{Chars: 5000},
	}, []search.RankedResult{{
		Candidate: search.Candidate{
			Handle:      knowledge.NodeChunkHandle("node:story", "notes/specs/checkout.md", "node_body", 0),
			Owner:       knowledge.NoteHandle("notes/specs/checkout.md"),
			Type:        "note",
			Path:        "notes/specs/checkout.md",
			Title:       "Faster checkout",
			Granularity: "node_body",
			NodeRefJSON: string(refJSON),
		},
		FinalScore: 0.9,
	}})

	require.NoError(t, err)
	require.Contains(t, packed.Text, "Ontology node context:")
	require.Contains(t, packed.Text, "- title: Faster checkout")
	require.Contains(t, packed.Text, "Matched body:")
	require.Contains(t, packed.Text, "Users need quicker checkout.")
	require.Contains(t, packed.Text, "parent: UserStoriesSection")
	require.Contains(t, packed.Text, "Schema context:")
	require.Contains(t, packed.Text, "context (docs/context.md)")
	require.Contains(t, packed.Text, "- fields: related=[[docs/context]]")
	require.NotContains(t, packed.Text, "Child nodes:")
	require.NotContains(t, packed.Text, "Sibling body should not appear")
}

func TestDefaultPacker_NodeRefResultKeepsChunkFallbackWithoutOntologyScope(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "story.md"), []byte(`# Story

Matched fallback evidence.
`), 0o644))

	refJSON, err := json.Marshal(ontology.NodeRef{
		NotePath: "notes/story.md",
		NodeID:   "story",
		TypeName: "Story",
		Kind:     ontology.NodeKindEmbedded,
	})
	require.NoError(t, err)

	packer := &DefaultPacker{VaultPath: root}
	packed, err := packer.Pack(context.Background(), search.QuerySpec{
		Text:   "fallback",
		Budget: search.Budget{Chars: 2000},
	}, []search.RankedResult{{
		Candidate: search.Candidate{
			Type:        "note",
			Path:        "notes/story.md",
			NoteID:      "notes/story.md",
			Title:       "Story",
			ChunkIndex:  0,
			NodeRefJSON: string(refJSON),
		},
		FinalScore: 0.8,
	}})
	require.NoError(t, err)
	require.Contains(t, packed.Text, "Matched fallback evidence.")
}

func TestDefaultPacker_RendersFallbackOntologyNodeContext(t *testing.T) {
	root := t.TempDir()
	schemaDir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(schemaDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(schemaDir, "schema.graphql"), []byte(`
type ReferenceDoc @node(paths: ["docs/*.md"]) {
  title: String @field
}
`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "untyped.md"), []byte(`# Untyped Research

Root prose should render.

## Findings

Fallback ontology evidence should survive presentation packing.
`), 0o644))
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	refJSON, err := json.Marshal(ontology.NodeRef{
		NotePath: "notes/untyped.md",
		TypeName: ontology.FallbackNoteTypeName,
		Kind:     ontology.NodeKindNote,
	})
	require.NoError(t, err)

	packer := &DefaultPacker{
		VaultPath:      root,
		VaultDef:       obsidian.VaultDefinition{Path: root},
		NoteReader:     &obsidian.Note{},
		OntologySchema: schema,
		Intel:          newPresentationOntologyIntel("notes/untyped.md"),
	}
	packed, err := packer.Pack(context.Background(), search.QuerySpec{
		Text:   "fallback ontology evidence",
		Budget: search.Budget{Chars: 4000},
	}, []search.RankedResult{{
		Candidate: search.Candidate{
			Handle:      knowledge.NodeChunkHandle("node:fallback", "notes/untyped.md", "node_body", 1),
			Owner:       knowledge.NoteHandle("notes/untyped.md"),
			Type:        "note",
			Path:        "notes/untyped.md",
			Title:       "Untyped Research",
			Granularity: "node_body",
			ChunkIndex:  1,
			NodeRefJSON: string(refJSON),
		},
		FinalScore: 0.9,
	}})

	require.NoError(t, err)
	require.Contains(t, packed.Text, "Ontology node context:")
	require.Contains(t, packed.Text, "- type: untyped note")
	require.Contains(t, packed.Text, "Matched body:")
	require.Contains(t, packed.Text, "Root prose should render.")
	require.NotContains(t, packed.Text, "Fallback ontology evidence should survive presentation packing.")
	require.NotContains(t, packed.Text, "_FallbackNote")
}

func TestDefaultPacker_ExpandsOntologyNeighborContextIncludeFromEdges(t *testing.T) {
	root, schema, story := setupOntologyPresentationFixture(t)
	refJSON, err := json.Marshal(story.Ref)
	require.NoError(t, err)

	intel := newPresentationOntologyIntel("notes/specs/checkout.md")
	intel.edges = []semdb.OntologyEdgeRow{{
		SrcPath:      "notes/specs/checkout.md",
		SrcNodeID:    story.Ref.NodeID,
		RelationName: "references",
		DstPath:      "docs/context.md",
		DstType:      "ReferenceDoc",
		Provenance:   "body_link",
	}}
	packer := &DefaultPacker{
		VaultPath:      root,
		VaultDef:       obsidian.VaultDefinition{Path: root},
		NoteReader:     &obsidian.Note{},
		OntologySchema: schema,
		Intel:          intel,
	}
	packed, err := packer.Pack(context.Background(), search.QuerySpec{
		Text:   "ambient reference",
		Budget: search.Budget{Chars: 5000},
	}, []search.RankedResult{{
		Candidate: search.Candidate{
			Handle:      knowledge.NodeChunkHandle("node:story", "notes/specs/checkout.md", "node_body", 0),
			Owner:       knowledge.NoteHandle("notes/specs/checkout.md"),
			Type:        "note",
			Path:        "notes/specs/checkout.md",
			Title:       "Faster checkout",
			Granularity: "node_body",
			NodeRefJSON: string(refJSON),
		},
		FinalScore: 0.9,
	}})

	require.NoError(t, err)
	require.Contains(t, packed.Text, "references: context (docs/context.md)")
	require.Equal(t, 1, intel.edgeBatchCalls)
}

func TestDefaultPackerPreservesAuthoredContextTargetPaths(t *testing.T) {
	for _, tc := range []struct{ authored, edge, want string }{
		{"[[docs/Context]]", "docs/Context.md", "Context (docs/Context.md)"},
		{"[[docs/Context.MD]]", "docs/Context.MD", "Context (docs/Context.MD)"},
		{"[[docs/decision.HTML]]", "docs/decision.HTML", "decision (docs/decision.HTML)"},
		{"docs/decision.HTML", "docs/decision.HTML", "decision (docs/decision.HTML)"},
		{"docs/decision", "docs/decision", "decision (docs/decision)"},
	} {
		t.Run(tc.authored, func(t *testing.T) {
			root, schema, _ := setupOntologyPresentationFixture(t)
			path := filepath.Join(root, "notes/specs/checkout.md")
			body, err := os.ReadFile(path)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, []byte(strings.Replace(string(body), "[[docs/context]]", tc.authored, 1)), 0o644))
			vault := obsidian.VaultDefinition{Path: root}
			projection, err := ontology.ProjectNote(context.Background(), vault, &obsidian.Note{}, schema, "notes/specs/checkout.md")
			require.NoError(t, err)
			container, err := ontology.ProjectNode(context.Background(), vault, &obsidian.Note{}, schema, projection.Fields["userStories"].SectionNodes[0])
			require.NoError(t, err)
			story, err := ontology.ProjectNode(context.Background(), vault, &obsidian.Note{}, schema, container.Fields["stories"].SectionNodes[0])
			require.NoError(t, err)
			refJSON, err := json.Marshal(story.Ref)
			require.NoError(t, err)
			intel := newPresentationOntologyIntel("notes/specs/checkout.md")
			intel.edges = []semdb.OntologyEdgeRow{{SrcPath: "notes/specs/checkout.md", SrcNodeID: story.Ref.NodeID, RelationName: "references", DstPath: tc.edge, DstType: "ReferenceDoc", Provenance: "body_link"}}
			packer := &DefaultPacker{VaultPath: root, VaultDef: vault, NoteReader: &obsidian.Note{}, OntologySchema: schema, Intel: intel}
			packed, err := packer.Pack(context.Background(), search.QuerySpec{Text: "context", Budget: search.Budget{Chars: 5000}}, []search.RankedResult{{Candidate: search.Candidate{Handle: knowledge.NodeChunkHandle("node:story", "notes/specs/checkout.md", "node_body", 0), Owner: knowledge.NoteHandle("notes/specs/checkout.md"), Type: "note", Path: "notes/specs/checkout.md", Granularity: "node_body", NodeRefJSON: string(refJSON)}, FinalScore: 0.9}})
			require.NoError(t, err)
			require.Contains(t, packed.Text, "related: "+tc.want)
			require.Contains(t, packed.Text, "references: "+tc.want)
		})
	}
}

func TestDefaultPacker_ContextIncludeNeighborAllowsInterfaceTarget(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	schemaDir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(schemaDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(schemaDir, "schema.graphql"), []byte(`
interface SupportingContext {
  title: String @field
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  references: [SupportingContext!] @neighbors(direction: OUTBOUND, type: "SupportingContext", contextInclude: true)
}

type ReferenceDoc implements SupportingContext @node(paths: ["docs/*.md"]) {
  title: String @field
}
`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes", "specs"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "context.md"), []byte(`---
title: Context Note
---
# Context Note
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "specs", "checkout.md"), []byte(`# Checkout Refresh

## User Stories

### Faster checkout
Users need quicker checkout.
`), 0o644))
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	rootProjection, err := ontology.ProjectNote(ctx, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes/specs/checkout.md")
	require.NoError(t, err)
	containerRef := rootProjection.Fields["userStories"].SectionNodes[0]
	container, err := ontology.ProjectNode(ctx, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, containerRef)
	require.NoError(t, err)
	storyRef := container.Fields["stories"].SectionNodes[0]
	refJSON, err := json.Marshal(storyRef)
	require.NoError(t, err)

	intel := newPresentationOntologyIntel("notes/specs/checkout.md")
	intel.edges = []semdb.OntologyEdgeRow{{
		SrcPath:      "notes/specs/checkout.md",
		SrcNodeID:    storyRef.NodeID,
		RelationName: "references",
		DstPath:      "docs/context.md",
		DstType:      "ReferenceDoc",
		Provenance:   "body_link",
	}}
	packer := &DefaultPacker{
		VaultPath:      root,
		VaultDef:       obsidian.VaultDefinition{Path: root},
		NoteReader:     &obsidian.Note{},
		OntologySchema: schema,
		Intel:          intel,
	}
	packed, err := packer.Pack(ctx, search.QuerySpec{
		Text:   "interface context",
		Budget: search.Budget{Chars: 4000},
	}, []search.RankedResult{{
		Candidate: search.Candidate{
			Handle:      knowledge.NodeChunkHandle("node:story", "notes/specs/checkout.md", "node_body", 0),
			Owner:       knowledge.NoteHandle("notes/specs/checkout.md"),
			Type:        "note",
			Path:        "notes/specs/checkout.md",
			Title:       "Faster checkout",
			NodeRefJSON: string(refJSON),
			Granularity: "node_body",
		},
		FinalScore: 0.9,
	}})

	require.NoError(t, err)
	require.Contains(t, packed.Text, "references: context (docs/context.md)")
}

func TestDefaultPacker_OntologyBodyHitShowsMatchedChunk(t *testing.T) {
	root, schema, story := setupLongOntologyPresentationFixture(t)
	refJSON, err := json.Marshal(story.Ref)
	require.NoError(t, err)

	packer := &DefaultPacker{
		VaultPath:      root,
		VaultDef:       obsidian.VaultDefinition{Path: root},
		NoteReader:     &obsidian.Note{},
		OntologySchema: schema,
		Intel:          newPresentationOntologyIntel("notes/specs/long.md"),
	}
	packed, err := packer.Pack(context.Background(), search.QuerySpec{
		Text:   "tail evidence",
		Budget: search.Budget{Chars: 5000},
	}, []search.RankedResult{{
		Candidate: search.Candidate{
			Handle:      knowledge.NodeChunkHandle("node:story", "notes/specs/long.md", "node_body", 1),
			Owner:       knowledge.NoteHandle("notes/specs/long.md"),
			Type:        "note",
			Path:        "notes/specs/long.md",
			Title:       "Long checkout",
			Granularity: "node_body",
			ChunkIndex:  1,
			NodeRefJSON: string(refJSON),
		},
		FinalScore: 0.9,
	}})

	require.NoError(t, err)
	require.Contains(t, packed.Text, "Matched body:")
	require.Contains(t, packed.Text, "TAIL-MATCH-EVIDENCE")
	require.NotContains(t, packed.Text, "HEAD-ONLY-EVIDENCE")
}

func TestDefaultPacker_OntologyContextFitsTightBudget(t *testing.T) {
	root, schema, story := setupOntologyPresentationFixture(t)
	refJSON, err := json.Marshal(story.Ref)
	require.NoError(t, err)

	packer := &DefaultPacker{
		VaultPath:      root,
		VaultDef:       obsidian.VaultDefinition{Path: root},
		NoteReader:     &obsidian.Note{},
		OntologySchema: schema,
		Intel:          newPresentationOntologyIntel("notes/specs/checkout.md"),
	}
	packed, err := packer.Pack(context.Background(), search.QuerySpec{
		Text:   "small budget",
		Budget: search.Budget{Chars: 360},
	}, []search.RankedResult{{
		Candidate: search.Candidate{
			Handle:      knowledge.NodeChunkHandle("node:story", "notes/specs/checkout.md", "node_body", 0),
			Owner:       knowledge.NoteHandle("notes/specs/checkout.md"),
			Type:        "note",
			Path:        "notes/specs/checkout.md",
			Title:       "Faster checkout",
			Granularity: "node_body",
			NodeRefJSON: string(refJSON),
		},
		FinalScore: 0.9,
	}})

	require.NoError(t, err)
	require.Contains(t, packed.Text, "Ontology node context:")
	require.Contains(t, packed.Text, "- type: UserStory")
	require.LessOrEqual(t, len(packed.Text), 360)
	require.LessOrEqual(t, packed.Meta["budgetUsed"].(int), 360)
}

func TestDefaultPacker_ReusesOntologyProjectionScopeAcrossResults(t *testing.T) {
	root, schema, story := setupOntologyPresentationFixture(t)
	refJSON, err := json.Marshal(story.Ref)
	require.NoError(t, err)
	noteReader := &countingNoteReader{delegate: &obsidian.Note{}}

	packer := &DefaultPacker{
		VaultPath:      root,
		VaultDef:       obsidian.VaultDefinition{Path: root},
		NoteReader:     noteReader,
		OntologySchema: schema,
		Intel:          newPresentationOntologyIntel("notes/specs/checkout.md"),
	}
	result := search.RankedResult{
		Candidate: search.Candidate{
			Handle:      knowledge.NodeChunkHandle("node:story", "notes/specs/checkout.md", "node_body", 0),
			Owner:       knowledge.NoteHandle("notes/specs/checkout.md"),
			Type:        "note",
			Path:        "notes/specs/checkout.md",
			Title:       "Faster checkout",
			Granularity: "node_body",
			NodeRefJSON: string(refJSON),
		},
		FinalScore: 0.9,
	}
	spec := search.QuerySpec{
		Text:   "repeat node",
		Budget: search.Budget{Chars: 8000},
	}
	_, err = packer.Pack(context.Background(), spec, []search.RankedResult{result})
	require.NoError(t, err)
	firstReads := noteReader.contentsByPath["notes/specs/checkout.md"]
	require.Positive(t, firstReads)
	noteReader.contentsByPath["notes/specs/checkout.md"] = 0
	packed, err := packer.Pack(context.Background(), spec, []search.RankedResult{result, result})

	require.NoError(t, err)
	require.Equal(t, 2, strings.Count(packed.Text, "Ontology node context:"))
	require.Equal(t, firstReads, noteReader.contentsByPath["notes/specs/checkout.md"])
}

func setupOntologyPresentationFixture(t *testing.T) (string, *ontology.Schema, *ontology.NodeProjection) {
	t.Helper()
	root := t.TempDir()
	schemaDir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(schemaDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(schemaDir, "schema.graphql"), []byte(`
type Spec @node(paths: ["notes/specs/*.md"]) {
  status: String @field
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  storyId: String @field
  related: ReferenceDoc @link(contextInclude: true)
  references: [ReferenceDoc!] @neighbors(direction: OUTBOUND, type: "ReferenceDoc", contextInclude: true)
}

type ReferenceDoc @node(paths: ["docs/*.md"]) {
  title: String @field
}
`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes", "specs"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "context.md"), []byte(`---
title: Context Note
---
# Context Note
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "specs", "checkout.md"), []byte(`---
status: draft
---
# Checkout Refresh

## User Stories

### Faster checkout
storyId:: US-1
related:: [[docs/context]]
Users need quicker checkout.

### Slower checkout
storyId:: US-2
Sibling body should not appear.
`), 0o644))

	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	rootProjection, err := ontology.ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes/specs/checkout.md")
	require.NoError(t, err)
	containerRef := rootProjection.Fields["userStories"].SectionNodes[0]
	container, err := ontology.ProjectNode(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, containerRef)
	require.NoError(t, err)
	storyRef := container.Fields["stories"].SectionNodes[0]
	story, err := ontology.ProjectNode(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, storyRef)
	require.NoError(t, err)
	return root, schema, story
}

func setupLongOntologyPresentationFixture(t *testing.T) (string, *ontology.Schema, *ontology.NodeProjection) {
	t.Helper()
	root := t.TempDir()
	schemaDir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(schemaDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(schemaDir, "schema.graphql"), []byte(`
type Spec @node(paths: ["notes/specs/*.md"]) {
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  storyId: String @field
}
`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes", "specs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "specs", "long.md"), []byte(`# Long Spec

## User Stories

### Long checkout
storyId:: US-LONG
HEAD-ONLY-EVIDENCE `+strings.Repeat("alpha ", 1700)+`

TAIL-MATCH-EVIDENCE `+strings.Repeat("omega ", 500)+`
`), 0o644))

	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	rootProjection, err := ontology.ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes/specs/long.md")
	require.NoError(t, err)
	containerRef := rootProjection.Fields["userStories"].SectionNodes[0]
	container, err := ontology.ProjectNode(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, containerRef)
	require.NoError(t, err)
	storyRef := container.Fields["stories"].SectionNodes[0]
	story, err := ontology.ProjectNode(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, storyRef)
	require.NoError(t, err)
	return root, schema, story
}

type fakeOntologyIntel struct {
	edges          []semdb.OntologyEdgeRow
	noteMetadata   []semdb.NoteMetadataRow
	edgeBatchCalls int
}

func newPresentationOntologyIntel(paths ...string) *fakeOntologyIntel {
	metadata := make([]semdb.NoteMetadataRow, 0, len(paths))
	for _, path := range paths {
		metadata = append(metadata, semdb.NoteMetadataRow{
			Path:     path,
			FormatID: string(noteformat.FormatID("markdown")),
			Projection: semdb.NoteProjectionState{
				Status: semdb.NoteProjectionStatusCurrent,
			},
		})
	}
	return &fakeOntologyIntel{noteMetadata: metadata}
}

func (f *fakeOntologyIntel) IntelAnchorByID(context.Context, string) (codeanchor.IntelAnchor, bool, error) {
	return codeanchor.IntelAnchor{}, false, nil
}

func (f *fakeOntologyIntel) CurrentNoteMetadataRows(context.Context) ([]semdb.NoteMetadataRow, error) {
	return append([]semdb.NoteMetadataRow(nil), f.noteMetadata...), nil
}

func (f *fakeOntologyIntel) CurrentNoteMetadataRowsByPaths(_ context.Context, paths []string) (map[string]semdb.NoteMetadataRow, error) {
	rows := make(map[string]semdb.NoteMetadataRow, len(paths))
	for _, row := range f.noteMetadata {
		for _, path := range paths {
			if row.Path == path {
				rows[path] = row
				break
			}
		}
	}
	return rows, nil
}

func (f *fakeOntologyIntel) CurrentNotePropertyValues(context.Context, []string, []string, semdb.NotePropertySource) ([]semdb.NotePropertyValueRow, error) {
	return nil, nil
}

func (f *fakeOntologyIntel) CurrentNoteTags(context.Context, []string) ([]semdb.NoteTagRow, error) {
	return nil, nil
}

func (f *fakeOntologyIntel) OntologyAssessmentsByPaths(context.Context, []string) (map[string]semdb.OntologyNoteAssessmentRow, error) {
	return map[string]semdb.OntologyNoteAssessmentRow{}, nil
}

func (f *fakeOntologyIntel) OntologyAssessmentFlags(context.Context) (map[string]semdb.OntologyAssessmentFlags, error) {
	return map[string]semdb.OntologyAssessmentFlags{}, nil
}

func (f *fakeOntologyIntel) GetOntologySchemaState(context.Context) (semdb.OntologySchemaState, error) {
	return semdb.OntologySchemaState{Ready: true, MaterializationVersion: ontology.OntologyMaterializationVersion}, nil
}

func (f *fakeOntologyIntel) OntologyTypesByPaths(context.Context, []string) (map[string]semdb.OntologyNoteTypeRow, error) {
	return map[string]semdb.OntologyNoteTypeRow{}, nil
}

func (f *fakeOntologyIntel) OntologyPathsByType(context.Context, string, int) ([]string, error) {
	return nil, nil
}

func (f *fakeOntologyIntel) OntologyEdgesForPaths(_ context.Context, paths []string, includeAmbient bool, relationFilter string, limit int) ([]semdb.OntologyEdgeRow, error) {
	f.edgeBatchCalls++
	pathSet := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		pathSet[path] = struct{}{}
	}
	var out []semdb.OntologyEdgeRow
	for _, edge := range f.edges {
		if edge.RelationName != relationFilter {
			continue
		}
		if !includeAmbient && !edge.Structural {
			continue
		}
		_, src := pathSet[edge.SrcPath]
		_, dst := pathSet[edge.DstPath]
		if !src && !dst {
			continue
		}
		out = append(out, edge)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out, nil
}

func (f *fakeOntologyIntel) OntologyStructuralEdgesBySources(ctx context.Context, paths []string, relation string, limit int) ([]semdb.OntologyEdgeRow, error) {
	rows, err := f.OntologyEdgesForPaths(ctx, paths, false, relation, limit)
	if err != nil {
		return nil, err
	}
	var out []semdb.OntologyEdgeRow
	for _, row := range rows {
		if row.Structural {
			out = append(out, row)
		}
	}
	return out, nil
}

func (f *fakeOntologyIntel) OntologyAmbientEdgesBySources(ctx context.Context, paths []string, relation string, limit int) ([]semdb.OntologyEdgeRow, error) {
	rows, err := f.OntologyEdgesForPaths(ctx, paths, true, relation, limit)
	if err != nil {
		return nil, err
	}
	var out []semdb.OntologyEdgeRow
	for _, row := range rows {
		if !row.Structural {
			out = append(out, row)
		}
	}
	return out, nil
}

func (f *fakeOntologyIntel) OntologyNodesBySourceLocators(context.Context, []string) (map[string]codeanchor.IntelOntologyNode, error) {
	return map[string]codeanchor.IntelOntologyNode{}, nil
}

func (f *fakeOntologyIntel) OntologyNodesByNoteFragments(context.Context, []string) (map[string]codeanchor.IntelOntologyNode, error) {
	return map[string]codeanchor.IntelOntologyNode{}, nil
}

func (f *fakeOntologyIntel) ReplaceOntologyNodes(context.Context, []string, []codeanchor.IntelOntologyNode) error {
	return nil
}

type countingNoteReader struct {
	delegate       obsidian.NoteReader
	contentsByPath map[string]int
}

func (r *countingNoteReader) GetContents(vaultDef obsidian.VaultDefinition, path string) (string, error) {
	if r.contentsByPath == nil {
		r.contentsByPath = map[string]int{}
	}
	r.contentsByPath[path]++
	return r.delegate.GetContents(vaultDef, path)
}

func (r *countingNoteReader) GetNotesList(vaultDef obsidian.VaultDefinition) ([]string, error) {
	return r.delegate.GetNotesList(vaultDef)
}

func (r *countingNoteReader) GetModTime(vaultDef obsidian.VaultDefinition, path string) (time.Time, error) {
	return r.delegate.GetModTime(vaultDef, path)
}

func (r *countingNoteReader) Title(path string) (string, bool) {
	return r.delegate.Title(path)
}
