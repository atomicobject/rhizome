package validate

import (
	"encoding/json"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/stretchr/testify/require"
)

func TestOntologyFindingsCarryOneVariantPerFamily(t *testing.T) {
	_, _, result := runPreparedOntologySuite(t, map[string]string{
		".rhizome/ontology/schema.graphql": `
type Person @node(paths: ["people/*.md"]) {
  name: String
}

type Topic @node(paths: ["topics/*.md"]) {
  name: String
}

type Meeting @node(matches: ["classification:meeting"]) @title(pattern: "^MTG") {
  owner: Person! @link
}

type CoachingSession @node(matches: ["classification:meeting"]) {
  name: String
}
`,
		"people/ana.md":          "---\nname: Ana\n---\n# Ana\n",
		"topics/alpha.md":        "---\ntype: Person\n---\n# Alpha\n",
		"notes/ambiguous.md":     "---\nclassification: meeting\n---\n# MTG ambiguous\n",
		"notes/ghost.md":         "---\ntype: Ghost\n---\n# Ghost\n",
		"notes/missing-owner.md": "---\ntype: Meeting\nclassification: meeting\n---\n# MTG missing owner\n",
		"notes/wrong-owner.md":   "---\ntype: Meeting\nclassification: meeting\nowner: \"[[topics/alpha]]\"\n---\n# MTG wrong owner\n",
		"notes/standup.md":       "---\ntype: Meeting\nclassification: meeting\nowner: \"[[people/ana]]\"\n---\n# Standup\n",
	})
	want := map[string]IssueVariant{
		"type_ambiguous":         {Key: "CoachingSession+Meeting", Label: "CoachingSession, Meeting"},
		"declared_type_mismatch": {Key: "Person->Topic", Label: "Person → Topic"},
		"unknown_declared_type":  {Key: "Ghost", Label: "Ghost"},
		"missing_required_field": {Key: "Meeting.owner", Label: "Meeting · owner"},
		"wrong_target_type":      {Key: "Meeting.owner->Person", Label: "Meeting · owner → Person"},
		"title_pattern_mismatch": {Key: "Meeting", Label: "Meeting"},
	}
	ontologyIssues := result.Checks[0].fullIssues
	require.Equal(t, CheckOntology, result.Checks[0].Name)
	seen := map[string]bool{}
	for _, issue := range ontologyIssues {
		variant, ok := want[issue.Code]
		require.True(t, ok, "unexpected ontology finding %s: %s", issue.Code, issue.Message)
		require.Equal(t, &variant, issue.Variant, issue.Code)
		seen[issue.Code] = true
		if issue.Code == "type_ambiguous" {
			var data OntologyIssueData
			require.NoError(t, json.Unmarshal(issue.Data, &data))
			require.Equal(t, []string{"CoachingSession", "Meeting"}, data.CandidateTypes)
		}
	}
	require.Len(t, seen, len(want), "every family in the fixture produced a finding")

	snapshot, err := BuildDiagnosticSnapshot(result, DiagnosticSnapshotContext{VaultIdentity: "vault", Generation: 1})
	require.NoError(t, err)
	for _, diagnostic := range snapshot.Diagnostics {
		variant := want[diagnostic.Code]
		require.Equal(t, &semdb.ValidationIssueVariant{Key: variant.Key, Label: variant.Label}, diagnostic.Variant, diagnostic.Code)
	}
}

func TestBrokenLinkVariantIsTheMissingTarget(t *testing.T) {
	result := runBrokenLinksFixture(t, map[string]string{
		"real.md":   "# Real\n\n## Present\n",
		"source.md": "See [[Ghost]], [[Ghost#Intro]], and [[Real#Missing]].\n",
	}, 20)
	got := map[string][]string{}
	for _, issue := range result.Issues {
		require.NotNil(t, issue.Variant, issue.Code)
		require.Equal(t, issue.Variant.Key, issue.Variant.Label)
		got[issue.Code] = append(got[issue.Code], issue.Variant.Key)
	}
	require.Equal(t, map[string][]string{
		IssueCodeBrokenNoteLink:    {"Ghost", "Ghost"},
		IssueCodeBrokenHeadingLink: {"Real#Missing"},
	}, got)
}

func TestStableIssueKeyExcludesVariant(t *testing.T) {
	issue := Issue{Code: "type_ambiguous", Path: "notes/a.md", Message: "ambiguous"}
	without, err := StableIssueKey(CheckOntology, issue)
	require.NoError(t, err)
	for _, variant := range []*IssueVariant{
		{Key: "A+B", Label: "A, B"},
		{Key: "C+D", Label: "C, D"},
	} {
		issue.Variant = variant
		key, err := StableIssueKey(CheckOntology, issue)
		require.NoError(t, err)
		require.Equal(t, without, key)
	}
	issue.Path = "notes/b.md"
	control, err := StableIssueKey(CheckOntology, issue)
	require.NoError(t, err)
	require.NotEqual(t, without, control, "identity fields still distinguish issues")
}
