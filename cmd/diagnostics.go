package cmd

import (
	"encoding/json"
	"fmt"

	diagnosticcli "github.com/atomicobject/rhizome/pkg/app/cli/diagnostics"
	evidence "github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/spf13/cobra"
)

func newDiagnosticsCmd() *cobra.Command {
	var vault string
	var asJSON bool
	command := &cobra.Command{
		Use: "diagnostics", Short: "Read retained operational evidence without starting a runtime",
		Long: "Read bounded reports and logs directly from .rhizome/diagnostics. Does not load the main index, start a runtime, or record its own activity. An explicit --vault directory bypasses configuration.",
		// Offline machine output has no pager or runtime preflight.
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
	}
	command.PersistentFlags().StringVarP(&vault, "vault", "v", "", "vault directory or registered name (defaults to nearest .rhizome directory)")
	command.PersistentFlags().BoolVar(&asJSON, "json", false, "write one machine-readable JSON result")
	for _, mode := range []string{"index", "reports", "report", "logs"} {
		opts := diagnosticcli.Options{Mode: mode}
		use, short := mode, "Read retained operation reports"
		switch mode {
		case "index":
			short = "Read the latest completed index attempt or bounded index history"
		case "report":
			use, short = "report <operation-id>", "Read one retained report by operation ID"
		case "logs":
			short = "Read bounded structured log events"
		}
		leaf := &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs, SilenceErrors: true, SilenceUsage: true}
		if mode == "report" {
			leaf.Args = cobra.ExactArgs(1)
		}
		validateArgs := leaf.Args
		leaf.Args = func(cmd *cobra.Command, args []string) error {
			err := validateArgs(cmd, args)
			if err == nil || !asJSON {
				return err
			}
			result := diagnosticcli.Result{SchemaVersion: evidence.SchemaVersion, Mode: opts.Mode, Error: err.Error()}
			if writeErr := json.NewEncoder(cmd.OutOrStdout()).Encode(result); writeErr != nil {
				return writeErr
			}
			return silentExitError{code: 1}
		}
		leaf.RunE = func(cmd *cobra.Command, args []string) error {
			opts.Vault = vault
			historyBounds := cmd.Flags().Changed("until") || cmd.Flags().Changed("limit") || cmd.Flags().Changed("max-bytes")
			if opts.Mode == "index" && historyBounds && opts.Since == "" && !opts.Last {
				opts.Since = "7d"
			}
			if opts.Mode == "index" && opts.Last && historyBounds {
				result := diagnosticcli.Result{SchemaVersion: evidence.SchemaVersion, Mode: opts.Mode, Error: "--last cannot be combined with history bounds"}
				if asJSON {
					if err := json.NewEncoder(cmd.OutOrStdout()).Encode(result); err != nil {
						return err
					}
					return silentExitError{code: 1}
				}
				return fmt.Errorf("%s", result.Error)
			}
			if len(args) != 0 {
				opts.OperationID = args[0]
			}
			result, err := diagnosticcli.Read(opts)
			if err != nil {
				result.Error = err.Error()
			}
			if asJSON {
				if writeErr := json.NewEncoder(cmd.OutOrStdout()).Encode(result); writeErr != nil {
					return writeErr
				}
				if err != nil {
					return silentExitError{code: 1}
				}
				return nil
			}
			if err != nil {
				return err
			}
			return diagnosticcli.WriteText(cmd.OutOrStdout(), result)
		}
		if mode != "report" {
			leaf.Flags().StringVar(&opts.Since, "since", "", "history start: 7d, 2h, or RFC3339 (default 7d; index defaults to latest)")
			leaf.Flags().StringVar(&opts.Until, "until", "", "history end: duration ago or RFC3339")
			leaf.Flags().IntVar(&opts.Limit, "limit", 100, "maximum retained records to return")
			leaf.Flags().Int64Var(&opts.MaxBytes, "max-bytes", 0, "maximum input bytes to read (0 uses bounded default)")
		}
		if mode == "index" {
			leaf.Flags().BoolVar(&opts.Last, "last", false, "read protected latest completed index attempt, independent of retention cutoff")
		}
		if mode == "reports" {
			leaf.Flags().StringVar(&opts.Kind, "kind", "", "exact stored operation kind (discover with diagnostics reports)")
			leaf.Flags().StringVar(&opts.Status, "status", "", "exact stored operation status")
			leaf.Flags().StringVar(&opts.OperationID, "operation-id", "", "exact operation ID")
		}
		if mode == "reports" || mode == "logs" {
			leaf.Flags().StringVar(&opts.TraceID, "trace-id", "", "exact trace ID, including related operations")
		}
		if mode == "logs" {
			leaf.Flags().StringVar(&opts.Level, "level", "info", "minimum severity: debug, info, warn, or error")
			leaf.Flags().StringVar(&opts.OperationID, "operation-id", "", "exact operation ID")
			leaf.Flags().StringVar(&opts.Subsystem, "subsystem", "", "exact subsystem name")
		}
		command.AddCommand(leaf)
	}
	return command
}

func init() { rootCmd.AddCommand(newDiagnosticsCmd()) }
