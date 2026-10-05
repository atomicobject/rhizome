package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	appserve "github.com/atomicobject/rhizome/pkg/app/cli/serve"
	"github.com/atomicobject/rhizome/pkg/app/indexing"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
)

// registerIndexJobHooks wires explicit index jobs onto the runtime's indexing
// lane (SPEC-0104 US2). The job runs the same unified core as `rzm index`,
// streams its progress and log lines to subscribers, and finishes with the
// timing summary in the terminal event.
func registerIndexJobHooks(hooks *appserve.ControlHooks, rt *bootstrap.LiveRuntime) {
	hooks.SubmitIndex = func(ctx context.Context) (lane.Handle, bool, error) {
		indexLane := rt.Lane()
		if indexLane == nil {
			return nil, false, lane.ErrClosed
		}
		return indexLane.Submit(ctx, lane.Request{
			Kind:     lane.KindExplicitIndex,
			Coalesce: true,
			Run: func(jobCtx context.Context, progress lane.Reporter) error {
				return runExplicitIndexJob(jobCtx, progress, rt)
			},
			Summary: indexJobSummary,
		})
	}
	hooks.LookupIndex = func(id string) (lane.Handle, bool) {
		indexLane := rt.Lane()
		if indexLane == nil {
			return nil, false
		}
		return indexLane.Lookup(id)
	}
}

func runExplicitIndexJob(ctx context.Context, progress lane.Reporter, rt *bootstrap.LiveRuntime) error {
	collector := indexingperf.FromContext(ctx)
	if collector == nil {
		collector = indexingperf.NewBounded()
		ctx = indexingperf.WithCollector(ctx, collector)
	}

	err := indexing.RunUnifiedCore(ctx, indexing.UnifiedOptions{
		VaultPath:    rt.VaultPath,
		VaultDef:     rt.VaultDef,
		NoteMetadata: rt.NoteMetadataIndexer(),
		ProgressBar:  &laneProgressBar{progress: progress},
		Verbose:      debug,
		TxLockMode:   sqliteutil.TxLockImmediate,
	})
	return err
}

func indexJobSummary(ctx context.Context) json.RawMessage {
	collector := indexingperf.FromContext(ctx)
	summary := strings.TrimSpace(collector.RenderSummary())
	encoded, err := json.Marshal(summary)
	if err != nil {
		return nil
	}
	return encoded
}

// laneProgressBar adapts the indexing progress bar to lane events: status lines
// become log lines, and the pipeline's segment label (or, failing that, the
// last status line) labels the progress segment the delegating CLI renders.
type laneProgressBar struct {
	progress lane.Reporter

	mu    sync.Mutex
	label string
}

func (b *laneProgressBar) Println(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	b.mu.Lock()
	b.label = line
	b.mu.Unlock()
	b.progress.Log(line)
}

// SetLabel receives the pipeline's segment label (the unified progress wrapper
// type-asserts for it), so the delegating CLI renders the same segment names
// as an in-process run instead of a generic "Indexing".
func (b *laneProgressBar) SetLabel(label string) {
	label = strings.TrimSpace(label)
	if label == "" {
		return
	}
	b.mu.Lock()
	b.label = label
	b.mu.Unlock()
}

func (b *laneProgressBar) Update(done, total int) {
	b.mu.Lock()
	label := b.label
	b.mu.Unlock()
	b.progress.Segment(label, int64(done), int64(total))
}
