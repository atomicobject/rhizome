package indexwriter

import (
	"errors"
	"runtime"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

func (q *Writer) run() {
	defer close(q.doneCh)
	defer q.cancel()

	ticker := time.NewTicker(q.cfg.PollInterval)
	defer ticker.Stop()
	writeNodeCtx := indexingperf.WithPhase(q.ctx, "total")

	s := &queueState{kinds: newPayloadKinds(q)}

	defer func() {
		err := q.Err()
		if err == nil {
			err = q.ctx.Err()
		}
		if err == nil {
			err = errors.New("index write queue stopped before control completed")
		}
		for _, ctrl := range s.pendingCtrls {
			ctrl.complete(err)
		}
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

	handleCommand = func(raw any) bool { return s.handleCommand(q, raw) }

	for {
		if err := q.ctx.Err(); err != nil {
			q.fail(err)
			return
		}
		barrierReady := len(s.pendingCtrls) > 0 && q.barrierSettled(s.pendingCtrls[0].barrier())
		s.holdingForCtrl = len(s.pendingCtrls) > 0 && !barrierReady
		if barrierReady {
			ctrl := s.pendingCtrls[0]
			s.pendingCtrls = s.pendingCtrls[1:]
			s.holdingForCtrl = false
			if err := drainPendingPayloads(); err != nil {
				ctrl.complete(err)
				q.fail(err)
				return
			}
			err := s.flushAll(q)
			if err == nil {
				err = q.ctx.Err()
			}
			if err == nil {
				err = ctrl.run(q)
			}
			ctrl.complete(err)
			if err != nil {
				q.fail(err)
				return
			}
			if ctrl.closesWriter() {
				return
			}
			continue
		}

		waitingForInput := len(q.ctrlCh) == 0 && len(q.codeCmdCh) == 0 && len(q.otherCmdCh) == 0 && !s.hasPendingPayloads()
		waitStarted := time.Now()
		if len(q.ctrlCh) > 0 {
			if waitingForInput {
				indexingperf.ObserveLatency(writeNodeCtx, indexingperf.NodeWriteStarved, time.Since(waitStarted))
			}
			if !handleCommand(<-q.ctrlCh) {
				return
			}
			continue
		}
		codeHot := q.cfg.codePriorityActive(time.Now(), s.lastCodeAt, s.kinds[0].pending()+len(q.codeCmdCh))
		if codeHot && len(q.codeCmdCh) > 0 {
			if waitingForInput {
				indexingperf.ObserveLatency(writeNodeCtx, indexingperf.NodeWriteStarved, time.Since(waitStarted))
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
				indexingperf.ObserveLatency(writeNodeCtx, indexingperf.NodeWriteStarved, time.Since(waitStarted))
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
				indexingperf.ObserveLatency(writeNodeCtx, indexingperf.NodeWriteStarved, time.Since(waitStarted))
			}
			if err := q.Err(); err != nil {
				return
			}
			q.fail(q.ctx.Err())
			return
		case raw := <-q.ctrlCh:
			if waitingForInput {
				indexingperf.ObserveLatency(writeNodeCtx, indexingperf.NodeWriteStarved, time.Since(waitStarted))
			}
			if !handleCommand(raw) {
				return
			}
		case raw := <-q.codeCmdCh:
			if waitingForInput {
				indexingperf.ObserveLatency(writeNodeCtx, indexingperf.NodeWriteStarved, time.Since(waitStarted))
			}
			if !handleCommand(raw) {
				return
			}
			if s.holdingForCtrl {
				runtime.Gosched()
			}
		case raw := <-q.otherCmdCh:
			if waitingForInput {
				indexingperf.ObserveLatency(writeNodeCtx, indexingperf.NodeWriteStarved, time.Since(waitStarted))
			}
			if !handleCommand(raw) {
				return
			}
			if s.holdingForCtrl {
				runtime.Gosched()
			}
		case <-ticker.C:
			if waitingForInput {
				indexingperf.ObserveLatency(writeNodeCtx, indexingperf.NodeWriteStarved, time.Since(waitStarted))
			}
			if err := s.flushExpired(q, time.Now()); err != nil {
				q.fail(err)
				return
			}
		}
	}
}
