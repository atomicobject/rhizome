package cmd

import (
	"fmt"
	"sort"

	"github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

var (
	brokenLinksSkipAnchors   bool
	brokenLinksSkipEmbeds    bool
	brokenLinksIncludeImages bool
	brokenLinksLimit         int
	brokenLinksAll           bool
)

var brokenLinksCmd = &cobra.Command{
	Use:   "broken-links",
	Short: "List wikilinks pointing to non-existent notes",
	Long: `Find all wikilinks in the vault that point to notes which do not exist.

This detects common link types:
  - Basic links: [[Note]]
  - Aliased links: [[Note|Display Text]]
  - Embedded links: ![[Note]]
  - Links with anchors: [[Note#Heading]]

By default, links to image files (png, jpg, gif, etc.) are excluded since these
are typically pasted attachments rather than note links.

Use --include-images to include broken image links in results.
Use --skip-anchors to ignore links containing anchors.
Use --skip-embeds to ignore embedded links.`,
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

		options := obsidian.BrokenLinksOptions{
			WikilinkOptions: obsidian.WikilinkOptions{
				SkipAnchors: brokenLinksSkipAnchors,
				SkipEmbeds:  brokenLinksSkipEmbeds,
			},
			IncludeImages: brokenLinksIncludeImages,
		}

		broken, err := actions.BrokenLinks(&vault, &note, options)
		if err != nil {
			return err
		}

		vaultPath, err := vault.Path()
		if err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Broken links in vault %q (%s):\n", selectedVault, vaultPath)

		if len(broken) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "  No broken links found.")
			return nil
		}

		// Group broken links by source file
		bySource := make(map[string][]obsidian.BrokenLink)
		for _, bl := range broken {
			bySource[bl.Source] = append(bySource[bl.Source], bl)
		}

		// Sort source files
		sources := make([]string, 0, len(bySource))
		for src := range bySource {
			sources = append(sources, src)
		}
		sort.Strings(sources)

		// Determine limit
		limit := brokenLinksLimit
		if brokenLinksAll {
			limit = 0 // 0 means no limit
		}

		totalShown := 0
		for _, src := range sources {
			links := bySource[src]
			sort.Slice(links, func(i, j int) bool {
				return links[i].Target < links[j].Target
			})

			fmt.Fprintf(cmd.OutOrStdout(), "  %s:\n", src)
			for _, bl := range links {
				if limit > 0 && totalShown >= limit {
					break
				}

				// Format the link display
				linkDisplay := formatBrokenLink(bl)
				fmt.Fprintf(cmd.OutOrStdout(), "    → %s\n", linkDisplay)
				totalShown++
			}

			if limit > 0 && totalShown >= limit {
				break
			}
		}

		// Show total count
		fmt.Fprintf(cmd.OutOrStdout(), "\nTotal: %d broken link(s) in %d file(s)", len(broken), len(bySource))
		if limit > 0 && totalShown < len(broken) {
			fmt.Fprintf(cmd.OutOrStdout(), "  (showing %d of %d)", totalShown, len(broken))
		}
		fmt.Fprintln(cmd.OutOrStdout())

		return nil
	},
}

func formatBrokenLink(bl obsidian.BrokenLink) string {
	switch bl.LinkType {
	case obsidian.BacklinkTypeEmbed:
		return fmt.Sprintf("![[%s]]", bl.Target)
	case obsidian.BacklinkTypeAlias:
		if bl.Alias != "" {
			return fmt.Sprintf("[[%s|%s]]", bl.Target, bl.Alias)
		}
		return fmt.Sprintf("[[%s]]", bl.Target)
	case obsidian.BacklinkTypeHeading:
		return fmt.Sprintf("[[%s#%s]]", bl.Target, bl.Fragment)
	case obsidian.BacklinkTypeBlock:
		return fmt.Sprintf("[[%s#^%s]]", bl.Target, bl.Fragment)
	default:
		return fmt.Sprintf("[[%s]]", bl.Target)
	}
}

func init() {
	brokenLinksCmd.Flags().BoolVar(&brokenLinksSkipAnchors, "skip-anchors", false, "skip wikilinks that contain anchors (e.g. [[Note#Section]])")
	brokenLinksCmd.Flags().BoolVar(&brokenLinksSkipEmbeds, "skip-embeds", false, "skip embedded wikilinks (e.g. ![[Embedded Note]])")
	brokenLinksCmd.Flags().BoolVar(&brokenLinksIncludeImages, "include-images", false, "include broken links to image files (png, jpg, etc.)")
	brokenLinksCmd.Flags().IntVar(&brokenLinksLimit, "limit", 25, "maximum number of broken links to display (0 for unlimited)")
	brokenLinksCmd.Flags().BoolVar(&brokenLinksAll, "all", false, "show all broken links (overrides --limit)")

	graphCmd.AddCommand(brokenLinksCmd)
}
