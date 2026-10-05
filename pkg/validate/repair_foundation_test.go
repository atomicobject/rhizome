package validate

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStableIssueKeyIgnoresJSONObjectOrdering(t *testing.T) {
	left := Issue{
		Code:   "duplicate_preferred_identifier",
		Path:   "docs/specs/one.md",
		Target: "SPEC-0001",
		Data:   json.RawMessage(`{"type":"Spec","identifier":"SPEC-0001"}`),
	}
	right := left
	right.Data = json.RawMessage(`{ "identifier": "SPEC-0001", "type": "Spec" }`)

	leftKey, err := StableIssueKey(CheckAliases, left)
	require.NoError(t, err)
	rightKey, err := StableIssueKey(CheckAliases, right)
	require.NoError(t, err)

	assert.Equal(t, leftKey, rightKey)
	assert.Contains(t, leftKey, "issue:v1:")
}

func TestStableIssueKeyExcludesPresentationAndNormalizesPaths(t *testing.T) {
	left := Issue{
		Code:    "broken_note_link",
		Path:    "docs\\specs\\one.md",
		Source:  "./docs/specs/source.md",
		Target:  "missing",
		Line:    10,
		Message: "first rendering",
	}
	right := left
	right.Path = "docs/specs/one.md"
	right.Source = "docs/specs/source.md"
	right.Line = 99
	right.Message = "different rendering"

	leftKey, err := StableIssueKey(CheckBrokenLinks, left)
	require.NoError(t, err)
	rightKey, err := StableIssueKey(CheckBrokenLinks, right)
	require.NoError(t, err)
	assert.Equal(t, leftKey, rightKey)
}

func TestStableActionKeyExcludesPresentationFields(t *testing.T) {
	left := FixAction{
		Check:         CheckBrokenLinks,
		IssueCode:     "broken_note_link",
		Kind:          FixKindRewriteLinkGroup,
		Safety:        FixSafetySafe,
		Title:         "first title",
		Summary:       "first summary",
		Question:      "first question",
		InstanceCount: 1,
		AffectedPaths: []string{"docs\\source.md"},
		Edits: []FixEdit{{
			Kind:      FixKindRewriteLinkGroup,
			NotePath:  "docs/source.md",
			OldTarget: "old",
			NewTarget: "new",
		}},
	}
	right := left
	right.Title = "different title"
	right.Summary = "different summary"
	right.Question = "different question"
	right.InstanceCount = 99

	leftKey, err := StableActionKey(left)
	require.NoError(t, err)
	rightKey, err := StableActionKey(right)
	require.NoError(t, err)
	assert.Equal(t, leftKey, rightKey)
	assert.Contains(t, leftKey, "action:v1:")
}

func TestSourceHashUsesRawBytes(t *testing.T) {
	lf := SourceHash([]byte("# Note\nbody\n"))
	crlf := SourceHash([]byte("# Note\r\nbody\r\n"))

	assert.NotEqual(t, lf, crlf)
	assert.Equal(t, lf, SourceHash([]byte("# Note\nbody\n")))
}

func TestRepairPlanFingerprintIsStableAcrossInputOrdering(t *testing.T) {
	left := RepairPlan{
		IssueKeys: []string{"issue:v1:b", "issue:v1:a"},
		Operations: []RepairOperation{
			{ID: "op-b", Kind: RepairOperationWrite, Path: "notes/b.md", SourceHash: "hash-b"},
			{ID: "op-a", Kind: RepairOperationRename, Path: "notes/a.md", DestinationPath: "notes/c.md", SourceHash: "hash-a"},
		},
	}
	right := RepairPlan{
		IssueKeys:  []string{"issue:v1:a", "issue:v1:b"},
		Operations: []RepairOperation{left.Operations[1], left.Operations[0]},
	}

	leftFingerprint, err := RepairPlanFingerprint(left)
	require.NoError(t, err)
	rightFingerprint, err := RepairPlanFingerprint(right)
	require.NoError(t, err)

	assert.Equal(t, leftFingerprint, rightFingerprint)
}

func TestRepairPlanFingerprintIncludesStableActionKeysAndSourceHashes(t *testing.T) {
	base := RepairPlan{
		Actions: []FixAction{{ID: "action:v1:a"}},
		Operations: []RepairOperation{{
			ID:         "op-a",
			ActionID:   "action:v1:a",
			IssueKey:   "issue:v1:a",
			Kind:       RepairOperationWrite,
			Path:       "notes/a.md",
			SourceHash: "sha256:one",
		}},
	}
	baseFingerprint, err := RepairPlanFingerprint(base)
	require.NoError(t, err)

	actionChanged := base
	actionChanged.Actions = []FixAction{{ID: "action:v1:b"}}
	actionFingerprint, err := RepairPlanFingerprint(actionChanged)
	require.NoError(t, err)

	sourceChanged := base
	sourceChanged.Operations = append([]RepairOperation(nil), base.Operations...)
	sourceChanged.Operations[0].SourceHash = "sha256:two"
	sourceFingerprint, err := RepairPlanFingerprint(sourceChanged)
	require.NoError(t, err)

	assert.NotEqual(t, baseFingerprint, actionFingerprint)
	assert.NotEqual(t, baseFingerprint, sourceFingerprint)
}

func TestFinalizeRepairPlanDerivesMembershipAndRejectsMissingPreconditions(t *testing.T) {
	plan := RepairPlan{
		IssueKeys: []string{"issue:v1:a"},
		Actions: []FixAction{{
			ID:        "action:v1:a",
			Check:     CheckOntology,
			IssueKeys: []string{"issue:v1:a"},
		}},
		Operations: []RepairOperation{{
			ID:         "op-a",
			ActionID:   "action:v1:a",
			IssueKey:   "issue:v1:a",
			Kind:       RepairOperationWrite,
			Path:       "notes/a.md",
			SourceHash: "sha256:one",
		}},
	}
	finalized, err := FinalizeRepairPlan(plan)
	require.NoError(t, err)
	assert.NotEmpty(t, finalized.Fingerprint)
	require.Len(t, finalized.Transactions, 1)
	assert.Equal(t, []string{"op-a"}, finalized.Actions[0].OperationIDs)

	plan.Operations[0].SourceHash = ""
	_, err = FinalizeRepairPlan(plan)
	require.Error(t, err)
	assert.ErrorContains(t, err, "source hash")
}

func TestGroupRepairTransactionsConnectsSharedPathsAndIdentities(t *testing.T) {
	operations := []RepairOperation{
		{ID: "op-a", Kind: RepairOperationWrite, Path: "notes/a.md", Identities: []string{"SPEC-0001"}},
		{ID: "op-b", Kind: RepairOperationRename, Path: "notes/b.md", DestinationPath: "notes/c.md", Identities: []string{"SPEC-0001"}},
		{ID: "op-c", Kind: RepairOperationDelete, Path: "notes/d.md", Identities: []string{"EFF-0001"}},
	}

	transactions, err := GroupRepairTransactions(operations)
	require.NoError(t, err)
	require.Len(t, transactions, 2)
	assert.Equal(t, []string{"op-a", "op-b"}, transactions[0].OperationIDs)
	assert.Equal(t, []string{"notes/a.md", "notes/b.md", "notes/c.md"}, transactions[0].AffectedPaths)
	assert.Equal(t, []string{"op-c"}, transactions[1].OperationIDs)
}

func TestGroupRepairTransactionsIsolatesOverlappingWriteSpans(t *testing.T) {
	operations := []RepairOperation{
		{
			ID:       "op-a",
			Kind:     RepairOperationWrite,
			Path:     "notes/a.md",
			Expected: []ExpectedText{{StartByte: 4, EndByte: 10, Text: "before"}},
		},
		{
			ID:       "op-b",
			Kind:     RepairOperationWrite,
			Path:     "notes/a.md",
			Expected: []ExpectedText{{StartByte: 8, EndByte: 12, Text: "other"}},
		},
		{
			ID:   "op-c",
			Kind: RepairOperationWrite,
			Path: "notes/independent.md",
		},
	}

	transactions, err := GroupRepairTransactions(operations)
	require.NoError(t, err)
	require.Len(t, transactions, 2)
	assert.Equal(t, []string{"op-a", "op-b"}, transactions[0].OperationIDs)
	require.Len(t, transactions[0].Conflicts, 1)
	assert.Equal(t, RepairConflictEditOverlap, transactions[0].Conflicts[0].Kind)
	assert.Equal(t, []string{"op-c"}, transactions[1].OperationIDs)
	assert.Empty(t, transactions[1].Conflicts)
}

func TestGroupRepairTransactionsRejectsIllegalOperationShape(t *testing.T) {
	_, err := GroupRepairTransactions([]RepairOperation{{
		ID:   "op-a",
		Kind: RepairOperationRename,
		Path: "notes/a.md",
	}})
	require.Error(t, err)
	assert.ErrorContains(t, err, "destination")
}

func TestGroupRepairTransactionsIsolatesCaseEquivalentDestinationCollision(t *testing.T) {
	transactions, err := GroupRepairTransactions([]RepairOperation{
		{
			ID:              "op-a",
			Kind:            RepairOperationRename,
			Path:            "notes/a.md",
			DestinationPath: "notes/result.md",
		},
		{
			ID:              "op-b",
			Kind:            RepairOperationRename,
			Path:            "notes/b.md",
			DestinationPath: "notes/RESULT.md",
		},
		{
			ID:   "op-c",
			Kind: RepairOperationDelete,
			Path: "notes/independent.md",
		},
	})
	require.NoError(t, err)
	require.Len(t, transactions, 2)
	require.Len(t, transactions[0].Conflicts, 1)
	assert.Equal(t, RepairConflictDestination, transactions[0].Conflicts[0].Kind)
	assert.Empty(t, transactions[1].Conflicts)
}

func TestClassifyLifecycleEditProtectsHistoricalPlanButAllowsLinkRepair(t *testing.T) {
	before := []byte("---\ntype: EffortNote\nstatus: complete\n---\n# Effort\n\n## Plan\n\nSee [[old-target|context]].\n")
	targetStart := bytes.Index(before, []byte("old-target"))
	protected := ClassifyLifecycleEdit(LifecycleEdit{
		TypeName:     "EffortNote",
		EffortStatus: "complete",
		Before:       before,
		After:        []byte("---\ntype: EffortNote\nstatus: complete\n---\n# Effort\n\n## Plan\n\nChanged.\n"),
	})
	allowed := ClassifyLifecycleEdit(LifecycleEdit{
		TypeName:     "EffortNote",
		EffortStatus: "complete",
		Before:       before,
		After:        []byte("---\ntype: EffortNote\nstatus: complete\n---\n# Effort\n\n## Plan\n\nSee [[new-target|context]].\n"),
		Claims: []LifecycleClaim{{
			Kind:         LifecycleEditBrokenLink,
			StartByte:    targetStart,
			EndByte:      targetStart + len("old-target"),
			ExpectedText: "old-target",
			Replacement:  "new-target",
		}},
	})

	assert.Equal(t, LifecycleProtected, protected.Decision)
	assert.Equal(t, LifecycleAllowed, allowed.Decision)
}

func TestClassifyLifecycleEditDerivesHistoricalStateAndRejectsSpoofedMetadata(t *testing.T) {
	before := []byte("---\ntype: EffortNote\nstatus: complete\n---\n# Effort\n\n## Plan\n\nBefore.\n")
	after := []byte("---\ntype: EffortNote\nstatus: complete\n---\n# Effort\n\n## Plan\n\nAfter.\n")

	derived := ClassifyLifecycleEdit(LifecycleEdit{Before: before, After: after})
	spoofed := ClassifyLifecycleEdit(LifecycleEdit{
		TypeName:     "ReferenceDoc",
		EffortStatus: "active",
		Before:       before,
		After:        after,
	})

	assert.Equal(t, LifecycleProtected, derived.Decision)
	assert.Equal(t, LifecycleProtected, spoofed.Decision)
}

func TestClassifyLifecycleEditRejectsBrokenLinkReplacementInjection(t *testing.T) {
	before := []byte("---\ntype: EffortNote\nstatus: complete\n---\n# Effort\n\n## Plan\n\nSee [[old-target|context]].\n")
	start := bytes.Index(before, []byte("old-target"))
	replacement := "new-target]]\nProtected rewrite"
	after := append([]byte(nil), before[:start]...)
	after = append(after, replacement...)
	after = append(after, before[start+len("old-target"):]...)

	result := ClassifyLifecycleEdit(LifecycleEdit{
		Before: before,
		After:  after,
		Claims: []LifecycleClaim{{
			Kind:         LifecycleEditBrokenLink,
			StartByte:    start,
			EndByte:      start + len("old-target"),
			ExpectedText: "old-target",
			Replacement:  replacement,
		}},
	})

	assert.Equal(t, LifecycleProtected, result.Decision)
}

func TestClassifyLifecycleEditRejectsBodyStatusSpoof(t *testing.T) {
	before := []byte("---\ntype: EffortNote\nstatus: complete\n---\n# Effort\n\n## Notes\n\nstatus: complete\n")
	start := bytes.LastIndex(before, []byte("complete"))
	after := append([]byte(nil), before[:start]...)
	after = append(after, "archived"...)
	after = append(after, before[start+len("complete"):]...)

	result := ClassifyLifecycleEdit(LifecycleEdit{
		Before: before,
		After:  after,
		Claims: []LifecycleClaim{{
			Kind:         LifecycleEditArchiveStatus,
			StartByte:    start,
			EndByte:      start + len("complete"),
			ExpectedText: "complete",
			Replacement:  "archived",
		}},
	})

	assert.Equal(t, LifecycleProtected, result.Decision)
}

func TestClassifyLifecycleEditRejectsEmbeddedHistoryHeading(t *testing.T) {
	before := []byte("---\r\ntype: EffortNote\r\nstatus: complete\r\n---\r\n# Effort\r\n\r\nParagraph mentions ## Deviations but has no section.\r\n\r\n## Status\r\n\r\nComplete.\r\n")
	insertAt := bytes.Index(before, []byte("## Status"))
	replacement := "- (post-closure: audit) must not authorize fake headings\r\n"
	after := append([]byte(nil), before[:insertAt]...)
	after = append(after, replacement...)
	after = append(after, before[insertAt:]...)

	result := ClassifyLifecycleEdit(LifecycleEdit{
		Before: before,
		After:  after,
		Claims: []LifecycleClaim{{
			Kind: LifecycleEditAppendHistory, Section: "Deviations",
			StartByte: insertAt, EndByte: insertAt, Replacement: replacement,
		}},
	})

	assert.Equal(t, LifecycleProtected, result.Decision)
}

func TestClassifyLifecycleEditRejectsFencedHistoryHeading(t *testing.T) {
	before := []byte("---\ntype: EffortNote\nstatus: complete\n---\n# Effort\n\n```md\n## Deviations\n```\n\n## Status\n\nComplete.\n")
	insertAt := bytes.Index(before, []byte("## Status"))
	replacement := "- (post-closure: audit) must not authorize fenced headings\n"
	after := append([]byte(nil), before[:insertAt]...)
	after = append(after, replacement...)
	after = append(after, before[insertAt:]...)

	result := ClassifyLifecycleEdit(LifecycleEdit{
		Before: before,
		After:  after,
		Claims: []LifecycleClaim{{
			Kind: LifecycleEditAppendHistory, Section: "Deviations",
			StartByte: insertAt, EndByte: insertAt, Replacement: replacement,
		}},
	})

	assert.Equal(t, LifecycleProtected, result.Decision)
}

func TestClassifyLifecycleEditAllowsHistoryAppendAtRealCRLFSectionEnd(t *testing.T) {
	before := []byte("---\r\ntype: EffortNote\r\nstatus: complete\r\n---\r\n# Effort\r\n\r\n## Deviations\r\n\r\nNone.\r\n\r\n##\tStatus\r\n\r\nComplete.\r\n")
	insertAt := bytes.Index(before, []byte("##\tStatus"))
	replacement := "- (post-closure: audit) approved correction\r\n"
	after := append([]byte(nil), before[:insertAt]...)
	after = append(after, replacement...)
	after = append(after, before[insertAt:]...)

	result := ClassifyLifecycleEdit(LifecycleEdit{
		Before: before,
		After:  after,
		Claims: []LifecycleClaim{{
			Kind: LifecycleEditAppendHistory, Section: "Deviations",
			StartByte: insertAt, EndByte: insertAt, Replacement: replacement,
		}},
	})

	assert.Equal(t, LifecycleAllowed, result.Decision)
}

func TestClassifyLifecycleEditAllowsHistoryAppendAtSectionEOF(t *testing.T) {
	before := []byte("---\ntype: EffortNote\nstatus: complete\n---\n# Effort\n\n## Execution Notes\n\nExisting.\n")
	replacement := "- (post-closure: audit) approved correction\n"
	after := append(append([]byte(nil), before...), replacement...)

	result := ClassifyLifecycleEdit(LifecycleEdit{
		Before: before,
		After:  after,
		Claims: []LifecycleClaim{{
			Kind: LifecycleEditAppendHistory, Section: "Execution Notes",
			StartByte: len(before), EndByte: len(before), Replacement: replacement,
		}},
	})

	assert.Equal(t, LifecycleAllowed, result.Decision)
}
