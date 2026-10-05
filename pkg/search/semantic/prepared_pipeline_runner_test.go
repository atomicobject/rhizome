package semantic

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
)

type runnerPrepared struct {
	id    int
	calls int
}

func (p runnerPrepared) pipelineProviderCalls() int { return p.calls }

type runnerOwner struct {
	prepared runnerPrepared
}

func (o runnerOwner) pipelineDoneCalls() int { return o.prepared.calls }

func TestRunPreparedPipelineReportsProgressAndFinalizes(t *testing.T) {
	t.Parallel()

	preparedCh := make(chan runnerPrepared, 8)
	var (
		mu         sync.Mutex
		finalized  []int
		progresses [][2]int
	)

	err := runPreparedPipeline(
		context.Background(),
		[]int{1, 2, 3},
		6,
		2,
		"",
		"",
		preparedCh,
		func(finalize func(runnerOwner) error) error {
			for prepared := range preparedCh {
				if err := finalize(runnerOwner{prepared: prepared}); err != nil {
					return err
				}
			}
			return nil
		},
		func(_ context.Context, task int) (runnerPrepared, error) {
			return runnerPrepared{id: task, calls: task}, nil
		},
		func(owner runnerOwner) error {
			mu.Lock()
			finalized = append(finalized, owner.prepared.id)
			mu.Unlock()
			return nil
		},
		func(done, total int) {
			mu.Lock()
			progresses = append(progresses, [2]int{done, total})
			mu.Unlock()
		},
	)
	requireNoErr(t, err)
	slices.Sort(finalized)
	if !slices.Equal(finalized, []int{1, 2, 3}) {
		t.Fatalf("finalized=%v", finalized)
	}
	if len(progresses) == 0 {
		t.Fatalf("expected progress updates")
	}
	last := progresses[len(progresses)-1]
	if last != [2]int{6, 6} {
		t.Fatalf("last progress=%v", last)
	}
}

func TestRunPreparedPipelineReturnsPrepareError(t *testing.T) {
	t.Parallel()

	preparedCh := make(chan runnerPrepared, 4)
	wantErr := errors.New("boom")
	err := runPreparedPipeline(
		context.Background(),
		[]int{1},
		1,
		1,
		"",
		"",
		preparedCh,
		func(finalize func(runnerOwner) error) error {
			for prepared := range preparedCh {
				if err := finalize(runnerOwner{prepared: prepared}); err != nil {
					return err
				}
			}
			return nil
		},
		func(context.Context, int) (runnerPrepared, error) {
			return runnerPrepared{}, wantErr
		},
		func(runnerOwner) error { return nil },
		nil,
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("got err %v want %v", err, wantErr)
	}
}

func TestRunPreparedPipelineUsesFixedTotalWhenProvided(t *testing.T) {
	t.Parallel()

	preparedCh := make(chan runnerPrepared, 4)
	var (
		mu         sync.Mutex
		progresses [][2]int
	)

	err := runPreparedPipeline(
		context.Background(),
		[]int{1, 2, 3},
		99,
		2,
		"",
		"",
		preparedCh,
		func(finalize func(runnerOwner) error) error {
			for prepared := range preparedCh {
				if err := finalize(runnerOwner{prepared: prepared}); err != nil {
					return err
				}
			}
			return nil
		},
		func(_ context.Context, task int) (runnerPrepared, error) {
			return runnerPrepared{id: task, calls: task}, nil
		},
		func(runnerOwner) error { return nil },
		func(done, total int) {
			mu.Lock()
			progresses = append(progresses, [2]int{done, total})
			mu.Unlock()
		},
	)
	requireNoErr(t, err)
	if len(progresses) == 0 {
		t.Fatalf("expected progress updates")
	}
	for _, progress := range progresses {
		if progress[1] != 99 {
			t.Fatalf("progress total changed: %v", progresses)
		}
	}
}

func requireNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}
