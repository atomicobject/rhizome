package tasks

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// TokenCap is the repository's daily token cap on this machine (SPEC-0127
// US14) and the tokens its scheduled runs used today. Days are the UTC days
// that token totals are stored in, so the cap resets at 00:00 UTC.
type TokenCap struct {
	Cap     int64  `json:"cap"`
	Used    int64  `json:"used"`
	Day     string `json:"day"`
	Reached bool   `json:"reached"`
}

// todayTokens is the cap's state at now, or nil when there is no cap.
func todayTokens(st *State, limit int64, now time.Time) *TokenCap {
	if limit <= 0 {
		return nil
	}
	day := tokenDay(now)
	used := st.tokensOn(day)
	return &TokenCap{Cap: limit, Used: used, Day: day, Reached: used >= limit}
}

// tokensOn sums every task's tokens for one UTC day.
func (s *State) tokensOn(day string) int64 {
	st := *s
	if st.Tokens == nil {
		st.seedTokens()
	}
	var total int64
	for _, days := range st.Tokens {
		total += days[day]
	}
	return total
}

// capBlocks reports whether a reached cap stops this task's scheduled
// agent work. Checks, notify-mode items, and the person's own Run go on.
func capBlocks(c *TokenCap, mode Mode, def Definition) bool {
	return c != nil && c.Reached && mode == ModeAuto && strings.TrimSpace(def.Prompt) != ""
}

// skipForTokenCap stops a scheduled run before its agent work when the
// repository reached today's cap: it records the run as skipped, which
// moves the task's clock, and notifies once per UTC day.
func (s *Service) skipForTokenCap(ctx context.Context, def Definition, a attempt, commit string) (Outcome, bool) {
	m, err := s.MachineSettings()
	if err != nil || m.Repository.DailyTokenCap <= 0 {
		return Outcome{}, false
	}
	st, err := s.store.Load()
	if err != nil {
		return Outcome{}, false
	}
	c := todayTokens(&st, m.Repository.DailyTokenCap, s.opts.Now())
	if !c.Reached {
		return Outcome{}, false
	}
	reason := fmt.Sprintf("token cap reached: %d of %d tokens used today (UTC day %s)", c.Used, c.Cap, c.Day)
	if err := s.store.Update(ctx, func(st *State) error {
		r := a.record(def.ID, OutcomeSkipped, s.opts.Now())
		r.Message, r.Commit = reason, commit
		st.Record(r)
		if st.TokenCapNotified != c.Day {
			st.TokenCapNotified = c.Day
			s.queueAlert(st, Notification{Task: def.ID, Outcome: NotifyTokenCap, Run: a.id, Summary: fmt.Sprintf("Scheduled tasks in %s stopped agent work for today: %s. They resume after 00:00 UTC; Run now still works.", s.repository(), reason)})
		}
		return nil
	}); err != nil {
		return Outcome{Task: def.ID, Outcome: OutcomeFailed, Reason: err.Error()}, true
	}
	return Outcome{Task: def.ID, Outcome: OutcomeSkipped, Reason: reason}, true
}
