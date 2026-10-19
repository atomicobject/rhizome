package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/atomicobject/rhizome/pkg/harness"
)

// Generate runs one ephemeral app-server turn under the sandboxed tool policy.
// The Codex CLI cannot remove every tool, so configuration removes what it can:
// launch overrides disable tool features and instruction blocks, a model
// catalog drops the per-model settings that bring tools back, thread/start
// selects no environments and disables every configured MCP server, and a
// read-only, network-less sandbox with approval "never" bounds the rest. It
// fails closed with ErrUnsupported, before any turn runs, when Codex is older
// than the verified version, the models cache lacks the model, config/read or
// thread/start rejects the configuration, or Codex reports ignoring an
// override. The global CODEX_HOME/AGENTS.md is still injected.
func (d *Driver) Generate(ctx context.Context, request harness.GenerateRequest) (harness.GenerateResult, error) {
	path, err := d.runner.LookPath(d.binary)
	if err != nil {
		return harness.GenerateResult{}, harness.Phase(harness.ErrNotInstalled, err)
	}
	version, err := d.checkedVersion(ctx, path)
	if err != nil {
		return harness.GenerateResult{}, err
	}
	if !versionAtLeast(version, generateMinimumVersion) {
		return harness.GenerateResult{}, unsupported("codex %s is older than %d.%d.%d, the version tool-free generation was verified against", version, generateMinimumVersion[0], generateMinimumVersion[1], generateMinimumVersion[2])
	}
	if len(request.Schema) > 0 && !json.Valid(request.Schema) {
		return harness.GenerateResult{}, harness.Phase(harness.ErrSchemaMismatch, errors.New("schema is not valid JSON"))
	}
	dir, err := os.MkdirTemp("", "rhizome-codex-generate-")
	if err != nil {
		return harness.GenerateResult{}, harness.Phase(harness.ErrSpawn, err)
	}
	defer os.RemoveAll(dir)
	catalog := filepath.Join(dir, "models.json")
	models, err := writeGenerateCatalog(catalog, request.Model)
	if err != nil {
		return harness.GenerateResult{}, unsupported("%v", err)
	}
	connection, err := d.start(ctx, path, generateArgs(catalog), dir)
	if err != nil {
		if isNotFound(err) {
			return harness.GenerateResult{}, harness.Phase(harness.ErrNotInstalled, err)
		}
		return harness.GenerateResult{}, harness.Phase(harness.ErrSpawn, err)
	}
	run := &generation{done: make(chan struct{})}
	s := newSession(connection, harness.SessionOptions{}, dir, turnCancelTimeout, d.shutdownTimeout, false, run.observe)
	defer s.Stop()
	s.client.experimentalAPI = true
	if _, err := s.client.initialize(ctx); err != nil {
		return harness.GenerateResult{}, s.preferTerminal(err)
	}

	var config configReadResponse
	if err := s.client.request(ctx, harness.ErrInitialize, "config/read", configReadParams{Cwd: dir}, &config, nil); err != nil {
		return harness.GenerateResult{}, rejected("config/read", s.preferTerminal(err))
	}
	threadConfig, keys := map[string]any{}, overrideKeys()
	for name := range config.Config.MCPServers {
		key := "mcp_servers." + name + ".enabled"
		threadConfig[key] = false
		keys = append(keys, key)
	}
	var started generateThreadResponse
	if err := s.client.request(ctx, harness.ErrThreadStart, "thread/start", generateThreadParams{
		Model: request.Model, Cwd: dir, Ephemeral: true, Environments: []json.RawMessage{},
		BaseInstructions: generateInstructions, ApprovalPolicy: "never", Sandbox: "read-only", Config: threadConfig,
	}, &started, nil); err != nil {
		return harness.GenerateResult{}, rejected("thread/start", s.preferTerminal(err))
	}
	if started.Thread.ID == "" {
		return harness.GenerateResult{}, harness.Phase(harness.ErrThreadStart, errors.New("response did not include thread.id"))
	}
	if !models[started.Model] {
		return harness.GenerateResult{}, unsupported("Codex selected model %q, which has no entry in the models cache", started.Model)
	}
	if ignored := run.ignoredOverride(keys); ignored != "" {
		return harness.GenerateResult{}, unsupported("Codex ignored a tool-removal override: %s", ignored)
	}
	if _, err := s.client.startTurn(ctx, turnStartParams{
		ThreadID: started.Thread.ID, Input: []userInput{{Type: "text", Text: request.Prompt}},
		Effort: request.Effort, OutputSchema: request.Schema,
	}, nil); err != nil {
		return harness.GenerateResult{}, s.preferTerminal(err)
	}
	select {
	case <-run.done:
	case <-ctx.Done():
		return harness.GenerateResult{}, harness.Phase(harness.ErrTimeout, ctx.Err())
	case <-s.lifetime.Done():
		return harness.GenerateResult{}, s.transportError()
	}
	return run.result(started.Model, request.Schema)
}

// generateMinimumVersion is the codex-cli release this recipe was verified
// against; older releases may ignore thread/start fields it relies on.
var generateMinimumVersion = [3]int{0, 163, 0}

const generateInstructions = "Respond to the user's request."

var generateFeatures = strings.Fields("multi_agent memories apps plugins hooks skill_search tool_suggest goals sleep_tool code_mode_host view_image image_generation browser_use computer_use chronicle workspace_dependencies recommended_plugins")

// generateOverrides are the launch -c overrides beyond feature flags.
var generateOverrides = []string{
	"include_permissions_instructions=false",
	"include_environment_context=false",
	"include_collaboration_mode_instructions=false",
	"include_apps_instructions=false",
	"skills.include_instructions=false",
	"skills.bundled.enabled=false",
	"tools.experimental_request_user_input={enabled=false}",
	`web_search="disabled"`,
}

func generateArgs(catalog string) []string {
	args := []string{"app-server"}
	for _, feature := range generateFeatures {
		args = append(args, "-c", "features."+feature+"=false")
	}
	for _, override := range generateOverrides {
		args = append(args, "-c", override)
	}
	return appendConfig(args, "model_catalog_json", catalog)
}

// overrideKeys are the launch configuration keys Generate sets, for matching
// Codex's "ignored setting" warnings.
func overrideKeys() []string {
	keys := []string{"model_catalog_json"}
	for _, feature := range generateFeatures {
		keys = append(keys, "features."+feature)
	}
	for _, override := range generateOverrides {
		key, _, _ := strings.Cut(override, "=")
		keys = append(keys, key)
	}
	return keys
}

// catalogOverrides are applied to every cached model entry. use_responses_lite
// makes the backend inject hosted tools, tool_mode brings back exec, and
// multi_agent_version brings back agent spawning.
var catalogOverrides = map[string]json.RawMessage{
	"use_responses_lite":            json.RawMessage(`false`),
	"supports_experimental_context": json.RawMessage(`false`),
	"supports_search_tool":          json.RawMessage(`false`),
	"node_repl_disabled":            json.RawMessage(`true`),
	"experimental_supported_tools":  json.RawMessage(`[]`),
	"apply_patch_tool_type":         json.RawMessage(`null`),
}

// writeGenerateCatalog copies every model in the Codex models cache, with the
// tool-bearing settings removed, so the requested or configured default model
// runs from a tool-free entry. It returns the model slugs written.
func writeGenerateCatalog(path, model string) (map[string]bool, error) {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("locate Codex home: %w", err)
		}
		home = filepath.Join(userHome, ".codex")
	}
	data, err := os.ReadFile(filepath.Join(home, "models_cache.json"))
	if err != nil {
		return nil, fmt.Errorf("read Codex models cache: %w", err)
	}
	var cache struct {
		Models []map[string]json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("parse Codex models cache: %w", err)
	}
	slugs := make(map[string]bool, len(cache.Models))
	for _, entry := range cache.Models {
		var slug string
		if json.Unmarshal(entry["slug"], &slug) != nil || slug == "" {
			return nil, errors.New("parse Codex models cache: model entry without a slug")
		}
		slugs[slug] = true
		delete(entry, "tool_mode")
		delete(entry, "multi_agent_version")
		for key, value := range catalogOverrides {
			entry[key] = value
		}
	}
	if model != "" && !slugs[model] {
		return nil, fmt.Errorf("Codex models cache has no entry for model %q", model)
	}
	if len(slugs) == 0 {
		return nil, errors.New("Codex models cache has no models")
	}
	encoded, err := json.Marshal(cache)
	if err != nil {
		return nil, err
	}
	return slugs, os.WriteFile(path, encoded, 0o600)
}

type configReadParams struct {
	Cwd string `json:"cwd"`
}

type configReadResponse struct {
	Config struct {
		MCPServers map[string]json.RawMessage `json:"mcp_servers"`
	} `json:"config"`
}

type generateThreadParams struct {
	Model            string            `json:"model,omitempty"`
	Cwd              string            `json:"cwd"`
	Ephemeral        bool              `json:"ephemeral"`
	Environments     []json.RawMessage `json:"environments"`
	BaseInstructions string            `json:"baseInstructions"`
	ApprovalPolicy   string            `json:"approvalPolicy"`
	Sandbox          string            `json:"sandbox"`
	Config           map[string]any    `json:"config,omitempty"`
}

type generateThreadResponse struct {
	Thread struct {
		ID string `json:"id"`
	} `json:"thread"`
	Model string `json:"model"`
}

// generation collects the notifications of one Generate turn. observe runs on
// the session read loop and never blocks.
type generation struct {
	mu       sync.Mutex
	warnings []string
	rerouted string
	usage    tokenUsage
	text     string
	status   string
	failure  string
	done     chan struct{}
	once     sync.Once
}

func (g *generation) observe(message rpcMessage) {
	var params struct {
		Summary    string           `json:"summary"`
		Message    string           `json:"message"`
		ToModel    string           `json:"toModel"`
		TokenUsage threadTokenUsage `json:"tokenUsage"`
		Item       threadItem       `json:"item"`
		Error      *turnError       `json:"error"`
		WillRetry  bool             `json:"willRetry"`
		Turn       struct {
			Status string     `json:"status"`
			Error  *turnError `json:"error"`
		} `json:"turn"`
	}
	_ = json.Unmarshal(message.Params, &params)
	g.mu.Lock()
	defer g.mu.Unlock()
	switch message.Method {
	case "configWarning":
		g.warnings = append(g.warnings, params.Summary)
	case "warning":
		g.warnings = append(g.warnings, params.Message)
	case "model/rerouted":
		g.rerouted = params.ToModel
	case "thread/tokenUsage/updated":
		g.usage = params.TokenUsage.Last
	case "item/completed":
		if params.Item.Type == "agentMessage" {
			g.text = params.Item.Text
		}
	case "error":
		if params.Error != nil && !params.WillRetry {
			g.failure = params.Error.Message
		}
	case "turn/completed":
		g.status = params.Turn.Status
		if params.Turn.Error != nil && params.Turn.Error.Message != "" {
			g.failure = params.Turn.Error.Message
		}
		g.once.Do(func() { close(g.done) })
	}
}

// ignoredOverride returns a warning naming one of keys. Codex 0.163.0 ignores
// an unknown key with a warning rather than failing; the configWarning for
// launch overrides arrives before the config/read response.
func (g *generation) ignoredOverride(keys []string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, warning := range g.warnings {
		for _, key := range keys {
			if strings.Contains(warning, key) {
				return warning
			}
		}
	}
	return ""
}

func (g *generation) result(model string, schema json.RawMessage) (harness.GenerateResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.status != "completed" {
		detail := g.failure
		if detail == "" {
			detail = "turn " + g.status
		}
		return harness.GenerateResult{}, harness.Phase(harness.ErrCommandFailed, errors.New(detail))
	}
	if g.rerouted != "" {
		model = g.rerouted
	}
	result := harness.GenerateResult{Usage: toHarnessUsage(g.usage), Model: model, ToolPolicy: harness.ToolPolicySandboxed}
	if len(schema) == 0 {
		result.Output, _ = json.Marshal(g.text)
		return result, nil
	}
	if !json.Valid([]byte(g.text)) {
		return harness.GenerateResult{}, harness.Phase(harness.ErrSchemaMismatch, errors.New("Codex did not return valid JSON for the output schema"))
	}
	result.Output = json.RawMessage(g.text)
	return result, nil
}

func unsupported(format string, args ...any) error {
	return harness.Phase(harness.ErrUnsupported, fmt.Errorf(format, args...))
}

// rejected turns a protocol rejection of the tool-free configuration into
// ErrUnsupported; transport failures and timeouts keep their phase.
func rejected(method string, err error) error {
	var rpcErr *rpcError
	if errors.As(err, &rpcErr) {
		return unsupported("Codex %s failed, so the tools cannot be removed: %s", method, rpcErr.Message)
	}
	return err
}

func versionAtLeast(version string, minimum [3]int) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	for i, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil {
			return false
		}
		if value != minimum[i] {
			return value > minimum[i]
		}
	}
	return true
}
