package validate

import (
	"encoding/json"
	"fmt"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

func TestBuildDiagnosticSnapshotUsesCompleteIssueAndExactActionMembership(t *testing.T) {
	issues := make([]Issue, 0, 520)
	issueKeys := make([]string, 0, 520)
	for i := 0; i < 520; i++ {
		key := fmt.Sprintf("issue-%03d", i)
		issueKeys = append(issueKeys, key)
		issues = append(issues, Issue{
			Key:               key,
			Code:              "missing_type",
			Path:              fmt.Sprintf("notes/%03d.md", i),
			AffectedPaths:     []string{fmt.Sprintf("sources/%03d.json", i)},
			AffectedNotePaths: []string{fmt.Sprintf("notes/%03d.md", i)},
			Message:           "type is missing",
		})
	}
	visible := append([]Issue(nil), issues[:20]...)
	result := Result{
		SelectedChecks: []string{CheckOntology, CheckViews},
		IssueCount:     len(issues),
		ErrorCount:     1,
		Checks: []CheckResult{
			{Name: CheckOntology, IssueCount: len(issues), Issues: visible, fullIssues: issues, DurationMs: 12},
			{Name: CheckViews, Error: "schema unavailable", DurationMs: 3},
		},
		FixPlan: &FixPlan{
			Fingerprint: "public-plan-fingerprint",
			Actions: []FixAction{{
				ID: "action-many", Check: CheckOntology, IssueCode: "missing_type",
				Kind: FixKindSetFrontmatter, Safety: FixSafetyConfirm, Title: "Set types",
				IssueKeys: issueKeys, AffectedPaths: []string{"notes/000.md", "notes/519.md"},
				OperationIDs: []string{"private-operation"}, Edits: []FixEdit{{Kind: FixKindSetFrontmatter, NotePath: "notes/000.md"}},
			}},
			Operations: []RepairOperation{{ID: "private-operation"}},
		},
	}

	snapshot, err := BuildDiagnosticSnapshot(result, DiagnosticSnapshotContext{
		VaultIdentity: "vault-identity", Scope: "default", Generation: 42,
		SchemaIdentity: "schema", ConfigIdentity: "config", InputRevision: "input",
		StartedAt: 10, FinishedAt: 20,
	})
	require.NoError(t, err)
	require.Equal(t, 520, snapshot.IssueCount)
	require.Equal(t, 1040, snapshot.AffectedFileCount)
	require.Equal(t, 520, snapshot.AffectedNoteCount)
	require.Len(t, snapshot.Diagnostics, 520)
	require.Len(t, snapshot.Actions, 1)
	require.Len(t, snapshot.Actions[0].IssueKeys, 520)
	require.Equal(t, []string{"notes/000.md", "notes/519.md"}, snapshot.Actions[0].AffectedPaths)
	require.Equal(t, "failed", snapshot.Checks[1].Outcome)
	require.Equal(t, "schema unavailable", snapshot.Checks[1].Error)

	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-operation")
	require.NotContains(t, string(encoded), `"edits"`)
	require.Equal(t, semdb.ValidationCompletionComplete, snapshot.Completion)
}

func TestBuildDiagnosticSnapshotKeepsExplicitMultiPathIssueWithoutAction(t *testing.T) {
	issue := Issue{
		Key: "multi", Code: "cross_file", Path: "notes/a.md", Message: "cross-file problem",
		AffectedPaths: []string{"notes/b.md", "config/schema.yml", "notes/a.md"},
		Location:      &IssueLocation{Unit: LocationUnitUTF8Bytes, Start: 7, End: 11, NodeID: "node-a", Field: "owner"},
	}
	result := Result{
		SelectedChecks: []string{CheckOntology}, IssueCount: 1,
		Checks: []CheckResult{{Name: CheckOntology, IssueCount: 1, Issues: []Issue{issue}, fullIssues: []Issue{issue}}},
	}

	snapshot, err := BuildDiagnosticSnapshot(result, DiagnosticSnapshotContext{VaultIdentity: "vault", Scope: "default", Generation: 1})
	require.NoError(t, err)
	require.Len(t, snapshot.Diagnostics, 1)
	require.Equal(t, []string{"config/schema.yml", "notes/a.md", "notes/b.md"}, snapshot.Diagnostics[0].AffectedPaths)
	require.Empty(t, snapshot.Diagnostics[0].ActionIDs)
	require.Equal(t, semdb.ValidationLocationUnitUTF8Bytes, snapshot.Diagnostics[0].Location.Unit)
}

func TestBuildDiagnosticSnapshotPreservesEmbeddedOntologyIssueIdentity(t *testing.T) {
	issue := ontologyValidationIssue(ontology.ValidationIssue{
		Code:      "field_type_mismatch",
		NotePath:  "notes/daily.md",
		TypeName:  "ActionItem",
		FieldName: "priority",
		NodeRef:   "notes/daily.md#^task-2",
		NodeID:    "notes/daily.md#^task-2",
		Line:      7,
		Message:   "priority has an invalid value",
	})
	require.Equal(t, []string{"notes/daily.md#^task-2"}, issue.AffectedNodeIDs)
	require.Equal(t, &IssueLocation{
		Unit: LocationUnitLine, Start: 7, End: 7,
		NodeID: "notes/daily.md#^task-2", Field: "priority",
	}, issue.Location)
	issueKey, err := StableIssueKey(CheckOntology, issue)
	require.NoError(t, err)
	issue.Key = issueKey
	result := Result{
		SelectedChecks: []string{CheckOntology}, IssueCount: 1,
		Checks: []CheckResult{{Name: CheckOntology, IssueCount: 1, Issues: []Issue{issue}, fullIssues: []Issue{issue}}},
	}

	snapshot, err := BuildDiagnosticSnapshot(result, DiagnosticSnapshotContext{VaultIdentity: "vault", Generation: 1})
	require.NoError(t, err)
	require.Len(t, snapshot.Diagnostics, 1)
	diagnostic := snapshot.Diagnostics[0]
	require.Equal(t, []string{"notes/daily.md#^task-2"}, diagnostic.AffectedNodeIDs)
	require.Equal(t, &semdb.ValidationDiagnosticLocation{
		Unit: semdb.ValidationLocationUnitLine, Start: 7, End: 7,
		NodeID: "notes/daily.md#^task-2", Field: "priority",
	}, diagnostic.Location)

	lineFreeIssue := ontologyValidationIssue(ontology.ValidationIssue{
		Code: "title_pattern_mismatch", NotePath: "notes/daily.md", TypeName: "ActionItem",
		NodeRef: "notes/daily.md#^task-2", NodeID: "notes/daily.md#^task-2",
		Message: "title does not match the required pattern",
	})
	lineFreeIssueKey, err := StableIssueKey(CheckOntology, lineFreeIssue)
	require.NoError(t, err)
	lineFreeIssue.Key = lineFreeIssueKey
	lineFreeResult := Result{
		SelectedChecks: []string{CheckOntology}, IssueCount: 1,
		Checks: []CheckResult{{Name: CheckOntology, IssueCount: 1, Issues: []Issue{lineFreeIssue}, fullIssues: []Issue{lineFreeIssue}}},
	}
	lineFreeSnapshot, err := BuildDiagnosticSnapshot(lineFreeResult, DiagnosticSnapshotContext{VaultIdentity: "vault", Generation: 1})
	require.NoError(t, err)
	require.Equal(t, []string{"notes/daily.md#^task-2"}, lineFreeSnapshot.Diagnostics[0].AffectedNodeIDs)
	require.Nil(t, lineFreeSnapshot.Diagnostics[0].Location)
}

func TestBuildDiagnosticSnapshotDerivesNoteMembershipFromProductionInventory(t *testing.T) {
	issue := Issue{
		Key: "untyped", Code: "broken_note_link", Path: "notes/untyped.md",
		AffectedPaths: []string{"config/schema.yml"},
		// This producer hint is intentionally wrong. The indexed inventory owns
		// the note/file distinction used for aggregate and scoped reads.
		AffectedNotePaths: []string{"config/schema.yml"},
	}
	result := Result{
		SelectedChecks: []string{CheckBrokenLinks}, IssueCount: 1,
		Checks: []CheckResult{{Name: CheckBrokenLinks, IssueCount: 1, Issues: []Issue{issue}, fullIssues: []Issue{issue}}},
	}

	snapshot, err := BuildDiagnosticSnapshot(result, DiagnosticSnapshotContext{
		VaultIdentity: "vault", Generation: 1, NotePaths: []string{"notes/untyped.md"},
	})
	require.NoError(t, err)
	require.Equal(t, 2, snapshot.AffectedFileCount)
	require.Equal(t, 1, snapshot.AffectedNoteCount)
	require.Equal(t, []string{"notes/untyped.md"}, snapshot.Diagnostics[0].AffectedNotePaths)
}

func TestBuildDiagnosticSnapshotMergesDuplicateIssueKeys(t *testing.T) {
	result := Result{
		SelectedChecks: []string{CheckOntology},
		IssueCount:     2,
		Checks: []CheckResult{{Name: CheckOntology, IssueCount: 2, DurationMs: 1, Issues: []Issue{
			{Key: "shared", Code: "missing_type", Path: "notes/a.md", AffectedNotePaths: []string{"notes/a.md"}, Message: "type is missing"},
			{Key: "shared", Code: "missing_type", Path: "notes/b.md", AffectedNotePaths: []string{"notes/b.md"}, Message: "type is missing"},
		}}},
	}
	snapshot, err := BuildDiagnosticSnapshot(result, DiagnosticSnapshotContext{VaultIdentity: "vault", Scope: "default", Generation: 1, StartedAt: 1, FinishedAt: 2})
	require.NoError(t, err)
	require.Len(t, snapshot.Diagnostics, 1)
	require.Equal(t, 1, snapshot.IssueCount)
	require.Equal(t, []string{"notes/a.md", "notes/b.md"}, snapshot.Diagnostics[0].AffectedNotePaths)
	require.Equal(t, 2, snapshot.AffectedNoteCount)
}
