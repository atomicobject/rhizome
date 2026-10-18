package cmd

import (
	"fmt"
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"sort"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	vaulthealth "github.com/atomicobject/rhizome/pkg/vault/health"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

func (commandOptions *graphInvocationOptions) makeBrokenLinksCmd() *cobra.Command {
	return &cobra.Command{
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
			selectedVault := commandOptions.vaultName
			if selectedVault == "" {
				vault := &repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config}
				defaultName, err := vault.DefaultName()
				if err != nil {
					return err
				}
				selectedVault = defaultName
			}

			vault := repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config, Name: selectedVault}
			note := obsidian.Note{}

			options := vaulthealth.BrokenLinksOptions{
				WikilinkOptions: markdownsyntax.WikilinkOptions{
					SkipAnchors: commandOptions.brokenLinksSkipAnchors,
					SkipEmbeds:  commandOptions.brokenLinksSkipEmbeds,
				},
				IncludeImages: commandOptions.brokenLinksIncludeImages,
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
			bySource := make(map[string][]vaulthealth.BrokenLink)
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
			limit := commandOptions.brokenLinksLimit
			if commandOptions.brokenLinksAll {
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
}

func formatBrokenLink(bl vaulthealth.BrokenLink) string {
	switch bl.LinkType {
	case markdownsyntax.BacklinkTypeEmbed:
		return fmt.Sprintf("![[%s]]", bl.Target)
	case markdownsyntax.BacklinkTypeAlias:
		if bl.Alias != "" {
			return fmt.Sprintf("[[%s|%s]]", bl.Target, bl.Alias)
		}
		return fmt.Sprintf("[[%s]]", bl.Target)
	case markdownsyntax.BacklinkTypeHeading:
		return fmt.Sprintf("[[%s#%s]]", bl.Target, bl.Fragment)
	case markdownsyntax.BacklinkTypeBlock:
		return fmt.Sprintf("[[%s#^%s]]", bl.Target, bl.Fragment)
	default:
		return fmt.Sprintf("[[%s]]", bl.Target)
	}
}

func (commandOptions *graphInvocationOptions) registerGraphBrokenLinksCommands(brokenLinksCmd, graphCmd *cobra.Command) {
	brokenLinksCmd.Flags().BoolVar(&commandOptions.brokenLinksSkipAnchors, "skip-anchors", false, "skip wikilinks that contain anchors (e.g. [[Note#Section]])")
	brokenLinksCmd.Flags().BoolVar(&commandOptions.brokenLinksSkipEmbeds, "skip-embeds", false, "skip embedded wikilinks (e.g. ![[Embedded Note]])")
	brokenLinksCmd.Flags().BoolVar(&commandOptions.brokenLinksIncludeImages, "include-images", false, "include broken links to image files (png, jpg, etc.)")
	brokenLinksCmd.Flags().IntVar(&commandOptions.brokenLinksLimit, "limit", 25, "maximum number of broken links to display (0 for unlimited)")
	brokenLinksCmd.Flags().BoolVar(&commandOptions.brokenLinksAll, "all", false, "show all broken links (overrides --limit)")

	graphCmd.AddCommand(brokenLinksCmd)
}
