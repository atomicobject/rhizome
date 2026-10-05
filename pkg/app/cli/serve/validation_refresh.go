package serve

import (
	"context"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/web"
)

const validationRefreshDebounce = 75 * time.Millisecond

// RequestValidationRefresh coalesces source-save and watcher notifications.
// The worker uses the serve lifetime rather than an HTTP request context so a
// response can report saved while validation continues to publication.
func (c *ReadinessCoordinator) RequestValidationRefresh() {
	if c == nil || c.validationRequests == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requestValidationRefreshLocked()
}

// ValidationRefreshPending reports queued or executing refresh work. It is
// separate from durable generation status, which can describe an older run.
func (c *ReadinessCoordinator) ValidationRefreshPending() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.validationRefreshPending
}

func (c *ReadinessCoordinator) validationAdmissionLocked() context.Context {
	if c.validationAdmission == nil {
		c.validationAdmission, c.cancelValidationAdmission = context.WithCancel(c.liveContext(context.Background()))
	}
	return c.validationAdmission
}

func (c *ReadinessCoordinator) requestValidationRefreshLocked() {
	ctx := c.validationAdmissionLocked()
	if ctx.Err() != nil {
		return
	}
	c.validationRefreshPending = true
	// Replace a buffered request from an invalidated admission period.
	select {
	case <-c.validationRequests:
	default:
	}
	c.validationRequests <- ctx
}

// Capture admission before startup waits or validation begins. Cancellation of
// either the caller or this admission period invalidates the captured request.
func (c *ReadinessCoordinator) validationRequestContext(ctx context.Context) (context.Context, func()) {
	c.mu.Lock()
	admission := c.validationAdmissionLocked()
	request, cancel := context.WithCancel(admission)
	c.mu.Unlock()
	stop := context.AfterFunc(ctx, cancel)
	if ctx.Err() != nil {
		cancel()
	}
	return request, func() { stop(); cancel() }
}

func (c *ReadinessCoordinator) runValidationRefresh(ctx context.Context) {
	c.mu.Lock()
	live := c.live
	c.mu.Unlock()
	c.refreshValidation(ctx, live)
}

func (c *ReadinessCoordinator) runValidationRefreshRequests(ctx context.Context) {
	defer c.finishValidationRefreshRequests(true)
	for {
		var request context.Context
		select {
		case <-ctx.Done():
			return
		case request = <-c.validationRequests:
		}

		timer := time.NewTimer(validationRefreshDebounce)
	debounce:
		for {
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return
			case request = <-c.validationRequests:
				if !timer.Stop() {
					<-timer.C
				}
				timer.Reset(validationRefreshDebounce)
			case <-timer.C:
				break debounce
			}
		}

		c.runValidationRefresh(request)
		c.finishValidationRefreshRequests(false)
	}
}

func (c *ReadinessCoordinator) finishValidationRefreshRequests(stopped bool) {
	c.mu.Lock()
	changed := c.validationRefreshPending && (stopped || len(c.validationRequests) == 0)
	if changed {
		c.validationRefreshPending = false
	}
	live := c.live
	c.mu.Unlock()
	if changed && live != nil {
		live.PublishGlobalEvent(web.GlobalEventValidateChanged, nil)
	}
}
