package runtime

import (
	"errors"
	"net/http"
	"strings"
	"time"
)

// Control API routes served by the runtime on its loopback listener. Health is
// open; everything else requires the manifest's control token.
const (
	HealthPath          = "/api/v1/runtime"
	ShutdownPath        = "/api/v1/runtime/shutdown"
	IndexJobsPath       = "/api/v1/runtime/index"
	IndexJobsPathPrefix = "/api/v1/runtime/index/"
	AgentOpsPathPrefix  = "/api/v1/agent/ops/"
)

// ExitCodeAlreadyRunning is returned by `rzm serve` when another runtime
// already owns the vault. It is distinct from the 0/1/2 outcome classes.
const ExitCodeAlreadyRunning = 3

// Timing defaults shared by clients and the runtime.
const (
	ProbeTimeout       = 2 * time.Second
	SpawnBudget        = 60 * time.Second
	SpawnLeaseStale    = 60 * time.Second
	StopGrace          = 10 * time.Second
	DefaultIdleTimeout = time.Hour
)

// LaneState summarizes the indexing lane for health output.
type LaneState struct {
	Busy    bool   `json:"busy"`
	JobID   string `json:"jobId,omitempty"`
	JobKind string `json:"jobKind,omitempty"`
	Queued  int    `json:"queued"`
	// Held names why background scheduling is paused (an external priority
	// request), or is empty.
	Held      string `json:"held,omitempty"`
	LastError string `json:"lastError,omitempty"`
}

// Health is the open, token-free liveness answer at HealthPath.
type Health struct {
	InstanceID  string    `json:"instanceId"`
	RunID       string    `json:"runId"`
	PID         int       `json:"pid"`
	VaultPath   string    `json:"vaultPath"`
	Mode        Mode      `json:"mode"`
	Version     string    `json:"version"`
	BuildID     string    `json:"buildId"`
	Ready       bool      `json:"ready"`
	Lane        LaneState `json:"lane"`
	IdleSeconds float64   `json:"idleSeconds"`
	StartedAt   time.Time `json:"startedAt"`
}

// An explicit index job takes no options: requests coalesce into one job, so
// per-request flags could not be honored. The job always emits progress, log
// lines, and a timings summary; the client decides what to render. Rebuild is
// rejected by the runtime: rebuilds run in-process with the runtime stopped.

// IndexJobResponse identifies the job a request joined or created.
type IndexJobResponse struct {
	JobID  string `json:"jobId"`
	Joined bool   `json:"joined"`
}

// AgentOpRequest carries one catalog operation call from a code-mode host.
type AgentOpRequest struct {
	Input     map[string]any `json:"input"`
	ReadWrite bool           `json:"readWrite"`
	SessionID string         `json:"sessionId,omitempty"`
}

// Error classes clients branch on.
var (
	ErrNoRuntime           = errors.New("no live vault runtime")
	ErrRuntimeUnresponsive = errors.New("vault runtime is alive but not responding")
	ErrAttachedMismatch    = errors.New("attached vault runtime runs a different build")
	ErrAutostartDisabled   = errors.New("vault runtime auto-start is disabled")
	ErrUnauthorized        = errors.New("control token rejected")
)

// BearerToken extracts the control token from a request, or "" when absent.
func BearerToken(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	const prefix = "Bearer "
	if !strings.HasPrefix(value, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(value, prefix))
}

// SetBearerToken authorizes an outgoing control request.
func SetBearerToken(r *http.Request, token string) {
	r.Header.Set("Authorization", "Bearer "+token)
}
