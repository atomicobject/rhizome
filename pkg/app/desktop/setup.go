package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/repoexec"
)

type SetupChoices struct {
	Workflow       string   `json:"workflow"`
	Addons         []string `json:"addons"`
	Agents         []string `json:"agents"`
	Search         string   `json:"search"`
	Skip           []string `json:"skip"`
	KeepIndexed    []string `json:"keepIndexed"`
	IncludeIgnored []string `json:"includeIgnored"`
}

type ScopeEdits struct {
	Skip           []string `json:"skip"`
	RemoveRules    []string `json:"removeRules"`
	IncludeIgnored []string `json:"includeIgnored"`
}

type InitializeResult struct {
	Result json.RawMessage `json:"result"`
	Folder FolderInfo      `json:"folder"`
}

func choiceFlags(choices *SetupChoices) []string {
	if choices == nil {
		return nil
	}
	var args []string
	if choices.Workflow != "" {
		args = append(args, "--workflow", choices.Workflow)
	}
	// No add-ons is none; no agent harness still writes AGENTS.md and the
	// shared skills, which the sheet promises for every setup.
	for _, list := range []struct {
		flag, empty string
		values      []string
	}{{"--addons", "none", choices.Addons}, {"--agents", "shared", choices.Agents}} {
		value := strings.Join(list.values, ",")
		if value == "" {
			value = list.empty
		}
		args = append(args, list.flag, value)
	}
	if choices.Search != "" {
		args = append(args, "--search", choices.Search)
	}
	args = appendEntries(args, "--skip", choices.Skip)
	args = appendEntries(args, "--keep-indexed", choices.KeepIndexed)
	return appendEntries(args, "--include-ignored", choices.IncludeIgnored)
}

func appendEntries(args []string, flag string, entries []string) []string {
	for _, entry := range entries {
		args = append(args, flag, entry)
	}
	return args
}

// Selection stays inert until configuration and trust checks have passed.
func (s *Service) setupTarget(ctx context.Context, req Request, configured bool) (FolderInfo, string, error) {
	info, plan, err := s.inspect(req.Folder)
	if err != nil {
		return info, "", err
	}
	if configured && !info.Configured {
		return info, "", problem("not_configured", "This folder needs Rhizome setup before its index scope can be read or changed.")
	}
	if !configured && info.Configured {
		return info, "", problem("invalid_request", "This folder is already configured for Rhizome.")
	}
	if info.TrustRequired {
		return info, "", problem("trust_required", "Confirm trust before running this folder's selected Rhizome executable.")
	}
	target, err := s.prepare(ctx, info, plan, req)
	if err != nil {
		return info, "", err
	}
	probe, flag := []string{"init", "--help"}, "--search-key-stdin"
	if configured {
		probe, flag = []string{"index", "scope", "--help"}, "--remove-rule"
	}
	help, err := repoexec.Probe(ctx, target, info.Path, probe...)
	if errors.Is(err, context.DeadlineExceeded) {
		return info, "", problem("runtime_error", "Rhizome took too long to answer. Try again.")
	}
	if err != nil || !strings.Contains(help, flag) {
		if configured {
			return info, "", problem("setup_unsupported", "This Rhizome version cannot show what gets indexed in the app. Update Rhizome, or run `rzm index --explain` and edit .rhizome/ignore.")
		}
		return info, "", problem("setup_unsupported", "This Rhizome version cannot set up from the app. Update Rhizome or run `rzm init` in a terminal.")
	}
	return info, target, nil
}

func (s *Service) SetupReport(ctx context.Context, req Request) (json.RawMessage, error) {
	if req.Key != "" {
		return nil, problem("invalid_request", "A setup report cannot accept a search key.")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	info, target, err := s.setupTarget(ctx, req, false)
	if err != nil {
		return nil, err
	}
	args := append([]string{"init", "--path", info.Path, "--check", "--json"}, choiceFlags(req.Setup)...)
	return runSetupJSON(ctx, target, info.Path, args, "", "setup_failed", false)
}

func (s *Service) Initialize(ctx context.Context, req Request) (InitializeResult, error) {
	if req.Setup == nil {
		return InitializeResult{}, problem("invalid_request", "Choose setup options before initializing this folder.")
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	info, target, err := s.setupTarget(ctx, req, false)
	if err != nil {
		return InitializeResult{}, err
	}
	args := append([]string{"init", "--path", info.Path, "--json"}, choiceFlags(req.Setup)...)
	if req.Key != "" {
		args = append(args, "--search-key-stdin")
	}
	doc, runErr := runSetupJSON(ctx, target, info.Path, args, req.Key, "setup_failed", true)
	// Configuration may have been written before an error or failed download.
	folder, err := s.Inspect(info.Path)
	if err == nil && folder.Configured {
		folder, err = s.Trust(folder.Path)
	}
	if runErr != nil {
		// The folder is configured now, so setup cannot run again; opening it
		// continues through the normal open steps.
		if err == nil && folder.Configured {
			var failed *Problem
			if errors.As(runErr, &failed) {
				return InitializeResult{}, problem("setup_partial", failed.Message+" Rhizome wrote part of the setup; open the workspace to continue.")
			}
		}
		return InitializeResult{}, runErr
	}
	if err != nil {
		return InitializeResult{}, err
	}
	return InitializeResult{Result: doc, Folder: folder}, nil
}

func (s *Service) Scope(ctx context.Context, req Request) (json.RawMessage, error) {
	if req.Key != "" || (req.Operation == "scope-edit" && req.Edits == nil) {
		return nil, problem("invalid_request", "Provide scope edits without a search key.")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	info, target, err := s.setupTarget(ctx, req, true)
	if err != nil {
		return nil, err
	}
	args := []string{"index", "scope", "--json"}
	if req.Operation == "scope-edit" {
		args = appendEntries(args, "--skip", req.Edits.Skip)
		args = appendEntries(args, "--remove-rule", req.Edits.RemoveRules)
		args = appendEntries(args, "--include-ignored", req.Edits.IncludeIgnored)
	}
	return runSetupJSON(ctx, target, info.Path, args, "", "scope_failed", false)
}

// Only stdout's JSON object crosses the process boundary. A pin-only failure
// is a completed setup result, even though the executable exits with status 1.
func runSetupJSON(ctx context.Context, target, folder string, args []string, key, failureCode string, allowPinFailure bool) (json.RawMessage, error) {
	cmd := exec.CommandContext(ctx, target, args...)
	cmd.Dir = folder
	cmd.Env = append(os.Environ(), "RZM_REPO_DELEGATED=1", "RZM_SKIP_REPO_DELEGATE=1")
	cmd.WaitDelay = time.Second
	if key != "" {
		cmd.Stdin = strings.NewReader(key + "\n")
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	var doc struct {
		Error    *Problem `json:"error"`
		SavedKey string   `json:"savedKey"`
		// A setup plan's pin is a version string; a setup result's is an object.
		Pin json.RawMessage `json:"pin"`
	}
	raw := bytes.TrimSpace(stdout.Bytes())
	// Even a misbehaving executable cannot echo the supplied secret to the app.
	redact := func(message string) string {
		if key != "" {
			return strings.ReplaceAll(message, key, "[redacted]")
		}
		return message
	}
	parseErr := json.Unmarshal(raw, &doc)
	if parseErr == nil && len(raw) > 0 && raw[0] == '{' {
		if doc.Error != nil {
			message := redact(doc.Error.Message)
			if failureCode == "setup_failed" && doc.SavedKey != "" {
				message += " The key stays saved in ~/.config/rhizome/config.yml."
			}
			return nil, problem(failureCode, message)
		}
		if key != "" && setupJSONContainsKey(raw, key) {
			return nil, problem(failureCode, "Rhizome returned a response containing the search key; the response was discarded.")
		}
		if runErr == nil || (allowPinFailure && pinFailed(doc.Pin) && cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1) {
			return json.RawMessage(raw), nil
		}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, problem(failureCode, "Rhizome took too long and was stopped. Try again.")
	}
	status := "exit status 0"
	if runErr != nil {
		status = runErr.Error()
	}
	message := fmt.Sprintf("Rhizome returned no valid result (%s).", status)
	detail := strings.TrimSpace(redact(stderr.String()))
	if len(detail) > 4096 {
		detail = detail[len(detail)-4096:]
	}
	if detail != "" {
		message += "\n" + detail
	}
	return nil, problem(failureCode, message)
}

// pinFailed reports whether a setup result's pinned-executable install failed.
func pinFailed(raw json.RawMessage) bool {
	var pin struct {
		Error string `json:"error"`
	}
	return json.Unmarshal(raw, &pin) == nil && pin.Error != ""
}

// Inspect decoded strings as well, since JSON may escape part of the key.
func setupJSONContainsKey(raw []byte, key string) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	var contains func(any) bool
	contains = func(value any) bool {
		switch value := value.(type) {
		case string:
			return strings.Contains(value, key)
		case []any:
			for _, entry := range value {
				if contains(entry) {
					return true
				}
			}
		case map[string]any:
			for name, entry := range value {
				if strings.Contains(name, key) || contains(entry) {
					return true
				}
			}
		}
		return false
	}
	return contains(value)
}
