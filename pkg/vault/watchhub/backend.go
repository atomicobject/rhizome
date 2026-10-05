package watchhub

import (
	"context"
	"errors"
)

// Backend abstracts platform-specific file system watching.
// On macOS with FSEvents, one stream watches entire trees recursively.
// On Linux/Windows, fsnotify watches individual directories.
type Backend interface {
	// Start begins watching. Called once after construction.
	Start(ctx context.Context) error

	// Close stops watching and releases resources.
	Close() error

	// AddPath adds a path to watch. For recursive backends (FSEvents),
	// this watches the entire subtree. For non-recursive backends (fsnotify),
	// this watches only the specified directory.
	AddPath(path string) error

	// RemovePath stops watching a path.
	RemovePath(path string) error

	// Events returns the channel that receives watch events.
	Events() <-chan BackendEvent

	// Errors returns the channel that receives watcher errors.
	Errors() <-chan error

	// IsRecursive returns true if the backend natively watches subtrees.
	// Used by Hub to skip manual directory walking on platforms like macOS.
	IsRecursive() bool

	// WatchList returns all explicitly added paths (for debugging/testing).
	WatchList() []string
}

// BackendEvent is the raw event from the backend before Hub processing.
type BackendEvent struct {
	Path string
	Op   Op
	// IsDir indicates if the path is a directory. Set by backend when known
	// (e.g., FSEvents provides this), otherwise Hub will stat the path.
	IsDir bool
	// IsDirKnown indicates whether IsDir was set by the backend.
	// If false, Hub should stat the path to determine if it's a directory.
	IsDirKnown bool
	// MustRescan indicates coalesced events requiring a full directory scan.
	// This is set when FSEvents reports MustScanSubDirs.
	MustRescan bool
}

// ErrEventOverflow is returned when the event buffer is full.
var ErrEventOverflow = errors.New("watchhub: event buffer overflow")

const defaultBackendEventBuffer = 4096
