package tasks

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/agentchat"
	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/harnesstest"
)

const testRunID = "20261031T120000Z-0123abcd"

func transcriptPath(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), testRunID)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "events.jsonl")
}

// record writes events and the final message to a new transcript and reads
// it back.
func record(t *testing.T, events []harness.Event, final string) []agentchat.Event {
	t.Helper()
	path := transcriptPath(t)
	transcript, err := NewTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		transcript.Add(event)
	}
	if err := transcript.Finish(final); err != nil {
		t.Fatal(err)
	}
	if err := transcript.Close(); err != nil {
		t.Fatal(err)
	}
	return readEvents(t, path)
}

func readEvents(t *testing.T, path string) []agentchat.Event {
	t.Helper()
	lines, _, err := ReadTranscript(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	events := make([]agentchat.Event, len(lines))
	for i, line := range lines {
		if err := json.Unmarshal(line, &events[i]); err != nil {
			t.Fatalf("line %d: %v", i+1, err)
		}
	}
	return events
}

// fixture loads a captured harness.Event stream.
func fixture(t *testing.T, name string) []harness.Event {
	t.Helper()
	file, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var events []harness.Event
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event harness.Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	return events
}

type line struct{ kind, content string }

func lines(events []agentchat.Event) []line {
	out := make([]line, len(events))
	for i, event := range events {
		out[i] = line{event.Type, event.Content}
	}
	return out
}

func requireLines(t *testing.T, events []agentchat.Event, want ...line) {
	t.Helper()
	got := lines(events)
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d:\n%q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i+1, got[i], want[i])
		}
	}
}

func requireSequence(t *testing.T, events []agentchat.Event) {
	t.Helper()
	for i, event := range events {
		if event.ID != int64(i+1) || event.SessionID != testRunID || event.CreatedAt.IsZero() {
			t.Fatalf("line %d: id=%d sessionId=%q createdAt=%v", i+1, event.ID, event.SessionID, event.CreatedAt)
		}
	}
}

func TestTranscriptCapturedCodexTurn(t *testing.T) {
	events := record(t, fixture(t, "codex-turn.jsonl"), "`ls` lists 2 files.")
	requireLines(t, events,
		line{"turn-started", ""},
		line{"assistant-text-delta", "I’ll count the entries listed by `ls` in the current directory."},
		line{"command-execution", ""},
		line{"command-execution", "alpha.txt\nbeta.txt\n"},
		line{"assistant-text-delta", "`ls` lists 2 files."},
		line{"turn-completed", ""},
		line{"final-message", "`ls` lists 2 files."},
		line{"token-usage", ""},
	)
	requireSequence(t, events)
	command := events[3].Result
	if command.Command != "/bin/zsh -lc ls" || command.ExitCode == nil || *command.ExitCode != 0 || command.Output != nil {
		t.Fatalf("command result: %+v", command)
	}
	if events[7].Result.Usage.TotalTokens == 0 {
		t.Fatal("token usage is empty")
	}
}

func TestTranscriptCapturedClaudeTurn(t *testing.T) {
	events := record(t, fixture(t, "claude-turn.jsonl"), "`ls` lists 2 files: `alpha.txt` and `beta.txt`.")
	requireLines(t, events,
		line{"turn-started", ""},
		line{"assistant-text-delta", "Running `ls` now."},
		line{"command-execution", ""},
		line{"command-execution", "alpha.txt\nbeta.txt"},
		line{"assistant-text-delta", "`ls` lists 2 files: `alpha.txt` and `beta.txt`."},
		line{"turn-completed", "`ls` lists 2 files: `alpha.txt` and `beta.txt`."},
		line{"final-message", "`ls` lists 2 files: `alpha.txt` and `beta.txt`."},
		line{"token-usage", ""},
	)
	requireSequence(t, events)
}

// The streams below are synthetic, shaped like the drivers' mappings.

func TestTranscriptClaudeThinkingBlocksSplitByDiagnostic(t *testing.T) {
	thinking := func(text string) harness.Event {
		return harness.Event{Kind: harness.EventReasoning, Phase: harness.PhaseUpdated, Text: text}
	}
	events := record(t, []harness.Event{
		thinking("First "), thinking("thought."),
		{Kind: harness.EventDiagnostic, Text: "Claude stream event: content_block_stop"},
		thinking("Second thought."),
		{Kind: harness.EventDiagnostic, Text: "Claude stream event: content_block_stop"},
	}, "")
	requireLines(t, events,
		line{"reasoning", "First thought."},
		line{"reasoning", "Second thought."},
		line{"final-message", ""},
	)
}

func TestTranscriptClaudeToolCallsAreBounded(t *testing.T) {
	read := strings.Repeat("line of a large file\n", 2000)
	readOutput, readCut := harness.BoundedRaw(mustJSON(t, read))
	edit := mustJSON(t, map[string]string{"file_path": "/repo/a.go", "old_string": strings.Repeat("a", 20000), "new_string": "b"})
	events := record(t, []harness.Event{
		{Kind: harness.EventToolCall, ItemID: "toolu_read", Name: "Read", Phase: harness.PhaseStarted, Input: mustJSON(t, map[string]string{"file_path": "/repo/big.txt"})},
		{Kind: harness.EventToolCall, ItemID: "toolu_read", Status: "completed", Phase: harness.PhaseCompleted, Text: read, Output: readOutput, Truncated: readCut},
		{Kind: harness.EventFileChange, ItemID: "toolu_edit", Name: "Edit", Phase: harness.PhaseStarted, Paths: []string{"/repo/a.go"}, Input: edit},
		{Kind: harness.EventFileChange, ItemID: "toolu_edit", Status: "completed", Phase: harness.PhaseCompleted, Text: "The file /repo/a.go has been updated.", Output: mustJSON(t, "The file /repo/a.go has been updated.")},
	}, "Done.")
	if len(events) != 5 {
		t.Fatalf("got %q", lines(events))
	}
	readResult := events[1]
	if len(readResult.Content) != transcriptTextBytes || !strings.HasPrefix(read, readResult.Content) || !readResult.Result.Truncated || readResult.Result.Output != nil {
		t.Fatalf("read result: %d bytes, truncated=%v, output=%d bytes", len(readResult.Content), readResult.Result.Truncated, len(readResult.Result.Output))
	}
	// BoundedRaw cuts before re-encoding, so escapes can add a few bytes.
	editStart := events[2]
	if len(editStart.Result.Input) > harness.MaxOutputBytes+1024 || !editStart.Result.Truncated || editStart.Result.Paths[0] != "/repo/a.go" {
		t.Fatalf("edit start: input=%d bytes truncated=%v", len(editStart.Result.Input), editStart.Result.Truncated)
	}
	if events[3].Result.Truncated || events[3].Result.Output != nil {
		t.Fatalf("edit result: %+v", events[3].Result)
	}
}

func TestTranscriptCodexReasoningCompletedReplacesDeltas(t *testing.T) {
	delta := func(text string) harness.Event {
		return harness.Event{Kind: harness.EventReasoning, ItemID: "rs_1", Phase: harness.PhaseUpdated, Text: text}
	}
	completed := harness.Event{Kind: harness.EventReasoning, ItemID: "rs_1", Status: "completed", Phase: harness.PhaseCompleted, Text: "Plan: run ls."}
	usage := func(total int64) harness.Event {
		return harness.Event{Kind: harness.EventTokenUsage, Usage: harness.TokenUsage{TotalTokens: total}}
	}

	t.Run("buffered", func(t *testing.T) {
		events := record(t, []harness.Event{
			{Kind: harness.EventReasoning, ItemID: "rs_1", Phase: harness.PhaseStarted},
			delta("Plan: "), usage(10), delta("run"), completed, usage(20),
			{Kind: harness.EventTurnCompleted, Status: "completed", Phase: harness.PhaseCompleted},
		}, "")
		requireLines(t, events,
			line{"reasoning", "Plan: run ls."},
			line{"turn-completed", ""},
			line{"final-message", ""},
			line{"token-usage", ""},
		)
		if events[3].Result.Usage.TotalTokens != 20 {
			t.Fatalf("usage: %+v", events[3].Result.Usage)
		}
	})

	t.Run("already written", func(t *testing.T) {
		events := record(t, []harness.Event{
			delta("Plan: "), {Kind: harness.EventDiagnostic, Text: "warning"}, completed,
		}, "")
		requireLines(t, events,
			line{"reasoning", "Plan: "},
			line{"reasoning", "Plan: run ls."},
			line{"final-message", ""},
		)
		if events[0].ID != events[1].ID || events[2].ID != 2 {
			t.Fatalf("ids: %d %d %d", events[0].ID, events[1].ID, events[2].ID)
		}
	})
}

func TestTranscriptCodexCompletedOnlyMessage(t *testing.T) {
	events := record(t, []harness.Event{
		{Kind: harness.EventAssistantTextDelta, ItemID: "msg_1", Status: "completed", Phase: harness.PhaseCompleted, Text: "All done."},
		{Kind: harness.EventAssistantTextDelta, ItemID: "msg_2", Phase: harness.PhaseCompleted},
	}, "All done.")
	requireLines(t, events,
		line{"assistant-text-delta", "All done."},
		line{"final-message", "All done."},
	)
	if events[0].Role != "assistant" {
		t.Fatalf("role %q", events[0].Role)
	}
}

func TestTranscriptFailedCommandKeepsExitCodeAndOutput(t *testing.T) {
	exit := 2
	events := record(t, []harness.Event{
		{Kind: harness.EventCommandExecution, ItemID: "exec_1", Command: "ls nope", Phase: harness.PhaseStarted, Status: "inProgress"},
		{Kind: harness.EventCommandExecution, ItemID: "exec_1", Phase: harness.PhaseUpdated, Text: "ls: nope"},
		{Kind: harness.EventCommandExecution, ItemID: "exec_1", Command: "ls nope", Phase: harness.PhaseFailed, Status: "failed", Text: "ls: nope: No such file or directory\n", ExitCode: &exit},
		{Kind: harness.EventToolCall, ItemID: "mcp_1", Name: "rhizome/search", Phase: harness.PhaseCompleted, Output: json.RawMessage(`{"hits":[]}`)},
	}, "")
	requireLines(t, events,
		line{"command-execution", ""},
		line{"command-execution", "ls: nope: No such file or directory\n"},
		line{"tool-call", ""},
		line{"final-message", ""},
	)
	if failed := events[1].Result; failed.Phase != harness.PhaseFailed || *failed.ExitCode != 2 {
		t.Fatalf("failed command: %+v", failed)
	}
	if string(events[2].Result.Output) != `{"hits":[]}` {
		t.Fatalf("tool output %s", events[2].Result.Output)
	}
}

func TestTranscriptCapKeepsFinalMessageAndUsage(t *testing.T) {
	big := strings.Repeat("x", transcriptTextBytes)
	var stream []harness.Event
	for range transcriptMaxBytes/transcriptTextBytes + 10 {
		stream = append(stream, harness.Event{Kind: harness.EventCommandExecution, ItemID: "exec", Phase: harness.PhaseCompleted, Text: big})
	}
	stream = append(stream, harness.Event{Kind: harness.EventTokenUsage, Usage: harness.TokenUsage{TotalTokens: 99}})
	events := record(t, stream, "Final words.")
	tail := lines(events[len(events)-3:])
	if tail[0] != (line{"transcript-truncated", ""}) || tail[1] != (line{"final-message", "Final words."}) || tail[2].kind != "token-usage" {
		t.Fatalf("tail %q", tail)
	}
	if len(events) >= len(stream)+2 {
		t.Fatalf("nothing was dropped: %d lines", len(events))
	}
	requireSequence(t, events)
}

func TestReadTranscriptPartialLineAndResume(t *testing.T) {
	path := transcriptPath(t)
	first := `{"id":1}` + "\n" + `{"id":2}` + "\n"
	if err := os.WriteFile(path, []byte(first+`{"id":`), 0o600); err != nil {
		t.Fatal(err)
	}
	events, next, err := ReadTranscript(path, 0)
	if err != nil || len(events) != 2 || next != int64(len(first)) {
		t.Fatalf("events=%d next=%d err=%v", len(events), next, err)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("3}\n"); err != nil {
		t.Fatal(err)
	}
	file.Close()
	events, next, err = ReadTranscript(path, next)
	if err != nil || len(events) != 1 || string(events[0]) != `{"id":3}` {
		t.Fatalf("resume: %q err=%v", events, err)
	}

	size := next
	for _, after := range []int64{size + 100, -5} {
		events, next, err = ReadTranscript(path, after)
		want := min(max(after, 0), size)
		if err != nil || next != size || (want == size) != (len(events) == 0) {
			t.Fatalf("after=%d: events=%d next=%d err=%v", after, len(events), next, err)
		}
	}
}

func TestTranscriptFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("owner-only modes apply on macOS and Linux (SPEC-0127)")
	}
	path := transcriptPath(t)
	transcript, err := NewTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	transcript.Close()
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v err=%v", info.Mode(), err)
	}
}

func TestRunPromptOutcomes(t *testing.T) {
	cases := []struct {
		name    string
		events  []harness.Event
		message string
		err     string
	}{
		{
			name: "retry then success",
			events: []harness.Event{
				{Kind: harness.EventError, Status: "retrying", Phase: harness.PhaseFailed, Text: "Reconnecting... 1/5"},
				{Kind: harness.EventAssistantTextDelta, Phase: harness.PhaseUpdated, Text: "Fixed it."},
				{Kind: harness.EventTokenUsage, Usage: harness.TokenUsage{TotalTokens: 42}},
				{Kind: harness.EventTurnCompleted, Status: "completed", Phase: harness.PhaseCompleted},
			},
			message: "Fixed it.",
		},
		{
			name: "failed turn without an error event",
			events: []harness.Event{
				{Kind: harness.EventAssistantTextDelta, Phase: harness.PhaseUpdated, Text: "Working"},
				{Kind: harness.EventTurnCompleted, Status: "failed", Phase: harness.PhaseFailed, Reason: "usage limit reached"},
			},
			err: "codex: usage limit reached",
		},
		{
			name: "error event",
			events: []harness.Event{
				{Kind: harness.EventError, Phase: harness.PhaseFailed, Text: "stream disconnected"},
				{Kind: harness.EventTurnCompleted, Status: "failed", Phase: harness.PhaseFailed, Reason: "other"},
			},
			err: "codex: stream disconnected",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			session := harnesstest.NewSession()
			for _, event := range tc.events {
				session.Emit(event)
			}
			agent := HarnessAgent{
				Drivers:  map[harness.Kind]harness.Harness{harness.KindCodex: &harnesstest.Harness{Session: session}},
				Settings: func() (agentchat.Settings, error) { return agentchat.Settings{Harness: harness.KindCodex}, nil },
			}
			path := transcriptPath(t)
			result, err := agent.RunPrompt(context.Background(), AgentRequest{Dir: t.TempDir(), Prompt: "go", Transcript: path})
			if tc.err != "" {
				if err == nil || err.Error() != tc.err {
					t.Fatalf("err = %v, want %q", err, tc.err)
				}
			} else if err != nil || result.Message != tc.message {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			events := readEvents(t, path)
			last := events[len(events)-1]
			if last.Type != eventFinalMessage && last.Type != string(harness.EventTokenUsage) {
				t.Fatalf("transcript ends with %q", last.Type)
			}
			if tc.events[0].Kind == harness.EventError && events[0].Type != string(harness.EventError) {
				t.Fatalf("error event not written: %q", lines(events))
			}
		})
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
