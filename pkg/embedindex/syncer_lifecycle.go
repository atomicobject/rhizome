package embedindex

// Docs:
// - [Indexing pipeline - Live updating (watcher runtime)](docs/reference/analysis/Indexing pipeline - Live updating (watcher runtime).md)
// - [Rhizome semantic code index spine](docs/specs/technical/semantic-code-index-spine.md)

import (
	"context"
	"errors"

	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
)

func (s *Syncer) MarkLastSync(ctx context.Context) error {
	if err := s.markPendingSourceHighWater(ctx); err != nil {
		return err
	}
	if versioned, ok := s.Index.(codeFingerprintVersionStore); ok {
		if err := versioned.SetFingerprintVersion(ctx, codeFingerprintVersion); err != nil {
			return err
		}
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

func (s *Syncer) recordPendingSourceHighWater(ts time.Time) {
	if ts.IsZero() || ts.Unix() <= 0 {
		return
	}
	s.freshnessMu.Lock()
	defer s.freshnessMu.Unlock()
	if s.pendingSourceHighWater.IsZero() || ts.After(s.pendingSourceHighWater) {
		s.pendingSourceHighWater = ts
	}
}

func (s *Syncer) markPendingSourceHighWater(ctx context.Context) error {
	s.freshnessMu.Lock()
	ts := s.pendingSourceHighWater
	s.pendingSourceHighWater = time.Time{}
	s.freshnessMu.Unlock()
	if ts.IsZero() || ts.Unix() <= 0 {
		return nil
	}
	return s.Index.UpdateSourceHighWater(ctx, ts)
}

func (s *Syncer) markPlanFreshness(ctx context.Context, plan SyncPlan, updateLastSync bool) error {
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

func (s *Syncer) shouldIndexAnchor(anchor codeanchor.IntelAnchor) bool {
	if s.Policy == nil {
		return true
	}
	if decider, ok := s.Policy.(AnchorIndexDecider); ok {
		return decider.ShouldIndexAnchor(anchor)
	}
	return true
}

func (s *Syncer) pruneOnlyAnchorTask(anchor codeanchor.IntelAnchor) codeTask {
	return codeTask{
		id:          codeindex.AnchorID(anchor.AnchorID),
		fingerprint: anchor.Fingerprint,
		payload: codeTaskPayload{
			ownerKind: strings.TrimSpace(anchor.Kind),
		},
	}
}

func (s *Syncer) progressf(format string, args ...any) {
	if s.OnProgress != nil {
		s.OnProgress(format, args...)
	}
}

func (s *Syncer) ensureReady(ctx context.Context) error {
	if s.Index == nil || s.Provider == nil {
		return errors.New("syncer requires index and provider")
	}
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	if s.setupErr != nil {
		return s.setupErr
	}
	if !s.schemaReady {
		if err := s.Index.EnsureSchema(ctx); err != nil {
			s.setupErr = err
			return err
		}
		s.schemaReady = true
	}
	// Re-validate each call until the provider has reported its real dimensions.
	// Voyage learns dims from its first embedding response, so the first call may
	// pass Dimensions=0; we follow up once the value is known.
	if !s.metaDimsKnown {
		dims := s.Provider.Dimensions()
		if err := s.Index.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{
			Provider:           s.ProviderInfo.Provider,
			Model:              s.ProviderInfo.Model,
			Dimensions:         dims,
			FingerprintVersion: codeFingerprintVersion,
		}); err != nil {
			s.setupErr = err
			return err
		}
		if dims > 0 {
			s.metaDimsKnown = true
		}
	}
	return nil
}

func (s *Syncer) batchSize() int {
	if s.BatchSize > 0 {
		return s.BatchSize
	}
	return EffectiveBatchSize(s.Provider, 0)
}

func (s *Syncer) planConcurrent() int {
	if s.PlanConcurrent > 0 {
		return s.PlanConcurrent
	}
	return defaultPlanConcurrency()
}

func (s *Syncer) maxConcurrent() int {
	if s.MaxConcurrent > 0 {
		return s.MaxConcurrent
	}
	return EffectiveMaxConcurrent(s.Provider, 0)
}

func groupByPath(anchors []codeanchor.IntelAnchor) map[string][]codeanchor.IntelAnchor {
	out := make(map[string][]codeanchor.IntelAnchor)
	for _, a := range anchors {
		out[a.Path] = append(out[a.Path], a)
	}
	for path := range out {
		sort.Slice(out[path], func(i, j int) bool { return out[path][i].FQN < out[path][j].FQN })
	}
	return out
}

// FileSystemLoader creates a FileLoader backed by os.ReadFile rooted at base.
func FileSystemLoader(base string) FileLoader {
	base = filepath.Clean(base)
	return func(relPath string) ([]byte, error) {
		if filepath.IsAbs(relPath) {
			return nil, fs.ErrNotExist
		}
		full := filepath.Clean(filepath.Join(base, filepath.FromSlash(relPath)))
		rel, err := filepath.Rel(base, full)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fs.ErrNotExist
		}
		return os.ReadFile(full)
	}
}
