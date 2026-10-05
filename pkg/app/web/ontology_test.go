package web

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestAssessmentIssueDetails_HydratesFixTargets(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "ontology.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	sourceAssessment := ontology.NoteAssessment{
		NotePath:     "specs/auth.md",
		ResolvedType: "Spec",
		Relations: []ontology.RelationAssessment{
			{
				Name: "implements",
				Issues: []ontology.ValidationIssue{{
					Code:         "inverse_mismatch",
					NotePath:     "specs/auth.md",
					FieldName:    "implements",
					LinkTarget:   "features/login.md",
					FixTarget:    "features/login.md",
					FixFieldName: "specs",
					Message:      "missing inverse on login",
				}},
			},
			{
				// Same code, different concrete target: each issue must fix its own note.
				Name: "dependsOn",
				Issues: []ontology.ValidationIssue{{
					Code:         "inverse_mismatch",
					NotePath:     "specs/auth.md",
					FieldName:    "dependsOn",
					LinkTarget:   "features/session.md",
					FixTarget:    "features/session.md",
					FixFieldName: "dependents",
					Message:      "missing inverse on session",
				}},
			},
		},
	}
	sessionAssessment := ontology.NoteAssessment{
		NotePath:     "features/session.md",
		ResolvedType: "Feature",
		Relations: []ontology.RelationAssessment{{
			Name:    "dependents",
			Targets: []ontology.RelationTarget{{Path: "specs/base.md", TypeName: "Spec"}},
		}},
	}
	targetAssessment := ontology.NoteAssessment{
		NotePath:     "features/login.md",
		ResolvedType: "Feature",
		Relations: []ontology.RelationAssessment{{
			Name: "specs",
			Targets: []ontology.RelationTarget{
				{Path: "specs/existing.md", TypeName: "Spec"},
			},
		}},
	}
	ambiguousAssessment := ontology.NoteAssessment{
		NotePath:       "ambig.md",
		CandidateTypes: []string{"Spec", "Feature"},
	}
	cleanAssessment := ontology.NoteAssessment{
		NotePath:     "clean.md",
		ResolvedType: "Feature",
	}
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Assessments: []semdb.OntologyNoteAssessmentRow{
			assessmentRow(t, sourceAssessment),
			assessmentRow(t, targetAssessment),
			assessmentRow(t, sessionAssessment),
			assessmentRow(t, ambiguousAssessment),
			assessmentRow(t, cleanAssessment),
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "notes", LoadedAt: 1, Ready: true},
	}))

	schema := &ontology.Schema{}
	requested := []string{"specs/auth.md", "ambig.md", "clean.md"}

	detailScope := noderead.NewService(obsidian.VaultDefinition{}, &obsidian.Note{}, store, schema).NewScope(ctx, noderead.ScopeOptions{})
	detailAssessments, err := detailScope.AssessmentsByPaths(ctx, requested)
	require.NoError(t, err)
	issuesByPath, itemsByPath, detailAmbiguous, detailIssues, err := assessmentIssueDetails(ctx, schema, detailAssessments, detailScope.AssessmentsByPaths)
	require.NoError(t, err)

	require.Equal(t, 1, detailAmbiguous)
	require.Equal(t, 1, detailIssues)

	require.Equal(t, map[string]bool{"specs/auth.md": true}, issuesByPath)
	require.Contains(t, itemsByPath, "specs/auth.md")
	require.Len(t, itemsByPath["specs/auth.md"], 2)
	byField := map[string]OntologyNoteIssueItem{}
	for _, item := range itemsByPath["specs/auth.md"] {
		byField[item.Field] = item
	}

	item := byField["implements"]
	require.True(t, item.Fixable)
	require.Len(t, item.FixOps, 1)
	require.Equal(t, "setLinkField", item.FixOps[0].Kind)
	require.Equal(t, "features/login.md", item.FixOps[0].Path)
	require.Equal(t, "specs", item.FixOps[0].Field)
	require.Equal(t, []string{"[[specs/existing.md]]", "[[specs/auth.md]]"}, item.FixOps[0].Values)

	dependsOn := byField["dependsOn"]
	require.True(t, dependsOn.Fixable)
	require.Len(t, dependsOn.FixOps, 1)
	require.Equal(t, "setLinkField", dependsOn.FixOps[0].Kind)
	require.Equal(t, "features/session.md", dependsOn.FixOps[0].Path)
	require.Equal(t, "dependents", dependsOn.FixOps[0].Field)
	require.Equal(t, []string{"[[specs/base.md]]", "[[specs/auth.md]]"}, dependsOn.FixOps[0].Values)
}

func assessmentRow(t *testing.T, assessment ontology.NoteAssessment) semdb.OntologyNoteAssessmentRow {
	t.Helper()
	data, err := json.Marshal(assessment)
	require.NoError(t, err)
	flags := ontology.FlagsForAssessment(&assessment)
	return semdb.OntologyNoteAssessmentRow{
		NotePath:       assessment.NotePath,
		ResolvedType:   assessment.ResolvedType,
		AssessmentJSON: string(data),
		HasIssues:      flags.HasIssues,
		TypeAmbiguous:  flags.TypeAmbiguous,
		SchemaHash:     "schema",
		UpdatedAt:      1,
	}
}

// These tests target the web annotation pass in isolation (no server, no
// store): mapRenderedSections + annotateTypedSections + interpolation. The
// schema is loaded from a real temp fixture so we exercise the full compile
// + section-type resolution path without mocking schema internals.

func loadTypedSectionsSchema(t *testing.T, sdl string) *ontology.Schema {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(sdl), 0o644))
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	require.NotNil(t, schema)
	return schema
}

const typedSpecSchema = `
enum StoryStatus { PLANNED IN_PROGRESS COMPLETE }

type Story implements Section @preview(template: "{{title}} - {{status}}") {
  status: StoryStatus @field
  owner: String @field
}

type StoriesSection implements Section {
  stories: [Story!] @contains(level: H3)
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String
  userStories: StoriesSection @contains(level: H2, heading: "Stories")
}
`

const inlineSectionSchema = `
type NarrativeSection implements Section {
}

type SpecWithSummary implements Section {
}

type UserStory implements Section @node(locator: EMBEDDED) {
  acceptanceCriteria: NarrativeSection @contains(level: H4, heading: "Acceptance Criteria", display: INLINE)
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["specs/*.md"]) {
  summarySection: NarrativeSection @contains(level: H2, heading: "Summary", display: INLINE)
  goals: NarrativeSection @contains(level: H2, heading: "Goals")
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`

func TestMapRenderedSections_ContentUsesOwnContent(t *testing.T) {
	md := "## Parent\n\nParent intro paragraph.\n\n### Child A\n^child-a\n\nChild A body.\n\n### Child B\n\nChild B body.\n"
	nodes := ontology.ParseSections("specs/foo.md", md)
	rendered := mapRenderedSections(nodes)

	require.Len(t, rendered, 1)
	parent := rendered[0]
	require.Equal(t, "Parent", parent.Title)
	// Own content: only the intro text — descendants are trimmed out. Naive
	// full-subtree rendering would duplicate the children.
	require.Contains(t, parent.Content, "Parent intro paragraph.")
	require.NotContains(t, parent.Content, "Child A body.")
	require.NotContains(t, parent.Content, "Child B body.")
	require.Len(t, parent.Children, 2)
	require.Equal(t, "Child A body.", parent.Children[0].Content)
	require.Equal(t, "child-a", parent.Children[0].BlockID)
	require.Equal(t, "Child B body.", parent.Children[1].Content)
	require.Empty(t, parent.Children[1].BlockID)
}

func TestSectionAnchorLookupKey_TreatsCaretFragmentsAsBlocks(t *testing.T) {
	require.Equal(t, "block:US-001", sectionAnchorLookupKey("#^US-001", obsidian.BacklinkTypeHeading))
	require.Equal(t, "block:validation", sectionAnchorLookupKey("^validation", obsidian.BacklinkTypeHeading))
}

func TestAnnotateTypedSections_ListChildrenAnnotated(t *testing.T) {
	// Spaced placeholders interpolate the same as compact ones.
	spacedTemplate := strings.Replace(typedSpecSchema, `"{{title}} - {{status}}"`, `"{{ title }} - {{ status }}"`, 1)
	require.NotEqual(t, typedSpecSchema, spacedTemplate)
	schema := loadTypedSectionsSchema(t, spacedTemplate)
	spec := schema.Types["Spec"]
	require.NotNil(t, spec)

	// Spec.userStories binds the "## Stories" H2 to StoriesSection;
	// StoriesSection.stories is a list-typed H3 field that matches every H3
	// under that H2.
	md := "## Stories\n\n### First story\n\nStatus:: IN_PROGRESS\nOwner:: alice\n\nBody of first story.\n\n### Second story\n\nStatus:: PLANNED\n\nBody of second story.\n"
	nodes := ontology.ParseSections("specs/foo.md", md)
	rendered := mapRenderedSections(nodes)

	idMap := make(map[string]*RenderedSection)
	indexRenderedSections(rendered, idMap)
	annotateTypedSections(nodes, spec, spec.PropertyCase, schema, idMap, "")

	require.Len(t, rendered, 1)
	storiesSection := rendered[0]
	require.Equal(t, "StoriesSection", storiesSection.TypeName)
	require.Equal(t, "userStories", storiesSection.FieldName)
	require.Equal(t, "userStories", storiesSection.FieldPath)
	require.False(t, storiesSection.FieldList)
	require.Len(t, storiesSection.Children, 2)

	first := storiesSection.Children[0]
	require.Equal(t, "Story", first.TypeName)
	require.Equal(t, "stories", first.FieldName)
	require.Equal(t, "userStories.stories", first.FieldPath)
	require.True(t, first.FieldList)
	require.Equal(t, "IN_PROGRESS", first.Properties["status"])
	require.Equal(t, "alice", first.Properties["owner"])
	require.Equal(t, "First story - IN_PROGRESS", first.PreviewTemplate)
	require.True(t, first.Collapsed)

	second := storiesSection.Children[1]
	require.Equal(t, "Story", second.TypeName)
	require.Equal(t, "stories", second.FieldName)
	require.Equal(t, "userStories.stories", second.FieldPath)
	require.True(t, second.FieldList)
	require.Equal(t, "PLANNED", second.Properties["status"])
	require.Equal(t, "Second story - PLANNED", second.PreviewTemplate)

	// The structural response carries the same typed hierarchy.
	structural := buildStructuralSections(rendered)
	require.Len(t, structural, 1)
	root := structural[0]
	require.Equal(t, "StoriesSection", root.TypeName)
	require.Equal(t, "SECTION", root.Locator)
	require.Equal(t, "Stories", root.Title)
	require.Len(t, root.Children, 2)
	structuralFirst := root.Children[0]
	require.Equal(t, "Story", structuralFirst.TypeName)
	require.Equal(t, "SECTION", structuralFirst.Locator)
	require.Equal(t, "First story", structuralFirst.Title)
	require.Equal(t, "First story - IN_PROGRESS", structuralFirst.Preview)
	require.Equal(t, "First story - IN_PROGRESS", structuralFirst.PreviewTemplate)
	require.Equal(t, "IN_PROGRESS", structuralFirst.Properties["status"])
	require.Equal(t, "alice", structuralFirst.Properties["owner"])
	require.Empty(t, structuralFirst.Children)
}

func TestAnnotateTypedSections_ListChildrenAnnotatedUnderSingleH1(t *testing.T) {
	schema := loadTypedSectionsSchema(t, typedSpecSchema)
	spec := schema.Types["Spec"]
	require.NotNil(t, spec)

	md := "# Spec title\n\n## Stories\n\n### First story\n\nStatus:: IN_PROGRESS\n"
	nodes := ontology.ParseSections("specs/foo.md", md)
	rendered := mapRenderedSections(nodes)

	idMap := make(map[string]*RenderedSection)
	indexRenderedSections(rendered, idMap)
	annotateTypedSections(nodes, spec, spec.PropertyCase, schema, idMap, "")

	require.Len(t, rendered, 1)
	require.Equal(t, "Spec title", rendered[0].Title)
	require.Empty(t, rendered[0].TypeName)
	require.Len(t, rendered[0].Children, 1)
	storiesSection := rendered[0].Children[0]
	require.Equal(t, "StoriesSection", storiesSection.TypeName)
	require.Equal(t, "userStories", storiesSection.FieldPath)
	require.Len(t, storiesSection.Children, 1)
	require.Equal(t, "Story", storiesSection.Children[0].TypeName)
	require.Equal(t, "userStories.stories", storiesSection.Children[0].FieldPath)
	require.Equal(t, "First story - IN_PROGRESS", storiesSection.Children[0].PreviewTemplate)
}

func TestAnnotateTypedSections_NoResolvedTypeLeavesSectionsUntyped(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(typedSpecSchema), 0o644))
	body := "## Stories\n\n### First story\n\nStatus:: IN_PROGRESS\n"
	for _, rel := range []string{"specs/typed.md", "notes/plain.md"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, filepath.Dir(rel)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644))
	}
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	vaultDef := obsidian.VaultDefinition{Name: "test", Path: root, Links: obsidian.LinkTypeBoth}
	_, err = ontology.EnsureFreshRuntimeWithStore(context.Background(), testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	srv := newFixtureServer(t, fixtureVault{
		root: root, vault: &obsidian.Vault{Name: "test"}, vaultDef: vaultDef, intelStore: store,
	}, &Runtime{IntelStore: store})

	// The same body in a schema-matched note is annotated, so the fixture can
	// observe annotation.
	typed, err := srv.readRenderedNote(t.Context(), "specs/typed.md")
	require.NoError(t, err)
	require.Equal(t, "Spec", typed.ResolvedType)
	require.Len(t, typed.Sections, 1)
	require.Equal(t, "StoriesSection", typed.Sections[0].TypeName)

	plain, err := srv.readRenderedNote(t.Context(), "notes/plain.md")
	require.NoError(t, err)
	require.Empty(t, plain.ResolvedType)
	require.Len(t, plain.Sections, 1)
	for _, section := range []RenderedSection{plain.Sections[0], plain.Sections[0].Children[0]} {
		require.Empty(t, section.TypeName, section.Title)
		require.Empty(t, section.FieldPath, section.Title)
		require.Empty(t, section.Properties, section.Title)
		require.Empty(t, section.PreviewTemplate, section.Title)
	}
	require.Len(t, plain.Sections[0].Children, 1)
}

func TestAnnotateTypedSections_MissingScalarStillAnnotatesStructure(t *testing.T) {
	schema := loadTypedSectionsSchema(t, typedSpecSchema)
	spec := schema.Types["Spec"]

	// Story omits every inline property — the annotation pass should still
	// set TypeName and interpolate {{title}} but leave {{status}} as a
	// literal placeholder. Validation surfaces the missing field through the
	// assessment side channel (tested in pkg/ontology), not through the
	// rendered response.
	md := "## Stories\n\n### Untyped story\n\nJust a body.\n"
	nodes := ontology.ParseSections("specs/foo.md", md)
	rendered := mapRenderedSections(nodes)

	idMap := make(map[string]*RenderedSection)
	indexRenderedSections(rendered, idMap)
	annotateTypedSections(nodes, spec, spec.PropertyCase, schema, idMap, "")

	require.Len(t, rendered, 1)
	stories := rendered[0]
	require.Equal(t, "StoriesSection", stories.TypeName)
	require.Len(t, stories.Children, 1)
	story := stories.Children[0]
	require.Equal(t, "Story", story.TypeName)
	require.Nil(t, story.Properties)
	// {{title}} always interpolates (it's the heading); {{status}} has no
	// value so it's left literal.
	require.Equal(t, "Untyped story - {{status}}", story.PreviewTemplate)
}

func TestAnnotateTypedSections_PropagatesInlineSectionDisplay(t *testing.T) {
	schema := loadTypedSectionsSchema(t, inlineSectionSchema)
	spec := schema.Types["Spec"]
	require.NotNil(t, spec)

	md := "## User Stories\n\n### Story Alpha\n\n#### Acceptance Criteria\n\nDone when it works.\n"
	nodes := ontology.ParseSections("specs/foo.md", md)
	rendered := mapRenderedSections(nodes)

	idMap := make(map[string]*RenderedSection)
	indexRenderedSections(rendered, idMap)
	annotateTypedSections(nodes, spec, spec.PropertyCase, schema, idMap, "")

	require.Len(t, rendered, 1)
	storiesSection := rendered[0]
	require.Len(t, storiesSection.Children, 1)
	story := storiesSection.Children[0]
	require.Len(t, story.Children, 1)
	criteria := story.Children[0]
	require.Equal(t, "acceptanceCriteria", criteria.FieldName)
	require.Equal(t, "userStories.stories.acceptanceCriteria", criteria.FieldPath)
	require.Equal(t, ontology.SectionDisplayInline, criteria.SectionDisplay)

	structural := buildStructuralSections(rendered)
	require.Len(t, structural, 1)
	require.Len(t, structural[0].Children, 1)
	require.Len(t, structural[0].Children[0].Children, 1)
	require.Equal(
		t,
		ontology.SectionDisplayInline,
		structural[0].Children[0].Children[0].SectionDisplay,
	)
}

func TestBuildStructuralView_PreservesLoosePrefaceOnFileRoot(t *testing.T) {
	rendered := RenderedFileResponse{
		Path:         "specs/foo.md",
		Title:        "Foo",
		ResolvedType: "Spec",
		Rendered:     "Testing 1 2 3\n\n## User Stories\n\nBody.\n",
		Sections: []RenderedSection{
			{
				ID:      "specs/foo.md#user-stories",
				Title:   "User Stories",
				Level:   ontology.SectionLevelH2,
				Content: "Body.",
			},
		},
	}

	view := buildStructuralView(rendered, nil)
	require.NotNil(t, view)
	require.Equal(t, "Testing 1 2 3", view.Root.Content)
	require.Len(t, view.Root.Children, 1)
	require.Equal(t, "User Stories", view.Root.Children[0].Title)
	require.Equal(t, "user-stories", view.Root.Children[0].Fragment)
}

func TestBuildStructuralView_HoistsInlineSummaryContentIntoFileRoot(t *testing.T) {
	schema := loadTypedSectionsSchema(t, inlineSectionSchema)
	spec := schema.Types["Spec"]
	require.NotNil(t, spec)

	md := "# Foo\n\nTesting 1 2 3\n\n## Summary\n\nSummary body.\n\n## Goals\n\nGoal body.\n"
	nodes := ontology.ParseSections("specs/foo.md", md)
	rendered := RenderedFileResponse{
		Path:         "specs/foo.md",
		Title:        "Foo",
		ResolvedType: "Spec",
		Rendered:     md,
		Sections:     mapRenderedSections(nodes),
	}

	idMap := make(map[string]*RenderedSection)
	indexRenderedSections(rendered.Sections, idMap)
	annotateTypedSections(nodes, spec, spec.PropertyCase, schema, idMap, "")

	view := buildStructuralView(rendered, nil)
	require.NotNil(t, view)
	require.Equal(t, "Testing 1 2 3", view.Root.Content)
	require.Len(t, view.Root.Children, 2)

	summary := view.Root.Children[0]
	require.Equal(t, "Summary", summary.Title)
	require.Equal(t, "Summary body.", summary.Content)
	require.Equal(t, ontology.SectionDisplayInline, summary.SectionDisplay)

	goals := view.Root.Children[1]
	require.Equal(t, "Goals", goals.Title)
	require.Equal(t, "Goal body.", goals.Content)
	require.Equal(t, ontology.SectionDisplayPane, goals.SectionDisplay)
}

func TestStructuralViews_SectionTabsPreserveFieldOrderAndScope(t *testing.T) {
	first := RenderedSection{ID: "spec.md#first", Title: "First", Level: ontology.SectionLevelH3}
	second := RenderedSection{ID: "spec.md#second", Title: "Second", Level: ontology.SectionLevelH3}
	group := RenderedSection{
		ID: "spec.md#group", Title: "Group", Level: ontology.SectionLevelH2, TypeName: "Group",
		Children: []RenderedSection{first, second},
	}
	doc := &ontology.TypeDoc{Fields: []ontology.FieldDoc{
		{Name: "ignored"},
		{Name: "chosen", Kind: ontology.FieldKindSection, SectionHeading: " First ", SectionLevel: ontology.SectionLevelH3, Description: "Heading wins"},
		{Name: "group", Kind: ontology.FieldKindSection, SectionHeading: "Group", SectionLevel: ontology.SectionLevelH2},
		{Name: "children", Kind: ontology.FieldKindSection, SectionLevel: ontology.SectionLevelH3, List: true, Description: " Stories "},
		{Name: "missing", Kind: ontology.FieldKindSection, SectionHeading: "Absent", SectionLevel: ontology.SectionLevelH3},
		{Name: "chosen", Kind: ontology.FieldKindSection, SectionHeading: "Second", SectionLevel: ontology.SectionLevelH3},
	}}
	file := RenderedFileResponse{
		Path: "spec.md", ResolvedType: "Spec",
		Sections: []RenderedSection{{
			ID: "spec.md#root", Level: ontology.SectionLevelH1,
			Children: []RenderedSection{group},
		}},
	}
	type tabSummary struct {
		key, label string
		nodeIDs    []string
	}
	for _, tc := range []struct {
		name string
		view *StructuralViewResponse
		want []tabSummary
	}{
		{"file", buildStructuralView(file, doc), []tabSummary{
			{"chosen", " First ", []string{first.ID}},
			{"group", "Group", []string{group.ID}},
			{"chosen", "Second", []string{second.ID}},
		}},
		{"section", buildSectionStructuralView(group, doc), []tabSummary{
			{"chosen", " First ", []string{first.ID}},
			{"children", " Stories ", []string{first.ID, second.ID}},
			{"chosen", "Second", []string{second.ID}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NotNil(t, tc.view)
			require.Equal(t, "structural", tc.view.DefaultView)
			require.Len(t, tc.view.Tabs, len(tc.want))
			for i, want := range tc.want {
				tab := tc.view.Tabs[i]
				require.Equal(t, want.key, tab.Key)
				require.Equal(t, want.key, tab.FieldName)
				require.Equal(t, want.label, tab.Label)
				require.Equal(t, len(want.nodeIDs), tab.Count)
				var ids []string
				for _, node := range tab.Nodes {
					ids = append(ids, node.NodeID)
				}
				require.Equal(t, want.nodeIDs, ids)
			}
		})
	}
}

func TestStructuralViews_DefaultViewWithoutTabs(t *testing.T) {
	for _, tc := range []struct {
		name, typeName, want string
		typeDoc              *ontology.TypeDoc
	}{
		{"typed without definition", "Spec", "structural", nil},
		{"typed without section fields", "Spec", "markdown", &ontology.TypeDoc{}},
		{"untyped without definition", "", "markdown", nil},
		{"untyped without section fields", "", "markdown", &ontology.TypeDoc{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			section := RenderedSection{ID: "spec.md#section", TypeName: tc.typeName, Level: ontology.SectionLevelH2}
			file := RenderedFileResponse{Path: "spec.md", ResolvedType: tc.typeName, Sections: []RenderedSection{section}}
			for _, view := range []*StructuralViewResponse{
				buildStructuralView(file, tc.typeDoc),
				buildSectionStructuralView(section, tc.typeDoc),
			} {
				require.NotNil(t, view)
				require.Equal(t, tc.want, view.DefaultView)
				require.Nil(t, view.Tabs)
			}
		})
	}
}

type countingNoteReader struct {
	contents map[string]string
	reads    map[string]int
}

func (r *countingNoteReader) GetContents(_ obsidian.VaultDefinition, path string) (string, error) {
	if r.reads == nil {
		r.reads = make(map[string]int)
	}
	r.reads[path]++
	content, ok := r.contents[path]
	if !ok {
		return "", errors.New("missing note")
	}
	return content, nil
}

func (r *countingNoteReader) GetNotesList(_ obsidian.VaultDefinition) ([]string, error) {
	paths := make([]string, 0, len(r.contents))
	for path := range r.contents {
		paths = append(paths, path)
	}
	return paths, nil
}

func (r *countingNoteReader) GetModTime(obsidian.VaultDefinition, string) (time.Time, error) {
	return time.Time{}, nil
}

func (r *countingNoteReader) Title(path string) (string, bool) {
	return path, true
}

func TestResolvedNoteLinks_CachesPerReferrer(t *testing.T) {
	reader := &countingNoteReader{
		contents: map[string]string{
			"notes/spec.md":   "# Spec",
			"notes/effort.md": "Links [[spec#Heading]] and [[spec#^block-1]].",
			"notes/other.md":  "Only [[spec#Other heading]].",
		},
	}
	server := &Server{
		cfg: Config{
			VaultDef:     obsidian.VaultDefinition{Name: "test", Path: "/test"},
			NoteMetadata: testNoteMetadataIndexer(t),
		},
	}
	cache := obsidian.BuildNotePathCache([]string{"notes/spec.md", "notes/effort.md", "notes/other.md"})
	targets := func(links []resolvedNoteLink) []string {
		out := make([]string, 0, len(links))
		for _, link := range links {
			out = append(out, link.Path+"#"+link.Fragment)
		}
		return out
	}
	effortLinks := []string{"notes/spec.md#Heading", "notes/spec.md#^block-1"}

	links, err := server.resolvedNoteLinks("notes/effort.md", reader, cache)
	require.NoError(t, err)
	require.Equal(t, effortLinks, targets(links))

	other, err := server.resolvedNoteLinks("notes/other.md", reader, cache)
	require.NoError(t, err)
	require.Equal(t, []string{"notes/spec.md#Other heading"}, targets(other))
	require.Equal(t, 1, reader.reads["notes/other.md"])

	links, err = server.resolvedNoteLinks("notes/effort.md", reader, cache)
	require.NoError(t, err)
	require.Equal(t, effortLinks, targets(links), "a cached referrer keeps its own links")
	require.Equal(t, 1, reader.reads["notes/effort.md"])
}

func TestModifiedOpNotePathPreservesTypedExtensions(t *testing.T) {
	require.Equal(t, "notes/Decision.MD", modifiedOpNotePath(OntologyEditOp{Path: "notes/Decision.MD#Summary"}))
	require.Equal(t, "notes/Decision.html", modifiedOpNotePath(OntologyEditOp{Path: "notes/Decision.html#Summary"}))
}
