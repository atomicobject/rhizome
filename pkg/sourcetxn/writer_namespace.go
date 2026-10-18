package sourcetxn

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/sourcetxn/admission"
)

type GitInput struct {
	GitDir, IndexPath, LockPath string
	RawOriginal                 GitWitness
	Rollback, Candidate         Snapshot
	Moves                       []Rename
}
type NamespaceRequest struct {
	RepairRequest
	Summary json.RawMessage
	Moves   []Rename
	Git     *GitInput
}

func (w *Writer) PrepareNamespace(request NamespaceRequest) (*Transaction, error) {
	if err := w.check(); err != nil {
		return nil, err
	}
	purpose := &NamespacePurpose{PurposeHeader: namespaceadmission.PurposeHeader{Version: namespaceadmission.PurposeVersion}, Summary: append(json.RawMessage(nil), request.Summary...), Moves: append([]Rename(nil), request.Moves...)}
	if input := request.Git; input != nil {
		var token [16]byte
		if _, err := rand.Read(token[:]); err != nil {
			return nil, err
		}
		purpose.Git = &GitJournal{GitDir: input.GitDir, IndexPath: input.IndexPath, LockPath: input.LockPath, RawOriginal: input.RawOriginal, Token: hex.EncodeToString(token[:]), LockMode: 0600}
		if !input.RawOriginal.Exists {
			purpose.Git.RawOriginal.Hash = ""
		}
		purpose.Git.LockHash = Hash(namespaceGitLockContent(purpose.Git))
		rollback, candidate := namespaceGitPaths(Manifest{TransactionID: request.TransactionID, Namespace: purpose})
		purpose.Git.Rollback = GitArtifact{Path: rollback, Hash: Hash(input.Rollback.Content), Mode: RepairModeBits(input.Rollback.Mode)}
		purpose.Git.Candidate = GitArtifact{Path: candidate, Hash: Hash(input.Candidate.Content), Mode: RepairModeBits(input.Candidate.Mode)}
		purpose.GitMoves = append([]Rename(nil), input.Moves...)
	}
	receipt := filepath.Join(w.run.VaultPath, repairCompletionDirectory, strings.TrimPrefix(Hash([]byte(request.TransactionID)), "sha256:")+".json")
	rel, err := filepath.Rel(w.run.VaultPath, receipt)
	if err != nil {
		return nil, err
	}
	outcome := struct {
		Decision        string          `json:"decision"`
		TransactionID   string          `json:"transactionId,omitempty"`
		Moves           []Rename        `json:"moves,omitempty"`
		GitMoves        []Rename        `json:"gitMoves,omitempty"`
		RecoveryPending bool            `json:"recoveryPending"`
		ReceiptPath     string          `json:"receiptPath,omitempty"`
		Summary         json.RawMessage `json:"summary,omitempty"`
	}{repairJournalCommitted, request.TransactionID, purpose.Moves, purpose.GitMoves, false, filepath.ToSlash(rel), purpose.Summary}
	content, err := json.Marshal(outcome)
	if err != nil {
		return nil, err
	}
	request.Completion = &CompletionArtifact{Path: receipt, Content: append(content, '\n')}
	t, err := w.prepare(request.RepairRequest, purpose, false)
	if err != nil {
		return nil, err
	}
	if request.Git == nil {
		return t, nil
	}
	for _, artifact := range []struct {
		path     string
		snapshot Snapshot
	}{{purpose.Git.Rollback.Path, request.Git.Rollback}, {purpose.Git.Candidate.Path, request.Git.Candidate}} {
		if err := createOwnedRepairArtifact(t.prepared.Directory, &t.prepared.Manifest, artifact.path, artifact.snapshot.Content, artifact.snapshot.Mode); err != nil {
			return w.failedPreparation(t, err)
		}
	}
	if err := acquireNamespaceGitExclusion(w.run, t.prepared); err != nil {
		if errors.Is(err, ErrNamespaceGitAdmission) {
			if err := abandonNativeGitPreparation(w.run, t.prepared); err != nil {
				return w.failedPreparation(t, err)
			}
			t.state = "aborted"
			w.active = nil
			request.Git = nil
			return w.PrepareNamespace(request)
		}
		return w.failedPreparation(t, err)
	}
	if err := verifyNamespaceGitOriginal(w.run, t.prepared.Manifest); err != nil {
		// No source has been installed. Today's Git index is outside rollback
		// authority when a cooperating writer finished before exclusion.
		cleanupErr := cleanupFailedPrepare(w.run, t.prepared.Directory, t.prepared.Manifest, w.options.Hooks)
		if cleanupErr == nil {
			t.state = "aborted"
			w.active = nil
			return nil, err
		}
		return w.failedPreparation(t, errors.Join(err, cleanupErr))
	}
	return t, nil
}

func (w *Writer) publishNamespace(t *Transaction) (Completion, error) {
	applyErr := commitPreparedRepairTransaction(w.run, t.prepared, w.options.Hooks)
	j := journal{Directory: t.prepared.Directory, Manifest: t.prepared.Manifest, State: repairJournalPrepared, DecisionSynced: applyErr == nil}
	if decision, err := nativeJournalDecision(j.Directory); err == nil {
		j.State = decision
	} else {
		applyErr = errors.Join(applyErr, err)
	}
	if applyErr != nil {
		var syncErr NamespaceDecisionSyncError
		if !errors.Is(applyErr, ErrSimulatedInterruption) && !errors.As(applyErr, &syncErr) && j.State == repairJournalPrepared {
			rollbackErr := restoreNamespacePrepared(w.run, t.prepared, w.options.Hooks)
			j.Manifest = t.prepared.Manifest
			j.DecisionSynced = t.prepared.DecisionSynced
			if decision, err := nativeJournalDecision(j.Directory); err == nil {
				j.State = decision
			}
			applyErr = errors.Join(applyErr, rollbackErr)
		}
	} else {
		if w.options.Hooks != nil && w.options.Hooks.BeforeNamespaceGitRelease != nil {
			applyErr = w.options.Hooks.BeforeNamespaceGitRelease(j.State)
		}
		if applyErr == nil {
			applyErr = settleNamespaceGitRelease(w.run, &j)
		}
	}
	t.prepared.Manifest = j.Manifest
	t.prepared.DecisionSynced = j.DecisionSynced
	t.state = j.State
	w.active = nil
	t.completion = w.completion(j)
	if applyErr != nil && j.Manifest.Namespace.SettledDecision == "" {
		w.blocked = applyErr
	}
	if j.State == repairJournalPrepared {
		t.state = "recovery_required"
		w.blocked = applyErr
		return t.completion, errors.Join(applyErr, fmt.Errorf("namespace publication requires recovery"))
	}
	return t.completion, applyErr
}

func (w *Writer) failedPreparation(t *Transaction, err error) (*Transaction, error) {
	t.state = "preparation_failed"
	if errors.Is(err, ErrSimulatedInterruption) {
		w.blocked = err
	}
	return t, err
}
