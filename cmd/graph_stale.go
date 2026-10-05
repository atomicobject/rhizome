package cmd

import (
	"fmt"
	"sort"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

var (
	staleDays  int
	staleLimit int
	staleAll   bool
)

var staleCmd = &cobra.Command{
	Use:   "stale",
	Short: "List notes not modified within a configurable number of days",
	Long: `Find notes that haven't been modified within the specified threshold.

Staleness is determined by filesystem modification time only.
Use --days to configure the threshold (default 90 days).

This helps identify notes that may need review, updating, or archival.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		selectedVault := vaultName
		if selectedVault == "" {
			vault := &obsidian.Vault{}
			defaultName, err := vault.DefaultName()
			if err != nil {
				return err
			}
			selectedVault = defaultName
		}

		vault := obsidian.Vault{Name: selectedVault}
		note := obsidian.Note{}

		staleNotes, err := actions.StaleNotes(&vault, &note, staleDays)
		if err != nil {
			return err
		}

		vaultPath, err := vault.Path()
		if err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Stale notes (>%d days) in vault %q (%s):\n", staleDays, selectedVault, vaultPath)

		if len(staleNotes) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "  No stale notes found.")
			return nil
		}

		// Sort by days since modification (oldest first)
		sort.Slice(staleNotes, func(i, j int) bool {
			return staleNotes[i].DaysSinceModification > staleNotes[j].DaysSinceModification
		})

		// Determine limit
		limit := staleLimit
		if staleAll {
			limit = 0 // 0 means no limit
		}

		shown := 0
		for _, sn := range staleNotes {
			if limit > 0 && shown >= limit {
				break
			}
			dateStr := sn.LastModified.Format(time.DateOnly)
			fmt.Fprintf(cmd.OutOrStdout(), "  %s (last modified: %s, %d days ago)\n", sn.Path, dateStr, sn.DaysSinceModification)
			shown++
		}

		// Show total count
		fmt.Fprintf(cmd.OutOrStdout(), "\nTotal: %d stale note(s)", len(staleNotes))
		if limit > 0 && shown < len(staleNotes) {
			fmt.Fprintf(cmd.OutOrStdout(), "  (showing %d of %d)", shown, len(staleNotes))
		}
		fmt.Fprintln(cmd.OutOrStdout())

		return nil
	},
}

func init() {
	staleCmd.Flags().IntVar(&staleDays, "days", 90, "number of days after which a note is considered stale")
	staleCmd.Flags().IntVar(&staleLimit, "limit", 25, "maximum number of stale notes to display (0 for unlimited)")
	staleCmd.Flags().BoolVar(&staleAll, "all", false, "show all stale notes (overrides --limit)")

	graphCmd.AddCommand(staleCmd)
}
