package validate

import (
	"encoding/hex"
	"encoding/json"
	"os"

	"github.com/atomicobject/rhizome/pkg/sourcetxn"
)

func DetectPendingRepairJournals(run RunContext) ([]RepairJournalEvidence, error) {
	return sourcetxn.PendingEvidence(sourceRoot(run))
}
func repairAbsPath(run RunContext, rel string) (string, error) {
	return sourcetxn.ResolvePath(sourceRoot(run), rel)
}
func repairCompletionTarget(run RunContext, path string) (string, string, error) {
	return sourcetxn.CompletionTarget(sourceRoot(run), path)
}
func repairModeBits(mode os.FileMode) uint32 { return sourcetxn.RepairModeBits(mode) }
func repairFileMode(bits uint32) os.FileMode { return sourcetxn.RepairFileMode(bits) }
func repairModeMatches(mode os.FileMode, bits uint32) bool {
	return sourcetxn.RepairModeMatches(mode, bits)
}
func validRepairJournalDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
func validateNamespaceSummary(summary json.RawMessage) error {
	return sourcetxn.ValidateNamespaceSummary(summary)
}
