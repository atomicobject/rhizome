package sourcetxn

import (
	"errors"
	"fmt"
)

func (w *Writer) transaction(t *Transaction) error {
	if err := w.check(); err != nil {
		return err
	}
	if t == nil || t.writer != w || t.prepared == nil {
		return fmt.Errorf("source transaction belongs to a different writer or is absent")
	}
	return nil
}
func (w *Writer) Publish(t *Transaction) (Completion, error) {
	if err := w.transaction(t); err != nil {
		return Completion{}, err
	}
	if t.state == repairJournalCommitted {
		return t.completion, nil
	}
	if t.state != repairJournalPrepared || w.active != t {
		return Completion{}, fmt.Errorf("source transaction cannot publish from %s", t.state)
	}
	if t.prepared.Manifest.Namespace != nil {
		return w.publishNamespace(t)
	}
	err := commitPreparedRepairTransaction(w.run, t.prepared, w.options.Hooks)
	if err != nil {
		var uncertain commitDecisionUncertainError
		if errors.As(err, &uncertain) {
			t.state = "recovery_required"
			w.blocked = err
			w.active = nil
			return Completion{}, &RecoveryRequiredError{CommitErr: uncertain.Err, RecoveryErr: err}
		}
		var syncErr NamespaceDecisionSyncError
		if errors.Is(err, ErrSimulatedInterruption) || errors.As(err, &syncErr) {
			t.state = "recovery_required"
			w.blocked = err
			w.active = nil
			return Completion{}, err
		}
		rollbackErr := abortPreparedRepairTransaction(w.run, t.prepared, w.options.Hooks)
		w.active = nil
		if rollbackErr != nil {
			t.state = "recovery_required"
			w.blocked = rollbackErr
			return Completion{}, &RecoveryRequiredError{CommitErr: err, RecoveryErr: rollbackErr}
		}
		t.state = "aborted"
		var precondition CommitPreconditionError
		if errors.As(err, &precondition) {
			err = &SourceConflictError{Path: precondition.Path, Err: err}
		}
		return Completion{}, err
	}
	t.state = repairJournalCommitted
	w.active = nil
	j := journal{Directory: t.prepared.Directory, Manifest: t.prepared.Manifest, State: repairJournalCommitted, DecisionSynced: true}
	t.completion = w.completion(j)
	return t.completion, nil
}
func (w *Writer) Abort(t *Transaction) error {
	if err := w.transaction(t); err != nil {
		return err
	}
	if t.state == "aborted" {
		return nil
	}
	if (t.state != repairJournalPrepared && t.state != "preparation_failed") || w.active != t {
		return fmt.Errorf("source transaction cannot abort from %s", t.state)
	}
	if err := abortPreparedRepairTransaction(w.run, t.prepared, w.options.Hooks); err != nil {
		t.state = "recovery_required"
		w.blocked = err
		w.active = nil
		return err
	}
	t.state = "aborted"
	w.active = nil
	return nil
}
