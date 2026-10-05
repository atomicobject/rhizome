package codeintel

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/indexingpipe"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
)

// MarkdownCandidateReadError reports a filesystem read failure from explicit
// ownership-routed Markdown intake. Callers must not acknowledge ownership
// reconciliation after this error because the candidate's prior derived state
// may still be stale.
type MarkdownCandidateReadError struct {
	Path string
	Err  error
}

func (e *MarkdownCandidateReadError) Error() string {
	if e == nil {
		return "Markdown candidate read error"
	}
	return fmt.Sprintf("read Markdown candidate %q: %v", e.Path, e.Err)
}

func (e *MarkdownCandidateReadError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func validateMarkdownCandidates(root string, candidates []indexingpipe.FileCandidate) error {
	vaultPaths, err := paths.NewVaultPaths(root)
	if err != nil || vaultPaths.Root() == "" {
		return fmt.Errorf("Markdown candidate vault root is required")
	}
	for _, candidate := range candidates {
		if candidate.Kind != indexingpipe.FileKindNote {
			return fmt.Errorf("Markdown candidate %q has kind %q, want %q", candidate.RelPath, candidate.Kind, indexingpipe.FileKindNote)
		}
		if !filepath.IsAbs(candidate.AbsPath) {
			return fmt.Errorf("Markdown candidate %q has non-absolute path %q", candidate.RelPath, candidate.AbsPath)
		}
		rel, err := paths.CleanNotePath(candidate.RelPath)
		if err != nil || rel.String() != candidate.RelPath {
			return fmt.Errorf("Markdown candidate has invalid vault-relative path %q", candidate.RelPath)
		}
		actual, err := vaultPaths.RelNotePathStrict(candidate.AbsPath)
		if err != nil || actual != rel {
			return fmt.Errorf("Markdown candidate absolute path %q does not match relative path %q", candidate.AbsPath, candidate.RelPath)
		}
	}
	return nil
}

// sealMarkdownCandidateSources validates immutable authored-source snapshots
// against the already selected Markdown candidate batch and returns a private
// map copy. It intentionally retains AuthoredSource values, never raw bytes.
func sealMarkdownCandidateSources(root string, candidates []indexingpipe.FileCandidate, sources map[paths.NotePath]noteformat.AuthoredSource) (map[paths.NotePath]noteformat.AuthoredSource, error) {
	if err := validateMarkdownCandidates(root, candidates); err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, nil
	}

	candidatesByPath := make(map[paths.NotePath]indexingpipe.FileCandidate, len(candidates))
	for _, candidate := range candidates {
		candidatesByPath[paths.NotePath(candidate.RelPath)] = candidate
	}
	sealed := make(map[paths.NotePath]noteformat.AuthoredSource, len(sources))
	for path, source := range sources {
		canonical, err := paths.CleanNotePath(path.String())
		if err != nil || canonical != path {
			return nil, fmt.Errorf("Markdown source map has invalid path %q", path)
		}
		candidate, ok := candidatesByPath[canonical]
		if !ok {
			return nil, fmt.Errorf("Markdown source %q has no candidate", canonical)
		}
		if source.Path() != canonical {
			return nil, fmt.Errorf("Markdown source path %q does not match map key %q", source.Path(), canonical)
		}
		if source.Format() != noteformat.FormatID("markdown") {
			return nil, fmt.Errorf("Markdown source %q has format %q", canonical, source.Format())
		}
		if source.Mtime() != candidate.ModTime {
			return nil, fmt.Errorf("Markdown source %q mtime %d does not match candidate mtime %d", canonical, source.Mtime(), candidate.ModTime)
		}
		content := source.Bytes()
		if source.Size() != int64(len(content)) || source.ContentHash() != HashContentBytes(content) {
			return nil, fmt.Errorf("Markdown source %q has invalid sealed content facts", canonical)
		}
		sealed[canonical] = source
	}
	return sealed, nil
}

// IngestMarkdownCandidates indexes only the supplied, preclassified Markdown
// candidates. It deliberately does not walk root, inspect filenames, or
// perform stale-path cleanup: ownership orchestration owns those decisions.
// The batch is validated before any candidate is read or index state is
// queried, so no partial candidate batch can mutate the index.
func IngestMarkdownCandidates(ctx context.Context, service *codeanchor.Service, root string, candidates []indexingpipe.FileCandidate, progress *indexingpipe.ProgressCallbacks) (NoteIngestResult, error) {
	return IngestMarkdownCandidatesWithSources(ctx, service, root, candidates, nil, progress)
}

// IngestMarkdownCandidatesWithSources indexes preclassified Markdown
// candidates, reusing any matching sealed authored-source snapshot instead of
// reading that candidate from disk. Candidates without a supplied source stay
// on indexingpipe.ProcessCandidates. Source and candidate batches are fully
// validated before index state is queried or work begins.
func IngestMarkdownCandidatesWithSources(ctx context.Context, service *codeanchor.Service, root string, candidates []indexingpipe.FileCandidate, sources map[paths.NotePath]noteformat.AuthoredSource, progress *indexingpipe.ProgressCallbacks) (NoteIngestResult, error) {
	sealedSources, err := sealMarkdownCandidateSources(root, candidates, sources)
	if err != nil {
		return NoteIngestResult{}, err
	}
	return ingestMarkdownCandidateRun(ctx, service, func(ctx context.Context, shouldRead func(indexingpipe.FileCandidate) bool, onSkip func(indexingpipe.FileCandidate), process func(indexingpipe.FilePayload) error) error {
		return processMarkdownCandidatesWithSources(ctx, candidates, sealedSources, progress, shouldRead, onSkip, process)
	}, markdownIngestPolicy{}, nil, markdownSourceSnapshotLookup(sealedSources))
}

func processMarkdownCandidatesWithSources(
	ctx context.Context,
	candidates []indexingpipe.FileCandidate,
	sources map[paths.NotePath]noteformat.AuthoredSource,
	progress *indexingpipe.ProgressCallbacks,
	shouldRead func(indexingpipe.FileCandidate) bool,
	onSkip func(indexingpipe.FileCandidate),
	process func(indexingpipe.FilePayload) error,
) error {
	fromFilesystem := make([]indexingpipe.FileCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		source, preloaded := sources[paths.NotePath(candidate.RelPath)]
		if !preloaded {
			fromFilesystem = append(fromFilesystem, candidate)
			continue
		}
		indexingperf.AddCount(ctx, "fs.discovered", 1)
		if progress != nil && progress.OnDiscovered != nil {
			progress.OnDiscovered(candidate)
		}
		if shouldRead != nil && !shouldRead(candidate) {
			if onSkip != nil {
				onSkip(candidate)
			}
			indexingperf.AddCount(ctx, "fs.completed", 1)
			if progress != nil && progress.OnCompleted != nil {
				progress.OnCompleted(candidate)
			}
			continue
		}
		if err := process(indexingpipe.FilePayload{Candidate: candidate, Content: source.Bytes()}); err != nil {
			return err
		}
		indexingperf.AddCount(ctx, "fs.completed", 1)
		if progress != nil && progress.OnCompleted != nil {
			progress.OnCompleted(candidate)
		}
	}
	return indexingpipe.ProcessCandidates(ctx, fromFilesystem, indexingpipe.CandidateProcessOptions{
		WorkerCount: ClampIndexWorkers(runtime.GOMAXPROCS(0)),
		Progress:    progress,
		OnReadError: func(_ context.Context, candidate indexingpipe.FileCandidate, err error) error {
			return &MarkdownCandidateReadError{Path: candidate.RelPath, Err: err}
		},
	}, shouldRead, onSkip, process)
}

func markdownSourceSnapshotLookup(sources map[paths.NotePath]noteformat.AuthoredSource) markdownSourceLookup {
	if len(sources) == 0 {
		return nil
	}
	return func(candidate indexingpipe.FileCandidate) (notemeta.NoteSourceSnapshot, bool) {
		source, ok := sources[paths.NotePath(candidate.RelPath)]
		if !ok {
			return notemeta.NoteSourceSnapshot{}, false
		}
		content := source.Bytes()
		return notemeta.NoteSourceSnapshot{
			Path:        source.Path(),
			Format:      source.Format(),
			Content:     string(content),
			ContentHash: source.ContentHash(),
			Mtime:       source.Mtime(),
			Size:        source.Size(),
		}, true
	}
}
