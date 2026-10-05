package indexingpipe

// Docs:
// - [Indexing pipeline (Hub)](docs/hubs/Indexing pipeline (Hub).md)
// - [[indexing-pipeline-architecture]]
// - [Indexing pipeline - Performance tradeoffs + guardrails](docs/reference/analysis/Indexing pipeline - Performance tradeoffs + guardrails.md)

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
)

type FileKind string

const (
	// FileKindNote identifies Markdown note candidates.
	FileKindNote FileKind = "note"
	// FileKindCode identifies code candidates supported by an indexer.
	FileKindCode FileKind = "code"
)

// FileCandidate is the cheap metadata discovered by the walker before content
// is read by a worker.
type FileCandidate struct {
	AbsPath string
	RelPath string
	ModTime int64
	Kind    FileKind
	Lang    codeanchor.Lang
	// ForceRead bypasses persisted freshness guards for this candidate only.
	// Full-index ownership reconciliation uses it after a transition invalidates
	// derived evidence even when the authored file's mtime and bytes are stable.
	// Discovery leaves it false; candidate values remain immutable to callers.
	ForceRead bool

	enqueuedAt time.Time
}

// FilePayload is a classified candidate plus file bytes ready for indexing.
type FilePayload struct {
	Candidate FileCandidate
	Content   []byte
}

// ProgressCallbacks reports discovery/completion against the bounded pipeline.
type ProgressCallbacks struct {
	OnDiscovered func(FileCandidate)
	OnCompleted  func(FileCandidate)
}

// ProcessOptions configures one bounded file-walk/read stage.
type ProcessOptions struct {
	Root    string
	Matcher *ignore.Matcher
	// AlternativeMatcher keeps a path traversable when either matcher admits
	// it. The classifier remains responsible for choosing which visibility
	// policy applies to the candidate.
	AlternativeMatcher *ignore.Matcher
	WorkerCount        int
	QueueCapacity      int
	Progress           *ProgressCallbacks
}

func ignoredByAllMatchers(opts ProcessOptions, rel string, isDir bool) bool {
	if opts.Matcher == nil || !opts.Matcher.IsIgnoredShallow(rel, isDir) {
		return false
	}
	return opts.AlternativeMatcher == nil || opts.AlternativeMatcher.IsIgnoredShallow(rel, isDir)
}

// CountFiles applies the same ignore and classification rules as ProcessFiles
// without reading content. It exists so progress totals match real candidates.
func CountFiles(
	ctx context.Context,
	opts ProcessOptions,
	classify func(path string, d os.DirEntry, modTime int64) (FileCandidate, bool, error),
) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	info, err := os.Stat(opts.Root)
	if err != nil || !info.IsDir() {
		return 0, nil
	}
	if classify == nil {
		return 0, nil
	}

	defaultIgnored := make(map[string]struct{})
	for _, name := range ignore.DefaultIgnoreDirnames() {
		if strings.TrimSpace(name) == "" {
			continue
		}
		defaultIgnored[name] = struct{}{}
	}

	vaultPaths, _ := paths.NewVaultPaths(opts.Root)
	count := 0
	err = filepath.WalkDir(opts.Root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		relPath, relErr := vaultPaths.RelStrict(path)
		if relErr != nil {
			return nil
		}
		rel := relPath.String()
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			if _, ok := defaultIgnored[name]; ok {
				return filepath.SkipDir
			}
			if ignoredByAllMatchers(opts, rel, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if ignoredByAllMatchers(opts, rel, false) {
			return nil
		}
		modTime := int64(0)
		if info, infoErr := d.Info(); infoErr == nil {
			modTime = info.ModTime().Unix()
		}
		_, ok, classifyErr := classify(path, d, modTime)
		if classifyErr != nil {
			return classifyErr
		}
		if ok {
			count++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// ProcessFiles walks Root, classifies files, and processes readable candidates
// through a bounded worker queue. Discovery errors are returned after accepted
// work drains; callers must not treat an incomplete inventory as deleted files.
func ProcessFiles(
	ctx context.Context,
	opts ProcessOptions,
	classify func(path string, d os.DirEntry, modTime int64) (FileCandidate, bool, error),
	shouldRead func(FileCandidate) bool,
	onSkip func(FileCandidate),
	process func(FilePayload) error,
) error {
	// WHY: discovery intentionally blocks on the worker queue. If the walker can
	// race far ahead, queue-wait timings stop describing real worker pressure.
	// Docs: [[indexing-pipeline-architecture#^spec-0012-us2-ac1]]
	info, err := os.Stat(opts.Root)
	if err != nil || !info.IsDir() {
		return nil
	}
	if classify == nil || process == nil {
		return nil
	}
	defaultIgnored := make(map[string]struct{})
	for _, name := range ignore.DefaultIgnoreDirnames() {
		if strings.TrimSpace(name) == "" {
			continue
		}
		defaultIgnored[name] = struct{}{}
	}
	workerCount := opts.WorkerCount
	if workerCount <= 0 {
		workerCount = runtime.GOMAXPROCS(0)
		if workerCount < 1 {
			workerCount = 1
		}
	}
	queueCap := opts.QueueCapacity
	if queueCap <= 0 {
		queueCap = workerCount * 4
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	vaultPaths, _ := paths.NewVaultPaths(opts.Root)
	candidateCh := make(chan FileCandidate, queueCap)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	var activeWorkers atomic.Int64
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for candidate := range candidateCh {
				select {
				case <-ctx.Done():
					return
				default:
				}
				active := activeWorkers.Add(1)
				indexingperf.SetGauge(ctx, "codeindex.active_workers", active)
				workerStarted := time.Now()
				if !candidate.enqueuedAt.IsZero() {
					indexingperf.ObserveLatency(ctx, "codeindex.file_queue_wait", time.Since(candidate.enqueuedAt))
				}
				readStarted := time.Now()
				content, err := os.ReadFile(candidate.AbsPath)
				indexingperf.ObserveLatency(ctx, "fs.read_latency", time.Since(readStarted))
				if err != nil {
					finishCandidateWorker(ctx, &activeWorkers, candidateCh, workerStarted)
					reportCandidateCompleted(opts.Progress, candidate)
					continue
				}
				indexingperf.AddCount(ctx, "fs.read", 1)
				indexingperf.AddBytes(ctx, "fs.read", int64(len(content)))
				if err := process(FilePayload{Candidate: candidate, Content: content}); err != nil {
					finishCandidateWorker(ctx, &activeWorkers, candidateCh, workerStarted)
					select {
					case errCh <- err:
					default:
					}
					cancel()
					return
				}
				finishCandidateWorker(ctx, &activeWorkers, candidateCh, workerStarted)
				indexingperf.AddCount(ctx, "fs.completed", 1)
				reportCandidateCompleted(opts.Progress, candidate)
			}
		}()
	}

	walkStarted := time.Now()
	err = filepath.WalkDir(opts.Root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		relPath, relErr := vaultPaths.RelStrict(path)
		if relErr != nil {
			return nil
		}
		rel := relPath.String()
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			if _, ok := defaultIgnored[name]; ok {
				return filepath.SkipDir
			}
			if ignoredByAllMatchers(opts, rel, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if ignoredByAllMatchers(opts, rel, false) {
			return nil
		}
		statStarted := time.Now()
		info, infoErr := d.Info()
		indexingperf.ObserveLatency(ctx, "fs.stat", time.Since(statStarted))
		modTime := int64(0)
		if infoErr == nil {
			modTime = info.ModTime().Unix()
		}
		candidate, ok, classifyErr := classify(path, d, modTime)
		if classifyErr != nil {
			return classifyErr
		}
		if !ok {
			return nil
		}
		indexingperf.AddCount(ctx, "fs.discovered", 1)
		if opts.Progress != nil && opts.Progress.OnDiscovered != nil {
			opts.Progress.OnDiscovered(candidate)
		}
		if shouldRead != nil && !shouldRead(candidate) {
			if onSkip != nil {
				onSkip(candidate)
			}
			indexingperf.AddCount(ctx, "fs.completed", 1)
			reportCandidateCompleted(opts.Progress, candidate)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		candidate.enqueuedAt = time.Now()
		enqueueStarted := time.Now()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case candidateCh <- candidate:
			indexingperf.ObserveLatency(ctx, "codeindex.file_enqueue_wait", time.Since(enqueueStarted))
			indexingperf.SetGauge(ctx, "codeindex.file_queue_depth", int64(len(candidateCh)))
		}
		return nil
	})
	indexingperf.ObserveLatency(ctx, "fs.walk", time.Since(walkStarted))
	close(candidateCh)
	wg.Wait()

	select {
	case procErr := <-errCh:
		if procErr != nil {
			return procErr
		}
	default:
	}
	if err != nil {
		return err
	}
	return ctx.Err()
}
