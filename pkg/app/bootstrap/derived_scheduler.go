package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search/graphdb"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

var errDerivedSuperseded = fmt.Errorf("derived work superseded: %w", lane.ErrSkipped)
var errDerivedExternalPriority = errors.New("derived work preempted by external priority")

type derivedScheduler struct {
	watcher *unifiedSemanticWatcher
	ctx     context.Context
	cancel  context.CancelFunc
	wake    chan struct{}
	done    chan struct{}
	mu      sync.Mutex
	running codeanchor.DerivedKind
	witness string
}

func newDerivedScheduler(w *unifiedSemanticWatcher) *derivedScheduler {
	ctx, cancel := context.WithCancel(w.runCtx)
	s := &derivedScheduler{watcher: w, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), done: make(chan struct{})}
	go s.run()
	return s
}
func (s *derivedScheduler) close() { s.cancel(); <-s.done }
func (s *derivedScheduler) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *derivedScheduler) run() {
	defer close(s.done)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.wake:
		case <-ticker.C:
		}
		work, err := s.watcher.intelStore.PendingDerivedWork(s.ctx, time.Now(), 32)
		if err != nil {
			if s.ctx.Err() != nil {
				return
			}
			continue
		}
		if len(work) > 0 {
			watcherEvent(s.ctx, slog.LevelInfo, "derived.pending", slog.Int("ready_tickets", len(work)))
		}
		for _, ticket := range work {
			if s.ctx.Err() != nil {
				return
			}
			s.mu.Lock()
			s.running = ticket.Kind
			s.mu.Unlock()
			err = s.execute(ticket)
			s.mu.Lock()
			s.running = ""
			s.mu.Unlock()
			if err != nil && !errors.Is(err, errDerivedSuperseded) && s.ctx.Err() == nil {
				delay := 250 * time.Millisecond << min(ticket.Attempt, 6)
				_ = s.withLease(s.ctx, ticket, false, "derived_retry", func(ctx context.Context) error {
					return s.watcher.intelStore.RetryDerivedWork(ctx, ticket, time.Now().Add(delay))
				})
			}
		}
	}
}

func (s *derivedScheduler) withLease(parent context.Context, ticket codeanchor.DerivedWork, ack bool, phase string, run func(context.Context) error) error {
	w := s.watcher
	handle, _, err := w.lane.Submit(parent, lane.Request{Kind: lane.KindEmbedCycle, Run: func(ctx context.Context, _ lane.Reporter) (jobErr error) {
		ctx, finish := watchPhase(ctx, phase)
		defer func() {
			if panicked := recover(); panicked != nil {
				finish(errors.New("diagnostic boundary panicked"))
				panic(panicked)
			}
			finish(jobErr)
		}()
		defer watchDuration(parent, phase)()
		current, err := w.intelStore.CurrentDerivedWork(ctx, ticket)
		if err != nil {
			return err
		}
		if !current {
			return errDerivedSuperseded
		}
		if s.witness != "" {
			fingerprint, err := w.intelStore.DerivedSourceFingerprint(ctx, ticket.DerivedScope)
			if err != nil {
				return err
			}
			if fingerprint != s.witness {
				return errDerivedSuperseded
			}
		}
		if err := run(ctx); err != nil {
			return err
		}
		if ack {
			if err := w.intelStore.AckDerivedWork(ctx, []codeanchor.DerivedWork{ticket}); err != nil {
				return err
			}
			indexingperf.AddCount(parent, "derived.acknowledged", 1)
			watcherEvent(ctx, slog.LevelInfo, "derived.acknowledged", derivedAttrs(ticket)...)
			if w.publishEvent != nil {
				data := map[string]any{"source": "vault-runtime", "domains": []string{string(ticket.Kind)}}
				if ticket.Path != "" {
					data["paths"] = []string{ticket.Path}
				}
				w.publishEvent(globalEventIndexChanged, data)
			}
		}
		return nil
	}})
	if err != nil {
		return err
	}
	select {
	case <-s.ctx.Done():
		handle.Cancel()
		<-handle.Done()
		return s.ctx.Err()
	case <-handle.Done():
		return handle.Err()
	}
}

func (s *derivedScheduler) computeContext(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				status := s.watcher.lane.Status()
				if status.JobKind == lane.KindExplicitIndex {
					cancel(lane.ErrPreempted)
					return
				}
				if indexlock.CheckPriority(obsidian.IndexLockPath(s.watcher.vaultPath)) {
					cancel(errDerivedExternalPriority)
					return
				}
			}
		}
	}()
	return ctx, func() { cancel(context.Canceled); <-done }
}

func (s *derivedScheduler) execute(ticket codeanchor.DerivedWork) (resultErr error) {
	ctx, finish := observeDerivedWork(s.ctx, ticket)
	defer func() {
		if panicked := recover(); panicked != nil {
			finish(errors.New("diagnostic boundary panicked"))
			panic(panicked)
		}
		finish(resultErr)
	}()
	return s.executeObserved(ctx, ticket)
}

func (s *derivedScheduler) executeObserved(ctx context.Context, ticket codeanchor.DerivedWork) error {
	s.witness = ""
	if err := s.withLease(ctx, ticket, false, "derived_prepare", func(ctx context.Context) error {
		var err error
		s.witness, err = s.watcher.intelStore.DerivedSourceFingerprint(ctx, ticket.DerivedScope)
		return err
	}); err != nil {
		return err
	}
	w := s.watcher
	switch ticket.Kind {
	case codeanchor.DerivedNotes:
		if w.noteSyncer == nil {
			if w.noteSemanticStore {
				return fmt.Errorf("note syncer unavailable")
			}
			return s.withLease(ctx, ticket, true, "derived_publication", func(context.Context) error { return nil })
		}
		var plan semantic.NotePlan
		err := s.withLease(ctx, ticket, false, "derived_prepare", func(ctx context.Context) error {
			var err error
			if ticket.Path == "" {
				plan, err = w.noteSyncer.Plan(ctx)
			} else {
				plan, err = w.noteSyncer.PlanPaths(ctx, []string{ticket.Path})
			}
			return err
		})
		if err != nil {
			return err
		}
		err = s.compute(ctx, "embed_notes", func(computeCtx context.Context) error {
			return w.noteSyncer.ComputeDerived(computeCtx, plan, func(_ context.Context, result semantic.NoteDerivedResult) error {
				return s.withLease(ctx, ticket, false, "derived_publication", func(ctx context.Context) error { return w.noteSyncer.PublishDerived(ctx, result) })
			})
		})
		if err != nil {
			return err
		}
		return s.withLease(ctx, ticket, true, "derived_publication", func(ctx context.Context) error { return w.noteSyncer.FinishDerived(ctx, plan) })
	case codeanchor.DerivedCode:
		if w.codeSyncer == nil {
			if w.codeSemanticStore {
				return fmt.Errorf("code syncer unavailable")
			}
			return s.withLease(ctx, ticket, true, "derived_publication", func(context.Context) error { return nil })
		}
		var plan semantic.SyncPlan
		err := s.withLease(ctx, ticket, false, "derived_prepare", func(ctx context.Context) error {
			var err error
			paths := []string{ticket.Path}
			if ticket.Path == "" {
				paths, err = w.intelStore.IndexedFilePaths(ctx)
				if err != nil {
					return err
				}
				// Full recovery must visit stranded legacy semantic items even
				// when structural ownership already removed their final file.
				items, err := w.codeSyncer.Index.ListItems(ctx)
				if err != nil {
					return err
				}
				for _, item := range items {
					paths = append(paths, item.Path)
				}

			}
			loader, err := w.publishedCodeLoader(ctx, paths)
			if err != nil {
				return err
			}
			plan, err = w.codeSyncer.PlanPublishedPaths(ctx, paths, loader)
			return err
		})
		if err != nil {
			return err
		}
		err = s.compute(ctx, "embed_code", func(computeCtx context.Context) error {
			return w.codeSyncer.ComputeDerived(computeCtx, plan, func(_ context.Context, result semantic.CodeDerivedResult) error {
				return s.withLease(ctx, ticket, false, "derived_publication", func(ctx context.Context) error { return w.codeSyncer.PublishDerived(ctx, result) })
			})
		})
		if err != nil {
			return err
		}
		return s.withLease(ctx, ticket, true, "derived_publication", func(context.Context) error { return nil })
	case codeanchor.DerivedOntology:
		if w.nodeSyncer == nil {
			if w.noteSemanticStore {
				return fmt.Errorf("ontology syncer unavailable")
			}
			return s.withLease(ctx, ticket, true, "derived_publication", func(context.Context) error { return nil })
		}
		var plan semantic.OntologyDerivedPlan
		var syncer semantic.OntologyNodeSyncer
		err := s.withLease(ctx, ticket, false, "derived_prepare", func(ctx context.Context) error {
			schema, err := ontology.LoadSchema(w.vaultPath)
			if err != nil {
				return err
			}
			syncer = *w.nodeSyncer
			syncer.Schema = schema
			if schema == nil {
				return nil
			}
			state, err := w.intelStore.GetOntologySchemaState(ctx)
			if err != nil {
				return err
			}
			if schema.Hash != state.SchemaHash {
				return errDerivedSuperseded
			}
			paths := []string{ticket.Path}
			if ticket.Path == "" {
				paths, err = ontology.ProjectableMetadataPaths(ctx, w.noteMetadataIndexer, w.intelStore)
				if err != nil {
					return err
				}
			}
			rows, err := w.intelStore.CurrentNoteMetadataRowsByPaths(ctx, paths)
			if err != nil {
				return err
			}
			live := paths[:0]
			for _, path := range paths {
				if _, ok := rows[path]; ok {
					live = append(live, path)
				}
			}
			reader, err := w.publishedNoteReader(ctx, live)
			if err != nil {
				return err
			}
			if len(live) == 0 {
				return nil
			}
			sources, err := w.noteMetadataIndexer.LoadNoteSourceSnapshots(ctx, w.vaultDef, reader, w.intelStore, live)
			if err != nil {
				return err
			}
			plan, err = syncer.PrepareSourceSnapshots(ctx, sources)
			return err
		})
		if err != nil {
			return err
		}
		var result semantic.OntologyDerivedResult
		err = s.compute(ctx, "embed_ontology_nodes", func(ctx context.Context) error {
			var err error
			result, err = syncer.ComputeDerived(ctx, plan)
			return err
		})
		if err != nil {
			return err
		}
		return s.withLease(ctx, ticket, true, "derived_publication", func(ctx context.Context) error { return syncer.PublishDerived(ctx, result) })
	case codeanchor.DerivedGraph:
		var scores graphdb.DerivedScores
		err := s.compute(ctx, "graph_scores", func(ctx context.Context) error {
			var err error
			scores, err = graphdb.ComputeDerivedScores(ctx, w.intelStore, graphdb.DocScoresOptions{WikilinkOptions: obsidian.DefaultWikilinkOptions, VaultRoot: w.vaultPath})
			return err
		})
		if err != nil {
			return err
		}
		return s.withLease(ctx, ticket, true, "derived_publication", func(ctx context.Context) error { return graphdb.PublishDerivedScores(ctx, w.intelStore, scores) })
	}
	return fmt.Errorf("unknown derived kind %q", ticket.Kind)
}

func (w *unifiedSemanticWatcher) closeDerived() {
	if w.derived != nil {
		w.derived.close()
	}
	if w.watcherDone != nil {
		<-w.watcherDone
	}
}
