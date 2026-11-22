package tasks

import (
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
	Task    string `json:"task,omitempty"`
	Outcome string `json:"outcome"`
	Run     string `json:"run,omitempty"`
	Item    string `json:"item,omitempty"`
	Summary string `json:"summary"`
}

// alertSummary is the notification text: the task, what happened, the
// run's notices (a push failure, restored protected files, a moved HEAD),
// and the agent's summary or the run's message.
func alertSummary(name, outcome string, r RunRecord, notice string) string {
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
	if text = joinNotices(notice, text); text == "" {
		return name + " " + phrase
	}
	return name + " " + phrase + ": " + text
}

// writeNotifyLog saves the notify command's output as the run's notify.log,
// replacing an earlier one. It writes only a new regular file in the real
// run folder, never through a link an agent left there.
func (s *Service) writeNotifyLog(runID string, out []byte) {
	path := s.runFile(runID, notifyLogFile)
	if len(out) == 0 || path == "" {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0o700) != nil || realDir(filepath.Dir(path)) != nil {
		return
	}
	_ = os.Remove(path)
	f, err := openRegular(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return
	}
	_, _ = f.Write(out)
	_ = f.Close()
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
	stderr := &cappedWriter{n: maxNotifyOutput}
	out, err := procrun.Run(ctx, cmd, stderr, maxNotifyOutput)
	out = append(out, stderr.buf...)
	switch {
	case err == nil:
		return out, nil
	case errors.Is(err, context.DeadlineExceeded):
		return out, fmt.Errorf("the notify command did not finish within %s", notifyTimeout)
	default:
		return out, fmt.Errorf("the notify command failed: %w", err)
	}
}

// env is the notification's environment. A pass-level alert has no task
// or run, so those variables are unset.
func (n Notification) env(repository string) []string {
	env := []string{"RZM_TASK_OUTCOME=" + n.Outcome, "RZM_TASK_REPOSITORY=" + repository}
	for _, kv := range [][2]string{{"RZM_TASK_ID", n.Task}, {"RZM_TASK_RUN_ID", n.Run}, {"RZM_TASK_ITEM", n.Item}} {
		if kv[1] != "" {
			env = append(env, kv[0]+"="+kv[1])
		}
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

// cappedWriter keeps the first n bytes written and drops the rest. It has
// no ReadFrom, so io.Copy (which os/exec uses for a command's output)
// cannot bypass the cap.
type cappedWriter struct {
	buf []byte
	n   int
}

func (c *cappedWriter) Write(p []byte) (int, error) {
	if room := c.n - len(c.buf); room > 0 {
		c.buf = append(c.buf, p[:min(len(p), room)]...)
	}
	return len(p), nil
}
