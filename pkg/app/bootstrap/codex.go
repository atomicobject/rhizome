package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/agent"
	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	appinit "github.com/atomicobject/rhizome/pkg/app/cli/init"
	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/llm"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	codeembsql "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex/sqlite"
	embsql "github.com/atomicobject/rhizome/pkg/search/embeddings/sqlite"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// CodexParams configures the codex bootstrap command.
type CodexParams struct {
	Task        string
	Queries     []string
	Files       []string
	Profile     string
	Model       string
	NoAuto      bool
	BudgetChars int
	VaultName   string
	Debug       bool
	DebugAll    bool
}

// RelatedEntry captures a tool call label and response.
type RelatedEntry struct {
	Label    string
	Response string
}

// ToolReport captures tool call debugging details.
type ToolReport struct {
	Name          string
	Arguments     map[string]interface{}
	ResponseChars int
	Response      string
	Error         string
}

// CodexResult captures the bootstrap prompt and warnings.
type CodexResult struct {
	Prompt   string
	Warnings []string
	Errors   []string
	Reports  []ToolReport
}

// RunCodex builds the bootstrap prompt and launches the Codex CLI.
func RunCodex(ctx context.Context, params CodexParams) error {
	return runBootstrap(ctx, params, "codex", launchCodex)
}

// RunClaude builds the bootstrap prompt and launches the Claude CLI.
func RunClaude(ctx context.Context, params CodexParams) error {
	return runBootstrap(ctx, params, "claude", launchClaude)
}

func runBootstrap(ctx context.Context, params CodexParams, name string, launcher func(context.Context, string) error) error {
	result, err := CodexBootstrap(ctx, params)
	if err != nil {
		return err
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
	}
	debug := params.Debug || params.DebugAll
	if debug {
		printDebugReport(result.Reports, params.DebugAll)
	}
	if len(result.Errors) > 0 {
		for _, errMsg := range result.Errors {
			fmt.Fprintf(os.Stderr, "error: %s\n", errMsg)
		}
		return fmt.Errorf("%s bootstrap failed", name)
	}
	if debug {
		return nil
	}
	return launcher(ctx, result.Prompt)
}

// CodexBootstrap prepares the bootstrap prompt without launching Codex.
func CodexBootstrap(ctx context.Context, params CodexParams) (CodexResult, error) {
	task := strings.TrimSpace(params.Task)
	if task == "" {
		return CodexResult{}, fmt.Errorf("task is required")
	}

	vault := obsidian.Vault{Name: params.VaultName}
	vaultDef, err := vault.Definition()
	if err != nil {
		return CodexResult{}, err
	}
	vaultPath := vaultDef.BasePath()
	budget := params.BudgetChars
	if budget <= 0 {
		budget = obsidian.GetBudgetChars(vaultPath)
	}

	warnings := []string{}
	errors := []string{}
	reports := []ToolReport{}
	if err := loadDotEnv(vaultPath); err != nil {
		warnings = append(warnings, fmt.Sprintf("dotenv load failed: %v", err))
	}
	rhizomeText, err := readRhizomeDoc(vaultPath)
	if err != nil {
		warnings = append(warnings, err.Error())
	}

	formats, err := builtin.NewRuntime()
	if err != nil {
		return CodexResult{}, fmt.Errorf("compose note formats: %w", err)
	}
	mcpCfg, err := newCodexAgentConfig(&vault, vaultPath, vaultDef, formats)
	if err != nil {
		return CodexResult{}, err
	}
	cleanup, initWarnings := initSemanticTools(ctx, vaultPath, &mcpCfg)
	warnings = append(warnings, initWarnings...)
	if cleanup != nil {
		defer cleanup()
	}

	queries := dedupeStrings(params.Queries)
	files := dedupeStrings(params.Files)
	files, fileWarnings := filterFileSeeds(vaultPath, files)
	warnings = append(warnings, fileWarnings...)

	userCalls := []agent.ToolCall{}
	if len(queries) > 0 {
		userCalls = append(userCalls, agent.ToolCall{
			Name:      "semantic_query",
			Arguments: map[string]interface{}{"queries": queries},
		})
	}
	if len(files) > 0 {
		userCalls = append(userCalls, agent.ToolCall{
			Name:      "file_context",
			Arguments: map[string]interface{}{"files": files},
		})
	}

	autoCalls := []agent.ToolCall{}
	if !params.NoAuto {
		planned, autoWarnings := planAutoToolCalls(ctx, params, vaultPath, queries, files)
		warnings = append(warnings, autoWarnings...)
		autoCalls = planned
	}
	calls := mergeToolCalls(userCalls, autoCalls)

	related := []RelatedEntry{}
	vaultText, err := agentapi.CallContextText(ctx, mcpCfg, "vault_context", map[string]interface{}{})
	if err != nil {
		errors = append(errors, fmt.Sprintf("vault_context failed: %v", err))
		reports = append(reports, newToolReport("vault_context", map[string]interface{}{}, "", err))
	} else if strings.TrimSpace(vaultText) != "" {
		related = append(related, RelatedEntry{Label: "vault_context", Response: vaultText})
		reports = append(reports, newToolReport("vault_context", map[string]interface{}{}, vaultText, nil))
	}

	callEntries, callReports, callWarnings, callErrors := executeToolCalls(ctx, mcpCfg, calls)
	warnings = append(warnings, callWarnings...)
	errors = append(errors, callErrors...)
	related = append(related, callEntries...)
	reports = append(reports, callReports...)

	prompt := BuildCodexPrompt(task, related, rhizomeText, budget)
	return CodexResult{Prompt: prompt, Warnings: warnings, Errors: errors, Reports: reports}, nil
}

func newCodexAgentConfig(vault *obsidian.Vault, vaultPath string, vaultDef obsidian.VaultDefinition, formats noteformat.Runtime) (agentapi.Config, error) {
	noteMetadata, err := notemeta.NewIndexer(formats)
	if err != nil {
		return agentapi.Config{}, fmt.Errorf("compose note metadata indexer: %w", err)
	}
	if err := noteMetadata.Validate(); err != nil {
		return agentapi.Config{}, fmt.Errorf("validate note metadata indexer: %w", err)
	}
	return agentapi.Config{
		Vault:        vault,
		VaultPath:    vaultPath,
		VaultDef:     vaultDef,
		NoteMetadata: noteMetadata,
		ReadWrite:    false,
	}, nil
}

func BuildCodexPrompt(task string, related []RelatedEntry, rhizomeText string, budget int) string {
	if budget <= 0 {
		budget = contextpack.DefaultBudgetChars
	}
	var b strings.Builder
	b.WriteString("USER PROMPT: ")
	b.WriteString(strings.TrimSpace(task))
	b.WriteString("\n\n# Related data\n")
	for _, entry := range related {
		label := strings.TrimSpace(entry.Label)
		if label != "" {
			b.WriteString(label)
			b.WriteString("\n")
		}
		response := strings.TrimSpace(entry.Response)
		if response != "" {
			b.WriteString("<response>\n")
			b.WriteString(response)
			b.WriteString("\n</response>\n\n")
		}
	}
	b.WriteString("# General usage\n")
	b.WriteString(strings.TrimSpace(rhizomeText))
	final := strings.TrimSpace(b.String())
	return contextpack.TrimToBudget(final, budget)
}

func readRhizomeDoc(vaultPath string) (string, error) {
	if vaultPath == "" {
		return "", fmt.Errorf("RHIZOME.md not found (vault path unknown)")
	}
	path := filepath.Join(vaultPath, "RHIZOME.md")
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("RHIZOME.md unavailable: %w", err)
	}
	return string(content), nil
}

func launchCodex(ctx context.Context, prompt string) error {
	path, err := exec.LookPath("codex")
	if err != nil {
		return fmt.Errorf("codex CLI not found on PATH")
	}
	cmd := exec.CommandContext(ctx, path, prompt)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func launchClaude(ctx context.Context, prompt string) error {
	path, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude CLI not found on PATH")
	}
	cmd := exec.CommandContext(ctx, path, prompt)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func initSemanticTools(ctx context.Context, vaultPath string, cfg *agentapi.Config) (func(), []string) {
	warnings := []string{}
	closers := []func(){}
	if cfg == nil || vaultPath == "" {
		return nil, warnings
	}
	embCfg, err := obsidian.LoadEmbeddingsConfig(vaultPath)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("semantic: failed to load embeddings config: %v", err))
		return nil, warnings
	}
	embCfg.IndexPath = obsidian.UnifiedIndexPath(vaultPath, embCfg.IndexPath)

	codeEmbCfg, _, codeEmbErr := obsidian.EffectiveCodeEmbeddingsConfig(vaultPath, embCfg)
	if codeEmbErr != nil {
		warnings = append(warnings, fmt.Sprintf("semantic: failed to load code embeddings config: %v", codeEmbErr))
	}

	if embCfg.Enabled {
		provider, providerCfg, err := embeddings.NewProviderForConfig(embCfg, "")
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("semantic: %v", embeddings.ProviderUnavailableError(embCfg, err)))
		} else {
			if closer, ok := provider.(io.Closer); ok {
				closers = append(closers, func() { _ = closer.Close() })
			}
			store, err := embsql.OpenWithMetadata(ctx, embCfg.IndexPath, provider, embeddings.MetadataForProvider(provider, providerCfg))
			if err != nil {
				var metaErr embeddings.MetadataError
				if errors.As(err, &metaErr) {
					warnings = append(warnings, fmt.Sprintf("semantic: metadata init failed: %v", metaErr))
				} else {
					warnings = append(warnings, fmt.Sprintf("semantic: index unavailable at %s: %v", embCfg.IndexPath, err))
				}
			} else {
				cfg.EmbeddingsOn = true
				cfg.Embeddings = store
				cfg.EmbedProvider = provider
				cfg.EmbeddingsPath = embCfg.IndexPath
				closers = append(closers, func() { _ = store.Close() })
			}
		}
	}

	if codeEmbCfg.Enabled {
		provider, providerCfg, err := embeddings.NewProviderForConfig(codeEmbCfg, "")
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("semantic: %v", embeddings.ProviderUnavailableError(codeEmbCfg, err)))
		} else {
			if closer, ok := provider.(io.Closer); ok {
				closers = append(closers, func() { _ = closer.Close() })
			}
			store, err := codeembsql.OpenWithMetadata(ctx, codeEmbCfg.IndexPath, provider, embeddings.MetadataForProvider(provider, providerCfg))
			if err != nil {
				var metaErr embeddings.MetadataError
				if errors.As(err, &metaErr) {
					warnings = append(warnings, fmt.Sprintf("semantic: code metadata init failed: %v", metaErr))
				} else {
					warnings = append(warnings, fmt.Sprintf("semantic: code index unavailable at %s: %v", codeEmbCfg.IndexPath, err))
				}
			} else {
				cfg.CodeEmbeddingsOn = true
				cfg.CodeEmbeddings = store
				cfg.CodeEmbedProvider = provider
				closers = append(closers, func() { _ = store.Close() })
			}
		}
	}

	if len(closers) == 0 {
		return nil, warnings
	}
	return func() {
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i]()
		}
	}, warnings
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, val := range values {
		trimmed := strings.TrimSpace(val)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func planAutoToolCalls(ctx context.Context, params CodexParams, vaultPath string, userQueries, userFiles []string) ([]agent.ToolCall, []string) {
	warnings := []string{}
	globalCfg, localCfg := loadLLMConfigs(vaultPath)
	resolved, err := llm.ResolveProfile(params.Profile, localCfg, globalCfg, params.Model)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("auto planning disabled: %v", err))
		return nil, warnings
	}
	provider, err := llm.NewProvider(resolved)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("auto planning disabled: %v", err))
		return nil, warnings
	}
	instructions, err := loadOnboardInstructions()
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("auto planning instructions unavailable: %v", err))
	}
	tools := []agent.ToolSpec{
		{
			Name:        "semantic_query",
			Description: "Find task-shaped code/doc context. Returns must-read evidence plus raw matches. Prefer one call with multiple queries.",
			ArgsSchema:  `{"queries":["string"],"paths":["string"],"limit":25}`,
		},
		{
			Name:        "file_context",
			Description: "Fetch context for files or directories. Prefer one call with multiple files.",
			ArgsSchema:  `{"files":["string"]}`,
		},
	}
	calls, err := agent.PlanToolCalls(ctx, provider, resolved, params.Task, instructions, tools, agent.PlanHints{Queries: userQueries, Files: userFiles})
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("auto planning failed: %v", err))
		return nil, warnings
	}
	filtered := make([]agent.ToolCall, 0, len(calls))
	for _, call := range calls {
		switch call.Name {
		case "semantic_query", "file_context":
			filtered = append(filtered, call)
		case "vault_context":
			warnings = append(warnings, "auto planning returned vault_context; skipping because it is always included")
		default:
			warnings = append(warnings, fmt.Sprintf("unsupported tool call ignored: %s", call.Name))
		}
	}
	return filtered, warnings
}

func mergeToolCalls(primary, secondary []agent.ToolCall) []agent.ToolCall {
	seen := make(map[string]struct{})
	out := make([]agent.ToolCall, 0, len(primary)+len(secondary))
	add := func(call agent.ToolCall) {
		key := toolCallKey(call)
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, call)
	}
	for _, call := range primary {
		add(call)
	}
	for _, call := range secondary {
		add(call)
	}
	return out
}

func toolCallKey(call agent.ToolCall) string {
	if call.Name == "" {
		return ""
	}
	args := call.Arguments
	if args == nil {
		args = map[string]interface{}{}
	}
	b, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	return call.Name + "|" + string(b)
}

func executeToolCalls(ctx context.Context, cfg agentapi.Config, calls []agent.ToolCall) ([]RelatedEntry, []ToolReport, []string, []string) {
	warnings := []string{}
	errors := []string{}
	entries := []RelatedEntry{}
	reports := []ToolReport{}
	for _, call := range calls {
		name := call.Name
		switch name {
		case "semantic_query":
			if !cfg.EmbeddingsOn || cfg.EmbedProvider == nil || cfg.Embeddings == nil {
				msg := "semantic_query unavailable (semantic index not configured)"
				errors = append(errors, msg)
				reports = append(reports, newToolReport(name, call.Arguments, "", fmt.Errorf("%s", msg)))
				continue
			}
			text, err := agentapi.CallContextText(ctx, cfg, name, call.Arguments)
			if err != nil {
				errors = append(errors, fmt.Sprintf("semantic_query failed: %v", err))
				reports = append(reports, newToolReport(name, call.Arguments, "", err))
				continue
			}
			entries = append(entries, RelatedEntry{
				Label:    formatToolCallLabel(call),
				Response: text,
			})
			reports = append(reports, newToolReport(name, call.Arguments, text, nil))
		case "file_context":
			text, err := agentapi.CallContextText(ctx, cfg, name, call.Arguments)
			if err != nil {
				errors = append(errors, fmt.Sprintf("file_context failed: %v", err))
				reports = append(reports, newToolReport(name, call.Arguments, "", err))
				continue
			}
			entries = append(entries, RelatedEntry{
				Label:    formatToolCallLabel(call),
				Response: text,
			})
			reports = append(reports, newToolReport(name, call.Arguments, text, nil))
		default:
			warnings = append(warnings, fmt.Sprintf("unsupported tool call ignored: %s", name))
		}
	}
	return entries, reports, warnings, errors
}

func formatToolCallLabel(call agent.ToolCall) string {
	switch call.Name {
	case "semantic_query":
		queries := extractStringArray(call.Arguments["queries"])
		paths := extractStringArray(call.Arguments["paths"])
		if len(queries) > 0 {
			return fmt.Sprintf("semantic query: %s", joinQuoted(queries))
		}
		if len(paths) > 0 {
			return fmt.Sprintf("semantic query: %s", joinQuoted(paths))
		}
		return "semantic query"
	case "file_context":
		files := extractStringArray(call.Arguments["files"])
		if len(files) > 0 {
			return fmt.Sprintf("file_context: %s", joinQuoted(files))
		}
		return "file_context"
	default:
		return call.Name
	}
}

func joinQuoted(values []string) string {
	if len(values) == 0 {
		return ""
	}
	parts := make([]string, 0, len(values))
	for _, val := range values {
		trimmed := strings.TrimSpace(val)
		if trimmed == "" {
			continue
		}
		parts = append(parts, "'"+trimmed+"'")
	}
	return strings.Join(parts, " ")
}

func loadOnboardInstructions() (string, error) {
	return appinit.LoadAgentHelperTemplate("skills/markdown/rhizome/references/onboarding.md")
}

func newToolReport(name string, args map[string]interface{}, response string, err error) ToolReport {
	copyArgs := map[string]interface{}{}
	for k, v := range args {
		copyArgs[k] = v
	}
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return ToolReport{
		Name:          name,
		Arguments:     copyArgs,
		ResponseChars: len(response),
		Response:      response,
		Error:         msg,
	}
}

func printDebugReport(reports []ToolReport, showResponses bool) {
	if len(reports) == 0 {
		fmt.Println("Debug: no tool calls executed")
		return
	}
	fmt.Println("Debug: tool calls")
	for _, report := range reports {
		args := formatArgs(report.Arguments)
		if report.Error != "" {
			fmt.Printf("- %s args=%s error=%s\n", report.Name, args, report.Error)
			continue
		}
		fmt.Printf("- %s args=%s response_chars=%d\n", report.Name, args, report.ResponseChars)
	}
	if !showResponses {
		return
	}
	for _, report := range reports {
		fmt.Printf("\n--- %s response (chars=%d) ---\n", report.Name, report.ResponseChars)
		if report.Response == "" {
			fmt.Println("(empty)")
			continue
		}
		fmt.Println(report.Response)
	}
}

func formatArgs(args map[string]interface{}) string {
	if args == nil {
		return "{}"
	}
	data, err := json.Marshal(args)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func extractStringArray(raw interface{}) []string {
	if raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return append([]string{}, v...)
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		return []string{v}
	default:
		return nil
	}
}

func filterFileSeeds(vaultPath string, files []string) ([]string, []string) {
	warnings := []string{}
	if vaultPath == "" {
		return files, warnings
	}
	vaultPaths, err := paths.NewVaultPaths(vaultPath)
	if err != nil || vaultPaths.Root() == "" {
		return files, warnings
	}
	out := make([]string, 0, len(files))
	for _, file := range files {
		trimmed := strings.TrimSpace(file)
		if trimmed == "" {
			continue
		}
		abs := trimmed
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(vaultPaths.Root(), abs)
		}
		if _, err := vaultPaths.RelStrict(abs); err != nil {
			warnings = append(warnings, fmt.Sprintf("skipping file seed outside vault: %s", trimmed))
			continue
		}
		out = append(out, trimmed)
	}
	return out, warnings
}

func loadLLMConfigs(vaultPath string) (*llm.Config, *llm.Config) {
	var localCfg *llm.Config
	if vaultPath != "" {
		if _, cfg, err := obsidian.FindLocalConfig(vaultPath); err == nil && cfg != nil {
			localCfg = cfg.LLM
		}
	}
	globalCfg := (*llm.Config)(nil)
	if cliCfg, err := obsidian.LoadCliConfig(true); err == nil {
		globalCfg = cliCfg.LLM
	}
	return globalCfg, localCfg
}

func loadDotEnv(vaultPath string) error {
	start := strings.TrimSpace(vaultPath)
	if start == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		start = cwd
	}
	_, err := vaultconfig.LoadDotEnvUpwards(start)
	return err
}
