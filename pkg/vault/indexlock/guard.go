package indexlock

import "errors"

var errGuardBusy = errors.New("index lock guard is held")
