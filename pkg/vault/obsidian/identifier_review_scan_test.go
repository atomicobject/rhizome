package obsidian

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScanIdentifierReviewCandidatesIncludesProseAndAllCodeFormsButExcludesStructuredRanges(t *testing.T) {
	content := strings.Join([]string{
		"Prose SPEC-0001 and XSPEC-0001 and SPEC-0001X.",
		"Inline `spec-0001`.",
		"```go",
		"const id = \"SPEC-0001\"",
		"```",
		"    SPEC-0001",
		"\tSPEC-0001",
		"Wiki [[target|SPEC-0001]].",
		"Markdown [SPEC-0001](target.md).",
		"id:: SPEC-0001",
	}, "\n")
	snapshot := ScanStructuredLinkSnapshot(content)
	fieldStart := strings.LastIndex(content, "SPEC-0001")
	handled := []StructuredLinkSpan{linkSpan(fieldStart, fieldStart+len("SPEC-0001"))}

	candidates, err := ScanIdentifierReviewCandidates(content, snapshot, []string{"SPEC-0001"}, handled)
	require.NoError(t, err)
	require.Len(t, candidates, 5)
	require.Equal(t, []IdentifierReviewRegion{
		IdentifierReviewProse,
		IdentifierReviewCode,
		IdentifierReviewCode,
		IdentifierReviewCode,
		IdentifierReviewCode,
	}, reviewCandidateRegions(candidates))
	require.Equal(t, []string{"SPEC-0001", "spec-0001", "SPEC-0001", "SPEC-0001", "SPEC-0001"}, reviewCandidateValues(candidates))
	wantStarts := []int{
		strings.Index(content, "Prose SPEC-0001") + len("Prose "),
		strings.Index(content, "`spec-0001`") + 1,
		strings.Index(content, "const id = \"SPEC-0001\"") + len("const id = \""),
		strings.Index(content, "    SPEC-0001") + 4,
		strings.Index(content, "\tSPEC-0001") + 1,
	}
	for i, candidate := range candidates {
		require.Equal(t, wantStarts[i], candidate.Span.Start)
		require.Equal(t, wantStarts[i]+len(candidate.Value), candidate.Span.End)
		require.Equal(t, candidate.Value, content[candidate.Span.Start:candidate.Span.End])
	}
}

func TestScanIdentifierReviewCandidatesRejectsUnsealedOrStaleSnapshotAndInvalidHandledRange(t *testing.T) {
	content := "`SPEC-0001`"
	snapshot := ScanStructuredLinkSnapshot(content)

	_, err := ScanIdentifierReviewCandidates(content, StructuredLinkScanSnapshot{}, []string{"SPEC-0001"}, nil)
	require.ErrorContains(t, err, "sealed")
	_, err = ScanIdentifierReviewCandidates(content+" drift", snapshot, []string{"SPEC-0001"}, nil)
	require.ErrorContains(t, err, "source")
	_, err = ScanIdentifierReviewCandidates(content, snapshot, []string{"SPEC-0001"}, []StructuredLinkSpan{linkSpan(-1, 2)})
	require.ErrorContains(t, err, "handled range")

	snapshot.protected[0].Start++
	_, err = ScanIdentifierReviewCandidates(content, snapshot, []string{"SPEC-0001"}, nil)
	require.ErrorContains(t, err, "changed")
}

func TestIdentifierReviewScannerWorkIsBoundedBySourceSpansAndMatches(t *testing.T) {
	var content strings.Builder
	for index := 0; index < 2_000; index++ {
		content.WriteString("`noise` prose ")
		if index%100 == 0 {
			content.WriteString("SPEC-0001 ")
		}
	}
	source := content.String()
	snapshot := ScanStructuredLinkSnapshot(source)
	candidates, stats, err := scanIdentifierReviewCandidates(source, snapshot, []string{"SPEC-0001", "SPEC-0002", "SPEC-0003"}, nil)
	require.NoError(t, err)
	require.Len(t, candidates, 20)
	require.LessOrEqual(t, stats.FailureSteps, stats.RuneSteps)
	require.LessOrEqual(t, stats.ExcludedSpanSteps+stats.ProtectedSpanSteps, len(snapshot.Links)+len(snapshot.protected)*2+len(candidates)+2)
}

func reviewCandidateRegions(input []IdentifierReviewCandidate) []IdentifierReviewRegion {
	out := make([]IdentifierReviewRegion, len(input))
	for index := range input {
		out[index] = input[index].Region
	}
	return out
}

func reviewCandidateValues(input []IdentifierReviewCandidate) []string {
	out := make([]string, len(input))
	for index := range input {
		out[index] = input[index].Value
	}
	return out
}
