package views

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/stretchr/testify/require"
)

func TestGeneratedDatedViewGroupsByMonthNewestFirst(t *testing.T) {
	service, _ := newTypeViewsService(t, map[string]string{
		"meetings/kickoff.md": "---\nheld: 2026-08-14\n---\n# Kickoff\n",
		"meetings/review.md":  "---\nheld: 2026-09-02\n---\n# Review\n",
		"meetings/retro.md":   "---\nheld: 2026-09-30\n---\n# Retro\n",
		"meetings/adhoc.md":   "---\ntitle: Ad hoc\n---\n# Ad hoc\n",
	})

	resp, err := service.Execute(context.Background(), viewconfig.GeneratedTypeID("Meeting"), ExecuteRequest{})
	require.NoError(t, err)

	type group struct{ Value, Label string }
	var groups []group
	for _, item := range resp.Groups {
		groups = append(groups, group{item.Value, item.Label})
	}
	require.Equal(t, []group{{"2026-09", "Sep 2026"}, {"2026-08", "Aug 2026"}, {"", "No held"}}, groups)
	require.Equal(t, []string{"Retro", "Review", "Kickoff", "Ad hoc"}, rowTitles(resp.Rows), "primary date descending within months")
}

func TestMonthBucketsRejectNonDateFields(t *testing.T) {
	service, _ := newTypeViewsService(t, map[string]string{"tasks/a.md": "---\nstatus: doing\n---\n# A\n"})
	for _, group := range []*viewconfig.GroupSpec{
		{Field: "status", Bucket: viewconfig.GroupBucketMonth},
		{Field: "due", Bucket: "week"},
	} {
		_, err := service.Execute(context.Background(), viewconfig.GeneratedTypeID("Task"), ExecuteRequest{Group: group})
		require.ErrorIs(t, err, ErrInvalidRequest)
	}
	resp, err := service.Execute(context.Background(), viewconfig.GeneratedTypeID("Task"), ExecuteRequest{Group: &viewconfig.GroupSpec{Field: "due", Bucket: viewconfig.GroupBucketMonth}})
	require.NoError(t, err)
	require.Equal(t, "No due", resp.Groups[0].Label)
}

func TestMonthBucketsReadDateTimeInItsOffset(t *testing.T) {
	rows := []TableRow{
		{Title: "late", Fields: map[string]any{"at": "2026-09-30T23:30:00-05:00"}},
		{Title: "early", Fields: map[string]any{"at": "2026-10-01T01:00:00+02:00"}},
	}
	buckets := monthBuckets(rows, "at", FieldCapability{Key: "at"})
	require.Equal(t, "2026-10", buckets[0].value)
	require.Equal(t, "Oct 2026", buckets[0].label)
	require.Equal(t, "2026-09", buckets[1].value)
}
