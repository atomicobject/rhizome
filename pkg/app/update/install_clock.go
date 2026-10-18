package update

import (
	"context"
	"time"
)

// installLockClock owns the deadline and waits for one install-lock attempt.
type installLockClock interface {
	Now() time.Time
	Wait(context.Context, time.Duration) error
}

type wallInstallLockClock struct{}

func (wallInstallLockClock) Now() time.Time { return time.Now() }

func (wallInstallLockClock) Wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
