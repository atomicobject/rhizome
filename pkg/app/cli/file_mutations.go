package actions

import (
	"context"
	"sync"
)

type fileMutation struct {
	notesTouched bool
	changes      map[string]int
	fileChanged  string
	err          error
}

type fileMutationProcessor func(ctx context.Context, notePath string) fileMutation

type fileMutationSummary struct {
	notesTouched int
	changes      map[string]int
	filesChanged []string
}

func runFileMutations(ctx context.Context, notes []string, processor fileMutationProcessor, workers int) (fileMutationSummary, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if workers < 1 {
		workers = 1
	}

	jobs := make(chan string, len(notes))
	results := make(chan fileMutation, len(notes))
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for notePath := range jobs {
				select {
				case <-ctx.Done():
					return
				case results <- processor(ctx, notePath):
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, notePath := range notes {
			select {
			case <-ctx.Done():
				return
			case jobs <- notePath:
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	summary := fileMutationSummary{
		changes:      make(map[string]int),
		filesChanged: make([]string, 0),
	}
	var firstErr error
	for delta := range results {
		if delta.err != nil {
			if firstErr == nil {
				firstErr = delta.err
				cancel()
			}
			continue
		}
		if delta.notesTouched {
			summary.notesTouched++
		}
		for name, count := range delta.changes {
			summary.changes[name] += count
		}
		if delta.fileChanged != "" {
			summary.filesChanged = append(summary.filesChanged, delta.fileChanged)
		}
	}
	if firstErr == nil {
		firstErr = ctx.Err()
	}
	return summary, firstErr
}
