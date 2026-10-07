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

	"github.com/atomicobject/rhizome/pkg/app/repoexec"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func setupFixture(t *testing.T, body string) (Request, string) {
	t.Helper()
	folder := t.TempDir()
	canonical, err := filepath.EvalSymlinks(folder)
	require.NoError(t, err)
	args := filepath.Join(canonical, "args")
	target := script(t, filepath.Join(t.TempDir(), "rzm"), `
if [ "$1" = init ] && [ "$2" = --help ]; then echo '--search-key-stdin'; exit; fi
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
					want = append(want, "--addons", "none", "--agents", "none")
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
			require.Equal(t, []string{"init", "--path", req.Folder, "--json", "--workflow", "knowledge-base", "--addons", "none", "--agents", "none", "--search", "voyage", "--search-key-stdin"}, argv(t, args))
			stdin, err := os.ReadFile(filepath.Join(req.Folder, "stdin"))
			require.NoError(t, err)
			require.Equal(t, key+"\n", string(stdin))
			trusted, err := s.trust.Trusted(req.Folder)
			require.NoError(t, err)
			require.Equal(t, outcome.configured, trusted)
		})
	}
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
		require.NoError(t, obsidian.SaveLocalConfig(req.Folder, obsidian.LocalConfig{}))
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
			require.NoError(t, obsidian.SaveLocalConfig(req.Folder, obsidian.LocalConfig{Rhizome: obsidian.LocalRhizomeConfig{BinaryManager: "external"}}))
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
	require.NoError(t, obsidian.SaveLocalConfig(req.Folder, obsidian.LocalConfig{Rhizome: obsidian.LocalRhizomeConfig{DevBinaryDir: "dev"}}))
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
