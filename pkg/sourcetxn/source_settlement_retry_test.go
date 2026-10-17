package sourcetxn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSettledEvidenceAllowsIdenticalSourceAndRepairRetry(t *testing.T) {
	for _, source := range []bool{true, false} {
		name := "repair"
		if source {
			name = "source"
		}
		t.Run(name, func(t *testing.T) {
			root, options, intent := sourceFixture(t)
			var dir string
			hooks := &Hooks{AfterJournalPublished: func(path string) error { dir = path; return ErrSimulatedInterruption }}
			if source {
				require.ErrorIs(t, apply(context.Background(), root, intent, options, hooks), ErrSimulatedInterruption)
			} else {
				writer, _, err := Open(root, options.Lease, WriterOptions{Role: RepairOwner, Hooks: hooks})
				require.NoError(t, err)
				_, err = writer.PrepareRepair(writerRepairRequest())
				require.ErrorIs(t, err, ErrSimulatedInterruption)
				writer.Close()
			}
			m, err := readRepairJournalManifest(dir)
			require.NoError(t, err)
			artifact := m.Entries[0].BackupPath
			require.NoError(t, os.WriteFile(artifact, []byte("partial"), 0600))
			require.NoError(t, recoverSourceFixture(root, options))
			manifest := mustReadEditTransactionFile(t, filepath.Join(dir, "manifest.json"))
			proof := mustReadEditTransactionFile(t, filepath.Join(dir, sourceSettledMarker))
			if source {
				require.NoError(t, applySourceFixture(context.Background(), root, intent, options))
			} else {
				writer, _, err := Open(root, options.Lease, WriterOptions{Role: RepairOwner})
				require.NoError(t, err)
				tx, err := writer.PrepareRepair(writerRepairRequest())
				require.NoError(t, err)
				require.NotEqual(t, m.TransactionID, tx.Info().Manifest.TransactionID)
				completion, err := writer.Publish(tx)
				require.NoError(t, err)
				require.NoError(t, writer.Complete(completion))
				writer.Close()
			}
			require.Equal(t, "new", mustReadEditTransactionFile(t, filepath.Join(root, "a.md")))
			require.NoError(t, recoverSourceFixture(root, options))
			require.NoError(t, recoverSourceFixture(root, options))
			require.NoError(t, CheckPending(root))
			require.Equal(t, "new", mustReadEditTransactionFile(t, filepath.Join(root, "a.md")))
			require.Equal(t, "partial", mustReadEditTransactionFile(t, artifact))
			require.Equal(t, manifest, mustReadEditTransactionFile(t, filepath.Join(dir, "manifest.json")))
			require.Equal(t, proof, mustReadEditTransactionFile(t, filepath.Join(dir, sourceSettledMarker)))
		})
	}
}

func TestInvalidSettlementProofBlocksOnlyRecordedPaths(t *testing.T) {
	root, options, _, dir, artifact := unpublishedPartialArtifact(t)
	require.NoError(t, recoverSourceFixture(root, options))
	proof := mustReadEditTransactionFile(t, filepath.Join(dir, sourceSettledMarker))
	const invalid = "{\"version\":1,\"manifestHash\":\"sha256:00\"}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, sourceSettledMarker), []byte(invalid), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "c.md"), []byte("old"), 0640))
	require.Error(t, CheckAdmission(root, []string{"a.md"}))
	require.NoError(t, CheckAdmission(root, []string{"c.md"}))
	require.NoError(t, recoverSourceFixture(root, options))
	require.Equal(t, invalid, mustReadEditTransactionFile(t, filepath.Join(dir, sourceSettledMarker)))
	require.Error(t, CheckAdmission(root, []string{"a.md"}))
	require.NoError(t, applySourceFixture(context.Background(), root, Intent{Fingerprint: "disjoint", Files: []FilePatch{{Path: "c.md", Original: []byte("old"), OriginalMode: 0640, Updated: []byte("new")}}}, options))
	require.Equal(t, "old", mustReadEditTransactionFile(t, filepath.Join(root, "a.md")))
	require.Equal(t, "partial", mustReadEditTransactionFile(t, artifact))
	require.NoError(t, os.WriteFile(filepath.Join(dir, sourceSettledMarker), []byte(proof), 0600))
	require.NoError(t, recoverSourceFixture(root, options))
	require.NoError(t, CheckPending(root))
	require.Equal(t, "new", mustReadEditTransactionFile(t, filepath.Join(root, "c.md")))
}

func TestSettlementProofBindsRawManifestIncludingUnknownFields(t *testing.T) {
	root, options, _, dir, artifact := unpublishedPartialArtifact(t)
	path := filepath.Join(dir, "manifest.json")
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(mustReadEditTransactionFile(t, path)), &raw))
	raw["futureField"] = json.RawMessage(`{"preserved":true}`)
	manifest, err := json.MarshalIndent(raw, "", "  ")
	require.NoError(t, err)
	manifest = append(manifest, '\n')
	require.NoError(t, os.WriteFile(path, manifest, 0600))
	require.NoError(t, recoverSourceFixture(root, options))
	var proof sourceSettlement
	require.NoError(t, json.Unmarshal([]byte(mustReadEditTransactionFile(t, filepath.Join(dir, sourceSettledMarker))), &proof))
	digest := sha256.Sum256(manifest)
	require.Equal(t, "sha256:"+hex.EncodeToString(digest[:]), proof.ManifestHash)
	require.NoError(t, recoverSourceFixture(root, options))
	require.NoError(t, CheckPending(root))
	require.Equal(t, string(manifest), mustReadEditTransactionFile(t, path))
	require.Equal(t, "partial", mustReadEditTransactionFile(t, artifact))
}
