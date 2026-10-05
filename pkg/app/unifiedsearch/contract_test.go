package unifiedsearch

import (
	"errors"
	"testing"
	"time"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/stretchr/testify/require"
)

func TestResolveEffectivePolicy(t *testing.T) {
	tests := []struct {
		name    string
		profile Profile
		intent  search.Intent
		limit   int
		want    EffectivePolicy
	}{
		{name: "interactive defaults", profile: ProfileInteractive, intent: search.IntentSearch, want: EffectivePolicy{Profile: ProfileInteractive, VisibleLimit: 10, CandidateWindow: 100, MaxPerOwner: 2, BudgetChars: 8_000}},
		{name: "agent defaults", profile: ProfileAgent, intent: search.IntentSearch, want: EffectivePolicy{Profile: ProfileAgent, VisibleLimit: 20, CandidateWindow: 240, MaxPerOwner: 3, BudgetChars: 24_000}},
		{name: "precision wins over profile diversity", profile: ProfileAgent, intent: search.IntentGoToDef, want: EffectivePolicy{Profile: ProfileAgent, VisibleLimit: 20, CandidateWindow: 100, MaxPerOwner: 1, BudgetChars: 24_000}},
		{name: "explicit limit wins", profile: ProfileInteractive, intent: search.IntentSearch, limit: 25, want: EffectivePolicy{Profile: ProfileInteractive, VisibleLimit: 25, CandidateWindow: 100, MaxPerOwner: 2, BudgetChars: 8_000}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveEffectivePolicy(tt.profile, tt.intent, tt.limit, 0, 0)
			require.NoError(t, err)
			require.Equal(t, tt.want.Profile, got.Profile)
			require.Equal(t, tt.want.VisibleLimit, got.VisibleLimit)
			require.Equal(t, tt.want.CandidateWindow, got.CandidateWindow)
			require.Equal(t, tt.want.MaxPerOwner, got.MaxPerOwner)
			require.Equal(t, tt.want.BudgetChars, got.BudgetChars)
			if got.Profile == ProfileAgent {
				require.Equal(t, 8*time.Second, got.Deadline)
			} else {
				require.Equal(t, 3*time.Second, got.Deadline)
			}
		})
	}
}

func TestContinuationBindsRequestAndWindow(t *testing.T) {
	cursor := ContinuationCursor{RequestIdentity: "request", IndexGeneration: "generation", WindowDigest: "window", CandidateWindow: 100, Offset: 20}
	token, err := EncodeContinuation(cursor)
	require.NoError(t, err)
	decoded, err := DecodeContinuation(token)
	require.NoError(t, err)
	require.NoError(t, ValidateContinuation(decoded, "request", "generation", "window", 100))
	require.ErrorIs(t, ValidateContinuation(decoded, "other", "generation", "window", 100), ErrCursorInvalid)
	require.ErrorIs(t, ValidateContinuation(decoded, "request", "new-generation", "window", 100), ErrCursorStale)
	require.ErrorIs(t, ValidateContinuation(decoded, "request", "generation", "window", 240), ErrCursorInvalid)
}

func TestContinuationRejectsLegacyVersion(t *testing.T) {
	_, err := DecodeContinuation("eyJ2ZXJzaW9uIjoxfQ")
	require.True(t, errors.Is(err, ErrCursorRefreshRequired) || errors.Is(err, ErrCursorInvalid))
}

func TestRequestIdentityUsesMembershipControlsOnly(t *testing.T) {
	base := EffectiveRequest{Queries: []QueryInput{{Text: "definition", Mode: "go_to_def"}, {Text: "tests", Mode: "tests_for_code"}}, Intent: search.IntentSearch, Profile: ProfileAgent, Filters: search.Filters{Inputs: []actions.ListInput{{Type: actions.InputTypeFind, Value: "query"}}, Types: []string{"code", "note"}, PathPrefixes: []string{"pkg/b", "pkg/a"}, NoteTypes: []string{"Spec", "Decision"}}, Policy: EffectivePolicy{Profile: ProfileAgent, VisibleLimit: 20, CandidateWindow: 240, MaxPerOwner: 3, BudgetChars: 24_000}}
	first, err := RequestIdentity(base, "vault")
	require.NoError(t, err)
	presentationChanged := base
	presentationChanged.Policy.VisibleLimit = 5
	presentationChanged.Policy.BudgetChars = 100
	presentationChanged.Filters.Types = []string{"note", "code", "code"}
	presentationChanged.Filters.PathPrefixes = []string{"pkg/a", "pkg/b"}
	presentationChanged.Filters.NoteTypes = []string{"decision", "spec"}
	presentationChanged.Filters.Inputs = []actions.ListInput{{Type: actions.InputTypeFind, Value: " query "}, {Type: actions.InputTypeFind, Value: "query"}}
	second, err := RequestIdentity(presentationChanged, "vault")
	require.NoError(t, err)
	require.Equal(t, first, second)

	facetChanged := base
	facetChanged.Queries = append([]QueryInput(nil), base.Queries...)
	facetChanged.Queries[1].Mode = "search"
	third, err := RequestIdentity(facetChanged, "vault")
	require.NoError(t, err)
	require.NotEqual(t, first, third)

	caseSensitivePath := base
	caseSensitivePath.Filters.PathPrefixes = []string{"Pkg/A", "pkg/b"}
	fourth, err := RequestIdentity(caseSensitivePath, "vault")
	require.NoError(t, err)
	require.NotEqual(t, first, fourth, "path filters use literal case-sensitive semantics")

	testEligibilityChanged := base
	testEligibilityChanged.Filters.ExcludeTests = true
	fifth, err := RequestIdentity(testEligibilityChanged, "vault")
	require.NoError(t, err)
	require.NotEqual(t, first, fifth, "test eligibility changes search membership")
}
