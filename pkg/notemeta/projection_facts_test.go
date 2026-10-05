package notemeta

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/stretchr/testify/require"
)

func TestProjectionDiagnosticRowsUseStableRangeCodeOccurrenceOrder(t *testing.T) {
	source := projectionTestSource(t, "notes/source.md", "0123456789")
	projection, err := noteformat.NewProjectionWithFacts(
		"provider-v1", "projection-v1", noteformat.ProjectionStatusCurrent,
		[]noteformat.Diagnostic{
			{Code: "z", Message: "without range"},
			{Code: "b", Category: noteformat.DiagnosticCategoryMetadata, Message: "later code", Range: exactRange(2, 3), AffectedOperation: noteformat.DiagnosticOperationMetadataRead},
			{Code: "a", Category: noteformat.DiagnosticCategoryLink, Message: "first same range", Range: exactRange(2, 4), AffectedOperation: noteformat.DiagnosticOperationLinkResolution},
			{Code: "a", Category: noteformat.DiagnosticCategoryLink, Message: "second same range", Range: exactRange(2, 5), AffectedOperation: noteformat.DiagnosticOperationLinkResolution},
		},
		markdown.New().Descriptor().Capabilities, noteformat.ProjectionFacts{},
	)
	require.NoError(t, err)
	entry, err := projectionEntryFrom(source, projection)
	require.NoError(t, err)
	rows := projectionDiagnosticRows(entry)
	require.Equal(t, []string{"a", "a", "b", "z"}, []string{rows[0].Code, rows[1].Code, rows[2].Code, rows[3].Code})
	require.Equal(t, "first same range", rows[0].Message)
	require.Equal(t, "second same range", rows[1].Message)
	require.Equal(t, "projection", rows[3].Category)
	require.Equal(t, "projection", rows[3].AffectedOperation)
}

func exactRange(start, end int) noteformat.OptionalSourceRange {
	return noteformat.OptionalSourceRange{Present: true, Range: noteformat.SourceRange{StartByte: start, EndByte: end}}
}
