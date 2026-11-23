package tasks

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

const (
	alertLock = "alert.lock"
	// maxPendingAlerts bounds the queue; older alerts are dropped and
	// counted in the next one delivered.
	maxPendingAlerts = 50
	// fetchAlertAfter is how long fetching the primary ref must keep
	// failing before the host alerts. A time, not a count of failed
	// fetches, because a pass fetches only when a task is due: three
	// failures take three days for a daily task and an hour for a
	// 20-minute one, while six hours measures how stale the ref is.
	fetchAlertAfter = 6 * time.Hour
	// passRetryEvery spaces the host passes NextDueAt allows while the
	// primary ref cannot be read, so each one records and alerts it.
	passRetryEvery = 15 * time.Minute
)

// Values of RZM_TASK_OUTCOME for a pass-level alert, which has no task or
// run (SPEC-0127 US14).
const (
	NotifyUntrusted   = PassUntrusted
	NotifyPassFailed  = "pass-failed"
	NotifyFetchFailed = "fetch-failed"
)

// PendingAlert is one notify-command call waiting for delivery. It is
// queued in the same state update that records its cause and delivered
// after run.lock is released, so a slow command never holds up runs and a
// process stopped mid-delivery leaves it for the next one.
type PendingAlert struct {
	ID string `json:"id"`
	Notification
}

// PassAlert is the pass-level failure a host last alerted on, so each
// distinct failure (code and message) alerts once until it clears.
type PassAlert struct {
	At          time.Time `json:"at"`
	Code        string    `json:"code"`
	Message     string    `json:"message"`
	NotifyError string    `json:"notifyError,omitempty"`
}

// queueAlert adds a notification to the queue, unless no notify command is
// set or this is a task test's own rzm, which must not page the person. A
// configuration that cannot be read is queued, so delivery records why.
func (s *Service) queueAlert(st *State, n Notification) {
	if os.Getenv("RZM_TASK_TEST") == "1" {
		return
	}
	if command, err := s.notifyCommand(); err == nil && len(command) == 0 {
		return
	}
	st.PendingAlerts = append(st.PendingAlerts, PendingAlert{ID: newRunID(s.opts.Now()), Notification: n})
	if extra := len(st.PendingAlerts) - maxPendingAlerts; extra > 0 {
		st.PendingAlerts = slices.Delete(st.PendingAlerts, 0, extra)
		st.DroppedAlerts += extra
	}
}

// queueRunAlert queues the alert for a run already recorded in st. notice
// is what the run raised beyond its outcome, such as a failed push; it
// makes the alert an attention one. Tests and runs the person stopped
// never alert.
func (s *Service) queueRunAlert(st *State, name, runID, notice string) {
	r, ok := st.runByID(runID)
	if !ok || r.Test || r.Outcome == OutcomeStopped {
		return
	}
	outcome := r.Outcome
	if r.Result.attention() || notice != "" {
		outcome = NotifyAttention
	}
	s.queueAlert(st, Notification{Task: r.Task, Outcome: outcome, Run: r.ID, Item: r.Item, Summary: alertSummary(name, outcome, r, notice)})
}

// deliverAlerts runs the notify command for each queued alert, oldest
// first, and removes each one only after its command finished, recording a
// failure on its run. Callers have released run.lock. One process delivers
// at a time; a stop or cancel mid-delivery leaves the alert for the next
// run-due or run.
func (s *Service) deliverAlerts(ctx context.Context) {
	if st, err := s.store.Load(); err != nil || len(st.PendingAlerts) == 0 || os.Getenv("RZM_TASK_TEST") == "1" {
		return
	}
	if s.opts.DeliverAlerts != nil {
		// A failed hand-off leaves the alerts queued for the next process.
		_ = s.opts.DeliverAlerts()
		return
	}
	s.DeliverAlerts(ctx)
}

// DeliverAlerts sends queued alerts in this process, oldest first, under
// alert.lock (`rzm tasks deliver-alerts`).
func (s *Service) DeliverAlerts(ctx context.Context) {
	release, err := acquire(ctx, filepath.Join(s.stateDir, alertLock), "tasks/alert", notifyTimeout+5*time.Second)
	if err != nil {
		return
	}
	defer func() { _ = release() }()
	for ctx.Err() == nil {
		st, err := s.store.Load()
		if err != nil || len(st.PendingAlerts) == 0 {
			return
		}
		p, dropped := st.PendingAlerts[0], st.DroppedAlerts
		if dropped > 0 {
			p.Summary += fmt.Sprintf(" (Rhizome dropped %d older alerts.)", dropped)
		}
		command, err := s.notifyCommand()
		var out []byte
		if err == nil && len(command) > 0 {
			out, err = s.runNotify(ctx, command, p.Notification)
		}
		if ctx.Err() != nil {
			return
		}
		s.writeNotifyLog(p.Run, out)
		if s.store.Update(context.WithoutCancel(ctx), func(st *State) error {
			st.PendingAlerts = slices.DeleteFunc(st.PendingAlerts, func(q PendingAlert) bool { return q.ID == p.ID })
			st.DroppedAlerts = max(0, st.DroppedAlerts-dropped)
			if err == nil {
				return nil
			}
			if p.Run != "" {
				st.patchRun(p.Run, func(r *RunRecord) { r.NotifyError = err.Error() })
			} else if st.PassAlert != nil {
				st.PassAlert.NotifyError = err.Error()
			}
			return nil
		}) != nil {
			return
		}
	}
}

// passFailure is the pass-level failure a host alerts on: the pass error,
// or a fetch of the primary ref that has kept failing for fetchAlertAfter.
func passFailure(st *State) (code, message string) {
	if pe := st.PassError; pe != nil {
		return cmp.Or(pe.Code, NotifyPassFailed), pe.Message
	}
	if f := st.Fetch; f != nil && f.Error != "" && !f.FailingSince.IsZero() && f.At.Sub(f.FailingSince) >= fetchAlertAfter {
		return NotifyFetchFailed, f.Error
	}
	return "", ""
}

// reviewPassAlert queues one alert for a new pass-level failure and forgets
// a failure that cleared, so it alerts again if it recurs (SPEC-0127 US14).
func (s *Service) reviewPassAlert(st *State) {
	code, message := passFailure(st)
	if code == "" {
		st.PassAlert = nil
		return
	}
	if pa := st.PassAlert; pa != nil && pa.Code == code && pa.Message == message {
		return
	}
	st.PassAlert = &PassAlert{At: s.opts.Now(), Code: code, Message: message}
	summary := fmt.Sprintf("Scheduled tasks in %s could not run: %s", s.repository(), message)
	if code == NotifyFetchFailed {
		summary = fmt.Sprintf("Scheduled tasks in %s are running from a stale primary ref: fetching it has failed since %s: %s", s.repository(), st.Fetch.FailingSince.Local().Format("Jan 2 15:04"), message)
	}
	s.queueAlert(st, Notification{Outcome: code, Summary: summary})
}

// notePassAlert applies reviewPassAlert, writing state only when the
// failure to alert on changed.
func (s *Service) notePassAlert(ctx context.Context) {
	st, err := s.store.Load()
	if err != nil {
		return
	}
	if code, message := passFailure(&st); passAlertKey(st.PassAlert) == [2]string{code, message} {
		return
	}
	_ = s.store.Update(ctx, func(st *State) error {
		s.reviewPassAlert(st)
		return nil
	})
}

func passAlertKey(pa *PassAlert) [2]string {
	if pa == nil {
		return [2]string{}
	}
	return [2]string{pa.Code, pa.Message}
}

// passRetryAt is when the host may try another pass while the primary ref
// cannot be read: passRetryEvery after the pass that recorded the error,
// or now when none has.
func passRetryAt(st *State, now time.Time) time.Time {
	if pe := st.PassError; pe != nil && pe.Code != PassUntrusted && !pe.At.IsZero() {
		return pe.At.Add(passRetryEvery)
	}
	return now
}
