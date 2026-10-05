package serve

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
)

// ControlHooks are the lane-owned pieces of the runtime control API
// (SPEC-0104). The serve command assembles a web.RuntimeControl from them;
// nil hooks leave their routes answering "not enabled". Lane B registers the
// index-job hooks and lane D the agent-operation hook, each from its own
// cmd/serve_*_hooks.go file, so the serve command itself needs no per-lane edit.
type ControlHooks struct {
	SubmitIndex func(ctx context.Context) (handle lane.Handle, joined bool, err error)
	LookupIndex func(id string) (lane.Handle, bool)
	AgentOp     func(ctx context.Context, name string, req appruntime.AgentOpRequest) (agentapi.CallOutcome, error)
}
