package validate

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

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
