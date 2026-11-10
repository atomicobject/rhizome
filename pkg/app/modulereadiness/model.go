// Package modulereadiness coordinates installed modules outside the index lane.
package modulereadiness

import "context"

type State string

const (
	Checking      State = "checking"
	Working       State = "working"
	RequiredInput State = "required_input"
	Blocked       State = "blocked"
	Failed        State = "failed"
	Ready         State = "ready"
)

type RecoveryKind string

const (
	Retry   RecoveryKind = "retry"
	Choice  RecoveryKind = "choice"
	Review  RecoveryKind = "review"
	Trust   RecoveryKind = "trust"
	Install RecoveryKind = "install"
)

type Recovery struct {
	Kind    RecoveryKind `json:"kind"`
	Label   string       `json:"label"`
	Input   string       `json:"input,omitempty"`
	Choices []string     `json:"choices,omitempty"`
	Paths   []string     `json:"paths,omitempty"`
}
type Snapshot struct {
	Module   string     `json:"module"`
	Title    string     `json:"title"`
	State    State      `json:"state"`
	Reason   string     `json:"reason,omitempty"`
	Recovery []Recovery `json:"recovery,omitempty"`
}
type Next string

const (
	NextReady   Next = "ready"
	NextPrepare Next = "prepare"
	NextIndex   Next = "index"
	NextInput   Next = "needs-input"
	NextBlocked Next = "blocked"
)

// Assessment is module-owned interpretation. Includes are exact source paths.
type Assessment struct {
	Next     Next           `json:"next"`
	Reason   string         `json:"reason"`
	Recovery []Recovery     `json:"recovery"`
	Input    map[string]any `json:"input"`
	Includes []string       `json:"includes"`
}
type Request struct {
	Kind  RecoveryKind `json:"kind"`
	Value string       `json:"value,omitempty"`
}
type API interface {
	Snapshot() []Snapshot
	Recover(string, Request) error
}
type IncludeSources func(context.Context, []string) error
