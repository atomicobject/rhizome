package serve

import (
	"context"
	"log/slog"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/app/web"
	"github.com/atomicobject/rhizome/pkg/diagnostics"
)

// ControlDeps are the runtime-owned pieces of the control API. The lane-owned
// pieces arrive separately through ControlHooks.
type ControlDeps struct {
	Context context.Context
	// Manifest returns the runtime's current published identity. Health is
	// derived from it so the answer can never disagree with runtime.json.
	Manifest func() appruntime.InstanceManifest
	Live     *bootstrap.LiveRuntime
	Idle     *IdleTracker
	// Shutdown stops the runtime gracefully; reason is logged.
	Shutdown func(reason string)
	Hooks    ControlHooks
}

// BuildRuntimeControl assembles the web layer's control surface. It must be
// installed in web.Config before the server starts listening: the control
// routes are registered at construction and health is what makes a published
// manifest trustworthy (SPEC-0104 US1).
func BuildRuntimeControl(deps ControlDeps) *web.RuntimeControl {
	manifest := deps.Manifest()
	return &web.RuntimeControl{
		Token: manifest.ControlToken,
		Health: func() appruntime.Health {
			current := deps.Manifest()
			return appruntime.Health{
				InstanceID:  current.InstanceID,
				RunID:       current.RunID,
				PID:         current.PID,
				VaultPath:   current.VaultPath,
				Mode:        current.Mode,
				Version:     current.Version,
				BuildID:     current.BuildID,
				Ready:       current.Ready,
				Lane:        laneStateOf(deps.Live),
				IdleSeconds: deps.Idle.IdleFor().Seconds(),
				StartedAt:   current.StartedAt,
			}
		},
		Shutdown: func(reason string) {
			if reason == "" {
				reason = "control request"
			}
			if recorder := diagnostics.FromContext(deps.Context); recorder != nil {
				recorder.Event(deps.Context, slog.LevelInfo, "runtime", "shutdown.requested", "", slog.String("reason_code", "control_request"))
			}
			deps.Shutdown(reason)
		},
		Touch:       deps.Idle.Touch,
		Hold:        deps.Idle.Hold,
		SubmitIndex: deps.Hooks.SubmitIndex,
		LookupIndex: deps.Hooks.LookupIndex,
		AgentOp:     deps.Hooks.AgentOp,
	}
}

// laneStateOf projects the indexing lane into the health payload. A runtime
// whose lane is not installed yet reports an idle lane rather than failing.
func laneStateOf(rt *bootstrap.LiveRuntime) appruntime.LaneState {
	l := rt.Lane()
	if l == nil {
		return appruntime.LaneState{}
	}
	status := l.Status()
	state := appruntime.LaneState{
		Busy:    status.Busy,
		JobID:   status.JobID,
		JobKind: string(status.JobKind),
		Queued:  status.Queued,
		Held:    status.Held,
	}
	if status.LastError != nil {
		state.LastError = status.LastError.Error()
	}
	return state
}
