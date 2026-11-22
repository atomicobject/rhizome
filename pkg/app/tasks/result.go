package tasks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Result statuses an agent may report in $RZM_TASK_RESULT_FILE
// (SPEC-0127 US14).
const (
	ResultQuiet     = "quiet"
	ResultOK        = "ok"
	ResultAttention = "attention"
)

const (
	resultFile    = "result.json"
	maxResultFile = 16 << 10
)

// RunResult is what a run's agent reported in its result file. Error says
// why a file that was written could not be used; the run then counts as ok.
type RunResult struct {
	Status  string `json:"status,omitempty"`
	Summary string `json:"summary,omitempty"`
	Error   string `json:"error,omitempty"`
}

// status is the reported status, or "" for no usable result.
func (r *RunResult) status() string {
	if r == nil {
		return ""
	}
	return r.Status
}

// attention reports whether the agent asked for the person.
func (r *RunResult) attention() bool { return r.status() == ResultAttention }

// notifiesFor is the quiet rule (SPEC-0127 US14): whether a finished run
// leaves an inbox item and runs the notify command. Failures, interrupted
// runs, notify-mode items, and an attention result always notify.
// Not-needed, no-changes, quiet, and ok runs record history only, except
// that committed changes left on a local task branch need the person to
// review them (review true); changes landed in place or handed to a pushed
// pull request do not. A new outcome that needs the person must be listed
// here.
func notifiesFor(outcome, status string, review bool) bool {
	if status == ResultAttention {
		return true
	}
	switch outcome {
	case OutcomeFailed, OutcomeInterrupted, OutcomeNotified:
		return true
	case OutcomeReady:
		return review
	}
	return false
}

// withResultFile gives a prompt run's locations the run's result file,
// which the agent's environment and preamble then name. Checks never get it.
func (s *Service) withResultFile(loc Locations, a attempt) Locations {
	loc.ResultFile = s.runFile(a.id, resultFile)
	return loc
}

// afterPrompt runs once a prompt run ends, however it ended: it trims the
// handoff note and reads the result file.
func afterPrompt(def Definition, loc Locations) *RunResult {
	if def.Memory {
		_ = trimHandoff(handoffPath(loc))
	}
	return readResult(loc.ResultFile)
}

// readResult reads a result file. A missing file is no result; a malformed
// one is a result with Error set and no status, which counts as ok, and so
// is anything but a regular file in the runner's own run folder: a FIFO,
// device, or symlink is refused without being read.
func readResult(path string) *RunResult {
	if path == "" {
		return nil
	}
	f, err := openRegular(path, os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return &RunResult{Error: err.Error()}
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxResultFile+1))
	if err != nil {
		return &RunResult{Error: err.Error()}
	}
	if len(data) > maxResultFile {
		return &RunResult{Error: fmt.Sprintf("the result file is larger than %d KB", maxResultFile>>10)}
	}
	var r RunResult
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&r); err != nil {
		return &RunResult{Error: "the result file is not a JSON object: " + err.Error()}
	}
	r.Error = ""
	r.Summary = strings.TrimSpace(r.Summary)
	switch r.Status {
	case ResultQuiet, ResultOK, ResultAttention:
		return &r
	}
	return &RunResult{Summary: r.Summary, Error: fmt.Sprintf("status must be %q, %q, or %q, not %q", ResultQuiet, ResultOK, ResultAttention, r.Status)}
}

func writeResultFile(b *strings.Builder, loc Locations) {
	if loc.ResultFile == "" {
		return
	}
	fmt.Fprintf(b, "Before you finish, you may write %s ($RZM_TASK_RESULT_FILE) with one JSON object,", loc.ResultFile)
	b.WriteString(" {\"status\": \"quiet\" | \"ok\" | \"attention\", \"summary\": \"...\"}: quiet when nothing happened worth mentioning, ok for a normal run, attention when the person must look at something. The summary is one or two sentences for the person. Attention always reaches the person, possibly on their phone; quiet and ok runs that change nothing do not.\n\n")
}
