package sourcetxn

import (
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestPreparedTransactionRechecksNewOwnershipBeforeAnyInstall(t *testing.T) {
	root, options, _ := sourceFixture(t)
	writer, _, err := Open(root, options.Lease, WriterOptions{Role: RepairOwner})
	require.NoError(t, err)
	defer writer.Close()
	request := RepairRequest{TransactionID: "ownership", Fingerprint: "ownership", Checks: []string{"ontology"}, Changed: []string{"a.md", "b.md"}}
	for _, name := range []string{"a.md", "b.md"} {
		request.Files = append(request.Files, Change{Path: name, Before: Snapshot{Exists: true, Content: []byte("old"), Mode: 0640}, After: Snapshot{Exists: true, Content: []byte("new"), Mode: 0640}})
	}
	transaction, err := writer.PrepareRepair(request)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome/source-ownership.yml"), []byte("sources:\n  - owner: external\n    reason: Use external tools.\n    paths: [b.md]\n"), 0644))
	_, err = writer.Publish(transaction)
	require.ErrorContains(t, err, "Use external tools.")
	for _, name := range []string{"a.md", "b.md"} {
		require.Equal(t, "old", mustReadEditTransactionFile(t, filepath.Join(root, name)))
	}
}
