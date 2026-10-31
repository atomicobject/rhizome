// Replaced by pkg/app/tasks types at integration.
package web

import (
	"encoding/json"

	"github.com/atomicobject/rhizome/pkg/app/tasks"
)

// tasksLiveState mirrors tasks.LiveState.
type tasksLiveState struct {
	Paused  bool              `json:"paused"`
	Running *tasks.RunRecord  `json:"running,omitempty"`
	Stale   bool              `json:"stale,omitempty"`
	Items   []tasks.Item      `json:"items"`
	Recent  []tasks.RunRecord `json:"recent"`
}

// tasksRunView mirrors tasks.RunView.
type tasksRunView struct {
	tasks.RunRecord
	HasTranscript bool `json:"hasTranscript"`
	HasCheckLog   bool `json:"hasCheckLog"`
}

// tasksRunDetail mirrors tasks.RunDetail.
type tasksRunDetail struct {
	Run      tasksRunView      `json:"run"`
	Events   []json.RawMessage `json:"events"`
	Next     int64             `json:"next"`
	CheckLog string            `json:"checkLog,omitempty"`
}
