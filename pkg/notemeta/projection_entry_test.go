package notemeta

import (
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func TestProjectionEntryFromPreservesProviderFactsWithoutParsing(t *testing.T) {
	content := "[Target]"
	source := projectionTestSource(t, "notes/Decision.MD", content)
	timestamp, err := time.Parse(time.RFC3339Nano, "2026-08-03T09:10:11.123456789-04:00")
	require.NoError(t, err)
	projection := projectionTestProjection(t, noteformat.ProjectionStatusCurrent, noteformat.ProjectionFacts{
		Title: &noteformat.TitleFact{Value: " Projected title "},
		RootMetadata: []noteformat.RootMetadataFact{
			{Key: "null", Value: metadataValue(t, nil)},
			{Key: "boolean", Value: metadataValue(t, true)},
			{Key: "integer", Value: metadataValue(t, int64(7))},
			{Key: "float", Value: metadataValue(t, 1.25)},
			{Key: "date", Value: metadataValue(t, time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC))},
			{Key: "timestamp", Value: metadataValue(t, timestamp)},
			{Key: "list", Value: metadataValue(t, []any{"one", int64(2), false})},
			{Key: "object", Value: metadataValue(t, map[string]any{"priority": int64(3)})},
		},
		InlineProperties: []noteformat.InlinePropertyFact{
			{Key: "owner", Value: "Ada"}, {Key: "owner", Value: "Grace"},
		},
		Tags:    []noteformat.TagFact{{Value: " topic "}, {Value: "topic"}, {Value: "nested/child"}},
		Aliases: []noteformat.AliasFact{{Value: "DEC-1"}, {Value: "Decision"}},
		Links: []noteformat.UnresolvedAuthoredLinkFact{{
			Resolution: noteformat.LinkResolutionRelativePath,
			Syntax:     "markdown", Subtype: "heading", ResolverInput: "Target#section",
			Target: "Target", Path: "Target", Fragment: "section",
			RawRange: noteformat.SourceRange{StartByte: 0, EndByte: 8}, TargetRange: noteformat.SourceRange{StartByte: 1, EndByte: 7},
		}},
		FragmentTargets: []noteformat.FragmentTargetFact{{Kind: "heading", Text: "Decision", NormalizedText: "decision", Ordinal: 1, Level: 1, Line: 1}},
	})

	mapped, err := projectionEntryFrom(source, projection)
	require.NoError(t, err)
	require.Equal(t, "notes/Decision.MD", mapped.Entry.Path)
	require.Equal(t, content, mapped.Entry.Content)
	require.Equal(t, source.Mtime(), mapped.Entry.Mtime)
	require.Equal(t, source.Size(), mapped.Entry.Size)
	require.Equal(t, source, mapped.Entry.Source)
	require.Equal(t, projection, mapped.Entry.Projection)
	require.Equal(t, "Projected title", mapped.Entry.Title)
	require.Equal(t, []string{"Ada", "Grace"}, mapped.Entry.InlineProps["owner"])
	require.Equal(t, []string{"topic", "nested/child"}, mapped.Entry.Tags)
	require.Equal(t, []string{"DEC-1", "Decision"}, mapped.Entry.Aliases)
	require.Equal(t, projection.Facts.Links, mapped.Entry.Links)
	require.Equal(t, projection.Facts.FragmentTargets, mapped.Entry.FragmentTargets)
	require.Equal(t, []string{"DEC-1", "Decision"}, mapped.Entry.Frontmatter["aliases"])
	require.Equal(t, source, mapped.Source)
	require.Equal(t, projection.Facts.Links, mapped.Links)
	require.Equal(t, projection.Facts.FragmentTargets, mapped.FragmentTargets)

	require.Nil(t, mapped.Entry.Frontmatter["null"])
	require.Equal(t, true, mapped.Entry.Frontmatter["boolean"])
	require.Equal(t, 7, mapped.Entry.Frontmatter["integer"])
	require.Equal(t, 1.25, mapped.Entry.Frontmatter["float"])
	date, ok := mapped.Entry.Frontmatter["date"].(time.Time)
	require.True(t, ok)
	require.Equal(t, "2026-08-03", date.Format(time.DateOnly))
	projectedTimestamp, ok := mapped.Entry.Frontmatter["timestamp"].(time.Time)
	require.True(t, ok)
	require.Equal(t, timestamp, projectedTimestamp)
	require.Equal(t, []any{"one", 2, false}, mapped.Entry.Frontmatter["list"])
	require.Equal(t, map[string]any{"priority": 3}, mapped.Entry.Frontmatter["object"])

	// The compatibility map is intentionally detached; canonical facts retain
	// the provider value shape needed by durable row translation.
	mapped.Entry.Frontmatter["integer"] = "changed"
	require.Equal(t, noteformat.MetadataInteger, mapped.RootMetadata[2].Value.Kind())
}

func TestProjectionEntryFromFatalKeepsStableFallbackIdentity(t *testing.T) {
	source := projectionTestSource(t, "notes/fatal.md", "content")
	projection, err := noteformat.NewProjectionWithFacts(
		"provider-v1", "projection-v1", noteformat.ProjectionStatusFatal,
		[]noteformat.Diagnostic{{Code: "parse_failed", Message: "bad source", Blocking: true}},
		markdown.New().Descriptor().Capabilities, noteformat.ProjectionFacts{},
	)
	require.NoError(t, err)

	mapped, err := projectionEntryFrom(source, projection)
	require.NoError(t, err)
	require.Equal(t, source.Path().String(), mapped.Entry.Path)
	require.Equal(t, "content", mapped.Entry.Content)
	require.Equal(t, source.Mtime(), mapped.Entry.Mtime)
	require.Equal(t, source.Size(), mapped.Entry.Size)
	require.Equal(t, source, mapped.Entry.Source)
	require.Equal(t, projection, mapped.Entry.Projection)
	require.Equal(t, "fatal", mapped.Entry.Title)
	require.Nil(t, mapped.Entry.Frontmatter)
	require.Nil(t, mapped.Entry.InlineProps)
	require.Empty(t, mapped.Entry.Tags)
	require.Empty(t, mapped.RootMetadata)
	require.Empty(t, mapped.Links)
}

func TestMetadataValueRowsPreserveCanonicalScalarKindsAndLists(t *testing.T) {
	timestamp, err := time.Parse(time.RFC3339Nano, "2026-08-03T09:10:11.123456789-04:00")
	require.NoError(t, err)
	cases := []struct {
		name  string
		value any
		text  string
		norm  string
		kind  semdb.NotePropertyValueKind
	}{
		{name: "null", value: nil, text: "", norm: "", kind: semdb.NotePropertyValueUnknown},
		{name: "boolean", value: true, text: "true", norm: "true", kind: semdb.NotePropertyValueBool},
		{name: "integer", value: int64(7), text: "7", norm: "7", kind: semdb.NotePropertyValueInt},
		{name: "float", value: 1.25, text: "1.25", norm: "1.25", kind: semdb.NotePropertyValueFloat},
		{name: "date", value: time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC), text: "2026-08-03", norm: "2026-08-03", kind: semdb.NotePropertyValueDate},
		{name: "timestamp", value: timestamp, text: "2026-08-03T09:10:11.123456789-04:00", norm: "2026-08-03t09:10:11.123456789-04:00", kind: semdb.NotePropertyValueDateTime},
		{name: "string", value: "  literal  ", text: "literal", norm: "literal", kind: semdb.NotePropertyValueString},
		{name: "object", value: map[string]any{"key": "value"}, text: "", norm: "", kind: semdb.NotePropertyValueUnknown},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			rows := metadataValueRows("notes/example.md", " State ", semdb.NotePropertySourceFrontmatter, metadataValue(t, test.value))
			require.Equal(t, []semdb.NotePropertyValueRow{{
				NotePath: "notes/example.md", PropertyName: "state", Source: semdb.NotePropertySourceFrontmatter,
				ValueText: test.text, ValueNorm: test.norm, ValueKind: test.kind,
			}}, rows)
		})
	}

	rows := metadataValueRows("notes/example.md", "items", semdb.NotePropertySourceFrontmatter, metadataValue(t, []any{"one", int64(2), false}))
	require.Equal(t, []semdb.NotePropertyValueRow{
		{NotePath: "notes/example.md", PropertyName: "items", Source: semdb.NotePropertySourceFrontmatter, ValueText: "one", ValueNorm: "one", ValueKind: semdb.NotePropertyValueString, IsList: true, ListOrdinal: 0},
		{NotePath: "notes/example.md", PropertyName: "items", Source: semdb.NotePropertySourceFrontmatter, ValueText: "2", ValueNorm: "2", ValueKind: semdb.NotePropertyValueInt, IsList: true, ListOrdinal: 1},
		{NotePath: "notes/example.md", PropertyName: "items", Source: semdb.NotePropertySourceFrontmatter, ValueText: "false", ValueNorm: "false", ValueKind: semdb.NotePropertyValueBool, IsList: true, ListOrdinal: 2},
	}, rows)

	empty := metadataValueRows("notes/example.md", "items", semdb.NotePropertySourceFrontmatter, metadataValue(t, []any{}))
	require.Equal(t, []semdb.NotePropertyValueRow{{
		NotePath: "notes/example.md", PropertyName: "items", Source: semdb.NotePropertySourceFrontmatter,
		ValueKind: semdb.NotePropertyValueUnknown, IsList: true,
	}}, empty)
}

func projectionTestSource(t *testing.T, notePath, content string) noteformat.AuthoredSource {
	t.Helper()
	source, err := noteformat.NewAuthoredSource(paths.NormalizeNotePath(notePath), markdown.New().Descriptor(), []byte(content), 42)
	require.NoError(t, err)
	return source
}

func projectionTestProjection(t *testing.T, status noteformat.ProjectionStatus, facts noteformat.ProjectionFacts) noteformat.Projection {
	t.Helper()
	descriptor := markdown.New().Descriptor()
	projection, err := noteformat.NewProjectionWithFacts(descriptor.ProviderVersion, descriptor.ProjectionVersion, status, nil, descriptor.Capabilities, facts)
	require.NoError(t, err)
	return projection
}

func metadataValue(t *testing.T, value any) noteformat.MetadataValue {
	t.Helper()
	metadata, err := noteformat.NewMetadataValue(value)
	require.NoError(t, err)
	return metadata
}
