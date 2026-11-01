package desktop

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/app/provideraccess"
	"github.com/stretchr/testify/require"
)

func TestCredentialStatusReportsTeamSessionWithoutTokens(t *testing.T) {
	env := testkit.Env(t)
	env.CLIConfigFile = filepath.Join(t.TempDir(), "config.yml")
	expires := time.Date(2026, 11, 30, 12, 0, 0, 0, time.UTC)
	service := &Service{stateDir: t.TempDir(), env: env, teamStatus: func() provideraccess.TeamStatus {
		return provideraccess.TeamStatus{SignedIn: true, Email: "person@example.test", Origin: "https://team.example.test", ExpiresAt: &expires}
	}}
	status, err := service.CredentialStatus()
	require.NoError(t, err)
	data, err := json.Marshal(status)
	require.NoError(t, err)
	require.Contains(t, string(data), `"team":{"signedIn":true,"email":"person@example.test","origin":"https://team.example.test","expiresAt":"2026-11-30T12:00:00Z"}`)

	signedOut := &Service{stateDir: t.TempDir(), env: env}
	status, err = signedOut.CredentialStatus()
	require.NoError(t, err)
	data, err = json.Marshal(status)
	require.NoError(t, err)
	require.Contains(t, string(data), `"team":{"signedIn":false}`)
}
