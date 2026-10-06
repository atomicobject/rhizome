package desktop

import (
	"bytes"
	"context"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/runtimestop"
)

type StopResult struct {
	Stopped bool `json:"stopped"`
}

// Stop shuts down the folder's runtime as `rzm stop` does: gracefully, then
// terminating a headless runtime that ignores shutdown. It runs no repository
// executable, so it needs no trust.
func (s *Service) Stop(ctx context.Context, req Request) (StopResult, error) {
	info, _, err := s.inspect(req.Folder)
	if err != nil {
		return StopResult{}, err
	}
	var out bytes.Buffer
	if err := runtimestop.Stop(ctx, runtimestop.StopOptions{VaultPath: info.Path, Out: &out}); err != nil {
		// Stop reports each runtime's failure in its output, not its error.
		message := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out.String()), info.Path+": "))
		if message == "" {
			message = err.Error()
		}
		return StopResult{}, problem("runtime_error", "Rhizome did not stop: "+message)
	}
	return StopResult{Stopped: true}, nil
}
