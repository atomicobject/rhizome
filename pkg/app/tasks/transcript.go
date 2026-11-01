package tasks

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/agentchat"
	"github.com/atomicobject/rhizome/pkg/harness"
)

const (
	transcriptMaxBytes   = 10 << 20
	transcriptTextBytes  = 16 << 10
	transcriptFinalBytes = 64 << 10

	eventFinalMessage        = "final-message"
	eventTranscriptTruncated = "transcript-truncated"
)

// Transcript writes one agent run's harness events to its events.jsonl as
// agentchat.Event lines (SPEC-0127 US8). Text and reasoning deltas become
// whole blocks, action progress is dropped, and token usage is written once
// by Finish. It is not safe for concurrent use.
type Transcript struct {
	file      *os.File
	runID     string
	seq       int64
	size      int64
	truncated bool
	err       error
	block     *agentchat.Event // unflushed text or reasoning
	blockKey  blockKey
	written   map[blockKey]agentchat.Event // latest written block per kind and item
	usage     *harness.Event
}

type blockKey struct {
	kind harness.EventKind
	item string
}

// NewTranscript opens path for appending. The run ID is the name of the
// folder that holds it.
func NewTranscript(path string) (*Transcript, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return &Transcript{
		file: file, runID: filepath.Base(filepath.Dir(path)), size: info.Size(),
		written: map[blockKey]agentchat.Event{},
	}, nil
}

// Add records one harness event.
func (t *Transcript) Add(event harness.Event) {
	switch event.Kind {
	case harness.EventTokenUsage:
		t.usage = &event
		return
	case harness.EventAssistantTextDelta, harness.EventReasoning:
		t.addText(event)
		return
	}
	t.flush()
	switch event.Kind {
	case harness.EventDiagnostic:
	case harness.EventCommandExecution, harness.EventToolCall, harness.EventFileChange:
		if event.Phase != harness.PhaseUpdated {
			t.write(t.action(event), false)
		}
	default:
		t.write(t.project(event), false)
	}
}

// Finish writes the pending block, the final message, and the last token
// usage, the last two even past the size cap. It returns the first write
// error.
func (t *Transcript) Finish(finalMessage string) error {
	t.flush()
	message, _ := cutText(finalMessage, transcriptFinalBytes)
	t.write(agentchat.Event{Type: eventFinalMessage, Content: message}, true)
	if t.usage != nil {
		t.write(t.project(*t.usage), true)
	}
	return t.err
}

func (t *Transcript) Close() error { return t.file.Close() }

// addText merges deltas per kind and item. A completed event carries the
// whole text, so it replaces the pending block or, when that block was
// already written, rewrites its line under the same id.
func (t *Transcript) addText(event harness.Event) {
	if event.Text == "" {
		return
	}
	key := blockKey{event.Kind, event.ItemID}
	if t.block != nil && t.blockKey != key {
		t.flush()
	}
	switch {
	case t.block != nil && event.Phase == harness.PhaseCompleted:
		t.block.Content = event.Text
	case t.block != nil:
		t.block.Content += event.Text
	case event.Phase == harness.PhaseCompleted && key.item != "" && t.written[key].ID != 0:
		prev := t.written[key]
		prev.Content = event.Text
		t.write(prev, false)
	default:
		block := t.project(event)
		block.CreatedAt = time.Now().UTC()
		t.block, t.blockKey = &block, key
	}
}

func (t *Transcript) flush() {
	if t.block == nil {
		return
	}
	block := *t.block
	t.block = nil
	if written, ok := t.write(block, false); ok {
		t.written[t.blockKey] = written
	}
}

func (t *Transcript) project(event harness.Event) agentchat.Event {
	projected := agentchat.EventFromHarness(t.runID, event)
	if len(event.Input) > 0 {
		var cut bool
		projected.Result.Input, cut = harness.BoundedRaw(event.Input)
		projected.Result.Truncated = projected.Result.Truncated || cut
	}
	return projected
}

// action keeps an action's text in preference to its structured output.
func (t *Transcript) action(event harness.Event) agentchat.Event {
	if event.Text != "" {
		event.Text, event.Truncated = cutText(event.Text, transcriptTextBytes)
		event.Output = nil
	}
	return t.project(event)
}

// write appends event as one line, giving it the next sequence id unless it
// already has one. Past the size cap it writes one transcript-truncated line
// and drops events, except those written with always.
func (t *Transcript) write(event agentchat.Event, always bool) (agentchat.Event, bool) {
	if t.err != nil || (t.truncated && !always) {
		return event, false
	}
	if event.ID == 0 {
		event.ID = t.seq + 1
	}
	event.SessionID = t.runID
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	line, err := json.Marshal(event)
	if err != nil {
		t.err = err
		return event, false
	}
	line = append(line, '\n')
	if !always && t.size+int64(len(line)) > transcriptMaxBytes {
		t.truncated = true
		t.write(agentchat.Event{Type: eventTranscriptTruncated}, true)
		return event, false
	}
	n, err := t.file.Write(line)
	t.size += int64(n)
	if err != nil {
		t.err = err
		return event, false
	}
	t.seq = max(t.seq, event.ID)
	return event, true
}

// cutText bounds s to limit bytes without splitting a character.
func cutText(s string, limit int) (string, bool) {
	if len(s) <= limit {
		return s, false
	}
	return strings.ToValidUTF8(s[:limit], ""), true
}

// ReadTranscript returns the complete lines after byte offset after, clamped
// to the file, and the offset to resume from. A trailing partial line is left
// for the next read.
func ReadTranscript(path string, after int64) ([]json.RawMessage, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, 0, err
	}
	after = min(max(after, 0), info.Size())
	data, err := io.ReadAll(io.NewSectionReader(file, after, info.Size()-after))
	if err != nil {
		return nil, 0, err
	}
	data = data[:bytes.LastIndexByte(data, '\n')+1]
	events := []json.RawMessage{}
	for line := range bytes.SplitSeq(data, []byte("\n")) {
		if len(line) > 0 {
			events = append(events, json.RawMessage(line))
		}
	}
	return events, after + int64(len(data)), nil
}
