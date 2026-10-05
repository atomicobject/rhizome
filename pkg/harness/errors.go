package harness

import (
	"errors"
	"fmt"
)

var (
	ErrNotInstalled    = errors.New("harness not installed")
	ErrNotLoggedIn     = errors.New("harness not logged in")
	ErrSpawn           = errors.New("harness spawn")
	ErrInitialize      = errors.New("harness initialize")
	ErrThreadStart     = errors.New("harness thread start")
	ErrTurnStart       = errors.New("harness turn start")
	ErrDecode          = errors.New("harness decode")
	ErrTransportClosed = errors.New("harness transport closed")
	ErrTimeout         = errors.New("harness timeout")
	ErrCommandFailed   = errors.New("harness command failed")
	ErrSchemaMismatch  = errors.New("harness schema mismatch")
)

type PhaseError struct {
	Phase error
	Err   error
}

func (e *PhaseError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Phase.Error()
	}
	return fmt.Sprintf("%s: %v", e.Phase, e.Err)
}

func (e *PhaseError) Unwrap() []error {
	if e == nil {
		return nil
	}
	return []error{e.Phase, e.Err}
}

func Phase(phase, err error) error {
	if err == nil {
		return nil
	}
	return &PhaseError{Phase: phase, Err: err}
}
