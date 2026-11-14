package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/repoexec"
	appupdate "github.com/atomicobject/rhizome/pkg/app/update"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func setupFixture(t *testing.T, body string) (Request, string) {
	t.Helper()
	folder := t.TempDir()
	canonical, err := filepath.EvalSymlinks(folder)
	require.NoError(t, err)
	args := filepath.Join(canonical, "args")
	target := script(t, filepath.Join(t.TempDir(), "rzm"), `
if [ "$1" = init ] && [ "$2" = --help ]; then echo '--search-key-stdin'; echo 'repository settings once set up'; exit; fi
if [ "$1" = index ] && [ "$2" = scope ] && [ "$3" = --help ]; then echo '--remove-rule'; exit; fi
[ "$RZM_REPO_DELEGATED" = 1 ] && [ "$RZM_SKIP_REPO_DELEGATE" = 1 ] || exit 7
printf '%s\n' "$@" > "$PWD/args"
`+body)
	return Request{Protocol: Protocol, Folder: canonical, GlobalExecutable: target}, args
}

func argv(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func TestSetupReportPreservesJSONAndChoiceFlags(t *testing.T) {
	// A setup plan names its pin as a string, unlike a setup result.
	const doc = `{"schema":1,"configured":false,"pin":"v0.50.5","unknownFutureField":{"value":42}}`
	for _, choices := range []*SetupChoices{nil, {}, {
		Workflow: "agentic-engineering", Addons: []string{"action-items"}, Agents: []string{"claude", "codex"}, Search: "off",
		Skip: []string{"build", "dist"}, KeepIndexed: []string{"testdata"}, IncludeIgnored: []string{"nested"},
	}} {
		t.Run(fmt.Sprintf("%+v", choices), func(t *testing.T) {
			s := testService(t)
			req, args := setupFixture(t, `echo '`+doc+`'`)
			req.Operation, req.Setup = "setup-report", choices
			response := s.Handle(context.Background(), req)
			require.Nil(t, response.Error)
			require.JSONEq(t, doc, string(response.Result.(json.RawMessage)))
			want := []string{"init", "--path", req.Folder, "--check", "--json"}
			if choices != nil {
				if choices.Workflow == "" {
					want = append(want, "--addons", "none", "--agents", "shared")
				} else {
					want = append(want, "--workflow", "agentic-engineering", "--addons", "action-items", "--agents", "claude,codex", "--search", "off", "--skip", "build", "--skip", "dist", "--keep-indexed", "testdata", "--include-ignored", "nested")
				}
			}
			require.Equal(t, want, argv(t, args))
			require.NoDirExists(t, s.trust.Dir)
		})
	}
}

func TestInitializeKeyIsOnlyOnStdinAndTrustFollowsConfiguration(t *testing.T) {
	const key = "synthetic-search-secret"
	for _, outcome := range []struct {
		name, body string
		configured bool
		wantCode   string
	}{
		{"success", `echo '{"schema":1,"summary":["ready"],"savedKey":"VOYAGE_API_KEY","pin":{}}'`, true, ""},
		{"pin failure", `echo '{"schema":1,"summary":["ready"],"pin":{"error":"download failed"}}'; exit 1`, true, ""},
		{"partial error", `echo '{"schema":1,"error":{"code":"write_failed","message":"later write failed"},"savedKey":"VOYAGE_API_KEY"}'; exit 1`, true, "setup_partial"},
		{"no writes", `echo '{"schema":1,"error":{"code":"write_failed","message":"no writes"}}'; exit 1`, false, "setup_failed"},
	} {
		t.Run(outcome.name, func(t *testing.T) {
			s := testService(t)
			body := `cat > "$PWD/stdin"` + "\n"
			if outcome.configured {
				body += `mkdir -p .rhizome; printf 'rhizome:\n  version: v1.2.3\n' > .rhizome/config.yml` + "\n"
			}
			req, args := setupFixture(t, body+outcome.body)
			req.Operation, req.Setup, req.Key = "initialize", &SetupChoices{Workflow: "knowledge-base", Search: "voyage"}, key
			response := s.Handle(context.Background(), req)
			if outcome.wantCode == "" {
				require.Nil(t, response.Error)
				result := response.Result.(InitializeResult)
				require.True(t, result.Folder.Configured)
				require.True(t, result.Folder.Trusted)
				require.False(t, result.Folder.TrustRequired)
				if outcome.name == "pin failure" {
					require.JSONEq(t, `{"schema":1,"summary":["ready"],"pin":{"error":"download failed"}}`, string(result.Result))
				}
			} else {
				require.Equal(t, outcome.wantCode, response.Error.Code)
				if outcome.name == "partial error" {
					require.Equal(t, "later write failed The key stays saved in ~/.config/rhizome/config.yml. Rhizome wrote part of the setup; open the workspace to continue.", response.Error.Message)
				}
			}
			data, err := json.Marshal(response)
			require.NoError(t, err)
			require.NotContains(t, string(data), key)
			require.Equal(t, []string{"init", "--path", req.Folder, "--json", "--workflow", "knowledge-base", "--addons", "none", "--agents", "shared", "--search", "voyage", "--search-key-stdin"}, argv(t, args))
			stdin, err := os.ReadFile(filepath.Join(req.Folder, "stdin"))
			require.NoError(t, err)
			require.Equal(t, key+"\n", string(stdin))
			trusted, err := s.trust.Trusted(req.Folder)
			require.NoError(t, err)
			require.Equal(t, outcome.configured, trusted)
		})
	}
}

func TestSetupReportsASlowProbeAsATimeout(t *testing.T) {
	s := testService(t)
	req, _ := setupFixture(t, "exit 9")
	req.GlobalExecutable = script(t, req.GlobalExecutable, "sleep 5")
	req.Operation = "setup-report"
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err := s.SetupReport(ctx, req)
	var failure *Problem
	require.ErrorAs(t, err, &failure)
	require.Equal(t, "runtime_error", failure.Code)
	require.Contains(t, failure.Message, "took too long")
}

func TestSetupChecksBeforeExecution(t *testing.T) {
	t.Run("unsupported", func(t *testing.T) {
		s := testService(t)
		req, args := setupFixture(t, "exit 9")
		req.GlobalExecutable = script(t, req.GlobalExecutable, "echo 'old init --help'")
		_, err := s.SetupReport(context.Background(), req)
		code(t, err, "setup_unsupported")
		require.ErrorContains(t, err, "rzm init")
		require.NoFileExists(t, args)
	})
	t.Run("configured", func(t *testing.T) {
		s := testService(t)
		req, args := setupFixture(t, "exit 9")
		require.NoError(t, repoconfig.SaveLocalConfig(req.Folder, repoconfig.LocalConfig{}))
		_, err := s.SetupReport(context.Background(), req)
		code(t, err, "invalid_request")
		req.Setup = &SetupChoices{}
		_, err = s.Initialize(context.Background(), req)
		code(t, err, "invalid_request")
		require.NoFileExists(t, args)
	})
	t.Run("trust before probe", func(t *testing.T) {
		s := testService(t)
		req, args := setupFixture(t, "exit 9")
		require.NoError(t, os.WriteFile(filepath.Join(req.Folder, "go.mod"), []byte("module github.com/atomicobject/rhizome\n"), 0o644))
		for _, dir := range []string{"cmd", "pkg", "web"} {
			require.NoError(t, os.Mkdir(filepath.Join(req.Folder, dir), 0o755))
		}
		script(t, filepath.Join(req.Folder, "bin", runtime.GOOS, repoexec.ExecutableName()), `touch '`+args+`'`)
		info, err := s.Inspect(req.Folder)
		require.NoError(t, err)
		require.True(t, info.TrustRequired)
		_, err = s.SetupReport(context.Background(), req)
		code(t, err, "trust_required")
		req.Setup = &SetupChoices{}
		_, err = s.Initialize(context.Background(), req)
		code(t, err, "trust_required")
		require.NoFileExists(t, args)
	})
	t.Run("report rejects key and initialize requires choices", func(t *testing.T) {
		s := testService(t)
		req, args := setupFixture(t, "exit 9")
		req.Operation, req.Key = "setup-report", "synthetic-key"
		response := s.Handle(context.Background(), req)
		require.Equal(t, "invalid_request", response.Error.Code)
		_, err := s.SetupReport(context.Background(), req)
		code(t, err, "invalid_request")
		req.Key = ""
		_, err = s.Initialize(context.Background(), req)
		code(t, err, "invalid_request")
		require.NoFileExists(t, args)
	})
}

func TestScopeUsesSelectedExecutableAndEdits(t *testing.T) {
	const doc = `{"schema":1,"rules":[{"pattern":"/build/"}],"builtin":true}`
	for _, operation := range []string{"scope", "scope-edit"} {
		t.Run(operation, func(t *testing.T) {
			s := testService(t)
			req, args := setupFixture(t, `echo '`+doc+`'`)
			require.NoError(t, repoconfig.SaveLocalConfig(req.Folder, repoconfig.LocalConfig{Rhizome: repoconfig.LocalRhizomeConfig{BinaryManager: "external"}}))
			req.Operation, req.Executable = operation, req.GlobalExecutable
			req.GlobalExecutable = "/must-not-select-global"
			req.Edits = &ScopeEdits{Skip: []string{"build", "dist"}, RemoveRules: []string{"/data/"}, IncludeIgnored: []string{"nested"}}
			response := s.Handle(context.Background(), req)
			require.Nil(t, response.Error)
			require.JSONEq(t, doc, string(response.Result.(json.RawMessage)))
			want := []string{"index", "scope", "--json"}
			if operation == "scope-edit" {
				want = append(want, "--skip", "build", "--skip", "dist", "--remove-rule", "/data/", "--include-ignored", "nested")
			}
			require.Equal(t, want, argv(t, args))
		})
	}
}

func TestScopeChecksAndErrorMapping(t *testing.T) {
	s := testService(t)
	req, args := setupFixture(t, `echo '{"schema":1,"error":{"code":"invalid_rule","message":"No such rule."}}'; exit 1`)
	req.Operation = "scope"
	_, err := s.Scope(context.Background(), req)
	code(t, err, "not_configured")
	require.NoFileExists(t, args)
	require.NoError(t, repoconfig.SaveLocalConfig(req.Folder, repoconfig.LocalConfig{Rhizome: repoconfig.LocalRhizomeConfig{DevBinaryDir: "dev"}}))
	dev := filepath.Join(req.Folder, "dev", runtime.GOOS, repoexec.ExecutableName())
	body, err := os.ReadFile(req.GlobalExecutable)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(dev), 0o755))
	require.NoError(t, os.WriteFile(dev, body, 0o755))
	_, err = s.Scope(context.Background(), req)
	code(t, err, "trust_required")
	require.NoFileExists(t, args)
	_, err = s.Trust(req.Folder)
	require.NoError(t, err)
	_, err = s.Scope(context.Background(), req)
	code(t, err, "scope_failed")
	require.EqualError(t, err, "No such rule.")
	script(t, dev, "echo '--remove-rule'; exit 1")
	_, err = s.Scope(context.Background(), req)
	code(t, err, "setup_unsupported")
}

func TestSettingsRunTheSelectedExecutableWithRerunOptions(t *testing.T) {
	const doc = `{"schema":1,"configured":true,"pin":"v0.51.0","writes":{"summary":[],"files":[]}}`
	for _, run := range []struct {
		operation string
		choices   *SettingsChoices
		want      []string
	}{
		{"settings-report", nil, []string{"--check"}},
		{"settings-apply", &SettingsChoices{
			DecisionSnapshot: "reviewed-snapshot",
			Take:             []string{".rhizome/modules/agentic-engineering/ontology/spec-driven.graphql", "path with spaces.md"},
			Keep:             []string{".agents/skills/rhizome/SKILL.md"},
		}, []string{"--decision-snapshot=reviewed-snapshot", "--take", ".rhizome/modules/agentic-engineering/ontology/spec-driven.graphql", "--take", "path with spaces.md", "--keep", ".agents/skills/rhizome/SKILL.md"}},
		{"settings-report", &SettingsChoices{}, []string{"--check"}},
		{"settings-apply", &SettingsChoices{DecisionSnapshot: "empty-decisions"}, []string{"--decision-snapshot=empty-decisions"}},
		{"settings-report", &SettingsChoices{Agents: []string{}}, []string{"--check", "--agents", "shared"}},
		{"settings-apply", &SettingsChoices{
			Workflow: "none", Agents: []string{"claude", "cursor"}, Search: "off",
			AddAddons: []string{"compounding"}, RemoveAddons: []string{"action-items", "ao-kb"},
		}, []string{"--workflow", "none", "--agents", "claude,cursor", "--search", "off", "--add-addon", "compounding", "--remove-addon", "action-items,ao-kb"}},
	} {
		t.Run(fmt.Sprintf("%s %+v", run.operation, run.choices), func(t *testing.T) {
			s := testService(t)
			req, args := setupFixture(t, `echo '`+doc+`'`)
			require.NoError(t, repoconfig.SaveLocalConfig(req.Folder, repoconfig.LocalConfig{Rhizome: repoconfig.LocalRhizomeConfig{BinaryManager: "external"}}))
			req.Operation, req.Settings = run.operation, run.choices
			req.Executable, req.GlobalExecutable = req.GlobalExecutable, "/must-not-select-global"
			response := s.Handle(context.Background(), req)
			require.Nil(t, response.Error)
			require.JSONEq(t, doc, string(response.Result.(json.RawMessage)))
			require.Equal(t, append([]string{"init", "--path", req.Folder, "--json"}, run.want...), argv(t, args))
		})
	}
}

func TestSettingsChecksBeforeExecution(t *testing.T) {
	s := testService(t)
	req, args := setupFixture(t, `echo '{"schema":1,"error":{"code":"setup_failed","message":"unknown add-on"}}'; exit 1`)
	req.Operation, req.Settings = "settings-apply", &SettingsChoices{}
	_, err := s.Settings(context.Background(), req)
	code(t, err, "not_configured")
	require.NoFileExists(t, args)

	require.NoError(t, repoconfig.SaveLocalConfig(req.Folder, repoconfig.LocalConfig{Rhizome: repoconfig.LocalRhizomeConfig{BinaryManager: "external"}}))
	for _, bad := range []Request{
		{Operation: "settings-apply"},
		{Operation: "settings-report", Setup: &SetupChoices{}},
		{Operation: "settings-report", Key: "synthetic-search-secret"},
	} {
		bad.Protocol, bad.Folder, bad.GlobalExecutable = Protocol, req.Folder, req.GlobalExecutable
		response := s.Handle(context.Background(), bad)
		require.NotNil(t, response.Error)
		require.Equal(t, "invalid_request", response.Error.Code)
	}
	require.NoFileExists(t, args)

	_, err = s.Settings(context.Background(), req)
	code(t, err, "settings_failed")
	require.EqualError(t, err, "unknown add-on")

	script(t, req.GlobalExecutable, "echo '--search-key-stdin'")
	_, err = s.Settings(context.Background(), req)
	code(t, err, "setup_unsupported")
}

func TestSetupFailuresNeverReturnKeyAndRejectInvalidJSON(t *testing.T) {
	const key = "synthetic-search-secret"
	for _, body := range []string{
		`echo '{"error":{"code":"failed","message":"synthetic-search-secret rejected"}}'; exit 1`,
		`echo '{"schema":1,"summary":["synthetic-search-secret"]}'`,
		`printf '%s\n' '{"schema":1,"summary":["synthetic\u002dsearch-secret"]}'`,
		`echo 'not JSON'; echo 'synthetic-search-secret rejected' >&2; exit 3`,
		`echo '{} {}'`, `echo '[]'`, `echo 'null'`,
		`echo '{"schema":1}'; echo 'failure detail' >&2; exit 2`,
	} {
		t.Run(body, func(t *testing.T) {
			s := testService(t)
			req, _ := setupFixture(t, body)
			req.Operation, req.Setup, req.Key = "initialize", &SetupChoices{}, key
			response := s.Handle(context.Background(), req)
			require.Equal(t, "setup_failed", response.Error.Code)
			data, err := json.Marshal(response)
			require.NoError(t, err)
			require.NotContains(t, string(data), key)
			if strings.Contains(body, "exit 3") {
				require.Contains(t, response.Error.Message, "exit status 3")
				require.Contains(t, response.Error.Message, "[redacted] rejected")
			}
		})
	}
}

func TestSetupProtocolRejectsUnknownNestedFields(t *testing.T) {
	for _, request := range []string{
		`{"protocol":1,"operation":"setup-report","setup":{"surprise":true}}`,
		`{"protocol":1,"operation":"scope-edit","edits":{"remove_rules":[]}}`,
	} {
		var output bytes.Buffer
		err := Serve(context.Background(), testService(t), strings.NewReader(request), &output)
		code(t, err, "invalid_request")
	}
}

func TestManagedWorktreeChoiceRunsSettingsAndScopeAfterTrust(t *testing.T) {
	for _, operation := range []string{"settings-report", "settings-apply", "scope", "scope-edit"} {
		t.Run(operation, func(t *testing.T) {
			s := testService(t)
			req, args := setupFixture(t, `echo '{"schema":1}'`)
			require.NoError(t, repoconfig.SaveLocalConfig(req.Folder, repoconfig.LocalConfig{Rhizome: repoconfig.LocalRhizomeConfig{Version: "v0.51.0"}}))
			req.Operation, req.Executable, req.GlobalExecutable = operation, req.GlobalExecutable, "/must-not-run"
			if operation == "settings-apply" {
				req.Settings = &SettingsChoices{}
			}
			if operation == "scope-edit" {
				req.Edits = &ScopeEdits{}
			}
			response := s.Handle(context.Background(), req)
			require.NotNil(t, response.Error)
			require.Equal(t, "trust_required", response.Error.Code)
			require.NoFileExists(t, args)
			_, err := s.Trust(req.Folder)
			require.NoError(t, err)
			s.ensurePinned = func(context.Context, appupdate.EnsureOptions) error {
				t.Fatal("an explicit choice must not install the pin")
				return nil
			}
			response = s.Handle(context.Background(), req)
			require.Nil(t, response.Error)
			require.FileExists(t, args)
			info, err := s.Repository(req.Folder, "")
			require.NoError(t, err)
			require.Equal(t, "v0.51.0", info.Worktrees[0].PinnedVersion)
		})
	}
}
