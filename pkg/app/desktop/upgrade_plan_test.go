package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/repoexec"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestUpgradePlanChecksPinEligibilityBeforeExecution(t *testing.T) {
	for _, tt := range []struct {
		name, pin, manager, dev, want string
		source, trusted               bool
	}{
		{name: "unmanaged", want: "pin_unsupported"},
		{name: "external", manager: "external", want: "pin_unsupported"},
		{name: "development", dev: "build", want: "pin_unsupported"},
		{name: "source", pin: "v1.0.0", source: true, want: "pin_unsupported"},
		{name: "untrusted", pin: "v1.0.0", want: "trust_required"},
		{name: "current", pin: "v1.2.0", trusted: true, want: "pin_unsupported"},
		{name: "newer", pin: "v2.0.0", trusted: true, want: "pin_unsupported"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := testService(t)
			s.available = func() string { return "v1.2.0" }
			folder := config(t, repoconfig.LocalRhizomeConfig{Version: tt.pin, BinaryManager: tt.manager, DevBinaryDir: tt.dev})
			if tt.source {
				require.NoError(t, os.WriteFile(filepath.Join(folder, "go.mod"), []byte("module github.com/atomicobject/rhizome\n"), 0o644))
			}
			if tt.trusted {
				_, err := s.Trust(folder)
				require.NoError(t, err)
			}
			previous := desktopExecutable
			desktopExecutable = func() (string, error) {
				t.Fatal("must refuse before choosing the plan executable")
				return "", nil
			}
			t.Cleanup(func() { desktopExecutable = previous })
			response := s.Handle(context.Background(), Request{Protocol: Protocol, Operation: "upgrade-plan", Folder: folder})
			require.NotNil(t, response.Error)
			require.Equal(t, tt.want, response.Error.Code)
		})
	}
}

func TestUpgradePlanUsesAppExecutableAndPreservesDocument(t *testing.T) {
	const doc = `{"schema":1,"configured":true,"writes":{"summary":["Update skills"],"files":[".agents/skills/rhizome/SKILL.md"],"kept":["edited.md"]},"future":{"value":42}}`
	s := testService(t)
	s.available = func() string { return "v1.2.0" }
	req, args := setupFixture(t, `echo '`+doc+`'`)
	require.NoError(t, repoconfig.SaveLocalConfig(req.Folder, repoconfig.LocalConfig{Rhizome: repoconfig.LocalRhizomeConfig{Version: "v1.0.0"}}))
	_, err := s.Trust(req.Folder)
	require.NoError(t, err)
	onPath(s, req.GlobalExecutable)
	request, err := json.Marshal(Request{Protocol: Protocol, Operation: "upgrade-plan", Folder: req.Folder})
	require.NoError(t, err)
	var output bytes.Buffer
	require.NoError(t, Serve(context.Background(), s, bytes.NewReader(request), &output))
	require.JSONEq(t, `{"protocol":1,"result":`+doc+`}`, output.String())
	require.Equal(t, []string{"init", "--path", req.Folder, "--check", "--json"}, argv(t, args))
	_, cfg, err := repoconfig.FindLocalConfig(req.Folder)
	require.NoError(t, err)
	require.Equal(t, "v1.0.0", cfg.Rhizome.Version)
}

func TestUpgradePlanFailures(t *testing.T) {
	for _, tt := range []struct {
		name, body, want string
		executableError  bool
		timeout          bool
	}{
		{name: "executable", executableError: true, want: "no Rhizome executable found"},
		{name: "JSON error", body: `echo '{"error":{"code":"plan_failed","message":"Cannot plan files"}}'; exit 1`, want: "Cannot plan files"},
		{name: "process", body: `echo 'plan crashed' >&2; exit 2`, want: "plan crashed"},
		{name: "timeout", body: "sleep 5", timeout: true, want: "took too long"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := testService(t)
			s.available = func() string { return "v1.2.0" }
			req, _ := setupFixture(t, tt.body)
			require.NoError(t, repoconfig.SaveLocalConfig(req.Folder, repoconfig.LocalConfig{Rhizome: repoconfig.LocalRhizomeConfig{Version: "v1.0.0"}}))
			_, err := s.Trust(req.Folder)
			require.NoError(t, err)
			if !tt.executableError {
				onPath(s, req.GlobalExecutable)
			} else {
				broken := filepath.Join(t.TempDir(), "broken-rzm")
				require.NoError(t, os.Symlink(broken, broken))
				previous := desktopExecutable
				desktopExecutable = func() (string, error) { return broken, nil }
				t.Cleanup(func() { desktopExecutable = previous })
			}
			ctx := context.Background()
			if tt.timeout {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()
			}
			_, err = s.UpgradePlan(ctx, req.Folder)
			code(t, err, "upgrade_plan_failed")
			require.ErrorContains(t, err, tt.want)
		})
	}
}

// onPath makes executable the global Rhizome an unbundled bridge finds, the
// one that reports the offered version.
func onPath(s *Service, executable string) {
	s.env.LookupEnv = func(key string) (string, bool) {
		if key == "PATH" {
			return filepath.Dir(executable), true
		}
		return "", key == repoexec.DevAppEnv
	}
}
