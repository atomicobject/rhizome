package ontology

import (
	"context"
	"fmt"
	"sort"
	"sync"

	textdiff "github.com/atomicobject/rhizome/pkg/textdiff"
)

func (s *EditSession) plan(ctx context.Context, opts planOptions) (CommitPlan, map[string]*documentState, error) {
	if err := ctx.Err(); err != nil {
		return CommitPlan{}, nil, err
	}
	if s.noteMgr == nil {
		return CommitPlan{}, nil, fmt.Errorf("note reader is required")
	}
	grouped := make(map[string][]editOp)
	for _, op := range s.ops {
		notePath := op.notePath()
		grouped[notePath] = append(grouped[notePath], op)
	}
	pathsList := make([]string, 0, len(grouped))
	for notePath := range grouped {
		pathsList = append(pathsList, notePath)
	}
	sort.Strings(pathsList)

	plan := CommitPlan{Files: make([]FileCommitPlan, 0, len(pathsList))}
	states := make(map[string]*documentState, len(pathsList))
	conflicts := make([]ConflictReport, 0)
	jobs := make([]fileEditPlanJob, 0, len(pathsList))

	for index, notePath := range pathsList {
		baseState := s.docs[notePath]
		loadedBase := baseState == nil
		if baseState == nil {
			// Capture the base snapshot at first replay, not at op staging time.
			// Browser sessions that need restart recovery seed this explicitly via
			// RestoreBaseFingerprints so drift remains visible after restore.
			baseSnapshot, err := s.loadEditDocumentSnapshot(ctx, notePath)
			if err != nil {
				return plan, nil, err
			}
			baseState = &documentState{
				notePath:              notePath,
				content:               baseSnapshot.Content,
				baseContent:           baseSnapshot.Content,
				baseSnapshot:          baseSnapshot,
				schema:                s.schema,
				formats:               s.formats,
				allowSelectorRecovery: s.allowSelectorRecovery,
			}
			s.docs[notePath] = baseState
		}
		var currentSnapshot *DocumentSnapshot
		if opts.CompareWithDisk && loadedBase {
			currentSnapshot = baseState.baseSnapshot
		} else if opts.CompareWithDisk {
			var err error
			currentSnapshot, err = s.loadEditDocumentSnapshot(ctx, notePath)
			if err != nil {
				return plan, nil, err
			}
		}
		jobs = append(jobs, fileEditPlanJob{
			index:           index,
			notePath:        notePath,
			baseState:       baseState,
			currentSnapshot: currentSnapshot,
			ops:             grouped[notePath],
		})
	}

	results := s.planFiles(jobs, opts)
	for _, result := range results {
		if result.err != nil {
			return plan, nil, result.err
		}
		if result.conflict != nil {
			conflicts = append(conflicts, *result.conflict)
		}
		states[result.notePath] = result.state
		plan.Files = append(plan.Files, result.filePlan)
	}
	if len(conflicts) > 0 {
		return plan, nil, conflictListError{Conflicts: conflicts}
	}
	return plan, states, nil
}

type fileEditPlanJob struct {
	index           int
	notePath        string
	baseState       *documentState
	currentSnapshot *DocumentSnapshot
	ops             []editOp
}

type fileEditPlanResult struct {
	index    int
	notePath string
	state    *documentState
	filePlan FileCommitPlan
	conflict *ConflictReport
	err      error
}

const maxConcurrentFilePlans = 8

func (s *EditSession) planFiles(jobs []fileEditPlanJob, opts planOptions) []fileEditPlanResult {
	results := make([]fileEditPlanResult, len(jobs))
	if len(jobs) == 0 {
		return results
	}
	workerCount := min(len(jobs), maxConcurrentFilePlans)
	work := make(chan fileEditPlanJob)
	var wg sync.WaitGroup
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range work {
				results[job.index] = s.planFile(job, opts)
			}
		}()
	}
	for _, job := range jobs {
		work <- job
	}
	close(work)
	wg.Wait()
	return results
}

func (s *EditSession) planFile(job fileEditPlanJob, opts planOptions) fileEditPlanResult {
	result := fileEditPlanResult{
		index:    job.index,
		notePath: job.notePath,
	}
	state := &documentState{
		notePath:              job.notePath,
		content:               job.baseState.baseContent,
		baseContent:           job.baseState.baseContent,
		baseSnapshot:          job.baseState.baseSnapshot,
		schema:                s.schema,
		formats:               s.formats,
		allowSelectorRecovery: s.allowSelectorRecovery,
		captureRefLineage:     opts.CapturePreviewRefLineage,
	}
	rebased := false
	currentFingerprint := job.baseState.baseSnapshot.ContentFingerprint
	currentContent := job.baseState.baseContent
	if opts.CompareWithDisk {
		if job.currentSnapshot == nil {
			result.err = fmt.Errorf("plan file %s: current snapshot is required when comparing with disk", job.notePath)
			return result
		}
		currentSnapshot := job.currentSnapshot
		currentFingerprint = currentSnapshot.ContentFingerprint
		currentContent = currentSnapshot.Content
		state.expectedCurrentFingerprint = currentFingerprint
		state.originalContent = currentContent
		if currentSnapshot.ContentFingerprint != job.baseState.baseSnapshot.ContentFingerprint {
			// IMPORTANT: replay ops against current disk content for commit and
			// PreviewCurrent, but keep the original base fingerprint in the plan
			// so callers can report a safe rebase instead of pretending the file
			// was unchanged.
			state.setContent(currentSnapshot.Content)
			rebased = true
		}
	}
	if opts.CompareWithDisk {
		if conflict := fieldChangedConflict(job.baseState, state, job.ops); conflict != nil {
			result.conflict = conflict
		}
	}
	finish := func() fileEditPlanResult {
		result.state = state
		result.filePlan = FileCommitPlan{
			NotePath:           job.notePath,
			BaseFingerprint:    job.baseState.baseSnapshot.ContentFingerprint,
			CurrentFingerprint: currentFingerprint,
			UpdatedFingerprint: hashText(state.content),
			Rebased:            rebased,
			HasMaterialChange:  state.content != currentContent,
		}
		result.err = populatePlanPayload(&result.filePlan, state, opts)
		return result
	}
	if result.conflict != nil {
		return finish()
	}
	baseNoop := false
	baseReplayNoop := false
	if opts.CompareWithDisk && rebased && !hasExplicitFieldWitness(job.ops) {
		baseReplay := &documentState{
			notePath:              job.notePath,
			content:               job.baseState.baseContent,
			baseContent:           job.baseState.baseContent,
			baseSnapshot:          job.baseState.baseSnapshot,
			schema:                s.schema,
			formats:               s.formats,
			allowSelectorRecovery: s.allowSelectorRecovery,
		}
		baseNoop = true
		for _, op := range job.ops {
			if !editOpMatchesBase(baseReplay, op) {
				baseNoop = false
				break
			}
		}
		if !baseNoop {
			baseReplayOK := applyEditOps(baseReplay, job.ops) == nil
			baseReplayNoop = baseReplayOK && baseReplay.content == job.baseState.baseContent
		}
	}
	if opts.CompareWithDisk && rebased && (baseNoop || baseReplayNoop) {
		state.setContent(currentContent)
		return finish()
	}
	if err := applyEditOps(state, job.ops); err != nil {
		conflict := conflictFromError(job.notePath, err.op, err.err)
		result.conflict = &conflict
	}
	return finish()
}

func hasExplicitFieldWitness(ops []editOp) bool {
	for _, rawOp := range ops {
		if op, ok := rawOp.(setFieldOp); ok && op.ExpectedCaptured {
			return true
		}
	}
	return false
}

func populatePlanPayload(filePlan *FileCommitPlan, state *documentState, opts planOptions) error {
	if opts.IncludeDiff {
		diffText, _, err := textdiff.GenerateUnifiedDiff(state.baseContent, state.content, state.notePath)
		if err != nil {
			return err
		}
		filePlan.Diff = diffText
	}
	if opts.IncludeUpdatedContentPreview {
		filePlan.UpdatedContentPreview = state.content
	}
	return nil
}

type editApplyError struct {
	op  editOp
	err error
}
