package indexing

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/app/codeintel"
)

// reconcilePendingNoteSemanticKeepSet provides the crash-recovery barrier for
// raw note embeddings. Ownership can have removed a source before a process
// stopped, leaving no rediscoverable deleted path for the streaming lane. A
// blocking full note sync prunes every embedding outside the current keep set
// before exact derived obligations are acknowledged.
func reconcilePendingNoteSemanticKeepSet(
	ctx context.Context,
	resources *unifiedSemanticResources,
	flush func(context.Context) error,
) error {
	if resources == nil || resources.noteStore == nil {
		return nil
	}
	if resources.noteSyncer == nil {
		return fmt.Errorf("note semantic store is available but note syncer is unavailable")
	}
	if flush == nil {
		return fmt.Errorf("note semantic keep-set flush is required")
	}
	if err := syncRawNoteEmbeddings(ctx, resources.noteSyncer); err != nil {
		return err
	}
	return flush(ctx)
}

func printNoteBuildWarnings(progress ProgressSink, buildErrors []codeintel.NoteIngestBuildError) {
	if progress == nil || len(buildErrors) == 0 {
		return
	}
	const maxShown = 5
	limit := len(buildErrors)
	if limit > maxShown {
		limit = maxShown
	}
	for i := 0; i < limit; i++ {
		warning := buildErrors[i]
		progress.Println(fmt.Sprintf("[index] Warning: skipped note indexing for %s: %s", warning.Path, warning.Error))
	}
	if len(buildErrors) > limit {
		progress.Println(fmt.Sprintf("[index] Warning: skipped note indexing for %d additional notes", len(buildErrors)-limit))
	}
}

func validationErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
