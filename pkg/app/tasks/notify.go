package tasks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/procrun"
	"github.com/atomicobject/rhizome/pkg/loginpath"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

const (
	notifyTimeout   = 30 * time.Second
	notifyLogFile   = "notify.log"
	maxNotifyOutput = 64 << 10
)

// Values of RZM_TASK_OUTCOME beyond a run's own outcome.
const (
	NotifyAttention = "attention"
	NotifyTokenCap  = "token-cap"
	NotifyTest      = "test"
)

// Notification is one call of the person's notify command (SPEC-0127
// US14): Summary goes to its stdin, the rest to its environment.
type Notification struct {
	Task    string `json:"task"`
	Outcome string `json:"outcome"`
	Run     string `json:"run"`
	Item    string `json:"item,omitempty"`
	Summary string `json:"summary"`
}

// alertRun runs the notify command for a recorded run that needs the
// person. Tests never notify.
func (s *Service) alertRun(ctx context.Context, name, runID string) {
	st, err := s.store.Load()
	if err != nil {
		return
	}
	r, ok := st.runByID(runID)
	if !ok || r.Test {
		return
	}
	outcome := r.Outcome
	if r.Result.attention() {
		outcome = NotifyAttention
	}
	s.notify(ctx, Notification{Task: r.Task, Outcome: outcome, Run: r.ID, Item: r.Item, Summary: alertSummary(name, outcome, r)})
}

// alertSummary is the notification text: the task, what happened, and the
// agent's summary or the run's message.
func alertSummary(name, outcome string, r RunRecord) string {
	phrase := map[string]string{
		OutcomeFailed:      "failed",
		OutcomeInterrupted: "was interrupted",
		OutcomeNotified:    "needs you",
		OutcomeReady:       "has changes to review",
		NotifyAttention:    "needs attention",
	}[outcome]
	if phrase == "" {
		phrase = outcome
	}
	text := r.Message
	if r.Result != nil && r.Result.Summary != "" {
		text = r.Result.Summary
	}
	if text == "" {
		return name + " " + phrase
	}
	return name + " " + phrase + ": " + text
}

// notify runs the person's notify command, if one is set, writes its
// output to the run's notify.log, and records a failure on the run. It
// never fails the run.
func (s *Service) notify(ctx context.Context, n Notification) {
	ctx = context.WithoutCancel(ctx)
	if os.Getenv("RZM_TASK_TEST") == "1" {
		return // a task test's own rzm must not page the person
	}
	command, err := s.notifyCommand()
	if err == nil && len(command) == 0 {
		return
	}
	var out []byte
	if err == nil {
		out, err = s.runNotify(ctx, command, n)
	}
	if path := s.runFile(n.Run, notifyLogFile); len(out) > 0 && path != "" {
		if os.MkdirAll(filepath.Dir(path), 0o700) == nil {
			_ = os.WriteFile(path, out, 0o600)
		}
	}
	if err != nil {
		_ = s.store.Update(ctx, func(st *State) error {
			st.patchRun(n.Run, func(r *RunRecord) { r.NotifyError = err.Error() })
			return nil
		})
	}
}

// notifyCommand is the person's tasks.notify.command, or nil.
func (s *Service) notifyCommand() ([]string, error) {
	if s.opts.Env.IsZero() {
		return nil, nil
	}
	cfg, err := repoconfig.LoadTasksUserConfig(s.opts.Env)
	if err != nil || cfg.Notify == nil {
		return nil, err
	}
	return cfg.Notify.Command, nil
}

// runNotify runs command the way checks run: the login shell's PATH with
// this runtime's rzm first, its own process group, everything it started
// killed when it ends, and a 30-second limit. It returns stdout then
// stderr, bounded.
func (s *Service) runNotify(ctx context.Context, command []string, n Notification) ([]byte, error) {
	name, err := loginpath.Look(command[0])
	if err != nil {
		return nil, fmt.Errorf("notify %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, notifyTimeout)
	defer cancel()
	cmd := exec.Command(name, command[1:]...)
	cmd.Dir = s.opts.Repo.MainCheckout
	cmd.Env = append(loginpath.WithPath(os.Environ(), s.taskPath()), n.env(s.repository())...)
	cmd.Stdin = strings.NewReader(n.Summary + "\n")
	stderr := &cappedBuffer{n: maxNotifyOutput}
	out, err := procrun.Run(ctx, cmd, stderr, maxNotifyOutput)
	out = append(out, stderr.Bytes()...)
	switch {
	case err == nil:
		return out, nil
	case errors.Is(err, context.DeadlineExceeded):
		return out, fmt.Errorf("the notify command did not finish within %s", notifyTimeout)
	default:
		return out, fmt.Errorf("the notify command failed: %w", err)
	}
}

func (n Notification) env(repository string) []string {
	env := []string{
		"RZM_TASK_ID=" + n.Task,
		"RZM_TASK_OUTCOME=" + n.Outcome,
		"RZM_TASK_RUN_ID=" + n.Run,
		"RZM_TASK_REPOSITORY=" + repository,
	}
	if n.Item != "" {
		env = append(env, "RZM_TASK_ITEM="+n.Item)
	}
	return env
}

// repository names the repository for notifications: its main checkout,
// else its Git common directory.
func (s *Service) repository() string {
	if s.opts.Repo.MainCheckout != "" {
		return s.opts.Repo.MainCheckout
	}
	return s.opts.Repo.CommonDir
}

// NotifyTest sends a sample notification through the person's notify
// command, so they can check that it reaches them. It returns the command
// and its output.
func (s *Service) NotifyTest(ctx context.Context) (Notification, []string, []byte, error) {
	command, err := s.notifyCommand()
	if err != nil {
		return Notification{}, nil, nil, err
	}
	if len(command) == 0 {
		return Notification{}, nil, nil, errors.New("no notify command is set; add tasks.notify.command to your user configuration (~/.config/rhizome/config.yml)")
	}
	n := Notification{Task: "notify-test", Outcome: NotifyTest, Run: newRunID(s.opts.Now()), Summary: "Rhizome test notification: your notify command works."}
	out, err := s.runNotify(ctx, command, n)
	return n, command, out, err
}

// runByID finds a scheduled run's record.
func (s *State) runByID(id string) (RunRecord, bool) {
	for i := len(s.History) - 1; i >= 0; i-- {
		if s.History[i].ID == id {
			return s.History[i], true
		}
	}
	return RunRecord{}, false
}

// patchRun edits a scheduled run's record in history and, when it is the
// task's latest, in Last.
func (s *State) patchRun(id string, fn func(*RunRecord)) {
	for i := len(s.History) - 1; i >= 0; i-- {
		if r := &s.History[i]; r.ID == id {
			fn(r)
			if last, ok := s.Last[r.Task]; ok && last.ID == id {
				s.Last[r.Task] = *r
			}
			return
		}
	}
}

// cappedBuffer keeps the first n bytes written and drops the rest.
type cappedBuffer struct {
	bytes.Buffer
	n int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.n - c.Len(); room > 0 {
		c.Buffer.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}
