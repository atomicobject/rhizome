package obsidian

import (
	"context"
	"time"
)

// NoteEntry captures cached note data that callers can reuse to avoid
// re-reading files when computing graph metadata.
type NoteEntry struct {
	Path        string
	Content     string
	Frontmatter map[string]interface{}
	Tags        []string
	ContentTime time.Time
}

// NoteEntriesProvider exposes cached note entries for efficient analysis.
// Implemented by cache-backed adapters.
type NoteEntriesProvider interface {
	NoteEntriesSnapshot(ctx context.Context) ([]NoteEntry, error)
}
