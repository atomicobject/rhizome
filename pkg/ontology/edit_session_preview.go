package ontology

import (
	"context"
	"errors"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/sourcetxn"
)

// Preview computes the current per-file diff without touching disk.
func (s *EditSession) Preview(ctx context.Context) (CommitPlan, error) {
	plan, _, err := s.plan(ctx, planOptions{IncludeDiff: true, IncludeUpdatedContentPreview: true})
	return plan, err
}

// PreviewWithRefLineage computes the current plan and the uniquely surviving
// field-edit identities produced by this session's own replay. Callers may use
// the lineage only for refs already resolved against this exact preview.
func (s *EditSession) PreviewWithRefLineage(ctx context.Context) (CommitPlan, []PreviewRefLineage, error) {
	plan, states, err := s.plan(ctx, planOptions{
		IncludeDiff:                  true,
		IncludeUpdatedContentPreview: true,
		CapturePreviewRefLineage:     true,
	})
	if err != nil {
		return plan, nil, err
	}
	lineage := make([]PreviewRefLineage, 0)
	for _, state := range states {
		lineage = append(lineage, state.refLineage...)
	}
	return plan, lineage, nil
}

// PreviewCurrent replays staged ops against current disk state without writing files.
// It surfaces safe rebases in the returned plan and reports typed conflicts
// when the underlying file changed in a way that invalidates staged assumptions.
func (s *EditSession) PreviewCurrent(ctx context.Context) (CommitPlan, []ConflictReport, error) {
	plan, _, conflicts, err := s.previewCurrent(ctx, false)
	return plan, conflicts, err
}

// PreviewCurrentWithRefLineage captures uniquely surviving field-edit refs
// during the same current-source replay used to authorize a commit. The caller
// may publish that lineage only after committing this exact plan.
func (s *EditSession) PreviewCurrentWithRefLineage(ctx context.Context) (CommitPlan, []PreviewRefLineage, []ConflictReport, error) {
	return s.previewCurrent(ctx, true)
}

func (s *EditSession) previewCurrent(ctx context.Context, captureLineage bool) (CommitPlan, []PreviewRefLineage, []ConflictReport, error) {
	plan, states, err := s.plan(ctx, planOptions{
		CompareWithDisk: true, IncludeDiff: true, IncludeUpdatedContentPreview: true,
		CapturePreviewRefLineage: captureLineage,
	})
	if err != nil {
		var conflictErr conflictListError
		if AsConflictList(err, &conflictErr) {
			return plan, nil, conflictErr.Conflicts, nil
		}
		return CommitPlan{}, nil, nil, err
	}
	var lineage []PreviewRefLineage
	for _, state := range states {
		lineage = append(lineage, state.refLineage...)
	}
	return plan, lineage, nil, nil
}

// Commit replays staged ops against current disk state and writes files when replay succeeds.
func (s *EditSession) Commit(ctx context.Context) (CommitResult, error) {
	return s.CommitWithOptions(ctx, CommitOptions{IncludeDiff: true, IncludeUpdatedContentPreview: true})
}

// CommitWithOptions replays staged ops against current disk state and writes files when replay succeeds.
func (s *EditSession) CommitWithOptions(ctx context.Context, opts CommitOptions) (CommitResult, error) {
	vaultPaths, err := paths.NewVaultPaths(s.vaultDef.BasePath())
	if err != nil {
		return CommitResult{Outcome: CommitOutcomeFailed}, err
	}
	lease, release, err := acquireSourceEditLease(ctx, vaultPaths.Root(), opts.WriteLease, true)
	if err != nil {
		return CommitResult{Outcome: CommitOutcomeFailed}, err
	}
	defer func() { _ = release() }()
	writer, _, err := sourcetxn.Open(vaultPaths.Root(), lease, sourcetxn.WriterOptions{Role: sourcetxn.SourceEditor})
	if writer != nil {
		defer writer.Close()
	}
	if err != nil {
		return CommitResult{Outcome: CommitOutcomeFailed}, err
	}
	plan, states, err := s.plan(ctx, planOptions{
		CompareWithDisk:              true,
		IncludeDiff:                  opts.IncludeDiff,
		IncludeUpdatedContentPreview: opts.IncludeUpdatedContentPreview,
	})
	if err != nil {
		var conflictErr conflictListError
		if AsConflictList(err, &conflictErr) {
			return CommitResult{
				Outcome:   CommitOutcomeConflicted,
				Plan:      plan,
				Conflicts: conflictErr.Conflicts,
				Applied:   false,
			}, nil
		}
		return CommitResult{}, err
	}
	if !planHasMaterialChange(plan) {
		return CommitResult{
			Outcome: CommitOutcomeUnchanged,
			Plan:    plan,
			Applied: false,
			Rebased: planHasRebased(plan),
		}, nil
	}
	for _, state := range states {
		rootOnly, formatErr := state.providerRootOnly()
		if formatErr != nil {
			return CommitResult{}, formatErr
		}
		if rootOnly {
			return CommitResult{}, fmt.Errorf("provider root metadata saves require the journaled application adapter")
		}
	}
	materialNotePaths := materialNotePathSet(plan)
	for notePath, state := range states {
		if state == nil || !materialNotePaths[notePath] {
			delete(states, notePath)
		}
	}
	committedResult := CommitResult{
		Outcome: CommitOutcomeCommitted,
		Plan:    plan,
		Applied: true,
		Rebased: planHasRebased(plan),
	}
	writeErr := publishSourceEditStates(ctx, states, &vaultPaths, writer)
	if writeErr != nil {
		var warning *sourcetxn.CommittedError
		if errors.As(writeErr, &warning) {
			committedResult.Warnings = []string{warning.Error()}
			return committedResult, nil
		}
		var recovery *sourcetxn.RecoveryRequiredError
		if errors.As(writeErr, &recovery) {
			return CommitResult{Outcome: CommitOutcomeFailed}, writeErr
		}
		if errors.Is(writeErr, sourcetxn.ErrSourceChanged) {
			return CommitResult{Outcome: CommitOutcomeConflicted, Plan: plan, Conflicts: []ConflictReport{{Kind: ConflictKindSourceChanged, NotePath: sourcetxn.ConflictPath(writeErr), Message: writeErr.Error()}}, Rebased: planHasRebased(plan)}, nil
		}
		return CommitResult{Outcome: CommitOutcomeFailed}, writeErr
	}

	return committedResult, nil
}
