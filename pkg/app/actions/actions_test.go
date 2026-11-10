package actions

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/repositorytrust"
	"github.com/atomicobject/rhizome/pkg/vault/actiondef"
)

// repo writes files under a temporary repository root.
func repo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		mode := os.FileMode(0o644)
		if strings.HasSuffix(rel, ".sh") {
			mode = 0o755
		}
		require.NoError(t, os.WriteFile(abs, []byte(content), mode))
	}
	return root
}

func trusted(context.Context) string { return "" }

// shAction declares demo/<name> running sh -c script.
func shAction(script string, extra string) string {
	quoted, _ := json.Marshal(script)
	return "description: test\n" + extra + "run:\n  command: [sh, -c, " + string(quoted) + "]\n"
}

func skipWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
}

func TestCommandEnvelopeForEachExit(t *testing.T) {
	skipWindows(t)
	for _, tc := range []struct {
		name, script string
		want         Envelope
	}{
		{"ok", `cat >/dev/null; echo '{"n": 1}'`, Envelope{Status: StatusOK, Result: json.RawMessage(`{"n": 1}`)}},
		{"refused", `echo first >&2; echo "mining in progress" >&2; echo >&2; exit 2`, Envelope{Status: StatusRefused, Reason: "mining in progress"}},
		{"failed", `echo boom >&2; exit 1`, Envelope{Status: StatusError, Reason: "exit status 1", StderrTail: "boom\n"}},
		{"not json", `echo hello`, Envelope{Status: StatusError, Reason: `stdout is not one JSON value; got "hello\n"`}},
		{"two values", `echo '1 2'`, Envelope{Status: StatusError, Reason: `stdout is not one JSON value; got "1 2\n"`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := repo(t, map[string]string{".rhizome/modules/demo/actions/go.yaml": shAction(tc.script, "")})
			got, err := (&Runner{RepoRoot: root, Trust: trusted}).Run(context.Background(), "demo/go", nil)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestCommandGetsInputOnStdinInRepositoryRoot(t *testing.T) {
	skipWindows(t)
	root := repo(t, map[string]string{
		".rhizome/modules/demo/actions/echo.yaml": "description: echo\ninputs:\n  items: {kind: list}\n" +
			"run:\n  command: [\"{module}/scripts/echo.sh\"]\n",
		".rhizome/modules/demo/scripts/echo.sh": "#!/bin/sh\nprintf '{\"cwd\": \"%s\", \"stdin\": %s}' \"$(pwd -P)\" \"$(cat)\"\n",
	})
	got, err := (&Runner{RepoRoot: root, Trust: trusted}).Run(context.Background(), "demo/echo", map[string]any{"items": []any{"a", "b"}})
	require.NoError(t, err)
	require.Equal(t, StatusOK, got.Status, got.Reason)
	resolved, _ := filepath.EvalSymlinks(root)
	require.JSONEq(t, `{"cwd": `+strconv.Quote(resolved)+`, "stdin": {"items": ["a", "b"]}}`, string(got.Result))
}

func TestInvalidInputStartsNoProcess(t *testing.T) {
	skipWindows(t)
	root := repo(t, map[string]string{".rhizome/modules/demo/actions/touch.yaml": shAction("touch ran; echo null",
		"inputs:\n  reason: {kind: string, required: true, enum: [wrong]}\n  count: {kind: int}\n")})
	runner := &Runner{RepoRoot: root, Trust: trusted}
	for _, input := range []map[string]any{
		{},
		{"reason": "other"},
		{"reason": "wrong", "extra": "x"},
		{"reason": "wrong", "count": 1.5},
	} {
		_, err := runner.Run(context.Background(), "demo/touch", input)
		require.ErrorIs(t, err, actiondef.ErrInvalidInput, "%v", input)
	}
	_, err := runner.Run(context.Background(), "demo/missing", nil)
	require.ErrorIs(t, err, ErrUnknownAction)
	require.NoFileExists(t, filepath.Join(root, "ran"))

	got, err := runner.Run(context.Background(), "demo/touch", map[string]any{"reason": "wrong", "count": float64(2)})
	require.NoError(t, err)
	require.Equal(t, StatusOK, got.Status, got.Reason)
	require.FileExists(t, filepath.Join(root, "ran"))
}

func TestUntrustedRepositoryIsRefused(t *testing.T) {
	skipWindows(t)
	root := repo(t, map[string]string{".rhizome/modules/demo/actions/touch.yaml": shAction("touch ran; echo null", "")})
	runner := &Runner{RepoRoot: root, Trust: func(context.Context) string {
		return repositorytrust.DenialReason(root, "actions", func(string) (bool, error) { return false, nil })
	}}
	got, err := runner.Run(context.Background(), "demo/touch", nil)
	require.NoError(t, err)
	require.Equal(t, StatusRefused, got.Status)
	require.Contains(t, got.Reason, "rzm trust")
	require.NoFileExists(t, filepath.Join(root, "ran"))
}

func TestActionTrustIsTheRecordedCheckout(t *testing.T) {
	store := repositorytrust.Store{Dir: filepath.Join(t.TempDir(), "trust")}
	t.Cleanup(func() { trustStore = repositorytrust.DefaultStore })
	trustStore = func() (repositorytrust.Store, error) { return store, nil }
	base := t.TempDir()
	main, other := filepath.Join(base, "main"), filepath.Join(base, "other")
	for _, dir := range []string{main, other} {
		output, err := exec.Command("git", "init", "-q", dir).CombinedOutput()
		require.NoError(t, err, "%s", output)
	}
	subfolder := filepath.Join(main, "untracked")
	forged := filepath.Join(base, "forged")
	// A vault whose .rhizome/config.yml lives below its repository root.
	configured := filepath.Join(other, "docs")
	for _, dir := range []string{subfolder, forged, configured} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	// A one-line .git file naming the trusted repository, which does not link back.
	require.NoError(t, os.WriteFile(filepath.Join(forged, ".git"), []byte("gitdir: "+filepath.Join(main, ".git")+"\n"), 0o644))
	for _, dir := range []string{main, configured} {
		_, err := store.Trust(dir)
		require.NoError(t, err)
	}
	for _, tc := range []struct {
		dir     string
		trusted bool
	}{{main, true}, {configured, true}, {subfolder, false}, {forged, false}} {
		reason := New(tc.dir).Trust(context.Background())
		if tc.trusted {
			require.Empty(t, reason, tc.dir)
		} else {
			require.Contains(t, reason, "rzm trust", tc.dir)
		}
	}
}

// fakeRzm stands in for `rzm agent code execute`: it records its input and
// stdin, then prints the given code-mode output.
func fakeRzm(t *testing.T, output string, exit int) (string, string) {
	t.Helper()
	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + record + ".args\nwhile [ $# -gt 0 ]; do [ \"$1\" = --input ] && printf '%s' \"$2\" > " + record + ".input; shift; done\n" +
		"cat > " + record + ".code\nprintf '%s' " + strconv.Quote(output) + "\nexit " + strconv.Itoa(exit) + "\n"
	path := filepath.Join(dir, "rzm")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
	return path, record
}

func TestScriptEnvelopeForEachResult(t *testing.T) {
	skipWindows(t)
	for _, tc := range []struct {
		name, output string
		exit         int
		want         Envelope
	}{
		{"ok", `{"ok":true,"result":{"done":true}}`, 0, Envelope{Status: StatusOK, Result: json.RawMessage(`{"done":true}`)}},
		{"refused", `{"ok":true,"result":{"refused":"busy"}}`, 0, Envelope{Status: StatusRefused, Reason: "busy"}},
		{"a null refusal is a result", `{"ok":true,"result":{"refused":null}}`, 0, Envelope{Status: StatusOK, Result: json.RawMessage(`{"refused":null}`)}},
		{"refused with more keys is a result", `{"ok":true,"result":{"refused":"busy","n":1}}`, 0, Envelope{Status: StatusOK, Result: json.RawMessage(`{"refused":"busy","n":1}`)}},
		{"thrown", `{"ok":false,"error":{"message":"bad thing"}}`, 1, Envelope{Status: StatusError, Reason: "bad thing"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := repo(t, map[string]string{
				".rhizome/modules/demo/actions/go.yaml": "description: go\ninputs:\n  path: {kind: string}\nrun:\n  script: scripts/go.js\n",
				".rhizome/modules/demo/scripts/go.js":   "return 1;\n",
			})
			rzm, record := fakeRzm(t, tc.output, tc.exit)
			got, err := (&Runner{RepoRoot: root, Rzm: rzm, Vault: "work", Trust: trusted}).Run(context.Background(), "demo/go", map[string]any{"path": "a.md"})
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			code, err := os.ReadFile(record + ".code")
			require.NoError(t, err)
			require.Equal(t, "return 1;\n", string(code))
			args, err := os.ReadFile(record + ".args")
			require.NoError(t, err)
			require.Contains(t, strings.Split(string(args), "\n"), "--read-write", "a trusted action script may write")
			require.Contains(t, string(args), "--vault\nwork\n", "the script binds to the runner's vault, not the default one")
			input, err := os.ReadFile(record + ".input")
			require.NoError(t, err)
			require.JSONEq(t, `{"action":"demo/go","input":{"path":"a.md"},"moduleDir":`+strconv.Quote(filepath.Join(root, ".rhizome/modules/demo"))+`,"repoRoot":`+strconv.Quote(root)+`}`, string(input))
		})
	}
}

func TestSelectedExecutableReachesCommandActions(t *testing.T) {
	skipWindows(t)
	root := repo(t, map[string]string{".rhizome/modules/demo/actions/check.yaml": shAction(`printf '{"rzm":"%s","skip":"%s","delegated":"%s"}' "$RZM" "$RZM_SKIP_REPO_DELEGATE" "$RZM_REPO_DELEGATED"`, "")})
	r := &Runner{RepoRoot: root, Trust: trusted, Rzm: "/absolute/selected/rzm"}
	result, err := r.Run(context.Background(), "demo/check", nil)
	require.NoError(t, err)
	require.Equal(t, StatusOK, result.Status, result.Reason)
	require.JSONEq(t, `{"rzm":"/absolute/selected/rzm","skip":"1","delegated":"1"}`, string(result.Result))
}
