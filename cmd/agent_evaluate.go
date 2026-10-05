package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/agentcode"
	"github.com/spf13/cobra"
)

func newAgentEvaluateCmd() *cobra.Command {
	return newAgentJSONStdinCmd("evaluate", "Evaluate supplied state with Jev using typed questions from JSON stdin")
}

func newAgentJSONStdinCmd(name, summary string) *cobra.Command {
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
			maybeSetString(input, "sessionId", agentSessionID)
			return runAgentJSONTool(command, 0, strings.ReplaceAll(name, "-", "_"), input)
		},
	}
}
