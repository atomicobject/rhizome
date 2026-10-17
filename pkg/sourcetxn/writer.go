package sourcetxn

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/sourcetxn/admission"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

type WriterRole uint8

const (
	SourceEditor WriterRole = iota
	RepairOwner
	RuntimeRecoveryOwner
)

// RepairEvidence is a copied policy input, never journal mutation authority.
type RepairEvidence struct {
	TransactionID string
	Checks        []string
}
type WriterOptions struct {
	Role         WriterRole
	RepairPolicy func(RepairEvidence) error
	Hooks        *Hooks
}

type Writer struct {
	run         writerContext
	options     WriterOptions
	closed      bool
	blocked     error
	active      *Transaction
	completions map[*completionState]bool
}
type Transaction struct {
	writer     *Writer
	prepared   *preparedTransaction
	state      string
	completion Completion
}
type completionState struct {
	journal   journal
	completed bool
}
type Completion struct {
	writer *Writer
	state  *completionState
}
type RecoveryOutcomes struct {
	Native, NativeCleanup, Committed, RolledBack, NoMutation, Partial, CleanupPending []Completion
}

// JournalInfo is detached reporting data. Its paths and manifest cannot grant
// publication, rollback or cleanup authority.
type JournalInfo struct {
	Directory             string
	Manifest              Manifest
	State                 string
	DecisionSynced        bool
	NativeCleanupDecision string
}

func Open(root string, lease *indexlock.Lease, options WriterOptions) (*Writer, RecoveryOutcomes, error) {
	w, r, err := openWriter(root, lease, options)
	if w != nil && err != nil {
		w.blocked = err
	}
	return w, r, err
}

func openWriter(root string, lease *indexlock.Lease, options WriterOptions) (*Writer, RecoveryOutcomes, error) {
	if options.Hooks != nil {
		copied := *options.Hooks
		options.Hooks = &copied
	}
	vault, err := paths.NewVaultPaths(root)
	if err != nil {
		return nil, RecoveryOutcomes{}, err
	}
	root = vault.Root()
	if err := lease.Validate(root); err != nil {
		return nil, RecoveryOutcomes{}, err
	}
	if options.Role != SourceEditor && options.Role != RepairOwner && options.Role != RuntimeRecoveryOwner {
		return nil, RecoveryOutcomes{}, fmt.Errorf("unsupported source writer role")
	}
	w := &Writer{run: writerContext{VaultPath: root, Lease: lease, role: options.Role}, options: options, completions: map[*completionState]bool{}}

	policy := func(m Manifest) error {
		if options.RepairPolicy == nil {
			return nil
		}
		return options.RepairPolicy(RepairEvidence{TransactionID: m.TransactionID, Checks: append([]string(nil), m.Checks...)})
	}
	journals, err := discoverRecoverableRepairJournals(w.run, policy, options.Hooks)
	if err != nil {
		return w, RecoveryOutcomes{}, err
	}
	if options.Role == SourceEditor {
		if err := namespaceadmission.Check(root); err != nil {
			return w, RecoveryOutcomes{}, err
		}
	}
	if err := recoverLegacy(w.run); err != nil {
		return w, RecoveryOutcomes{}, err
	}
	journals, err = outsideQuarantine(w.run, journals)
	if err != nil {
		return w, RecoveryOutcomes{}, err
	}
	var eligible []journal
	for _, j := range journals {
		if options.Role == RepairOwner && !j.Manifest.Source && j.Manifest.Namespace == nil && j.State == repairJournalCommitted {
			if err := verifyCommittedRepairJournal(w.run, j.Manifest); err != nil {
				if err := quarantineRepair(w.run, j.Directory, err); err != nil {
					return w, RecoveryOutcomes{}, err
				}
				continue
			}
		}
		eligible = append(eligible, j)
	}
	recovered, recoveryErr := stabilizeRepairJournals(w.run, eligible, options.Hooks)
	var result RecoveryOutcomes
	for _, group := range []struct {
		in  []journal
		out *[]Completion
	}{
		{recovered.Native, &result.Native}, {recovered.NativeCleanup, &result.NativeCleanup}, {recovered.Committed, &result.Committed}, {recovered.RolledBack, &result.RolledBack}, {recovered.NoMutation, &result.NoMutation}, {recovered.Partial, &result.Partial}, {recovered.CleanupPending, &result.CleanupPending},
	} {
		for _, j := range group.in {
			if j.Manifest.Source {
				if j.State == repairJournalCleanupPending {
					continue
				}
				if err := cleanupRepairJournal(w.run, j.Directory, j.Manifest, options.Hooks); err != nil {
					return w, result, errors.Join(recoveryErr, err)
				}
				continue
			}
			*group.out = append(*group.out, w.completion(j))
		}
	}
	return w, result, recoveryErr
}

// Close invalidates in-memory handles. The caller still owns the borrowed
// lease; unfinished durable work is classified by the next Open.
func (w *Writer) Close() {
	if w != nil {
		w.closed = true
	}
}
func (w *Writer) check() error {
	if w == nil || w.closed {
		return fmt.Errorf("source writer is closed or absent")
	}
	if w.blocked != nil {
		return fmt.Errorf("source writer requires recovery: %w", w.blocked)
	}
	return w.run.Lease.Validate(w.run.VaultPath)
}
func (w *Writer) completion(j journal) Completion {
	s := &completionState{journal: j}
	w.completions[s] = true
	return Completion{writer: w, state: s}
}
func (w *Writer) owned(c Completion) error {
	if err := w.check(); err != nil {
		return err
	}
	if c.writer != w || c.state == nil || !w.completions[c.state] {
		return fmt.Errorf("source completion belongs to a different writer or is absent")
	}
	return nil
}
func info(j journal) JournalInfo {
	// The wire record contains only serializable values. Copy nested slices and
	// pointers so consumers cannot rewrite the owner's authoritative witnesses.
	encoded, _ := json.Marshal(j.Manifest)
	var m Manifest
	_ = json.Unmarshal(encoded, &m)
	return JournalInfo{Directory: j.Directory, Manifest: m, State: j.State, DecisionSynced: j.DecisionSynced, NativeCleanupDecision: j.NativeCleanupDecision}
}
func (c Completion) Info() JournalInfo {
	if c.state == nil {
		return JournalInfo{}
	}
	return info(c.state.journal)
}
func (t *Transaction) Info() JournalInfo {
	if t == nil || t.prepared == nil {
		return JournalInfo{}
	}
	j := journal{Directory: t.prepared.Directory, Manifest: t.prepared.Manifest, State: t.state, DecisionSynced: t.prepared.DecisionSynced}
	if j.Manifest.Namespace != nil {
		if state, err := nativeJournalDecision(j.Directory); err == nil {
			j.State = state
		}
	}
	return info(j)
}
func (w *Writer) Verify(c Completion) error {
	if err := w.owned(c); err != nil {
		return err
	}
	if c.state.completed {
		return nil
	}
	j := c.state.journal
	if j.Manifest.Namespace == nil && j.State == repairJournalCommitted {
		return verifyCommittedRepairJournal(w.run, j.Manifest)
	}
	return nil
}
func (w *Writer) Complete(c Completion) error {
	if err := w.owned(c); err != nil {
		return err
	}
	if c.state.completed {
		return nil
	}
	j := c.state.journal
	if j.State == "recovery_partial" || j.State == repairJournalPrepared {
		return fmt.Errorf("source completion is not terminal")
	}
	if err := w.Verify(c); err != nil {
		return err
	}
	if j.State == repairJournalCleanupPending {
		if err := cleanupDetachedRepairJournal(w.run, j.Directory, w.options.Hooks); err != nil {
			return err
		}
	} else if err := cleanupRepairJournal(w.run, j.Directory, j.Manifest, w.options.Hooks); err != nil {
		return err
	}
	c.state.completed = true
	return nil
}
