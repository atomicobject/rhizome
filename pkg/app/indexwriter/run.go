package indexwriter

import (
	"errors"
	"fmt"
	"runtime"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	codeindex "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
	"github.com/atomicobject/rhizome/pkg/search/intentstore"
)

func (q *Writer) run() {
	defer close(q.doneCh)

	ticker := time.NewTicker(q.cfg.PollInterval)
	defer ticker.Stop()
	writeNodeCtx := indexingperf.WithPhase(q.ctx, "total")

	s := &queueState{
		codeWorks:       make(map[string]*queueBatch[codeanchor.CodeIndexWork]),
		noteWorks:       make(map[string]*queueBatch[codeanchor.NoteIndexWork]),
		noteMetas:       make(map[string]*queueBatch[embeddings.NoteFileInfo]),
		itemEmbeds:      make(map[string]*queueBatch[codeindex.ItemEmbeddingUpsert]),
		codeChunkJobs:   make(map[string]*queueBatch[codeindex.ItemChunksUpsert]),
		noteChunkSyncs:  make(map[string]*queueBatch[embeddings.NoteChunkSync]),
		noteChunkJobs:   make(map[string]*queueBatch[noteChunkWrite]),
		intelChunks:     make(map[string]*intelChunkBatch),
		intelEmbeds:     make(map[string]*intelEmbeddingBatch),
		ontologyNodes:   make(map[string]*queueBatch[ontologyNodeWrite]),
		ontologyStates:  make(map[string]*queueBatch[codeanchor.IntelOntologyNodeEmbeddingState]),
		noteMetadata:    make(map[string]*queueBatch[noteMetadataDeltaWrite]),
		ontologyOps:     make(map[string]*ontologyDeltaBatch),
		validation:      make(map[string]*queueBatch[ValidationStateWrite]),
		intentSnapshots: make(map[string]*queueBatch[intentstore.Snapshot]),
	}

	defer func() {
		for phase := range s.intentSnapshots {
			indexingperf.SetGauge(indexingperf.WithPhase(q.ctx, phase), intentSnapshotPendingRows, 0)
		}
	}()

	ackControl := func(ctrl pendingControl, result semdb.OwnershipTransitionResult, err error) {
		switch {
		case ctrl.graph != nil:
			ctrl.graph.ack <- err
		case ctrl.structural != nil:
			ctrl.structural.ack <- err
		case ctrl.derived != nil:
			ctrl.derived.ack <- derivedAck{err: err}
		case ctrl.transitionAck != nil:
			ctrl.transitionAck <- ownershipTransitionAck{result: result, err: err}
		case ctrl.reconciliationAck != nil:
			ctrl.reconciliationAck <- err
		default:
			ctrl.ack <- err
		}
	}

	ackPendingControls := func(err error) {
		if len(s.pendingCtrls) == 0 {
			return
		}
		if err == nil {
			err = q.Err()
		}
		if err == nil {
			err = q.ctx.Err()
		}
		if err == nil {
			err = errors.New("index write queue stopped before control completed")
		}
		for _, ctrl := range s.pendingCtrls {
			ackControl(ctrl, semdb.OwnershipTransitionResult{}, err)
		}
		s.pendingCtrls = nil
	}
	defer func() {
		ackPendingControls(q.Err())
	}()

	var handleCommand func(raw any) bool

	drainPendingPayloads := func() error {
		s.drainingToCtrl = true
		defer func() { s.drainingToCtrl = false }()
		codePending := len(q.codeCmdCh)
		otherPending := len(q.otherCmdCh)
		for i := 0; i < codePending; i++ {
			if !handleCommand(<-q.codeCmdCh) {
				return q.Err()
			}
		}
		for i := 0; i < otherPending; i++ {
			if !handleCommand(<-q.otherCmdCh) {
				return q.Err()
			}
		}
		return nil
	}

	handleCommand = func(raw any) bool { return s.handleCommand(q, raw, drainPendingPayloads, ackControl) }

	for {
		barrierReady := len(s.pendingCtrls) > 0 && q.barrierSettled(s.pendingCtrls[0].barrier)
		s.holdingForCtrl = len(s.pendingCtrls) > 0 && !barrierReady
		if barrierReady {
			ctrl := s.pendingCtrls[0]
			s.pendingCtrls = s.pendingCtrls[1:]
			s.holdingForCtrl = false
			if err := drainPendingPayloads(); err != nil {
				ackControl(ctrl, semdb.OwnershipTransitionResult{}, err)
				q.fail(err)
				return
			}
			err := s.flushAll(q)
			if err != nil {
				ackControl(ctrl, semdb.OwnershipTransitionResult{}, err)
				q.fail(err)
				return
			}
			if ctrl.close {
				ackControl(ctrl, semdb.OwnershipTransitionResult{}, nil)
				return
			}
			if ctrl.graph != nil {
				if q.handlers.ApplyGraphScores == nil {
					err = fmt.Errorf("graph scores queue not initialized")
				} else {
					err = q.handlers.ApplyGraphScores(q.ctx, ctrl.graph.scores)
				}
				ctrl.graph.ack <- err
				if err != nil {
					q.fail(err)
					return
				}
				continue
			}
			if ctrl.structural != nil {
				if q.handlers.ApplyStructuralFinalize == nil {
					err = fmt.Errorf("structural finalization queue not initialized")
				} else {
					err = q.handlers.ApplyStructuralFinalize(q.ctx, ctrl.structural.work)
				}
				ctrl.structural.ack <- err
				if err != nil {
					q.fail(err)
					return
				}
				continue
			}
			if ctrl.derived != nil {
				result := q.applyDerived(ctrl.derived)
				ctrl.derived.ack <- result
				if result.err != nil {
					q.fail(result.err)
					return
				}
				continue
			}
			if ctrl.transitionAck != nil {
				if q.handlers.ApplyOwnershipTransitions == nil {
					err = errors.New("ownership transition queue not initialized")
					q.fail(err)
					ackControl(ctrl, semdb.OwnershipTransitionResult{}, err)
					return
				}
				result, applyErr := q.handlers.ApplyOwnershipTransitions(ctxWithPhaseOp(q.ctx, indexQueueDefaultPhaseLabel, "intel.apply_ownership_transition"), ctrl.transitions)
				if applyErr != nil {
					q.fail(applyErr)
					ackControl(ctrl, result, applyErr)
					return
				}
				ackControl(ctrl, result, nil)
				continue
			}
			if ctrl.reconciliationAck != nil {
				if q.handlers.AcknowledgeOwnershipReconciliation == nil {
					err = errors.New("ownership reconciliation queue not initialized")
				} else {
					acknowledged, acknowledgeErr := q.handlers.AcknowledgeOwnershipReconciliation(ctxWithPhaseOp(q.ctx, indexQueueDefaultPhaseLabel, "intel.acknowledge_ownership_reconciliation"), ctrl.reconciliationGeneration)
					err = acknowledgeErr
					if err == nil && !acknowledged {
						err = fmt.Errorf("ownership reconciliation generation %d was not acknowledged", ctrl.reconciliationGeneration)
					}
				}
				if err != nil {
					q.fail(err)
					ackControl(ctrl, semdb.OwnershipTransitionResult{}, err)
					return
				}
				recordReconciliationAcknowledged(q.ctx, ctrl.reconciliationGeneration)
				ackControl(ctrl, semdb.OwnershipTransitionResult{}, nil)
				continue
			}
			ackControl(ctrl, semdb.OwnershipTransitionResult{}, nil)
			continue
		}

		waitingForInput := len(q.ctrlCh) == 0 &&
			len(q.codeCmdCh) == 0 &&
			len(q.otherCmdCh) == 0 &&
			len(s.codeWorks) == 0 &&
			len(s.noteWorks) == 0 &&
			len(s.noteMetas) == 0 &&
			len(s.itemEmbeds) == 0 &&
			len(s.codeChunkJobs) == 0 &&
			len(s.noteChunkSyncs) == 0 &&
			len(s.noteChunkJobs) == 0 &&
			len(s.intelChunks) == 0 &&
			len(s.intelEmbeds) == 0 &&
			len(s.noteMetadata) == 0 &&
			len(s.ontologyOps) == 0 &&
			len(s.validation) == 0 &&
			len(s.intentSnapshots) == 0
		waitStarted := time.Now()
		if len(q.ctrlCh) > 0 {
			if waitingForInput {
				indexingperf.ObserveLatency(writeNodeCtx, "node.write.starved", time.Since(waitStarted))
			}
			if !handleCommand(<-q.ctrlCh) {
				return
			}
			continue
		}
		codeHot := q.cfg.codePriorityActive(time.Now(), s.lastCodeAt, len(s.codeWorks)+len(q.codeCmdCh))
		if codeHot && len(q.codeCmdCh) > 0 {
			if waitingForInput {
				indexingperf.ObserveLatency(writeNodeCtx, "node.write.starved", time.Since(waitStarted))
			}
			if !handleCommand(<-q.codeCmdCh) {
				return
			}
			if s.holdingForCtrl {
				runtime.Gosched()
			}
			continue
		}
		if !codeHot && len(q.otherCmdCh) > 0 {
			if waitingForInput {
				indexingperf.ObserveLatency(writeNodeCtx, "node.write.starved", time.Since(waitStarted))
			}
			if !handleCommand(<-q.otherCmdCh) {
				return
			}
			if s.holdingForCtrl {
				runtime.Gosched()
			}
			continue
		}

		select {
		case <-q.ctx.Done():
			if waitingForInput {
				indexingperf.ObserveLatency(writeNodeCtx, "node.write.starved", time.Since(waitStarted))
			}
			if err := q.Err(); err != nil {
				return
			}
			q.fail(q.ctx.Err())
			return
		case raw := <-q.ctrlCh:
			if waitingForInput {
				indexingperf.ObserveLatency(writeNodeCtx, "node.write.starved", time.Since(waitStarted))
			}
			if !handleCommand(raw) {
				return
			}
		case raw := <-q.codeCmdCh:
			if waitingForInput {
				indexingperf.ObserveLatency(writeNodeCtx, "node.write.starved", time.Since(waitStarted))
			}
			if !handleCommand(raw) {
				return
			}
			if s.holdingForCtrl {
				runtime.Gosched()
			}
		case raw := <-q.otherCmdCh:
			if waitingForInput {
				indexingperf.ObserveLatency(writeNodeCtx, "node.write.starved", time.Since(waitStarted))
			}
			if !handleCommand(raw) {
				return
			}
			if s.holdingForCtrl {
				runtime.Gosched()
			}
		case <-ticker.C:
			if waitingForInput {
				indexingperf.ObserveLatency(writeNodeCtx, "node.write.starved", time.Since(waitStarted))
			}
			if err := s.flushExpired(q, time.Now()); err != nil {
				q.fail(err)
				return
			}
		}
	}
}
