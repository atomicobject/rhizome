package validate

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRepairPlanFingerprintBindsPrivatePayloads(t *testing.T) {
	base := RepairOperation{
		ID: "op", Kind: RepairOperationWrite, Path: "note.md", SourceHash: SourceHash([]byte("old source")),
		Content:  []byte("new source"),
		Expected: []ExpectedText{{StartByte: 0, EndByte: 3, Text: "old", Replacement: "new"}},
	}
	fingerprint := func(operation RepairOperation) string {
		t.Helper()
		value, err := RepairPlanFingerprint(RepairPlan{Operations: []RepairOperation{operation}})
		require.NoError(t, err)
		require.Contains(t, value, "plan:v3:")
		return value
	}
	original := fingerprint(base)
	baseJSON, err := json.Marshal(base)
	require.NoError(t, err)
	for _, test := range []struct {
		name   string
		mutate func(*RepairOperation)
	}{
		{"content", func(op *RepairOperation) { op.Content = []byte("different output") }},
		{"expected text", func(op *RepairOperation) { op.Expected[0].Text = "different source witness" }},
		{"replacement", func(op *RepairOperation) { op.Expected[0].Replacement = "different replacement" }},
		{"empty replacement", func(op *RepairOperation) { op.Expected[0].Replacement = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := base
			changed.Expected = append([]ExpectedText(nil), base.Expected...)
			test.mutate(&changed)
			publicJSON, err := json.Marshal(changed)
			require.NoError(t, err)
			require.JSONEq(t, string(baseJSON), string(publicJSON), "private payloads stay out of public JSON")
			require.NotEqual(t, original, fingerprint(changed), "review authority includes every private payload")
		})
	}
}

func TestRepairPlanFingerprintDistinguishesAbsentAndEmptyContent(t *testing.T) {
	operation := RepairOperation{ID: "op", Kind: RepairOperationWrite, Path: "note.md", SourceHash: SourceHash([]byte("source"))}
	absent, err := RepairPlanFingerprint(RepairPlan{Operations: []RepairOperation{operation}})
	require.NoError(t, err)
	operation.Content = []byte{}
	empty, err := RepairPlanFingerprint(RepairPlan{Operations: []RepairOperation{operation}})
	require.NoError(t, err)
	require.NotEqual(t, absent, empty, "an empty write and no full-file write have different effects")
}
