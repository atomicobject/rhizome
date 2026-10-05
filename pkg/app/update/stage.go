package update

import (
	"errors"
	"fmt"
)

// Install pipeline stage names used in user-facing failure messages.
const (
	StageManifest  = "manifest"
	StageDownload  = "download"
	StageChecksum  = "checksum"
	StageExtract   = "extract"
	StageSmokeTest = "smoke test"
	StageReplace   = "replace"
)

// StageError tags an install failure with the pipeline stage that failed so
// callers can produce actionable messages (and tests can assert the stage).
type StageError struct {
	Stage string
	Err   error
}

func (e *StageError) Error() string {
	return fmt.Sprintf("%s stage failed: %v", e.Stage, e.Err)
}

func (e *StageError) Unwrap() error {
	return e.Err
}

// stageErr wraps err with a stage name unless it already carries one.
func stageErr(stage string, err error) error {
	if err == nil {
		return nil
	}
	var existing *StageError
	if errors.As(err, &existing) {
		return err
	}
	return &StageError{Stage: stage, Err: err}
}
