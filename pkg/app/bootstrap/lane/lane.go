// Package lane defines the single serialized executor for every indexing job a
// vault runtime performs (SPEC-0104 US3). Exactly one job runs at a time; it is
// the only runtime code that acquires .rhizome/index.lock. Explicit jobs preempt
// background jobs by cancelling their context; background jobs requeue on
// cancellation. Lane B implements Lane; this file is the contract and the fake.
package lane

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Kind orders and labels jobs. Explicit jobs preempt every background kind.
type Kind string

const (
	KindExplicitIndex     Kind = "explicit-index"
	KindBootCatchUp       Kind = "boot-catch-up"
	KindWatcherBatch      Kind = "watcher-batch"
	KindValidationRefresh Kind = "validation-refresh"
	KindEmbedCycle        Kind = "embed-cycle"
	KindGraphCycle        Kind = "graph-cycle"
)

// Background reports whether a job of this kind yields to explicit work and
// external priority requests.
func (k Kind) Background() bool { return k != KindExplicitIndex }

// LockRole is written into the index lock's role field while a job holds it.
func (k Kind) LockRole() string { return "runtime/" + string(k) }

// Reporter receives progress from a running job. Implementations fan out to
// job subscribers; calls must be cheap and never block the job.
type Reporter interface {
	Segment(label string, done, total int64)
	Log(line string)
}

// Request describes work to run on the lane.
type Request struct {
	Kind Kind
	// Trigger is a curated diagnostic label, never user content.
	Trigger string
	// Run performs the job. ctx is cancelled on preemption, external priority,
	// explicit cancel, or runtime shutdown; Run must stop promptly and leave the
	// index consistent (the writer lane's own barriers still apply).
	Run func(ctx context.Context, progress Reporter) error
	// Summary runs after the job drains and its total span closes, before Done.
	Summary func(context.Context) json.RawMessage
	// Coalesce joins an already queued or running job of the same Kind instead
	// of queueing another. Explicit index requests always coalesce.
	Coalesce bool
}

// EventType tags subscriber events.
type EventType string

const (
	EventProgress EventType = "progress"
	EventLog      EventType = "log"
	EventDone     EventType = "done"
)

// Outcome classes carried by EventDone. They mirror the CLI exit classes so a
// delegating `rzm index` can exit exactly as the in-process path would.
const (
	OutcomeOK        = "ok"
	OutcomeFailed    = "failed"
	OutcomeCancelled = "cancelled"
)

// Event is one subscriber notification. Done carries the terminal outcome.
type Event struct {
	Type    EventType `json:"type"`
	JobID   string    `json:"jobId"`
	Kind    Kind      `json:"kind"`
	Label   string    `json:"label,omitempty"`
	Done    int64     `json:"done,omitempty"`
	Total   int64     `json:"total,omitempty"`
	Line    string    `json:"line,omitempty"`
	OK      bool      `json:"ok,omitempty"`
	Outcome string    `json:"outcome,omitempty"`
	Error   string    `json:"error,omitempty"`
	// Summary is the job's timings/summary text for the CLI to print (Done only).
	Summary json.RawMessage `json:"summary,omitempty"`
	Elapsed float64         `json:"elapsedSeconds,omitempty"`
	At      time.Time       `json:"at"`
}

// Handle tracks one submitted job.
type Handle interface {
	ID() string
	Kind() Kind
	// Done closes when the job finished, was cancelled, or was dropped at shutdown.
	Done() <-chan struct{}
	// Err is valid after Done: nil, context.Canceled, or the job's error.
	Err() error
	// Subscribe replays every event retained for this job (the lane keeps a
	// bounded history, newest kept) and then streams live events until Done.
	// The channel closes after EventDone; the returned cancel releases the
	// subscription early. A joiner or a reconnecting client therefore sees the
	// terminal event even if it subscribed late.
	Subscribe() (<-chan Event, func())
	// Cancel requests cancellation of the whole job. It is job-wide: when
	// several clients joined one explicit job, any one of them cancels it for
	// all. Idempotent.
	Cancel()
}

// Status is a point-in-time view for health output.
type Status struct {
	Busy    bool
	JobID   string
	JobKind Kind
	Queued  int
	// Held names why background scheduling is paused (an external priority
	// request) or is empty.
	Held      string
	LastError error
}

// Lane is the serialized executor owned by the vault runtime.
type Lane interface {
	// Submit queues or joins a job. It never blocks on the job itself. ctx
	// bounds only the submission (a caller that gives up before the job is
	// accepted); the job's own lifetime belongs to the lane and ends on
	// preemption, cancel, or Close. joined reports that an existing job of the
	// same Kind was returned because Coalesce was set.
	Submit(ctx context.Context, req Request) (handle Handle, joined bool, err error)
	// Lookup finds a live or recently finished job by id.
	Lookup(id string) (Handle, bool)
	Status() Status
	// Close cancels the running job, drops the queue, and waits for exit.
	Close()
}

// ErrClosed is returned by Submit after Close.
var ErrClosed = errors.New("indexing lane is closed")

// ErrSkipped classifies expected work that became unnecessary. It changes
// diagnostics only; the caller still receives the job's original error.
var ErrSkipped = errors.New("indexing job skipped")

// ErrPreempted wraps context.Canceled on a background job that an explicit
// index job displaced. Submitters use it to decide whether lost work needs
// redoing: a boot catch-up does not (the explicit index just did the same
// work), a watcher batch does.
var ErrPreempted = errors.New("background job preempted by an explicit index job")

// PreemptedError is the error a preempted background job reports; By is the
// explicit job that displaced it, so a submitter can wait for that job and
// decide whether its own work still needs doing.
type PreemptedError struct {
	By  Handle
	err error
}

func (e *PreemptedError) Error() string { return e.err.Error() }
func (e *PreemptedError) Unwrap() error { return e.err }

// ErrDatabaseReplaced is returned by a job whose preflight found that the
// index database on disk is no longer the file the runtime opened. The runtime
// shuts down so the next client starts one against the new file; a delegating
// `rzm index` falls back to in-process indexing.
var ErrDatabaseReplaced = errors.New("index database was replaced on disk; the vault runtime is restarting")

// ErrRebuildNotAllowed is returned when a client asks the runtime to rebuild.
// Rebuilds run in-process with the runtime stopped (SPEC-0104 US2).
var ErrRebuildNotAllowed = errors.New("rebuild is not allowed inside a live runtime; stop it first")
