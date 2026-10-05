package cmd

import (
	"errors"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/cli/harnesscheck"
	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/claude"
	"github.com/atomicobject/rhizome/pkg/harness/codex"
	"github.com/spf13/cobra"
)

type harnessRegistry func() map[harness.Kind]harness.Harness

func registeredHarnesses() map[harness.Kind]harness.Harness {
	return map[harness.Kind]harness.Harness{
		harness.KindCodex:  codex.New(),
		harness.KindClaude: claude.New(),
	}
}

var harnessCmd = newAgentHarnessCmdWithRegistry(registeredHarnesses)

func init() { rootCmd.AddCommand(harnessCmd) }

func newAgentHarnessCmdWithRegistry(registry harnessRegistry) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "harness",
		Short: "Inspect and smoke-test coding-agent harnesses",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newAgentHarnessStatusCmd(registry))
	cmd.AddCommand(newAgentHarnessSmokeCmd(registry))
	return cmd
}

func newAgentHarnessStatusCmd(registry harnessRegistry) *cobra.Command {
	var requested string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Report coding-agent harness readiness",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := harnesscheck.Status(cmd.Context(), registry(), requested)
			if err != nil {
				return writeHarnessCommandError(err)
			}
			return writeAgentPayload(cmd, result)
		},
	}
	cmd.Flags().StringVar(&requested, "harness", "", "harness to inspect (codex or claude)")
	return cmd
}

func newAgentHarnessSmokeCmd(registry harnessRegistry) *cobra.Command {
	var requested, model, effort, cwd, prompt string
	cmd := &cobra.Command{
		Use:   "smoke",
		Short: "Run generation and a streamed turn through a coding-agent harness",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(requested) == "" {
				return writeHarnessCommandError(errors.New("--harness is required"))
			}
			driver, err := harnesscheck.Select(registry(), requested)
			if err != nil {
				return writeHarnessCommandError(err)
			}
			summary, err := harnesscheck.Smoke(cmd.Context(), driver, harnesscheck.Options{
				Model: model, Effort: effort, Cwd: cwd, Prompt: prompt,
			}, func(event harness.Event) error {
				return writeAgentPayload(cmd, event)
			})
			if err != nil {
				return writeHarnessCommandError(err)
			}
			return writeAgentPayload(cmd, summary)
		},
	}
	cmd.Flags().StringVar(&requested, "harness", "", "harness to test (codex)")
	cmd.Flags().StringVar(&model, "model", "", "model override")
	cmd.Flags().StringVar(&effort, "effort", "", "reasoning effort override")
	cmd.Flags().StringVar(&cwd, "cwd", "", "working directory")
	cmd.Flags().StringVar(&prompt, "prompt", "Reply with the single word pong.", "session prompt")
	return cmd
}

func writeHarnessCommandError(err error) error {
	writeAgentError(err)
	return silentExitError{code: 1}
}
