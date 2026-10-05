package validate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func runBrokenLinksIn(t *testing.T, files map[string]string) (string, RunContext, CheckResult) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, files))
	runCtx := brokenLinkRunContext(root)
	result := RunBrokenLinks(runCtx, Options{})
	require.Empty(t, result.Error)
	return root, runCtx, result
}

func TestRunBrokenLinksUsesAgentRequiredActionsForMissingFragments(t *testing.T) {
	_, _, result := runBrokenLinksIn(t, map[string]string{
		"docs/target.md": "# Target\n",
		"heading-ref.md": "[[docs/target#Missing]]\n",
		"block-ref.md":   "[[docs/target#^missing]]\n",
	})

	require.Len(t, result.Fixes, 2)
	heading := fixByTarget(t, result.Fixes, "docs/target#Missing")
	block := fixByTarget(t, result.Fixes, "docs/target#^missing")
	for _, fix := range []FixAction{heading, block} {
		require.Equal(t, FixKindReviewBrokenLink, fix.Kind)
		require.Equal(t, FixSafetyAgent, fix.Safety)
		require.Empty(t, fix.Edits)
		require.Equal(t, []string{"docs/target.md"}, fix.CandidatePaths)
		require.Contains(t, fix.Summary, "rzm agent node-link --target")
		require.Contains(t, fix.Summary, "--ensure plan")
	}
	require.Equal(t, []string{"heading-ref.md"}, heading.AffectedPaths)
	require.Contains(t, heading.Summary, "Missing")
	require.Equal(t, []string{"block-ref.md"}, block.AffectedPaths)
	require.Contains(t, block.Summary, "^missing")
}

func TestRunBrokenLinksGroupsByReasonAndFragmentDeterministically(t *testing.T) {
	_, _, result := runBrokenLinksIn(t, map[string]string{
		"docs/semantic-code-index-spine-details.md": "# Semantic code index spine details\n",
		"z.md": "[[semantic code index spine#^missing]]\n",
		"b.md": "[[semantic code index spine#Missing Heading]]\n",
		"a.md": "[[semantic code index spine#Missing Heading]]\n",
	})

	require.Len(t, result.Fixes, 2)
	require.Equal(t, []int{2, 1}, []int{result.Fixes[0].InstanceCount, result.Fixes[1].InstanceCount})
	require.Equal(t, []int{2, 1}, []int{len(result.Fixes[0].IssueKeys), len(result.Fixes[1].IssueKeys)})
	require.NotEqual(t, result.Fixes[0].IssueKeys[0], result.Fixes[0].IssueKeys[1])
	require.Equal(t, []string{"a.md", "b.md"}, result.Fixes[0].AffectedPaths)
	require.Equal(t, []string{"z.md"}, result.Fixes[1].AffectedPaths)
	for _, fix := range result.Fixes {
		// A partial title match is weak evidence: review, never rewrite.
		require.Equal(t, FixSafetyAgent, fix.Safety)
		require.Equal(t, candidateConfidenceLow, fix.Confidence)
		require.Empty(t, fix.Edits)
		require.Contains(t, fix.Summary, "#")
	}
}

func TestRunBrokenLinksClassifiesMissingNoteWithoutUniqueCandidate(t *testing.T) {
	_, _, result := runBrokenLinksIn(t, map[string]string{
		"source.md":                        "[[architecture overview#Missing]]\n[[no candidate]]\n",
		"docs/architecture-overview.md":    "# Architecture overview\n",
		"notes/architecture-overview.md":   "# Architecture overview\n",
		"notes/unrelated-existing-note.md": "# Unrelated\n",
	})

	require.Len(t, result.Fixes, 2)
	ambiguous := fixByTarget(t, result.Fixes, "architecture overview#Missing")
	absent := fixByTarget(t, result.Fixes, "no candidate")
	for _, fix := range []FixAction{ambiguous, absent} {
		require.Equal(t, FixKindReviewBrokenLink, fix.Kind)
		require.Equal(t, FixSafetyAgent, fix.Safety)
		require.Empty(t, fix.Edits)
		require.Equal(t, []string{"source.md"}, fix.AffectedPaths)
		require.Contains(t, fix.Summary, "rzm agent semantic-query --query")
	}
	require.Equal(t, []string{
		"docs/architecture-overview.md",
		"notes/architecture-overview.md",
	}, ambiguous.CandidatePaths)
	require.Contains(t, ambiguous.Summary, "architecture overview#Missing")
	require.Empty(t, absent.CandidatePaths)
}

func TestRunBrokenLinksRetargetPreservesAuthoredFragment(t *testing.T) {
	root, runCtx, result := runBrokenLinksIn(t, map[string]string{
		"source.md": "# Source\n\n[[semantic code index spine details#Missing Heading]]\n",
		"docs/semantic-code-index-spine-details.md": "# Semantic code index spine details\n\n## Missing Heading\n",
	})

	require.Len(t, result.Fixes, 1)
	fix := result.Fixes[0]
	require.Equal(t, FixSafetyConfirm, fix.Safety)
	require.Equal(t, candidateConfidenceHigh, fix.Confidence)
	require.Contains(t, fix.Question, "semantic-code-index-spine-details#Missing Heading")
	applyCheckFixThroughPlan(t, runCtx, result, fix)
	updated, err := os.ReadFile(filepath.Join(root, "source.md"))
	require.NoError(t, err)
	require.Equal(t, "# Source\n\n[[semantic-code-index-spine-details#Missing Heading|semantic code index spine details#Missing Heading]]\n", string(updated))
}

func TestRunBrokenLinksFragmentConfirmationDoesNotRewriteSiblingFragment(t *testing.T) {
	root, runCtx, result := runBrokenLinksIn(t, map[string]string{
		"source.md": "# Source\n\n[[semantic code index spine details#A]]\n[[semantic code index spine details#B]]\n",
		"docs/semantic-code-index-spine-details.md": "# Semantic code index spine details\n\n## A\n\n## B\n",
	})

	require.Len(t, result.Fixes, 2)
	fixA := fixByTarget(t, result.Fixes, "semantic code index spine details#A")
	require.Contains(t, fixA.Question, "#A")
	applyCheckFixThroughPlan(t, runCtx, result, fixA)
	updated, err := os.ReadFile(filepath.Join(root, "source.md"))
	require.NoError(t, err)
	require.Equal(t, "# Source\n\n[[semantic-code-index-spine-details#A|semantic code index spine details#A]]\n[[semantic code index spine details#B]]\n", string(updated))
}

func TestRunBrokenLinksQuotesReviewCommandTarget(t *testing.T) {
	_, _, result := runBrokenLinksIn(t, map[string]string{"source.md": "[[O'Brien note]]\n"})

	require.Len(t, result.Fixes, 1)
	require.Contains(t, result.Fixes[0].Summary, `rzm agent semantic-query --query 'O'"'"'Brien note'`)
}

// The scanner passes a note-catalog read error to the live action builder
// (RunBrokenLinksContext -> buildBrokenLinkFixesFromGroups), and a strong
// candidate equal to the authored target cannot come from a real scan of an
// unresolved link. Both states are therefore driven at the builder directly.
func TestBuildBrokenLinkFixesFromGroupsKeepsUnexecutableGroupsAsAgentReview(t *testing.T) {
	missing := obsidian.BrokenLink{Source: "source.md", Target: "missing", Reason: obsidian.BrokenLinkReasonNoteMissing}
	self := obsidian.BrokenLink{Source: "docs/ref.md", Target: "Architecture Overview", Reason: obsidian.BrokenLinkReasonNoteMissing}
	tests := []struct {
		name         string
		group        brokenLinkGroup
		allNotes     []string
		candidateErr error
		summary      []string
	}{
		{
			name:         "candidate evidence unavailable",
			group:        brokenLinkGroup{key: groupKeyFor(missing), links: []obsidian.BrokenLink{missing}, sources: []string{"source.md"}},
			candidateErr: errors.New("catalog unreadable"),
			summary:      []string{"Candidate evidence is unavailable", "catalog unreadable", "rzm agent semantic-query --query missing"},
		},
		{
			name: "pathless self retarget",
			group: brokenLinkGroup{
				key: groupKeyFor(self), links: []obsidian.BrokenLink{self}, sources: []string{"docs/ref.md"},
				strong: []string{"docs/Architecture Overview.md"},
			},
			allNotes: []string{"docs/Architecture Overview.md"},
			summary:  []string{"would not change the authored target"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixes, err := buildBrokenLinkFixesFromGroups(context.Background(), []brokenLinkGroup{tt.group}, tt.allNotes, tt.candidateErr)
			require.NoError(t, err)
			require.Len(t, fixes, 1)
			require.Equal(t, FixKindReviewBrokenLink, fixes[0].Kind)
			require.Equal(t, FixSafetyAgent, fixes[0].Safety)
			require.Empty(t, fixes[0].Edits, "an unexecutable group must never become an executable edit")
			for _, want := range tt.summary {
				require.Contains(t, fixes[0].Summary, want)
			}

			execution, err := ApplyFixPlan(context.Background(), brokenLinkRunContext(t.TempDir()), &FixPlan{Actions: fixes}, Options{Fix: true, NonInteractive: true})
			require.NoError(t, err)
			require.Equal(t, []string{fixes[0].ID}, execution.Skipped)
			require.Empty(t, execution.Applied)
		})
	}
}

// findCandidateTiers is the live ranking owner used by broken-link grouping.
func TestFindCandidateTiersRanksOnlyPhraseBackedEvidence(t *testing.T) {
	genericTitle := []string{
		"docs/reference/analysis/Graph analysis (wikilinks + communities + authority).md",
		"docs/unrelated.md",
	}
	tests := []struct {
		name   string
		notes  []string
		target string
		exact  []string
		fuzzy  []string
	}{
		{"exact normalized title", []string{"docs/hello.md", "docs/world.md", "specs/hello.md"}, "hello", []string{"docs/hello.md", "specs/hello.md"}, nil},
		{"no match", []string{"docs/hello.md", "docs/world.md"}, "missing", nil, nil},
		{"forward phrase containment", []string{"docs/indexing-pipeline-architecture.md", "docs/unrelated.md"}, "Indexing pipeline architecture (Design)", nil, []string{"docs/indexing-pipeline-architecture.md"}},
		{"reverse phrase containment", []string{"docs/semantic-code-index-spine-details.md", "docs/unrelated.md"}, "semantic code index spine", nil, []string{"docs/semantic-code-index-spine-details.md"}},
		{"generic substring", genericTitle, "wikilinks", nil, nil},
		{"generic substring case insensitive", genericTitle, "Wikilinks", nil, nil},
		{"too short for containment", []string{"docs/note.md", "docs/noted.md"}, "note", []string{"docs/note.md"}, nil},
		{"exact preferred over containment", []string{"docs/Hello World.md", "docs/Hello World Extended.md"}, "Hello World", []string{"docs/Hello World.md"}, nil},
		{"phrase-backed fuzzy only", []string{"docs/semantic-code-index-spine-details.md", "docs/semantic-code-index-spine-current-state.md"}, "semantic code index spine current", nil, []string{"docs/semantic-code-index-spine-current-state.md"}},
		{"shared tokens without phrase", []string{"docs/specs/technical/semantic-code-index-spine.md", "docs/unrelated.md"}, "Semantic Code Index (Design)", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exact, fuzzy, err := newBrokenLinkMatcherIndex(tt.notes).findCandidateTiers(context.Background(), tt.target)
			require.NoError(t, err)
			require.Equal(t, tt.exact, nilIfEmpty(exact))
			require.Equal(t, tt.fuzzy, nilIfEmpty(fuzzy))
		})
	}
}

func nilIfEmpty(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return values
}
