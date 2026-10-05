package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/atomicobject/rhizome/pkg/validate/namespaceadmission"
)

type namespaceJournalPurpose struct {
	namespaceadmission.PurposeHeader
	Summary  json.RawMessage      `json:"summary,omitempty"`
	Moves    []PathRename         `json:"moves"`
	GitMoves []PathRename         `json:"gitMoves,omitempty"`
	Git      *namespaceGitJournal `json:"git,omitempty"`
}

type namespaceGitWitness struct {
	Exists bool   `json:"exists"`
	Hash   string `json:"hash,omitempty"`
	Mode   uint32 `json:"mode,omitempty"`
}

type namespaceGitArtifact struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
	Mode uint32 `json:"mode"`
}

type namespaceGitJournal struct {
	GitDir      string               `json:"gitDir"`
	IndexPath   string               `json:"indexPath"`
	LockPath    string               `json:"lockPath"`
	RawOriginal namespaceGitWitness  `json:"rawOriginal"`
	Rollback    namespaceGitArtifact `json:"rollback"`
	Candidate   namespaceGitArtifact `json:"candidate"`
	Token       string               `json:"token"`
	LockHash    string               `json:"lockHash"`
	LockMode    uint32               `json:"lockMode"`
}

func validateNamespacePurpose(manifest repairJournalManifest) error {
	if manifest.Namespace == nil {
		return nil
	}
	if manifest.Version != namespaceadmission.NamespaceVersion ||
		manifest.Namespace.Version != namespaceadmission.PurposeVersion ||
		len(manifest.Checks) != 0 || len(manifest.ActionIDs) != 0 || len(manifest.IssueKeys) != 0 || len(manifest.Renamed) == 0 {
		return fmt.Errorf("unsupported or mixed namespace journal purpose")
	}
	if err := validateNamespaceSummary(manifest.Namespace.Summary); err != nil {
		return err
	}
	if decision := manifest.Namespace.SettledDecision; decision != "" && decision != repairJournalCommitted && decision != repairJournalRestored {
		return fmt.Errorf("invalid native settled decision")
	}
	if len(manifest.OperationIDs) == 0 {
		return fmt.Errorf("namespace journal operation membership is absent")
	}
	if len(manifest.Namespace.Moves) != len(manifest.Renamed) || len(sortedUniqueRenames(manifest.Namespace.Moves)) != len(manifest.Renamed) {
		return fmt.Errorf("namespace move reporting does not match transaction")
	}
	allowed := make(map[PathRename]bool, len(manifest.Renamed))
	for _, move := range manifest.Renamed {
		allowed[move] = true
	}
	for _, move := range append(append([]PathRename(nil), manifest.Namespace.Moves...), manifest.Namespace.GitMoves...) {
		if !allowed[move] {
			return fmt.Errorf("namespace move reporting is outside transaction")
		}
	}
	if len(sortedUniqueRenames(manifest.Namespace.GitMoves)) != len(manifest.Namespace.GitMoves) || (manifest.Namespace.Git == nil && len(manifest.Namespace.GitMoves) != 0) {
		return fmt.Errorf("namespace Git move reporting is invalid")
	}
	return validateRepairJournalOperationMembership(manifest)
}

func validateNamespaceSummary(summary json.RawMessage) error {
	if len(summary) == 0 {
		return nil
	}
	if len(summary) > 64*1024 {
		return fmt.Errorf("namespace summary exceeds 64 KiB")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(summary, &object); err != nil || object == nil {
		return fmt.Errorf("namespace summary must be a JSON object")
	}
	return nil
}

func nativeJournalDecision(dir string) (string, error) {
	decision := repairJournalPrepared
	for _, item := range []struct{ name, content, state string }{
		{namespaceadmission.CommittedMarker, "committed\n", repairJournalCommitted},
		{namespaceadmission.RestoredMarker, "restored\n", repairJournalRestored},
	} {
		marker := filepath.Join(dir, item.name)
		info, err := os.Lstat(marker)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			return "", fmt.Errorf("invalid native %s marker", item.name)
		}
		content, err := os.ReadFile(marker)
		if err != nil || string(content) != item.content || decision != repairJournalPrepared {
			return "", fmt.Errorf("invalid or conflicting native decisions")
		}
		decision = item.state
	}
	return decision, nil
}

func markNamespaceRestored(dir string, hooks ...*repairExecutionHooks) error {
	return markNamespaceDecision(dir, namespaceadmission.RestoredMarker, []byte("restored\n"), firstRepairExecutionHooks(hooks))
}

func markNamespaceDecision(dir, name string, content []byte, hooks *repairExecutionHooks) error {
	file, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(content)
	if writeErr == nil {
		writeErr = file.Chmod(0o600)
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	if hooks != nil && hooks.BeforeNamespaceDecisionDirectorySync != nil {
		if err := hooks.BeforeNamespaceDecisionDirectorySync(name); err != nil {
			return err
		}
	}
	return syncDirectory(dir)
}

func syncNamespaceDecision(dir, state string) error {
	decision, err := nativeJournalDecision(dir)
	if err != nil || decision != state {
		return fmt.Errorf("native decision changed before Git release")
	}
	name := namespaceadmission.CommittedMarker
	if state == repairJournalRestored {
		name = namespaceadmission.RestoredMarker
	}
	// Never truncate an existing decision during replay. A crash must not erase
	// a terminal decision that already ended historical rollback authority.
	file, err := os.OpenFile(filepath.Join(dir, name), os.O_RDWR, 0)
	if err != nil {
		return err
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	return syncDirectory(dir)
}
