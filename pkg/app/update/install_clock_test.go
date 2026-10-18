package update

import (
	"context"
	"time"
)

type controlledInstallClock struct {
	now    time.Time
	waits  []time.Duration
	onWait func(context.Context, time.Duration) error
}

func (c *controlledInstallClock) Now() time.Time { return c.now }

func (c *controlledInstallClock) Wait(ctx context.Context, delay time.Duration) error {
	c.waits = append(c.waits, delay)
	c.now = c.now.Add(delay)
	if c.onWait != nil {
		return c.onWait(ctx, delay)
	}
	return ctx.Err()
}
