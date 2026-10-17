package serve

import "time"

// validationRefreshTimer keeps debounce scheduling local to one coordinator.
// Its owner stops or resets it before consuming a replacement request.
type validationRefreshTimer interface {
	Ticks() <-chan time.Time
	Stop() bool
	Reset(time.Duration) bool
}

type wallValidationTimer struct{ *time.Timer }

func newWallValidationTimer(delay time.Duration) validationRefreshTimer {
	return wallValidationTimer{time.NewTimer(delay)}
}

func (t wallValidationTimer) Ticks() <-chan time.Time { return t.C }
