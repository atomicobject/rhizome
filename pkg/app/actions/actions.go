// Package actions runs module actions ([[module-actions|SPEC-0131]]): it
// finds a declaration, checks the input against it, checks repository trust,
// runs the command or code-mode script through procrun, and maps the outcome
// to one envelope. HTTP, the agent catalog, and the module command line all
// call Runner.Run.
package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/procrun"
	"github.com/atomicobject/rhizome/pkg/app/tasks"
	"github.com/atomicobject/rhizome/pkg/repositorytrust"
	"github.com/atomicobject/rhizome/pkg/vault/actiondef"
)

const (
	maxStdout  = 1 << 20
	stderrTail = 4 << 10

	StatusOK      = "ok"
	StatusRefused = "refused"
	StatusError   = "error"
)

// ErrUnknownAction marks an id no usable declaration has.
var ErrUnknownAction = errors.New("unknown action")

// Envelope is every run's outcome.
type Envelope struct {
	Status     string          `json:"status"`
	Result     json.RawMessage `json:"result,omitempty"`
	Reason     string          `json:"reason,omitempty"`
	StderrTail string          `json:"stderrTail,omitempty"`
}

// Runner runs one repository's actions.
type Runner struct {
	// RepoRoot is the root of the checkout serving the vault.
	RepoRoot string
	// Rzm runs code-mode scripts; empty means this executable.
	Rzm string
	// Vault names the vault code-mode scripts bind to, so a vault with no
	// .rhizome/config.yml in RepoRoot does not fall back to the default one.
	Vault string
	// Trust explains why the repository may not run actions, or returns "".
	Trust func(ctx context.Context) string
}

// New returns a Runner for repoRoot that checks trust the way scheduled
// tasks do, against the person's trust store.
func New(repoRoot string) *Runner {
	return &Runner{RepoRoot: repoRoot, Trust: func(ctx context.Context) string {
		store, err := repositorytrust.DefaultStore()
		if err != nil {
			return "repository trust could not be read: " + err.Error()
		}
		return tasks.Untrusted(ctx, repoRoot, "actions", store.Trusted)
	}}
}

// List returns the repository's usable actions, sorted by id. Declarations
// with problems are left out; `rzm validate actions` reports them.
func (r *Runner) List() ([]actiondef.Action, error) {
	list, _, err := actiondef.Load(r.RepoRoot)
	if list == nil {
		list = []actiondef.Action{}
	}
	return list, err
}

// Find returns the action with id.
func (r *Runner) Find(id string) (actiondef.Action, error) {
	list, err := r.List()
	for _, a := range list {
		if a.ID == id {
			return a, nil
		}
	}
	if err != nil {
		return actiondef.Action{}, err
	}
	return actiondef.Action{}, fmt.Errorf("%w %q", ErrUnknownAction, id)
}

// Run runs the action with id. An unknown id returns ErrUnknownAction and
// input that does not match the declaration returns
// actiondef.ErrInvalidInput; neither starts a process. Every run that
// starts returns an envelope and a nil error.
func (r *Runner) Run(ctx context.Context, id string, input map[string]any) (Envelope, error) {
	a, err := r.Find(id)
	if err != nil {
		return Envelope{}, err
	}
	if input == nil {
		input = map[string]any{}
	}
	if err := a.CheckInput(input); err != nil {
		return Envelope{}, err
	}
	if r.Trust == nil {
		return refused("repository trust is not configured; run `rzm trust` in " + r.RepoRoot), nil
	}
	if reason := r.Trust(ctx); reason != "" {
		return refused(reason), nil
	}
	ctx, cancel := context.WithTimeout(ctx, a.Timeout)
	defer cancel()
	if a.Run.Script != "" {
		return r.runScript(ctx, a, input), nil
	}
	return r.runCommand(ctx, a, input), nil
}

func (r *Runner) runCommand(ctx context.Context, a actiondef.Action, input map[string]any) Envelope {
	argv := a.Command()
	name, err := procrun.LookLoginPath(argv[0])
	if err != nil {
		return failed(err.Error(), "")
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return failed(err.Error(), "")
	}
	cmd := exec.Command(name, argv[1:]...)
	cmd.Dir = r.RepoRoot
	cmd.Env = procrun.WithPath(os.Environ(), procrun.LoginPath())
	cmd.Stdin = bytes.NewReader(payload)
	var stderr tail
	out, err := procrun.Run(ctx, cmd, &stderr, maxStdout)
	var exit *exec.ExitError
	switch {
	case ctx.Err() != nil:
		return stopped(ctx, a, &stderr)
	case errors.As(err, &exit) && exit.ExitCode() == 2:
		reason := stderr.lastLine()
		if reason == "" {
			reason = "action refused"
		}
		return refused(reason)
	case err != nil:
		return failed(err.Error(), stderr.String())
	}
	result, ok := oneJSONValue(out)
	if !ok {
		return failed(fmt.Sprintf("stdout is not one JSON value; got %.200q", out), stderr.String())
	}
	return Envelope{Status: StatusOK, Result: result}
}

func (r *Runner) runScript(ctx context.Context, a actiondef.Action, input map[string]any) Envelope {
	moduleDir := filepath.Join(r.RepoRoot, filepath.FromSlash(a.ModuleDir()))
	code, err := os.ReadFile(filepath.Join(moduleDir, filepath.FromSlash(a.Run.Script)))
	if err != nil {
		return failed("read action script: "+err.Error(), "")
	}
	rzm := r.Rzm
	if rzm == "" {
		if rzm, err = os.Executable(); err != nil {
			return failed(err.Error(), "")
		}
	}
	scriptInput := map[string]any{"action": a.ID, "input": input, "moduleDir": moduleDir, "repoRoot": r.RepoRoot}
	cmd, err := procrun.CodeModeCommand(rzm, r.RepoRoot, a.Timeout, scriptInput, code)
	if err != nil {
		return failed(err.Error(), "")
	}
	// Trust already passed, and an action may write, as a command action can.
	cmd.Args = append(cmd.Args, "--read-write")
	if r.Vault != "" {
		cmd.Args = append(cmd.Args, "--vault", r.Vault)
	}
	cmd.Env = procrun.WithPath(os.Environ(), procrun.LoginPath())
	var stderr tail
	out, err := procrun.Run(ctx, cmd, &stderr, maxStdout)
	if ctx.Err() != nil {
		return stopped(ctx, a, &stderr)
	}
	// A script that throws still prints code mode's result, which holds its
	// message, and exits non-zero.
	result, ok, message, parseErr := procrun.CodeModeResult(out)
	switch {
	case parseErr != nil && err != nil:
		return failed(err.Error(), stderr.String())
	case parseErr != nil:
		return failed("code mode output is not JSON: "+parseErr.Error(), stderr.String())
	case !ok:
		if message == "" {
			message = "script failed"
		}
		return failed(message, stderr.String())
	}
	var refusal map[string]json.RawMessage
	if json.Unmarshal(result, &refusal) == nil && len(refusal) == 1 {
		var reason string
		// Only a string is a refusal; {"refused": null} is an ordinary result.
		if raw, found := refusal["refused"]; found && bytes.HasPrefix(bytes.TrimSpace(raw), []byte(`"`)) && json.Unmarshal(raw, &reason) == nil {
			return refused(reason)
		}
	}
	if len(result) == 0 {
		result = json.RawMessage("null")
	}
	return Envelope{Status: StatusOK, Result: result}
}

// CoerceFlags turns command-line values into input values by declared kind:
// a list keeps every value, other kinds take the last one.
func CoerceFlags(a actiondef.Action, values map[string][]string) (map[string]any, error) {
	input := map[string]any{}
	for name, given := range values {
		spec, ok := a.Inputs[name]
		if !ok || len(given) == 0 {
			continue
		}
		last := given[len(given)-1]
		switch spec.Kind {
		case actiondef.KindList:
			input[name] = given
		case actiondef.KindInt:
			n, err := strconv.ParseInt(last, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("%w: input %q must be an integer", actiondef.ErrInvalidInput, name)
			}
			input[name] = n
		case actiondef.KindBool:
			b, err := strconv.ParseBool(last)
			if err != nil {
				return nil, fmt.Errorf("%w: input %q must be true or false", actiondef.ErrInvalidInput, name)
			}
			input[name] = b
		default:
			input[name] = last
		}
	}
	return input, nil
}

func stopped(ctx context.Context, a actiondef.Action, stderr *tail) Envelope {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return failed(fmt.Sprintf("timed out after %s", a.Timeout), stderr.String())
	}
	return failed("stopped: "+ctx.Err().Error(), stderr.String())
}

func refused(reason string) Envelope { return Envelope{Status: StatusRefused, Reason: reason} }

func failed(reason, stderr string) Envelope {
	return Envelope{Status: StatusError, Reason: reason, StderrTail: stderr}
}

// oneJSONValue reports whether out holds exactly one JSON value.
func oneJSONValue(out []byte) (json.RawMessage, bool) {
	dec := json.NewDecoder(bytes.NewReader(out))
	var value json.RawMessage
	if err := dec.Decode(&value); err != nil {
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, false
	}
	return value, true
}

// tail keeps the last stderrTail bytes written to it.
type tail struct{ buf []byte }

func (t *tail) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - stderrTail; over > 0 {
		t.buf = append(t.buf[:0], t.buf[over:]...)
	}
	return len(p), nil
}

func (t *tail) String() string { return string(t.buf) }

func (t *tail) lastLine() string {
	lines := strings.Split(strings.TrimRight(string(t.buf), "\r\n\t "), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}
