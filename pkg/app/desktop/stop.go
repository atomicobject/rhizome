package desktop

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/app/runtimestop"
)

type StopResult struct {
	Stopped bool `json:"stopped"`
}

// Stop shuts down the folder's runtime as `rzm stop` does: gracefully, then
// terminating a headless runtime that ignores shutdown. Like Open, it acts
// only on a runtime whose manifest and health name this folder. It runs no
// repository executable, so it needs no trust.
func (s *Service) Stop(ctx context.Context, req Request) (StopResult, error) {
	info, _, err := s.inspect(req.Folder)
	if err != nil {
		return StopResult{}, err
	}
	if err := checkRecordedRuntimeIdentity(ctx, info.Path); err != nil {
		return StopResult{}, err
	}
	if err := runtimestop.StopVault(ctx, info.Path, 0); err != nil {
		return StopResult{}, problem("runtime_error", "Rhizome did not stop: "+err.Error())
	}
	return StopResult{Stopped: true}, nil
}
