package ontology

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func sourceSpanIdentityResolver(content string) (*projectionResolver, []*MarkdownSourceSpan) {
	const notePath = "docs/spec.md"
	snapshot, err := BuildDocumentSnapshot(notePath, content, time.Time{})
	if err != nil {
		panic(err)
	}
	return &projectionResolver{
		snapshot:        snapshot,
		schema:          &Schema{},
		sectionTypeByID: map[string]string{},
		parentIDByID:    sourceSpanParents(snapshot.SourceSpans),
	}, snapshot.SourceSpans
}

func sourceSpanParents(spans []*MarkdownSourceSpan) map[string]string {
	parents := make(map[string]string, len(spans))
	for _, span := range spans {
		parents[span.ID] = span.ParentID
	}
	return parents
}

func sourceSpanByTitle(t *testing.T, spans []*MarkdownSourceSpan, title string) *MarkdownSourceSpan {
	t.Helper()
	for _, span := range spans {
		if span.Title == title {
			return span
		}
	}
	t.Fatalf("source span %q not found", title)
	return nil
}

func TestSourceSpanStructuralFingerprintIgnoresParentNarrativeEdits(t *testing.T) {
	baseResolver, baseSpans := sourceSpanIdentityResolver("# Spec\n\n## Acceptance Criteria\n\nContext before.\n\n- Target criterion\n")
	base := baseResolver.sourceSpanNodeRef(sourceSpanByTitle(t, baseSpans, "Target criterion"))

	updatedResolver, updatedSpans := sourceSpanIdentityResolver("# Spec\n\n## Acceptance Criteria\n\nChanged narrative before the list.\n\n- Target criterion\n")
	updated := updatedResolver.sourceSpanNodeRef(sourceSpanByTitle(t, updatedSpans, "Target criterion"))

	require.Equal(t, base.Structural, updated.Structural)
}

func TestSourceSpanStructuralFingerprintUsesDurableParentAnchor(t *testing.T) {
	baseResolver, baseSpans := sourceSpanIdentityResolver("## Acceptance Criteria\n^criteria\n\n- Target criterion\n")
	base := baseResolver.sourceSpanNodeRef(sourceSpanByTitle(t, baseSpans, "Target criterion"))

	renamedResolver, renamedSpans := sourceSpanIdentityResolver("## Renamed Criteria\n^criteria\n\n- Target criterion\n")
	renamed := renamedResolver.sourceSpanNodeRef(sourceSpanByTitle(t, renamedSpans, "Target criterion"))
	require.Equal(t, base.Structural, renamed.Structural)
}

func TestSourceSpanStructuralFingerprintChangesWhenParentAnchorIsAdded(t *testing.T) {
	baseResolver, baseSpans := sourceSpanIdentityResolver("## Acceptance Criteria\n\n- Target criterion\n")
	base := baseResolver.sourceSpanNodeRef(sourceSpanByTitle(t, baseSpans, "Target criterion"))

	anchoredResolver, anchoredSpans := sourceSpanIdentityResolver("## Acceptance Criteria\n^criteria\n\n- Target criterion\n")
	anchored := anchoredResolver.sourceSpanNodeRef(sourceSpanByTitle(t, anchoredSpans, "Target criterion"))
	require.NotEqual(t, base.Structural, anchored.Structural)
}

func TestSourceSpanStructuralFingerprintFollowsOwnedContent(t *testing.T) {
	baseResolver, baseSpans := sourceSpanIdentityResolver("- First criterion.\n- Target criterion!\n")
	base := baseResolver.sourceSpanNodeRef(sourceSpanByTitle(t, baseSpans, "Target criterion!"))

	insertedResolver, insertedSpans := sourceSpanIdentityResolver("Preface changed.\n\n- First criterion.\n- Distinct criterion.\n- Target criterion!\n")
	inserted := insertedResolver.sourceSpanNodeRef(sourceSpanByTitle(t, insertedSpans, "Target criterion!"))
	require.Equal(t, base.Structural, inserted.Structural)
	require.NotEqual(t, base.NodeID, inserted.NodeID)

	punctuationResolver, punctuationSpans := sourceSpanIdentityResolver("- First criterion.\n- Target criterion?\n")
	punctuation := punctuationResolver.sourceSpanNodeRef(sourceSpanByTitle(t, punctuationSpans, "Target criterion?"))
	require.NotEqual(t, base.Structural, punctuation.Structural)
}

func TestSourceSpanStructuralFingerprintIncludesDetailButExcludesChildren(t *testing.T) {
	baseResolver, baseSpans := sourceSpanIdentityResolver("- **Criterion**: Summary\n  First detail.\n  - Nested child\n")
	base := baseResolver.sourceSpanNodeRef(sourceSpanByTitle(t, baseSpans, "Criterion"))

	detailResolver, detailSpans := sourceSpanIdentityResolver("- **Criterion**: Summary\n  Changed detail.\n  - Nested child\n")
	detail := detailResolver.sourceSpanNodeRef(sourceSpanByTitle(t, detailSpans, "Criterion"))
	require.NotEqual(t, base.Structural, detail.Structural)

	childResolver, childSpans := sourceSpanIdentityResolver("- **Criterion**: Summary\n  First detail.\n  - Changed nested child\n")
	child := childResolver.sourceSpanNodeRef(sourceSpanByTitle(t, childSpans, "Criterion"))
	require.Equal(t, base.Structural, child.Structural)

	childMarkerResolver, childMarkerSpans := sourceSpanIdentityResolver("- **Criterion**: Summary\n  First detail.\n  - Nested child #changed\n")
	childMarker := childMarkerResolver.sourceSpanNodeRef(sourceSpanByTitle(t, childMarkerSpans, "Criterion"))
	require.Equal(t, base.Structural, childMarker.Structural)
}

func TestSourceSpanStructuralFingerprintIncludesOwnedInlineFields(t *testing.T) {
	todoResolver, todoSpans := sourceSpanIdentityResolver("- Target criterion #criterion status:: todo\n  assignee:: Alice\n")
	todo := todoResolver.sourceSpanNodeRef(sourceSpanByTitle(t, todoSpans, "Target criterion"))

	doneResolver, doneSpans := sourceSpanIdentityResolver("- Target criterion #criterion status:: done\n  assignee:: Alice\n")
	done := doneResolver.sourceSpanNodeRef(sourceSpanByTitle(t, doneSpans, "Target criterion"))

	require.NotEqual(t, todo.Structural, done.Structural)

	bobResolver, bobSpans := sourceSpanIdentityResolver("- Target criterion #criterion status:: todo\n  assignee:: Bob\n")
	bob := bobResolver.sourceSpanNodeRef(sourceSpanByTitle(t, bobSpans, "Target criterion"))
	require.NotEqual(t, todo.Structural, bob.Structural)
}

func TestResolveSourceSpanRefRejectsIdenticalUnanchoredContent(t *testing.T) {
	resolver, spans := sourceSpanIdentityResolver("- Same criterion.\n- Same criterion.\n")
	ref := resolver.sourceSpanNodeRef(spans[0])

	_, _, ok, err := resolver.resolveSourceSpanRef(ref)
	require.False(t, ok)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrAmbiguousSourceSpanIdentity)
	require.True(t, strings.Contains(err.Error(), "matches 2 unanchored items"), err.Error())
}

func TestResolveSourceSpanRefPrefersContentIdentityOverReusedOffset(t *testing.T) {
	baseResolver, baseSpans := sourceSpanIdentityResolver("- First item\n- Target item\n")
	base := baseResolver.sourceSpanNodeRef(sourceSpanByTitle(t, baseSpans, "Target item"))

	updatedResolver, updatedSpans := sourceSpanIdentityResolver("- First item\n- Other! item\n- Target item\n")
	reusedOffset := updatedResolver.snapshot.SourceSpansByID[base.NodeID]
	require.NotNil(t, reusedOffset)
	require.Equal(t, "Other! item", reusedOffset.Title)

	resolved, resolvedRef, ok, err := updatedResolver.resolveSourceSpanRef(base)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "Target item", resolved.Title)
	require.Equal(t, sourceSpanByTitle(t, updatedSpans, "Target item").ID, resolvedRef.NodeID)
}

func TestSourceSpanUnanchoredIdentitySurvivesBlockIDInsertion(t *testing.T) {
	baseResolver, baseSpans := sourceSpanIdentityResolver("## Action Items\n\n- [ ] Update docs #action-item\n")
	baseSpan := sourceSpanByTitle(t, baseSpans, "Update docs")
	base := hashText(baseResolver.stableSourceSpanFingerprint(baseSpan))

	updatedResolver, updatedSpans := sourceSpanIdentityResolver("## Action Items\n\n- [ ] Update docs #action-item ^ai-1\n")
	updatedSpan := sourceSpanByTitle(t, updatedSpans, "Update docs")
	updated := hashText(updatedResolver.unanchoredSourceSpanFingerprint(updatedSpan))
	require.Equal(t, base, updated)
}
