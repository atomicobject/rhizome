package validate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyFixPlanComposesDisjointEditSpans(t *testing.T) {
	for _, tc := range []struct {
		name, after string
		spans       []ExpectedText
	}{
		{
			name: "distinct insertions", after: "aXbcdeYf\n",
			spans: []ExpectedText{
				{StartByte: 1, EndByte: 1, Replacement: "X"},
				{StartByte: 5, EndByte: 5, Replacement: "Y"},
			},
		},
		{
			name: "adjacent replacements", after: "aXYf\n",
			spans: []ExpectedText{
				{StartByte: 1, EndByte: 3, Text: "bc", Replacement: "X"},
				{StartByte: 3, EndByte: 5, Text: "de", Replacement: "Y"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const before = "abcdef\n"
			root := t.TempDir()
			path := filepath.Join(root, "note.md")
			require.NoError(t, os.WriteFile(path, []byte(before), 0o640))
			var operations []RepairOperation
			for index, span := range tc.spans {
				operations = append(operations, RepairOperation{
					ID: []string{"first", "second"}[index], Kind: RepairOperationWrite, Path: "note.md",
					SourceHash: SourceHash([]byte(before)), Expected: []ExpectedText{span},
					Content: []byte(before[:span.StartByte] + span.Replacement + before[span.EndByte:]),
				})
			}
			plan := mustRepairPlan(t, []repairPlanInput{{
				actionID: "action:compose", issueKey: "issue:compose", operations: operations,
			}})
			require.Len(t, plan.Transactions, 1)
			require.Empty(t, plan.Transactions[0].Conflicts)
			execution, err := ApplyFixPlan(t.Context(), repairRunContext(t, root), &plan, ApplyOptions{
				Fix: true, NonInteractive: true,
			})
			require.NoError(t, err)
			assert.Equal(t, []string{"action:compose"}, execution.Applied)
			assert.Equal(t, tc.after, string(mustReadFile(t, path)))
		})
	}
}
