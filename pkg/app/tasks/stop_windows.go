//go:build windows

package tasks

import (
	"context"
	"errors"
	"time"
)

// ponytail: Windows cannot stop a run yet (scheduled tasks are not
// supported there, SPEC-0127 Delivery Phases); use taskkill /T when they are.
func stopProcessTree(context.Context, int, time.Duration) error {
	return errors.New("stopping a task run is not supported on Windows")
}
