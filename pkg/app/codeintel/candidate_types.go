package codeintel

// Docs:
// - [Indexing pipeline (Hub)](docs/hubs/Indexing pipeline (Hub).md)
// - [[indexing-pipeline-architecture]]
// - [Indexing pipeline - Performance tradeoffs + guardrails](docs/reference/analysis/Indexing pipeline - Performance tradeoffs + guardrails.md)

import (
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"time"
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
