package validate

import (
	"errors"
	"testing"

	"github.com/atomicobject/rhizome/pkg/skilloverlay"
	"github.com/stretchr/testify/require"
)

func TestRunSkillOverlaysUsesCallerFactsWithoutChangingThem(t *testing.T) {
	manifest := skilloverlay.Manifest{
		Entries: []skilloverlay.Entry{{OverlayID: "domain-a"}, {Outcome: "stripped_empty_slot"}},
		Issues: []skilloverlay.Issue{
			{Code: "unknown_slot", Path: "z.yaml", Slot: "missing", Overlay: "domain-a", Skill: "plan", Message: "unknown slot"},
			{Code: "invalid_operation", Path: "a.yaml", Message: "invalid operation"},
		},
	}
	calls := 0
	result := RunSkillOverlays(RunContext{SkillOverlays: func() (skilloverlay.Manifest, error) { calls++; return manifest, nil }})
	require.Equal(t, 1, calls)
	require.False(t, result.OK)
	require.Empty(t, result.Error)
	require.Equal(t, "1 overlay fragment(s) checked", result.Summary)
	require.Equal(t, 2, result.IssueCount)
	require.Equal(t, []Issue{
		{Code: "invalid_operation", Path: "a.yaml", Message: "invalid operation"},
		{Code: "unknown_slot", Path: "z.yaml", Field: "missing", Source: "domain-a", Target: "plan", Message: "unknown slot"},
	}, result.Issues)
	require.Equal(t, "z.yaml", manifest.Issues[0].Path, "validation must not reorder caller-owned facts")
}

func TestRunSkillOverlaysReportsMissingOrFailedComposition(t *testing.T) {
	for _, test := range []struct {
		name     string
		callback func() (skilloverlay.Manifest, error)
		want     string
	}{
		{name: "missing", want: "skill overlay validator is not configured"},
		{name: "bundle failure", callback: func() (skilloverlay.Manifest, error) {
			return skilloverlay.Manifest{}, errors.New("read bundled overlays: denied")
		}, want: "read bundled overlays: denied"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := RunSkillOverlays(RunContext{SkillOverlays: test.callback})
			require.Equal(t, CheckSkillOverlays, result.Name)
			require.False(t, result.OK)
			require.Equal(t, test.want, result.Error)
			require.Empty(t, result.Issues)
		})
	}
}
