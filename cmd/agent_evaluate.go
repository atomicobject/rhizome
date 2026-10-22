package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/agentcode"
	"github.com/spf13/cobra"
)

func (commandOptions *vaultCommandOptions) newAgentEvaluateCmd() *cobra.Command {
	return commandOptions.newAgentJSONStdinCmd("evaluate", "Evaluate supplied state with Jev using typed questions from JSON stdin")
}

func (commandOptions *vaultCommandOptions) newAgentGenerateCmd() *cobra.Command {
	var provider, model, effort, promptFile, schemaFile string
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Run one tool-free model call through Claude Code or Codex, prompt from stdin or --prompt-file",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			var prompt []byte
			var err error
			if promptFile != "" {
				prompt, err = os.ReadFile(promptFile)
			} else {
				prompt, err = io.ReadAll(command.InOrStdin())
			}
			if err != nil {
				return agentCodeError(fmt.Errorf("read prompt: %w", err))
			}
			input := map[string]any{"prompt": string(prompt)}
			maybeSetString(input, "provider", provider)
			maybeSetString(input, "model", model)
			maybeSetString(input, "effort", effort)
			if schemaFile != "" {
				data, err := os.ReadFile(schemaFile)
				if err != nil {
					return agentCodeError(fmt.Errorf("read schema: %w", err))
				}
				var schema map[string]any
				if err := json.Unmarshal(data, &schema); err != nil {
					return agentCodeError(fmt.Errorf("decode schema: %w", err))
				}
				input["schema"] = schema
			}
			if err := agentapi.ValidateCodeInput("generate", input); err != nil {
				return agentCodeError(err)
			}
			return commandOptions.runAgentJSONTool(command, 0, "generate", input)
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "", "provider CLI to run: claude or codex (default: the agent.harness setting, else the only one installed and signed in)")
	cmd.Flags().StringVar(&model, "model", "", "provider model ID or alias (default: the CLI's configured model)")
	cmd.Flags().StringVar(&effort, "effort", "", "provider reasoning effort, such as low or high")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "read the prompt from this file instead of stdin")
	cmd.Flags().StringVar(&schemaFile, "schema", "", "JSON Schema file for a structured result")
	return cmd
}

func (commandOptions *vaultCommandOptions) newAgentJSONStdinCmd(name, summary string) *cobra.Command {
	return &cobra.Command{
		Use: name, Short: summary, Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			data, err := io.ReadAll(io.LimitReader(command.InOrStdin(), agentcode.MaxExecuteCodeBytes+1))
			if err != nil {
				return agentCodeError(fmt.Errorf("read request: %w", err))
			}
			if len(data) > agentcode.MaxExecuteCodeBytes {
				return agentCodeError(fmt.Errorf("request exceeds %d-byte input limit", agentcode.MaxExecuteCodeBytes))
			}
			var input map[string]any
			if err := json.Unmarshal(data, &input); err != nil {
				return agentCodeError(fmt.Errorf("decode request: %w", err))
			}
			if err := agentapi.ValidateCodeInput(strings.ReplaceAll(name, "-", "_"), input); err != nil {
				return agentCodeError(err)
			}
			maybeSetString(input, "sessionId", commandOptions.agentSessionID)
			return commandOptions.runAgentJSONTool(command, 0, strings.ReplaceAll(name, "-", "_"), input)
		},
	}
}
