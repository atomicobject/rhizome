package validate

import "github.com/atomicobject/rhizome/pkg/sourcetxn"

func sourceRoot(c RunContext) string {
	if c.VaultPath != "" {
		return c.VaultPath
	}
	return c.VaultDef.BasePath()
}

type repairJournalManifest = sourcetxn.Manifest
type repairJournalEntry = sourcetxn.Entry
type repairExecutionHooks = sourcetxn.Hooks
type RepairJournalEvidence = sourcetxn.JournalEvidence

const (
	repairJournalVersion         = 1
	repairJournalPrepared        = "prepared"
	repairJournalCommitted       = "committed"
	repairJournalRestored        = "restored"
	repairJournalCleanupPending  = "cleanup_pending"
	repairJournalCleanupPrefix   = ".cleanup-"
	cleanupAfterArtifactRemoved  = "after_artifact_removed"
	cleanupBeforeDetach          = "before_detach"
	cleanupAfterDetach           = "after_detach"
	cleanupAfterCommittedRemoved = "after_committed_removed"
	cleanupAfterManifestRemoved  = "after_manifest_removed"
	cleanupAfterDirectoryRemoved = "after_directory_removed"
)

type recoveredRepairJournal struct {
	completion            sourcetxn.Completion
	nativeCleanupDecision string
	decisionSynced        bool
	dir                   string
	manifest              repairJournalManifest
	state                 string
}
type repairJournalRecovery struct {
	native, nativeCleanup, committed, rolledBack, noMutation, partial, cleanupPending []recoveredRepairJournal
}

func sourceChanges(states []repairFileState) []sourcetxn.Change {
	out := make([]sourcetxn.Change, 0, len(states))
	for _, s := range states {
		if s.omit {
			continue
		}
		out = append(out, sourcetxn.Change{Path: s.rel, OriginalPath: s.originalRel, Before: sourcetxn.Snapshot{Exists: s.originalExists, Content: s.originalContent, Mode: s.originalMode}, After: sourcetxn.Snapshot{Exists: s.finalExists, Content: s.finalContent, Mode: s.finalMode}})
	}
	return out
}
func repairJournal(c sourcetxn.Completion) recoveredRepairJournal {
	j := c.Info()
	return recoveredRepairJournal{completion: c, nativeCleanupDecision: j.NativeCleanupDecision, decisionSynced: j.DecisionSynced, dir: j.Directory, manifest: j.Manifest, state: j.State}
}
func repairJournals(cs []sourcetxn.Completion) []recoveredRepairJournal {
	out := make([]recoveredRepairJournal, len(cs))
	for i, c := range cs {
		out[i] = repairJournal(c)
	}
	return out
}
func repairRecovery(r sourcetxn.RecoveryOutcomes) repairJournalRecovery {
	return repairJournalRecovery{native: repairJournals(r.Native), nativeCleanup: repairJournals(r.NativeCleanup), committed: repairJournals(r.Committed), rolledBack: repairJournals(r.RolledBack), noMutation: repairJournals(r.NoMutation), partial: repairJournals(r.Partial), cleanupPending: repairJournals(r.CleanupPending)}
}

var errSimulatedRepairInterruption = sourcetxn.ErrSimulatedInterruption
