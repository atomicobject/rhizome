package cmd

import (
	"fmt"
	"sort"

	"github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

var (
	deadEndsSkipAnchors bool
	deadEndsSkipEmbeds  bool
	deadEndsLimit       int
	deadEndsAll         bool
)

var deadEndsCmd = &cobra.Command{
	Use:   "dead-ends",
	Short: "List notes with incoming links but no outgoing links",
	Long: `Find notes that are linked to but don't link to other notes.

Dead-end notes are potential stubs or incomplete notes that may need expansion.
They have at least one inbound link but zero outbound wikilinks.

Orphan notes (no inbound or outbound links) are not included here;
use 'graph orphans' to find those.`,
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
		noteMetadata, err := newNoteMetadataIndexer()
		if err != nil {
			return err
		}

		params := actions.GraphAnalysisParams{
			UseConfig:    true,
			NoteMetadata: noteMetadata,
			Options: obsidian.GraphAnalysisOptions{
				WikilinkOptions: obsidian.WikilinkOptions{
					SkipAnchors: deadEndsSkipAnchors,
					SkipEmbeds:  deadEndsSkipEmbeds,
				},
				MinDegree:  graphMinDegree,
				MutualOnly: graphMutualOnly,
			},
			ExcludePatterns: graphExcludePatterns,
			IncludePatterns: graphIncludePatterns,
		}

		deadEnds, err := actions.DeadEnds(&vault, &note, params)
		if err != nil {
			return err
		}

		vaultPath, err := vault.Path()
		if err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Dead-end notes in vault %q (%s):\n", selectedVault, vaultPath)

		if len(deadEnds) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "  No dead-end notes found.")
			return nil
		}

		// Sort by path
		sort.Slice(deadEnds, func(i, j int) bool {
			return deadEnds[i].Path < deadEnds[j].Path
		})

		// Determine limit
		limit := deadEndsLimit
		if deadEndsAll {
			limit = 0 // 0 means no limit
		}

		shown := 0
		for _, de := range deadEnds {
			if limit > 0 && shown >= limit {
				break
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  %s (%d inbound link(s))\n", de.Path, de.InboundLinks)
			shown++
		}

		// Show total count
		fmt.Fprintf(cmd.OutOrStdout(), "\nTotal: %d dead-end note(s)", len(deadEnds))
		if limit > 0 && shown < len(deadEnds) {
			fmt.Fprintf(cmd.OutOrStdout(), "  (showing %d of %d)", shown, len(deadEnds))
		}
		fmt.Fprintln(cmd.OutOrStdout())

		return nil
	},
}

func init() {
	deadEndsCmd.Flags().BoolVar(&deadEndsSkipAnchors, "skip-anchors", false, "skip wikilinks that contain anchors (e.g. [[Note#Section]])")
	deadEndsCmd.Flags().BoolVar(&deadEndsSkipEmbeds, "skip-embeds", false, "skip embedded wikilinks (e.g. ![[Embedded Note]])")
	deadEndsCmd.Flags().IntVar(&deadEndsLimit, "limit", 25, "maximum number of dead-end notes to display (0 for unlimited)")
	deadEndsCmd.Flags().BoolVar(&deadEndsAll, "all", false, "show all dead-end notes (overrides --limit)")

	graphCmd.AddCommand(deadEndsCmd)
}
