package sourcetxn

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

type Snapshot struct {
	Exists  bool
	Content []byte
	Mode    os.FileMode
}
type Change struct {
	Path, OriginalPath string
	Before, After      Snapshot
}
type RepairRequest struct {
	TransactionID, Fingerprint                                   string
	OperationIDs, ActionIDs, IssueKeys, Checks, Changed, Deleted []string
	Renamed                                                      []Rename
	Files                                                        []Change
	Completion                                                   *CompletionArtifact
}

func (w *Writer) PrepareRepair(request RepairRequest) (*Transaction, error) {
	return w.prepare(request, nil, false)
}
func (w *Writer) prepare(request RepairRequest, purpose *NamespacePurpose, source bool) (*Transaction, error) {
	if err := w.check(); err != nil {
		return nil, err
	}
	if w.active != nil {
		return nil, fmt.Errorf("source writer already has an active preparation")
	}
	if strings.TrimSpace(request.TransactionID) == "" || strings.TrimSpace(request.Fingerprint) == "" {
		return nil, fmt.Errorf("source transaction identity and fingerprint are required")
	}
	if w.options.RepairPolicy != nil {
		if err := w.options.RepairPolicy(RepairEvidence{TransactionID: request.TransactionID, Checks: append([]string(nil), request.Checks...)}); err != nil {
			return nil, err
		}
	}
	if purpose == nil {
		attempt, err := repairAttemptID(w.run, request.TransactionID)
		if err != nil {
			return nil, err
		}
		request.TransactionID = attempt
	}
	m := Manifest{Version: repairJournalVersion, Source: source, TransactionID: request.TransactionID, PlanFingerprint: request.Fingerprint, OperationIDs: append([]string(nil), request.OperationIDs...), ActionIDs: append([]string(nil), request.ActionIDs...), IssueKeys: append([]string(nil), request.IssueKeys...), Checks: append([]string(nil), request.Checks...), Changed: append([]string(nil), request.Changed...), Deleted: append([]string(nil), request.Deleted...), Renamed: append([]Rename(nil), request.Renamed...), Namespace: purpose}
	if purpose != nil {
		m.Version = 2
	}
	var states []fileState
	for _, file := range request.Files {
		abs, err := repairAbsPath(w.run, file.Path)
		if err != nil {
			return nil, err
		}
		states = append(states, fileState{Path: file.Path, Absolute: abs, OriginalPath: file.OriginalPath, OriginalExists: file.Before.Exists, OriginalContent: append([]byte(nil), file.Before.Content...), OriginalMode: file.Before.Mode, FinalExists: file.After.Exists, FinalContent: append([]byte(nil), file.After.Content...), FinalMode: file.After.Mode})
	}
	if request.Completion != nil {
		state, err := prepareRepairCompletionState(w.run, request.Completion)
		if err != nil {
			return nil, err
		}
		if state != nil {
			states = append(states, *state)
		}
	}
	sort.SliceStable(states, func(i, j int) bool {
		if states[i].FinalExists != states[j].FinalExists {
			return !states[i].FinalExists
		}
		return states[i].Path < states[j].Path
	})
	prepared, err := stageRepairFileStates(w.run, m, states, w.options.Hooks)
	if err != nil {
		if dir, dirErr := repairJournalDir(w.run, request.TransactionID); dirErr == nil {
			if _, statErr := os.Lstat(dir); statErr == nil {
				w.blocked = err
			}
		}
		return nil, err
	}
	t := &Transaction{writer: w, prepared: prepared, state: repairJournalPrepared}
	w.active = t
	return t, nil
}
