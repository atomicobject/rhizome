package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/internal/command"
)

// Generate runs one tool-free, sessionless print-mode call. Safe mode keeps
// CLAUDE.md, skills, and plugins out of the prompt while preserving the user's
// login, and the minimal system prompt replaces the coding-agent prompt; together
// they cut a call from about 17k to about 1k input tokens (2.1.280). The
// five-minute prompt cache suits short-gap call sequences; Claude Code otherwise
// writes the one-hour cache at twice the input price.
func (d *Driver) Generate(ctx context.Context, request harness.GenerateRequest) (harness.GenerateResult, error) {
	path, err := d.runner.LookPath(d.binary)
	if err != nil {
		return harness.GenerateResult{}, harness.Phase(harness.ErrNotInstalled, err)
	}
	if _, err := d.checkedVersion(ctx, path); err != nil {
		return harness.GenerateResult{}, err
	}
	args := []string{"-p", "--output-format", "json"}
	if request.Model != "" {
		args = append(args, "--model", request.Model)
	}
	if request.Effort != "" {
		args = append(args, "--effort", request.Effort)
	}
	args = append(args, "--tools", "", "--disable-slash-commands", "--strict-mcp-config", "--permission-mode", "dontAsk", "--settings", `{"disableAllHooks":true}`, "--no-session-persistence", "--safe-mode", "--system-prompt", generateSystemPrompt)
	if len(request.Schema) > 0 {
		if !json.Valid(request.Schema) {
			return harness.GenerateResult{}, harness.Phase(harness.ErrSchemaMismatch, errors.New("schema is not valid JSON"))
		}
		args = append(args, "--json-schema", string(request.Schema))
	}
	result, err := d.runner.Run(ctx, command.Spec{Path: path, Args: args, Stdin: strings.NewReader(request.Prompt), Env: []string{"FORCE_PROMPT_CACHING_5M=1"}})
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) {
			return harness.GenerateResult{}, harness.Phase(harness.ErrTimeout, ctx.Err())
		}
		if isNotFound(err) {
			return harness.GenerateResult{}, harness.Phase(harness.ErrNotInstalled, err)
		}
		return harness.GenerateResult{}, harness.Phase(harness.ErrSpawn, err)
	}
	if result.ExitCode != 0 {
		detail := strings.TrimSpace(result.Stderr)
		if looksLikeAuthFailure(detail) {
			return harness.GenerateResult{}, harness.Phase(harness.ErrNotLoggedIn, errors.New(detail))
		}
		return harness.GenerateResult{}, harness.Phase(harness.ErrCommandFailed, fmt.Errorf("exit %d: %s", result.ExitCode, detail))
	}
	var output struct {
		Result           string          `json:"result"`
		StructuredOutput json.RawMessage `json:"structured_output"`
		IsError          bool            `json:"is_error"`
		Usage            *usage          `json:"usage"`
		TotalCostUSD     float64         `json:"total_cost_usd"`
		ModelUsage       map[string]struct {
			InputTokens              int64 `json:"inputTokens"`
			OutputTokens             int64 `json:"outputTokens"`
			CacheReadInputTokens     int64 `json:"cacheReadInputTokens"`
			CacheCreationInputTokens int64 `json:"cacheCreationInputTokens"`
		} `json:"modelUsage"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(result.Stdout)), &output); err != nil {
		return harness.GenerateResult{}, harness.Phase(harness.ErrDecode, err)
	}
	if output.IsError {
		if looksLikeAuthFailure(output.Result) {
			return harness.GenerateResult{}, harness.Phase(harness.ErrNotLoggedIn, errors.New(output.Result))
		}
		return harness.GenerateResult{}, harness.Phase(harness.ErrCommandFailed, errors.New(output.Result))
	}
	if len(request.Schema) > 0 {
		if len(output.StructuredOutput) == 0 || string(output.StructuredOutput) == "null" || !json.Valid(output.StructuredOutput) {
			return harness.GenerateResult{}, harness.Phase(harness.ErrSchemaMismatch, errors.New("Claude did not return valid structured_output"))
		}
		if err := validateStructuredOutput(request.Schema, output.StructuredOutput); err != nil {
			return harness.GenerateResult{}, harness.Phase(harness.ErrSchemaMismatch, err)
		}
	}
	generated := harness.GenerateResult{Output: append(json.RawMessage(nil), output.StructuredOutput...), Usage: toHarnessUsage(output.Usage), ToolPolicy: harness.ToolPolicyNone}
	if len(request.Schema) == 0 {
		generated.Output, _ = json.Marshal(output.Result)
	}
	generated.Usage.CostUSD = output.TotalCostUSD
	// modelUsage also lists auxiliary calls; the generation is the entry matching top-level usage.
	for model, used := range output.ModelUsage {
		if output.Usage != nil && used.InputTokens == output.Usage.InputTokens && used.OutputTokens == output.Usage.OutputTokens {
			generated.Model = model
		}
	}
	// A structured reply that took several requests reports only the last one
	// in usage; with a single model, its modelUsage entry covers them all.
	if generated.Model == "" && len(output.ModelUsage) == 1 {
		for model, used := range output.ModelUsage {
			generated.Model = model
			total := used.InputTokens + used.CacheReadInputTokens + used.CacheCreationInputTokens + used.OutputTokens
			generated.Usage = harness.TokenUsage{InputTokens: used.InputTokens, CachedInputTokens: used.CacheReadInputTokens, CacheWriteInputTokens: used.CacheCreationInputTokens, OutputTokens: used.OutputTokens, ReasoningOutputTokens: generated.Usage.ReasoningOutputTokens, TotalTokens: total, CostUSD: output.TotalCostUSD}
		}
	}
	return generated, nil
}

const generateSystemPrompt = "Respond to the user's request."

func validateStructuredOutput(schemaJSON, outputJSON []byte) error {
	var schema map[string]any
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		return err
	}
	var output any
	if err := json.Unmarshal(outputJSON, &output); err != nil {
		return err
	}
	expected, _ := schema["type"].(string)
	switch expected {
	case "object":
		object, ok := output.(map[string]any)
		if !ok {
			return errors.New("structured_output must be an object")
		}
		if required, ok := schema["required"].([]any); ok {
			for _, value := range required {
				key, _ := value.(string)
				if key != "" {
					if _, exists := object[key]; !exists {
						return fmt.Errorf("structured_output.%s is required", key)
					}
				}
			}
		}
	case "array":
		if _, ok := output.([]any); !ok {
			return errors.New("structured_output must be an array")
		}
	}
	return nil
}

func looksLikeAuthFailure(value string) bool {
	value = strings.ToLower(value)
	for _, fragment := range []string{"not logged in", "authentication required", "login required", "claude auth login", "please log in", "oauth token"} {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}
