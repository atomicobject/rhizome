package web

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/modulereadiness"
	"github.com/stretchr/testify/require"
)

type readinessAPI struct{ runs int }

func (r *readinessAPI) Snapshot() []modulereadiness.Snapshot {
	return []modulereadiness.Snapshot{{Module: "demo", State: modulereadiness.Blocked, Reason: "artifact and Artifact conflict"}}
}
func (r *readinessAPI) Recover(_ string, _ modulereadiness.Request) error { r.runs++; return nil }
func TestStarterReadinessIsUngatedAndGETNeverRunsSetup(t *testing.T) {
	owner := &readinessAPI{}
	s := &Server{cfg: Config{ModuleReadiness: owner}}
	w := httptest.NewRecorder()
	s.handleStarterReadiness(w, httptest.NewRequest("GET", "/api/v1/starter-readiness", nil))
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), "artifact and Artifact conflict")
	require.Zero(t, owner.runs)
	w = httptest.NewRecorder()
	s.handleStarterReadiness(w, httptest.NewRequest("POST", "/api/v1/starter-readiness/demo", strings.NewReader(`{"kind":"retry"}`)))
	require.Equal(t, 202, w.Code)
	require.Equal(t, 1, owner.runs)
}
