package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/agentcode"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newAgentCodeExecuteCmd() *cobra.Command {
	var options agentcode.ExecuteOptions
	var input, inputFile string
	command := &cobra.Command{
		Use: "execute", Short: "Run JavaScript with Rhizome operations and managed process lifetime", Args: cobra.NoArgs,
		Long:    "Run JavaScript supplied with --code or standard input. Use await rzm.files({...}) and return a JSON value. --input or --input-file passes one JSON value the script reads as input (null when omitted). Rhizome owns client setup and cleanup; Node.js must be on PATH. JavaScript runs with the caller's host permissions.",
		Example: "  rzm agent code execute --code 'return await rzm.files({inputs: [\"README.md\"]});'\n  rzm agent code execute --session-id <id> < task.js\n  rzm agent code execute --session-id <id> --input '{\"path\":\"Notes/Topic.md\"}' < scripts/subject.js",
		RunE: func(command *cobra.Command, _ []string) error {
			if err := assertRuntimeFreeAgentOperation(command); err != nil {
				return agentCodeError(err)
			}
			code, err := readAgentCodeInput(command, options.Code)
			if err != nil {
				return agentCodeError(err)
			}
			options.Code = code
			if options.Input, err = readAgentCodeScriptInput(command, input, inputFile); err != nil {
				return agentCodeError(err)
			}
			options.ExecutablePath, err = os.Executable()
			if err != nil {
				return agentCodeError(err)
			}
			def, err := vaultDefOrDefaultContext(command.Context())
			if err != nil {
				return agentCodeError(err)
			}
			options.VaultPath, err = canonicalAgentCodeVaultPath(def.BasePath())
			if err != nil {
				return agentCodeError(err)
			}
			options.SessionID = agentSessionID
			result, err := agentcode.Execute(command.Context(), options)
			if err != nil {
				return agentCodeError(err)
			}
			if err := writeAgentPayload(command, result); err != nil {
				return err
			}
			if !result.OK {
				return silentExitError{code: 1}
			}
			return nil
		},
	}
	command.Flags().StringVar(&options.Code, "code", "", "JavaScript function body; supports await and return (otherwise read stdin)")
	command.Flags().DurationVar(&options.Timeout, "timeout", agentcode.DefaultExecuteTimeout, "whole-execution deadline, including setup (maximum 24h)")
	command.Flags().BoolVar(&options.ReadWrite, "read-write", false, "allow supported explicit Rhizome writes; operation-specific apply controls still apply")
	command.Flags().StringVar(&input, "input", "", "JSON value the script reads as input")
	command.Flags().StringVar(&inputFile, "input-file", "", "file holding the JSON value the script reads as input")
	return suppressAgentCommandNoise(command)
}

func readAgentCodeInput(command *cobra.Command, code string) (string, error) {
	if !command.Flags().Changed("code") {
		input := command.InOrStdin()
		if file, ok := input.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
			return "", fmt.Errorf("provide JavaScript with --code or pipe it to standard input")
		}
		data, err := io.ReadAll(io.LimitReader(input, agentcode.MaxExecuteCodeBytes+1))
		if err != nil {
			return "", fmt.Errorf("read JavaScript: %w", err)
		}
		code = string(data)
	}
	if len(code) > agentcode.MaxExecuteCodeBytes {
		return "", fmt.Errorf("JavaScript exceeds the %d-byte input limit", agentcode.MaxExecuteCodeBytes)
	}
	if strings.TrimSpace(code) == "" {
		return "", fmt.Errorf("provide JavaScript with --code or pipe it to standard input")
	}
	return code, nil
}

// readAgentCodeScriptInput reads the optional JSON value exposed to the script
// as input. Standard input already carries the script.
func readAgentCodeScriptInput(command *cobra.Command, input, inputFile string) (json.RawMessage, error) {
	switch {
	case command.Flags().Changed("input") && command.Flags().Changed("input-file"):
		return nil, fmt.Errorf("use only one of --input or --input-file")
	case command.Flags().Changed("input-file"):
		file, err := os.Open(inputFile)
		if err != nil {
			return nil, fmt.Errorf("read --input-file: %w", err)
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, agentcode.MaxExecuteCodeBytes+1))
		if err != nil {
			return nil, fmt.Errorf("read --input-file: %w", err)
		}
		input = string(data)
	case !command.Flags().Changed("input"):
		return nil, nil
	}
	if len(input) > agentcode.MaxExecuteCodeBytes {
		return nil, fmt.Errorf("script input exceeds %d byte limit", agentcode.MaxExecuteCodeBytes)
	}
	raw := json.RawMessage(bytes.TrimSpace([]byte(input)))
	if len(raw) == 0 {
		return nil, fmt.Errorf("script input must be one JSON value")
	}
	if err := agentcode.ValidateExecuteInput(raw); err != nil {
		return nil, fmt.Errorf("script %w", err)
	}
	return raw, nil
}
