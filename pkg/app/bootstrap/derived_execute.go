package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"slices"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embedindex"
	graphindex "github.com/atomicobject/rhizome/pkg/graph/index"
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

func (s *derivedScheduler) execute(members ...codeanchor.DerivedWork) (resultErr error) {
	ctx, finish := observeDerivedWork(s.ctx, members[0])
	defer func() {
		if panicked := recover(); panicked != nil {
			finish(errors.New("diagnostic boundary panicked"))
			panic(panicked)
		}
		finish(resultErr)
	}()
	return s.executeObserved(ctx, members...)
}

func (s *derivedScheduler) executeObserved(ctx context.Context, members ...codeanchor.DerivedWork) error {
	s.witness = nil
	w := s.watcher
	var notePlan embedindex.NotePlan
	var codePlan embedindex.SyncPlan
	var ontologyPlan embedindex.OntologyDerivedPlan
	var nodeSyncer embedindex.OntologyNodeSyncer
	var err error
	members, err = s.withLease(ctx, members, false, "derived_prepare", func(ctx context.Context, live []codeanchor.DerivedWork) ([]codeanchor.DerivedWork, error) {
		inputs := map[string][]string{}
		for _, member := range live {
			inputs[member.Path] = []string{member.Path}
		}
		switch live[0].Kind {
		case codeanchor.DerivedNotes:
			if w.noteSyncer == nil {
				if w.noteSemanticStore {
					return live, fmt.Errorf("note syncer unavailable")
				}
				break
			}
			paths := memberPaths(live)
			if live[0].Path == "" {
				var err error
				paths, err = w.intelStore.NotePaths(ctx)
				if err != nil {
					return live, err
				}
				inputs[""] = append(inputs[""], paths...)
			}
			_, changed, err := w.publishedNoteReader(ctx, paths)
			if err != nil {
				return live, err
			}
			live, err = s.captureWitness(ctx, live, inputs, changed)
			if err != nil || len(live) == 0 {
				return live, err
			}
			if live[0].Path == "" {
				notePlan, err = w.noteSyncer.Plan(ctx)
			} else {
				notePlan, err = w.noteSyncer.PlanPaths(ctx, memberPaths(live))
			}
			return live, err
		case codeanchor.DerivedCode:
			if w.codeSyncer == nil {
				if w.codeSemanticStore {
					return live, fmt.Errorf("code syncer unavailable")
				}
				break
			}
			paths := memberPaths(live)
			if live[0].Path == "" {
				var err error
				paths, err = w.intelStore.IndexedFilePaths(ctx)
				if err != nil {
					return live, err
				}
				items, err := w.codeSyncer.Index.ListItems(ctx)
				if err != nil {
					return live, err
				}
				for _, item := range items {
					paths = append(paths, item.Path)
				}
				slices.Sort(paths)
				paths = slices.Compact(paths)
				inputs[""] = append(inputs[""], paths...)
			}
			loader, changed, err := w.publishedCodeLoader(ctx, paths)
			if err != nil {
				return live, err
			}
			live, err = s.captureWitness(ctx, live, inputs, changed)
			if err != nil || len(live) == 0 {
				return live, err
			}
			if live[0].Path != "" {
				paths = memberPaths(live)
			}
			codePlan, err = w.codeSyncer.PlanPublishedPaths(ctx, paths, loader)
			return live, err
		case codeanchor.DerivedOntology:
			if w.nodeSyncer == nil {
				if w.noteSemanticStore {
					return live, fmt.Errorf("ontology syncer unavailable")
				}
				break
			}
			schema, err := ontoschema.LoadSchema(w.vaultPath)
			if err != nil && !errors.Is(err, ontoschema.ErrNoOntologyFiles) {
				return live, err
			}
			nodeSyncer = *w.nodeSyncer
			nodeSyncer.Schema = schema
			if schema == nil {
				break
			}
			state, err := w.intelStore.GetOntologySchemaState(ctx)
			if err != nil {
				return live, err
			}
			if schema.Hash != state.SchemaHash {
				return live, errDerivedSuperseded
			}
			var projectable []string
			loaded := false
			current := func() ([]string, error) {
				if !loaded {
					var err error
					projectable, err = ontology.ProjectableMetadataPaths(ctx, w.noteMetadataIndexer, w.intelStore)
					if err != nil {
						return nil, err
					}
					loaded = true
				}
				return projectable, nil
			}
			for _, member := range live {
				var paths []string
				if member.Path == "" {
					paths, err = current()
					paths = append([]string{""}, paths...)
				} else {
					paths, err = ontology.RecordSourceSetPaths(schema, []string{member.Path}, current)
				}
				if err != nil {
					return live, err
				}
				inputs[member.Path] = paths
			}
			paths := consumedPaths(inputs)
			paths = slices.DeleteFunc(paths, func(path string) bool { return path == "" })
			reader, changed, err := w.publishedNoteReader(ctx, paths)
			if err != nil {
				return live, err
			}
			live, err = s.captureWitness(ctx, live, inputs, changed)
			if err != nil || len(live) == 0 {
				return live, err
			}
			for path := range inputs {
				if !slices.ContainsFunc(live, func(member codeanchor.DerivedWork) bool { return path == member.Path }) {
					delete(inputs, path)
				}
			}
			paths = consumedPaths(inputs)
			rows, err := w.intelStore.CurrentNoteMetadataRowsByPaths(ctx, paths)
			if err != nil {
				return live, err
			}
			paths = slices.DeleteFunc(paths, func(path string) bool { _, ok := rows[path]; return !ok })
			if len(paths) == 0 {
				return live, nil
			}
			sources, err := w.noteMetadataIndexer.LoadNoteSourceSnapshots(ctx, w.vaultDef, reader, w.intelStore, paths)
			if err != nil {
				return live, err
			}
			ontologyPlan, err = nodeSyncer.PrepareSourceSnapshots(ctx, sources)
			return live, err
		}
		return s.captureWitness(ctx, live, inputs, nil)
	})
	if err != nil {
		return err
	}
	switch members[0].Kind {
	case codeanchor.DerivedNotes:
		if w.noteSyncer != nil {
			err = computeAndPublish(s, ctx, members, func(ctx context.Context, sink func(context.Context, embedindex.NoteDerivedResult) error) error {
				return s.compute(ctx, "embed_notes", func(ctx context.Context) error { return w.noteSyncer.ComputeDerived(ctx, notePlan, sink) })
			}, w.noteSyncer.PublishDerived, w.noteSyncer.CacheDerived)
			if err != nil {
				return err
			}
		}
		_, err = s.withLease(ctx, members, true, "derived_publication", func(ctx context.Context, live []codeanchor.DerivedWork) ([]codeanchor.DerivedWork, error) {
			if w.noteSyncer == nil {
				return live, nil
			}
			paths := memberPaths(live)
			if live[0].Path == "" {
				paths = nil
			}
			return live, w.noteSyncer.FinishDerived(ctx, notePlan, paths)
		})
	case codeanchor.DerivedCode:
		if w.codeSyncer != nil {
			err = computeAndPublish(s, ctx, members, func(ctx context.Context, sink func(context.Context, embedindex.CodeDerivedResult) error) error {
				return s.compute(ctx, "embed_code", func(ctx context.Context) error { return w.codeSyncer.ComputeDerived(ctx, codePlan, sink) })
			}, w.codeSyncer.PublishDerived, w.codeSyncer.CacheDerived)
			if err != nil {
				return err
			}
		}
		_, err = s.withLease(ctx, members, true, "derived_publication", nil)
	case codeanchor.DerivedOntology:
		if w.nodeSyncer == nil {
			_, err = s.withLease(ctx, members, true, "derived_publication", nil)
			break
		}
		var result embedindex.OntologyDerivedResult
		err = s.compute(ctx, "embed_ontology_nodes", func(ctx context.Context) error {
			var err error
			result, err = nodeSyncer.ComputeDerived(ctx, ontologyPlan)
			return err
		})
		if err == nil {
			var live []codeanchor.DerivedWork
			live, err = s.withLease(ctx, members, true, "derived_publication", func(ctx context.Context, live []codeanchor.DerivedWork) ([]codeanchor.DerivedWork, error) {
				if len(live) != len(members) {
					if err := nodeSyncer.CacheDerived(ctx, result); err != nil {
						return live, err
					}
					return live, errDerivedReplan
				}
				return live, nodeSyncer.PublishDerived(ctx, result)
			})
			if errors.Is(err, errDerivedReplan) {
				return s.executeObserved(ctx, live...)
			}
		}
		if err != nil && result.HasVectors() {
			err = errors.Join(err, s.inLane(context.WithoutCancel(ctx), "derived_vector_cache", func(ctx context.Context) error { return nodeSyncer.CacheDerived(ctx, result) }))
		}
	case codeanchor.DerivedGraph:
		var scores semdb.DerivedGraphScores
		err = s.compute(ctx, "graph_scores", func(ctx context.Context) error {
			var err error
			scores, err = graphindex.ComputeDerivedScores(ctx, w.intelStore, graphindex.DocScoresOptions{WikilinkOptions: markdownsyntax.DefaultWikilinkOptions, VaultRoot: w.vaultPath})
			return err
		})
		if err == nil {
			_, err = s.withLease(ctx, members, true, "derived_publication", func(ctx context.Context, live []codeanchor.DerivedWork) ([]codeanchor.DerivedWork, error) {
				return live, graphindex.PublishDerivedScores(ctx, w.intelStore, scores)
			})
		}
	default:
		return fmt.Errorf("unknown derived kind %q", members[0].Kind)
	}
	return err
}

var errDerivedReplan = errors.New("replan independent ontology members")
