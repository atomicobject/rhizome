package embedindex

import (
	"context"

	"fmt"

	"strings"

	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

func (s *NoteSyncer) MarkLastSync(ctx context.Context) error {
	if err := s.markPendingSourceHighWater(ctx); err != nil {
		return err
	}
	if generationCommitter, ok := s.Index.(interface {
		CommitSyncGeneration(context.Context) error
	}); ok {
		if err := generationCommitter.CommitSyncGeneration(ctx); err != nil {
			return err
		}
	}
	return s.Index.UpdateLastSync(ctx, time.Now())
}

func (s *NoteSyncer) recordPendingSourceHighWater(ts time.Time) {
	if ts.IsZero() || ts.Unix() <= 0 {
		return
	}
	s.freshnessMu.Lock()
	defer s.freshnessMu.Unlock()
	if s.pendingSourceHighWater.IsZero() || ts.After(s.pendingSourceHighWater) {
		s.pendingSourceHighWater = ts
	}
}

func (s *NoteSyncer) markPendingSourceHighWater(ctx context.Context) error {
	s.freshnessMu.Lock()
	ts := s.pendingSourceHighWater
	s.pendingSourceHighWater = time.Time{}
	s.freshnessMu.Unlock()
	if ts.IsZero() || ts.Unix() <= 0 {
		return nil
	}
	return s.Index.UpdateSourceHighWater(ctx, ts)
}

func (s *NoteSyncer) markPlanFreshness(ctx context.Context, plan NotePlan, updateLastSync bool) error {
	if updateLastSync {
		if !plan.state.sourceHighWater.IsZero() && plan.state.sourceHighWater.Unix() > 0 {
			if err := s.Index.UpdateSourceHighWater(ctx, plan.state.sourceHighWater); err != nil {
				return err
			}
		}
		return s.MarkLastSync(ctx)
	}
	s.recordPendingSourceHighWater(plan.state.sourceHighWater)
	return nil
}

func (s *NoteSyncer) applyNotePlanCleanup(ctx context.Context, lazyIndex embeddings.LazyPruningNoteIndex, state notePlanState) error {
	if err := s.pruneTypedRawNoteSurfaces(ctx, state.typedRawPrunePaths); err != nil {
		return err
	}
	if state.incremental {
		for _, id := range state.deleteIDs {
			if err := s.Index.DeleteNote(ctx, id); err != nil {
				return fmt.Errorf("delete note %s: %w", id, err)
			}
		}
		return nil
	}
	return s.pruneStaleNotes(ctx, lazyIndex, state.useLazyPruning, state.ids)
}

// pruneStaleNotes removes notes not seen in current sync, using lazy pruning if supported.
func (s *NoteSyncer) pruneStaleNotes(ctx context.Context, lazyIndex embeddings.LazyPruningNoteIndex, supportsLazy bool, ids []embeddings.NoteID) error {
	if supportsLazy {
		threshold := s.StaleNoteThreshold
		if threshold == 0 {
			threshold = 0.3 // Default: preserve notes if stale fraction < 30%
		}
		if pruned, err := lazyIndex.PruneStaleNotes(ctx, threshold); err != nil {
			return fmt.Errorf("prune stale notes: %w", err)
		} else if pruned > 0 {
			s.progressf("Pruned %d stale notes (above %.0f%% threshold)", pruned, threshold*100)
		}
	} else {
		if err := s.Index.DeleteNotesNotIn(ctx, ids); err != nil {
			return fmt.Errorf("delete removed notes: %w", err)
		}
	}
	return nil
}

func (s *NoteSyncer) rawNoteEmbeddingPaths(ctx context.Context, paths []string) ([]string, []string, error) {
	paths = normalizeNotePaths(paths)
	if len(paths) == 0 {
		return nil, nil, nil
	}
	if s.RawEligibility == nil {
		return paths, nil, nil
	}
	eligibility, err := s.RawEligibility.RawNoteEmbeddingEligibility(ctx, paths)
	if err != nil {
		return nil, nil, fmt.Errorf("load raw note embedding eligibility: %w", err)
	}
	typedSet := stringSet(eligibility.TypedPaths)
	raw := make([]string, 0, len(paths))
	typed := make([]string, 0, len(eligibility.TypedPaths))
	for _, path := range paths {
		if _, ok := typedSet[path]; ok {
			typed = append(typed, path)
			continue
		}
		if eligible, ok := eligibility.RawEligible[path]; ok && !eligible {
			typed = append(typed, path)
			continue
		}
		raw = append(raw, path)
	}
	return raw, normalizeNotePaths(typed), nil
}

func (s *NoteSyncer) pruneTypedRawNoteSurfaces(ctx context.Context, paths []string) error {
	paths = normalizeNotePaths(paths)
	if len(paths) == 0 {
		return nil
	}
	for _, path := range paths {
		if err := s.Index.DeleteNote(ctx, embeddings.NoteID(path)); err != nil {
			return fmt.Errorf("delete raw typed-note embeddings %s: %w", path, err)
		}
	}
	if s.ChunkWriter == nil {
		indexingperf.AddCount(ctx, indexingperf.NoteplanRawChunksPruned, 0)
		return nil
	}
	byPath, err := s.loadDocSectionsByPath(ctx, paths)
	if err != nil {
		return err
	}
	ownerIDs := make([]string, 0)
	for _, path := range paths {
		for _, section := range byPath[path] {
			if strings.TrimSpace(section.SectionID) == "" {
				continue
			}
			ownerIDs = append(ownerIDs, section.SectionID)
		}
	}
	ownerIDs = normalizeNotePaths(ownerIDs)
	indexingperf.AddCount(ctx, indexingperf.NoteplanRawChunksPruned, int64(len(ownerIDs)))
	if len(ownerIDs) == 0 {
		return nil
	}
	if s.WriteQueue != nil {
		if err := s.WriteQueue.SubmitIntelChunksByFamily(ctx, ownerIDs, codeanchor.IntelChunkFamilyAuthoredSection, nil); err != nil {
			return err
		}
		return s.WriteQueue.FlushAndWait(ctx)
	}
	if familyWriter, ok := s.ChunkWriter.(ChunkFamilyWriter); ok {
		return familyWriter.ReplaceIntelChunksByFamily(ctx, ownerIDs, codeanchor.IntelChunkFamilyAuthoredSection, nil)
	}
	return s.ChunkWriter.ReplaceIntelChunks(ctx, ownerIDs, nil)
}
